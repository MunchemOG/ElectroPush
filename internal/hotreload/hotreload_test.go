package hotreload

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The proof only proves anything if the class is absent from the APK. A
// parent-first classloader resolves anything the APK already has from there,
// so a colliding package would silently test nothing.
func TestTheProofPackageCannotCollideWithTheSDK(t *testing.T) {
	for _, taken := range []string{
		"com.qualcomm",
		"org.firstinspires.ftc.teamcode",
		"org.firstinspires.ftc.robotcore",
	} {
		if strings.HasPrefix(Package, taken) {
			t.Errorf("%q is under %q, which the APK already has", Package, taken)
		}
	}
}

// The trigger path is what the robot controller watches. Getting it wrong means
// the dex lands and nothing happens.
func TestTriggerMatchesWhatTheSDKWatches(t *testing.T) {
	if TriggerFile != "/sdcard/FIRST/java/status/buildSuccessful.txt" {
		t.Errorf("got %q", TriggerFile)
	}
}

// Compiling needs a JDK, the Android build-tools and the FTC jars. Skipped
// where those are missing rather than failing, since CI has none of them.
// The class name is discovered by listing .class entries inside the .jar, and
// the class is loaded from the .dex. Pushing only the dex loads nothing,
// because the name is never enumerated, which is exactly what went wrong the
// first time this was tried on a robot.
func TestBothTheJarAndTheDexAreBuilt(t *testing.T) {
	tc, err := FindToolchain()
	if err != nil {
		t.Skip(err)
	}
	defer tc.Cleanup()

	files, err := buildAll(tc, t.TempDir(), "Pusher Reload test", "m1")
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{files.Jar, files.Dex} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", path)
		}
	}

	// The jar has to hold the class under its package path, or the SDK builds
	// a name the classloader cannot resolve.
	archive, err := zip.OpenReader(files.Jar)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()

	want := strings.ReplaceAll(Package, ".", "/") + "/" + ClassName + ".class"
	found := false
	for _, entry := range archive.File {
		if entry.Name == want {
			found = true
		}
	}
	if !found {
		var names []string
		for _, entry := range archive.File {
			names = append(names, entry.Name)
		}
		t.Errorf("the jar has no %s, only %v", want, names)
	}
}

func TestTheOpModeCompilesToADex(t *testing.T) {
	tc, err := FindToolchain()
	if err != nil {
		t.Skip(err)
	}
	defer tc.Cleanup()

	work := t.TempDir()

	dex, err := buildDex(tc, work, "Pusher Reload test", "m1")
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(dex)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("the dex is empty")
	}

	// A dex begins with "dex\n". Anything else means d8 wrote something else.
	data, err := os.ReadFile(dex)
	if err != nil {
		t.Fatal(err)
	}
	if string(data[:4]) != "dex\n" {
		t.Errorf("this is not a dex file: %q", data[:8])
	}

	// The marker has to survive into the dex, or a second run cannot be told
	// from the first on the Driver Station.
	if !strings.Contains(string(data), "Pusher Reload test") {
		t.Error("the OpMode name is not in the dex")
	}
}

// The output directory is not a fixed path. getCurrentOutputDir() reads an
// absolute path out of currentOnBotJavaDir.txt and uses that, so writing to any
// other directory is invisible to the SDK no matter how correct it looks. Two
// attempts were lost to assuming a layout here.
func TestThePointerFileIsWhereTheSDKLooks(t *testing.T) {
	if PointerFile != "/sdcard/FIRST/java/status/currentOnBotJavaDir.txt" {
		t.Errorf("got %q", PointerFile)
	}

	// Output goes under the build directory, which is where OnBotJava's own
	// jars live, rather than beside it.
	if !strings.HasPrefix(OutputRoot, JavaDir+"/build/") {
		t.Errorf("got %q, want it under the build directory", OutputRoot)
	}

	// The pointer and the trigger are different files doing different jobs.
	if PointerFile == TriggerFile {
		t.Error("the pointer and the trigger are the same file")
	}
}

// Overwriting the dex in place breaks the mapping the running app holds on it,
// and the reload then finds nothing loadable, so the OpMode vanishes instead of
// changing. Rotating the directory is what OnBotJava itself does, and is why
// currentOnBotJavaDir.txt is an indirection rather than a fixed path.
func TestEachAttemptGetsItsOwnDirectory(t *testing.T) {
	seen := map[string]bool{}

	for _, marker := range []string{"11:02:03", "11:02:04", "12:00:00"} {
		dir := OutputRoot + "/" + DirPrefix + strings.NewReplacer(":", "", " ", "-").Replace(marker)

		if seen[dir] {
			t.Errorf("%q reuses a directory", marker)
		}
		seen[dir] = true

		if !strings.HasPrefix(dir, OutputRoot+"/"+DirPrefix) {
			t.Errorf("got %q, which cleanup would not recognise as ours", dir)
		}
		if strings.ContainsAny(strings.TrimPrefix(dir, OutputRoot+"/"), ": ") {
			t.Errorf("%q has characters that need quoting on the far side", dir)
		}
	}
}

// A stack trace leads with the exception and its message; the frames under it
// only say the event loop called it, which is already known. Two attempts at
// reading a real failure returned nothing but frames.
func TestTheExceptionIsPickedOutOfTheFrames(t *testing.T) {
	lines := []string{
		"I/OnBotJava( 1): starting",
		"E/OnBotJavaHelperImpl( 1): java.io.FileNotFoundException: something.jar (No such file)",
		"E/OnBotJavaHelperImpl( 1):     at java.util.jar.JarFile.<init>(JarFile.java:1)",
		"E/OnBotJavaHelperImpl( 1):     at org.firstinspires.ftc.onbotjava.OnBotJavaHelperImpl.x(Y.java:2)",
		"V/RegisteredOpModes( 1): noting that OnBotJava changed",
	}

	got := firstException(lines)

	if !strings.Contains(got, "FileNotFoundException") {
		t.Errorf("got %q", got)
	}
	// The tag and pid are noise once the message is isolated.
	if strings.Contains(got, "E/OnBotJavaHelperImpl") {
		t.Errorf("the tag was not stripped: %q", got)
	}
}

