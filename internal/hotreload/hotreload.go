// Package hotreload proves, or disproves, that an OpMode can be loaded onto a
// robot without installing an APK.
//
// The robot controller already does this for OnBotJava. RegisteredOpModes
// watches <FIRST>/java/status/buildSuccessful.txt, and when it changes it
// rebuilds OnBotJavaClassLoader from the .dex files in the OnBotJava output
// directory and re-registers those OpModes. A stock robot controller installs
// OnBotJavaHelperImpl already, so none of that needs code on the robot.
//
// So the experiment is: compile one OpMode here, push the dex there, touch the
// file, and see whether it appears on the Driver Station.
//
// This is a measurement, not a feature. It answers whether `pusher extreme` is
// a gradle-and-adb problem or a classloading one.
package hotreload

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/andreibanu/pusher/internal/adb"
)

// FirstDir is the robot controller's own folder on the hub.
const FirstDir = "/sdcard/FIRST"

// JavaDir is where OnBotJava keeps everything.
const JavaDir = FirstDir + "/java"

// TriggerFile is what RegisteredOpModes watches. Touching it is the whole
// reload signal.
const TriggerFile = JavaDir + "/status/buildSuccessful.txt"

// PointerFile holds the absolute path of the directory OnBotJava is currently
// building into. getCurrentOutputDir() reads this file and uses whatever it
// says, so the output directory is not a fixed location: it is wherever this
// points. Writing to any other directory is invisible to the SDK.
const PointerFile = JavaDir + "/status/currentOnBotJavaDir.txt"

// FallbackOutputDir is used when OnBotJava has never built anything, so the
// pointer file does not exist yet and there is nothing to join.
const FallbackOutputDir = JavaDir + "/build/jars/pusher"

// Package is deliberately not one the SDK or a team project would use. The
// classloader is parent-first, so a class that also exists in the APK would
// resolve there and prove nothing.
const Package = "org.firstinspires.ftc.pusherproof"

// ClassName is the OpMode that gets built.
const ClassName = "PusherReloadProof"

// ProofName is what the pushed files are called on the hub.
const ProofName = "pusher-reload-proof"

// Result is what the experiment did.
type Result struct {
	OpModeName string
	DexBytes   int64
	JarBytes   int64
	RemoteDir  string
	RemoteDex  string
	RemoteJar  string
	// Pointed records that the pointer file had to be written, meaning
	// OnBotJava had never built anything here.
	Pointed bool
	// ColdStart records that the status directory did not exist. The app
	// watches that directory for the reload signal and attaches the watch when
	// it starts, so a directory created now is not being watched yet.
	ColdStart bool

	Compile time.Duration
	Push    time.Duration

	Steps []string
	Err   error

	// Diagnosis is why nothing appeared, when nothing appears.
	Diagnosis Diagnosis
}

func (r *Result) step(format string, args ...any) {
	r.Steps = append(r.Steps, fmt.Sprintf(format, args...))
}

// Toolchain is what building a dex needs.
type Toolchain struct {
	Javac string
	D8    string
	Jars  []string
}

// Cleanup removes the jars unpacked out of the AARs.
func (t Toolchain) Cleanup() {
	for _, jar := range t.Jars {
		os.Remove(jar)
	}
}

// FindToolchain locates javac, d8 and the FTC jars to compile against.
func FindToolchain() (Toolchain, error) {
	var tc Toolchain

	tc.Javac = findJavac()
	if tc.Javac == "" {
		return tc, fmt.Errorf("no javac found; install a JDK or Android Studio")
	}

	tc.D8 = findD8()
	if tc.D8 == "" {
		return tc, fmt.Errorf("no d8 found; install Android SDK build-tools")
	}

	jars, err := ftcJars()
	if err != nil {
		return tc, err
	}
	tc.Jars = jars

	return tc, nil
}

func findJavac() string {
	if home := os.Getenv("JAVA_HOME"); home != "" {
		if path := filepath.Join(home, "bin", exeName("javac")); exists(path) {
			return path
		}
	}
	if path, err := exec.LookPath("javac"); err == nil {
		return path
	}

	for _, studio := range studioHomes() {
		if path := filepath.Join(studio, "bin", exeName("javac")); exists(path) {
			return path
		}
	}
	return ""
}

