package extreme

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/MunchemOG/ElectroPush/internal/adb"
)

// Deciding whether a reload is equivalent to an install means asking whether
// anything outside team code changed. The obvious way, comparing the APK
// against the one on the robot, does not work: two builds of an unchanged
// project produce different APKs. Measured on a real project, classes3.dex and
// classes5.dex differ every time while every other entry is byte for byte the
// same, because D8 does not pack classes into dex files deterministically.
//
// So the inputs are compared instead. If the gradle files, the SDK sources, the
// local libraries and the parts of the module that are not team code are all
// unchanged, the APK on the robot is equivalent to the one this project builds,
// whatever the two files hash to.

// signatureFile is where the robot records which project state it holds.
const signatureFile = "/data/local/tmp/epsh/extreme-signature"

// signatureInputs are what the APK is made of, other than team code.
//
// Team sources are deliberately absent: changing them is precisely the case
// that should reload rather than install.
func signatureInputs(root string) []string {
	var paths []string

	// Build files anywhere in the project.
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			// Build outputs are derived, and walking them is slow.
			switch info.Name() {
			case "build", ".git", ".gradle", ".idea":
				return filepath.SkipDir
			}
			return nil
		}

		if isBuildInput(info.Name()) {
			paths = append(paths, path)
		}
		return nil
	})

	// The SDK module, and the parts of the team module that are packaged:
	// manifest, resources and assets all end up in the APK.
	for _, dir := range []string{
		filepath.Join(root, "FtcRobotController", "src"),
		filepath.Join(root, Module, "src", "main", "res"),
		filepath.Join(root, Module, "src", "main", "assets"),
	} {
		filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				paths = append(paths, path)
			}
			return nil
		})
	}

	manifest := filepath.Join(root, Module, "src", "main", "AndroidManifest.xml")
	if _, err := os.Stat(manifest); err == nil {
		paths = append(paths, manifest)
	}

	sort.Strings(paths)
	return paths
}

// isBuildInput reports whether a file decides what the APK contains.
//
// The Kotlin DSL names build files .gradle.kts, which does not end in .gradle.
// Missing them meant a project on that DSL could change how the APK is built
// and still be signed as unchanged, so the next deploy reloaded team code onto
// a robot whose APK no longer matched: everything reports success and the robot
// runs stale code. One converted module in an otherwise Groovy project is
// enough for that.
func isBuildInput(name string) bool {
	switch {
	case strings.HasSuffix(name, ".gradle"), strings.HasSuffix(name, ".gradle.kts"):
		return true
	case name == "gradle.properties":
		return true
	// A version catalog decides which versions of every library go into the
	// APK, so editing one changes the build as surely as editing a build file.
	// Found on a real project, where gradle/libs.versions.toml carried the SDK
	// and every library and was signed as if it were documentation.
	case strings.HasSuffix(name, ".versions.toml"):
		return true
	case strings.HasSuffix(name, ".aar"), strings.HasSuffix(name, ".jar"):
		return true
	}
	return false
}

// Signature identifies everything the APK is built from except team code.
func Signature(root string) (string, error) {
	sum := sha256.New()

	paths := signatureInputs(root)
	if len(paths) == 0 {
		return "", fmt.Errorf("nothing to sign in %s", root)
	}

	var buildText strings.Builder
	for _, path := range paths {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			continue
		}
		hashFile(sum, filepath.ToSlash(rel), path)

		if isBuildInput(filepath.Base(path)) && !isArchive(path) {
			if data, err := os.ReadFile(path); err == nil {
				buildText.Write(data)
				buildText.WriteByte('\n')
			}
		}
	}

	if repo := localRepository(); repo != "" {
		for _, path := range localLibraries(repo, buildText.String()) {
			rel, err := filepath.Rel(repo, path)
			if err != nil {
				continue
			}
			hashFile(sum, "mavenLocal/"+filepath.ToSlash(rel), path)
		}
	}

	return hex.EncodeToString(sum.Sum(nil)), nil
}

// hashFile adds one file to the signature. The name goes in as well as the
// contents, so moving a file counts as a change rather than cancelling out
// against another one.
func hashFile(sum io.Writer, name, path string) {
	fmt.Fprintf(sum, "%s\n", name)

	file, err := os.Open(path)
	if err != nil {
		return
	}
	io.Copy(sum, file)
	file.Close()
}

func isArchive(path string) bool {
	return strings.HasSuffix(path, ".aar") || strings.HasSuffix(path, ".jar")
}