// Every line of a trace matches the tag filter, so frames have to be told from
// the message by shape rather than by tag.
func TestFramesAreNotMistakenForMessages(t *testing.T) {
	for _, frame := range []string{
		"E/OnBotJavaHelperImpl( 1798):     at java.lang.Thread.run(Thread.java:761)",
		"E/X( 1): \tat com.example.Thing.method(Thing.java:1)",
		"E/X( 1):     ... 3 more",
	} {
		if !isFrame(frame) {
			t.Errorf("%q was not recognised as a stack frame", frame)
		}
	}

	if isFrame("E/OnBotJavaHelperImpl( 1): java.lang.RuntimeException: boom") {
		t.Error("the message was mistaken for a frame")
	}
}

// The two attempts have to bind different motors, or a reload that quietly did
// nothing would look identical to one that worked.
func TestMotorsAlternate(t *testing.T) {
	if len(Motors) < 2 {
		t.Fatal("there is nothing to alternate between")
	}

	seen := map[string]bool{}
	for _, m := range Motors {
		if seen[m] {
			t.Errorf("%q appears twice", m)
		}
		seen[m] = true
	}
}

// The motor has to reach the compiled OpMode, or every attempt binds the same
// one and the test proves nothing.
func TestTheMotorNameEndsUpInTheDex(t *testing.T) {
	tc, err := FindToolchain()
	if err != nil {
		t.Skip(err)
	}
	defer tc.Cleanup()

	for _, motor := range Motors {
		files, err := buildAll(tc, t.TempDir(), "Pusher Reload test", motor)
		if err != nil {
			t.Fatal(err)
		}

		data, err := os.ReadFile(files.Dex)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), motor) {
			t.Errorf("%q is not in the dex", motor)
		}
	}
}

// Every adb command is a process of its own and logs its own noise as it exits.
// A warning from `pm list packages` shutting down was reported as "the robot
// threw while reloading", which sends the next hour in the wrong direction.
func TestOnlyTheRobotControllersProcessIsRead(t *testing.T) {
	lines := []string{
		"D/AndroidRuntime( 2713): Calling main entry com.android.commands.pm.Pm",
		"W/MessageQueue( 2713): java.lang.IllegalStateException: dead thread",
		"E/OnBotJavaHelperImpl(  976): java.util.zip.ZipException: the real one",
	}

	kept := onlyFrom(lines, "976")

	if len(kept) != 1 {
		t.Fatalf("got %d lines, want only the app's: %v", len(kept), kept)
	}
	if !strings.Contains(firstException(kept), "ZipException") {
		t.Errorf("got %q", firstException(kept))
	}

	// And a warning is not an exception even inside the right process.
	if got := firstException([]string{"W/X(  976): java.lang.IllegalStateException: noise"}); got != "" {
		t.Errorf("a warning was reported as a throw: %q", got)
	}

	// The pid is padded to a fixed width, so it has to be read rather than
	// matched against one shape.
	for _, line := range []string{
		"E/Tag(  976): x", "E/Tag( 976): x", "E/Tag(976): x", "E/Tag(    976): x",
	} {
		if got := pidOf(line); got != "976" {
			t.Errorf("pidOf(%q) = %q", line, got)
		}
	}
}

// Nothing may reach the trigger unchecked: one bad file empties the whole
// OpMode list rather than being skipped, so a robot ends up with no OpModes at
// all rather than one that did not update.
func TestABadJarIsCaughtBeforeAnythingIsSent(t *testing.T) {
	dir := t.TempDir()

	jar := filepath.Join(dir, "x.jar")
	dex := filepath.Join(dir, "x.dex")

	// A jar that is not a zip.
	if err := os.WriteFile(jar, []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dex, append([]byte("dex\n035\x00"), make([]byte, 64)...), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := verifyLocal(built{Jar: jar, Dex: dex}); err == nil {
		t.Error("a jar that is not a zip was accepted")
	}
}

func TestABadDexIsCaughtBeforeAnythingIsSent(t *testing.T) {
	tc, err := FindToolchain()
	if err != nil {
		t.Skip(err)
	}
	defer tc.Cleanup()

	work := t.TempDir()
	files, err := buildAll(tc, work, "Pusher Reload test", "m1")
	if err != nil {
		t.Fatal(err)
	}

	// A real jar, and a dex that is not one.
	if err := os.WriteFile(files.Dex, []byte("PK\x03\x04 wrong"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := verifyLocal(files); err == nil {
		t.Error("a dex that does not start like one was accepted")
	}

	// And an empty dex, which is what an interrupted transfer leaves.
	if err := os.WriteFile(files.Dex, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyLocal(files); err == nil {
		t.Error("an empty dex was accepted")
	}
}

// What the tool builds itself has to pass its own check, or the check is wrong.
func TestWhatIsBuiltPassesVerification(t *testing.T) {
	tc, err := FindToolchain()
	if err != nil {
		t.Skip(err)
	}
	defer tc.Cleanup()

	files, err := buildAll(tc, t.TempDir(), "Pusher Reload test", "m1")
	if err != nil {
		t.Fatal(err)
	}

	if err := verifyLocal(files); err != nil {
		t.Errorf("the tool's own output failed verification: %v", err)
	}
}