func studioHomes() []string {
	home, _ := os.UserHomeDir()

	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Android Studio.app/Contents/jbr/Contents/Home",
			filepath.Join(home, "Applications/Android Studio.app/Contents/jbr/Contents/Home"),
		}
	case "linux":
		return []string{
			"/opt/android-studio/jbr",
			filepath.Join(home, "android-studio/jbr"),
		}
	case "windows":
		return []string{filepath.Join(os.Getenv("ProgramFiles"), "Android", "Android Studio", "jbr")}
	}
	return nil
}

// findD8 takes the newest build-tools, since d8 is backwards compatible.
func findD8() string {
	if path, err := exec.LookPath("d8"); err == nil {
		return path
	}

	for _, root := range sdkRoots() {
		entries, err := os.ReadDir(filepath.Join(root, "build-tools"))
		if err != nil {
			continue
		}

		var versions []string
		for _, e := range entries {
			versions = append(versions, e.Name())
		}
		sort.Sort(sort.Reverse(sort.StringSlice(versions)))

		for _, v := range versions {
			if path := filepath.Join(root, "build-tools", v, exeName("d8")); exists(path) {
				return path
			}
		}
	}
	return ""
}

func sdkRoots() []string {
	roots := []string{os.Getenv("ANDROID_HOME"), os.Getenv("ANDROID_SDK_ROOT")}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots,
			filepath.Join(home, "Library/Android/sdk"),
			filepath.Join(home, "Android/Sdk"),
			filepath.Join(home, "AppData/Local/Android/Sdk"))
	}
	return roots
}

// ftcJars pulls the SDK's classes.jar out of the AARs Gradle has already
// downloaded, so nothing has to be fetched to run this.
func ftcJars() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	root := filepath.Join(home, ".gradle/caches/modules-2/files-2.1/org.firstinspires.ftc")

	// RobotCore has the OpMode classes; Hardware has the device interfaces an
	// OpMode of any substance touches.
	var jars []string
	for _, module := range []string{"RobotCore", "Hardware"} {
		matches, _ := filepath.Glob(filepath.Join(root, module, "*", "*", module+"-*.aar"))
		if len(matches) == 0 {
			continue
		}

		sort.Strings(matches)
		aar := matches[len(matches)-1]

		jar, err := classesJar(aar)
		if err != nil {
			return nil, err
		}
		jars = append(jars, jar)
	}

	if len(jars) == 0 {
		return nil, fmt.Errorf("no FTC SDK jars in the Gradle cache; build your FTC project once first")
	}
	return jars, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// Run performs the whole experiment and reports what happened at each step.
//
// marker distinguishes one attempt from the next: it goes into the OpMode's
// displayed name, so seeing it change on the Driver Station is what proves a
// reload rather than a first load.
func Run(serial, marker string) *Result {
	out := &Result{OpModeName: "Pusher Reload " + marker}

	tc, err := FindToolchain()
	if err != nil {
		out.Err = err
		return out
	}
	defer tc.Cleanup()
	out.step("toolchain: %s, %s", filepath.Base(tc.Javac), filepath.Base(tc.D8))

	out.ColdStart = !statusDirExists(serial)

	work, err := os.MkdirTemp("", "pusher-reload-*")
	if err != nil {
		out.Err = err
		return out
	}
	defer os.RemoveAll(work)

	start := time.Now()
	files, err := buildAll(tc, work, out.OpModeName)
	if err != nil {
		out.Err = err
		return out
	}
	out.Compile = time.Since(start)

	if info, err := os.Stat(files.Dex); err == nil {
		out.DexBytes = info.Size()
	}
	if info, err := os.Stat(files.Jar); err == nil {
		out.JarBytes = info.Size()
	}
	out.step("built a %d byte jar and a %d byte dex in %s",
		out.JarBytes, out.DexBytes, out.Compile.Round(time.Millisecond))

	dir, pointed, err := outputDir(serial)
	if err != nil {
		out.Err = err
		return out
	}
	out.RemoteDir = dir
	out.Pointed = pointed
	out.RemoteDex = dir + "/" + ProofName + ".dex"
	out.RemoteJar = dir + "/" + ProofName + ".jar"
	if pointed {
		out.step("no OnBotJava build on this hub, so %s now points at %s", PointerFile, dir)
	} else {
		out.step("output directory the SDK is reading: %s", dir)
	}

	// The jar is what gets the class name discovered, the dex is what loads it.
	// Both, or nothing happens.
	start = time.Now()
	if err := adb.Push(serial, files.Jar, out.RemoteJar); err != nil {
		out.Err = fmt.Errorf("cannot push the jar: %w", err)
		return out
	}
	if err := adb.Push(serial, files.Dex, out.RemoteDex); err != nil {
		out.Err = fmt.Errorf("cannot push the dex: %w", err)
		return out
	}
	out.Push = time.Since(start)
	out.step("pushed the jar and the dex in %s", out.Push.Round(time.Millisecond))

	if err := trigger(serial); err != nil {
		out.Err = err
		return out
	}
	out.step("wrote %s", TriggerFile)

	if out.ColdStart {
		out.step("%s did not exist, so the app is not watching it yet", JavaDir+"/status")
	}

	out.Diagnosis = Diagnose(serial)
	if out.Diagnosis.Package != "" {
		out.step("robot controller: %s", out.Diagnosis.Package)
	}

	return out
}