// A library published with `publishToMavenLocal` lives outside the project, in
// Maven's local repository, and is republished under the same version every
// time it changes. Nothing in the project moves when that happens, so the
// signature agreed with a robot whose APK still carried the old library while
// team code compiled against the new one was reloaded onto it. That is stale
// code with everything reporting success, or a NoSuchMethodError at init.
//
// Gradle only reads that repository when a build file asks for mavenLocal, and
// only for the coordinates the build names, so those are what get hashed. The
// rest of the repository is left alone, however large it is.

// localRepository is where `publishToMavenLocal` writes. A variable so tests can
// point it somewhere else.
var localRepository = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".m2", "repository")
}

var (
	// 'group:artifact' or 'group:artifact:version', in either quote, which is
	// how both gradle dialects and a version catalog's module = "..." write it.
	coordinateRe = regexp.MustCompile(`["']([A-Za-z0-9_.\-]+):([A-Za-z0-9_.\-]+)(?::[^"'\s]*)?["']`)
	// A version catalog's other form: group = "...", name = "...".
	catalogRe = regexp.MustCompile(`group\s*=\s*"([^"]+)"\s*,\s*name\s*=\s*"([^"]+)"`)
)

// localLibraries is every file in the local repository that a build naming
// these coordinates could read, sorted so the signature is stable.
func localLibraries(repo, buildText string) []string {
	if !strings.Contains(buildText, "mavenLocal") {
		return nil
	}

	seen := map[string]bool{}
	var dirs []string
	add := func(group, artifact string) {
		dir := filepath.Join(append([]string{repo}, append(strings.Split(group, "."), artifact)...)...)
		if seen[dir] {
			return
		}
		seen[dir] = true
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			dirs = append(dirs, dir)
		}
	}
	for _, m := range coordinateRe.FindAllStringSubmatch(buildText, -1) {
		add(m[1], m[2])
	}
	for _, m := range catalogRe.FindAllStringSubmatch(buildText, -1) {
		add(m[1], m[2])
	}

	// Every version, because which one the build picks can come from a
	// variable. Publishing an unrelated version costs one install, which is
	// the safe way to be wrong.
	var paths []string
	for _, dir := range dirs {
		filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			// What a build actually consumes. Metadata files carry publish
			// timestamps, and would make every republish an install even
			// when nothing in the library changed.
			if isArchive(path) || strings.HasSuffix(path, ".pom") {
				paths = append(paths, path)
			}
			return nil
		})
	}

	sort.Strings(paths)
	return paths
}

// RecordSignature notes on the robot which project state it now holds.
func RecordSignature(serial, signature string) {
	_, _ = adb.Shell(serial, "mkdir", "-p", filepath.Dir(signatureFile))

	local, err := os.CreateTemp("", "epsh-signature-*")
	if err != nil {
		return
	}
	defer os.Remove(local.Name())

	if _, err := local.WriteString(signature); err != nil {
		local.Close()
		return
	}
	local.Close()

	_ = adb.Push(serial, local.Name(), signatureFile)
}

// RecordedSignature is the project state the robot holds, empty when unknown.
func RecordedSignature(serial string) string {
	out, err := adb.Shell(serial, "cat", signatureFile, "2>/dev/null")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.TrimRight(out, "\r\n"))
}

// ForgetSignature makes the next deploy install.
func ForgetSignature(serial string) {
	_, _ = adb.Shell(serial, "rm", "-f", signatureFile)
}

// configsFile records which config classes the bridge registered, so the next
// reload can take away the ones it stops registering rather than leaving them
// pointing into a classloader that no longer exists.
const configsFile = "/data/local/tmp/epsh/extreme-configs"

// RecordRegisteredConfigs notes what the bridge put into the dashboard.
func RecordRegisteredConfigs(serial string, names []string) {
	_, _ = adb.Shell(serial, "mkdir", "-p", filepath.Dir(configsFile))

	local, err := os.CreateTemp("", "epsh-configs-*")
	if err != nil {
		return
	}
	defer os.Remove(local.Name())

	if _, err := local.WriteString(strings.Join(names, "\n")); err != nil {
		local.Close()
		return
	}
	local.Close()

	_ = adb.Push(serial, local.Name(), configsFile)
}

// RegisteredConfigs is what the previous reload put into the dashboard.
func RegisteredConfigs(serial string) []string {
	out, err := adb.Shell(serial, "cat", configsFile, "2>/dev/null")
	if err != nil {
		return nil
	}

	var names []string
	for _, line := range strings.Split(out, "\n") {
		if name := strings.TrimSpace(strings.TrimRight(line, "\r")); name != "" {
			names = append(names, name)
		}
	}
	return names
}