// outputDir is the directory the SDK will actually read, which is whatever the
// pointer file names.
//
// When OnBotJava has already built something the pointer is valid and the files
// go alongside its own, leaving it working. When it has not, there is nothing
// to disturb and the pointer gets written to a directory of ours.
func outputDir(serial string) (string, bool, error) {
	if dir := currentOutputDir(serial); dir != "" {
		return dir, false, nil
	}

	if _, err := adb.Shell(serial, "mkdir", "-p", FallbackOutputDir); err != nil {
		return "", false, fmt.Errorf("cannot create %s on the hub: %w", FallbackOutputDir, err)
	}

	if err := writePointer(serial, FallbackOutputDir); err != nil {
		return "", false, err
	}

	return FallbackOutputDir, true, nil
}

// currentOutputDir reads the pointer file and confirms it names a directory
// that is really there.
func currentOutputDir(serial string) string {
	out, err := adb.Shell(serial, "cat", PointerFile, "2>/dev/null")
	if err != nil {
		return ""
	}

	dir := strings.TrimSpace(strings.TrimRight(out, "\r\n"))
	if dir == "" {
		return ""
	}

	// isDirectory() on the far side decides, so check the same thing here
	// rather than trusting the file.
	if _, err := adb.Shell(serial, "ls", "-d", shellQuote(dir), "2>/dev/null"); err != nil {
		return ""
	}
	return dir
}

// writePointer tells the SDK where to look. No trailing newline: the value is
// read back and handed straight to new File().
func writePointer(serial, dir string) error {
	if _, err := adb.Shell(serial, "mkdir", "-p", JavaDir+"/status"); err != nil {
		return fmt.Errorf("cannot create the status directory: %w", err)
	}
	if _, err := adb.Shell(serial, "sh", "-c",
		fmt.Sprintf("'printf %%s %s > %s'", shellQuote(dir), PointerFile)); err != nil {
		return fmt.Errorf("cannot write %s: %w", PointerFile, err)
	}
	return nil
}

// statusDirExists reports whether the directory the app watches was already
// there. FileModifyObserver watches the parent directory as well as the file,
// but a watch on a directory that does not exist never fires, and the watch is
// attached when the app starts. So a status directory created now only starts
// being watched after the next restart.
func statusDirExists(serial string) bool {
	_, err := adb.Shell(serial, "ls", "-d", JavaDir+"/status", "2>/dev/null")
	return err == nil
}

// trigger writes the file RegisteredOpModes watches. Writing rather than
// touching, because toybox touch on an old hub is not guaranteed to create a
// missing file.
func trigger(serial string) error {
	if _, err := adb.Shell(serial, "mkdir", "-p", JavaDir+"/status"); err != nil {
		return fmt.Errorf("cannot create the status directory: %w", err)
	}
	if _, err := adb.Shell(serial, "sh", "-c",
		fmt.Sprintf("'date > %s'", TriggerFile)); err != nil {
		return fmt.Errorf("cannot touch %s: %w", TriggerFile, err)
	}
	return nil
}

// Clean removes what the experiment left on the robot.
func Clean(serial string) error {
	dir := currentOutputDir(serial)
	if dir == "" {
		dir = FallbackOutputDir
	}

	if _, err := adb.Shell(serial, "rm", "-f",
		shellQuote(dir+"/"+ProofName+".dex"), shellQuote(dir+"/"+ProofName+".jar")); err != nil {
		return err
	}

	// Only drop the pointer when it is ours. Removing OnBotJava's would break
	// a hub somebody actually uses it on.
	if dir == FallbackOutputDir {
		_, _ = adb.Shell(serial, "rm", "-f", PointerFile)
	}

	return trigger(serial)
}
