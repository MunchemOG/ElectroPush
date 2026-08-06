package extreme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Gradle's own chatter surrounds the answer, so the block markers are what make
// the output readable rather than the line shapes.
func TestClasspathIsReadFromTheMarkedBlock(t *testing.T) {
	output := `Downloading gradle...
> Task :TeamCode:pusherClasspath
PUSHER_CP_BEGIN
CP /a/one.jar
CP /a/two.jar
BOOT /sdk/android.jar
PUSHER_CP_END

BUILD SUCCESSFUL in 763ms
CP /this/is/not/in/the/block.jar`

	cp := parseClasspath(output)

	if len(cp.Compile) != 2 {
		t.Fatalf("got %v", cp.Compile)
	}
	if len(cp.Boot) != 1 {
		t.Fatalf("got %v", cp.Boot)
	}
	for _, entry := range cp.Compile {
		if strings.Contains(entry, "not/in/the/block") {
			t.Error("a line outside the block was read")
		}
	}
}

// javac needs the platform as a boot classpath, not as an ordinary dependency,
// or Android classes and the JDK's own resolve against each other.
func TestArgsSeparateThePlatformFromTheDependencies(t *testing.T) {
	cp := Classpath{
		Compile: []string{"/a/one.jar", "/a/two.jar"},
		Boot:    []string{"/sdk/android.jar"},
	}

	args := cp.Args()

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-bootclasspath /sdk/android.jar") {
		t.Errorf("got %v", args)
	}
	if !strings.Contains(joined, "-classpath /a/one.jar") {
		t.Errorf("got %v", args)
	}

	// Nothing to say when there is nothing to say.
	if got := (Classpath{}).Args(); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestAnEmptyClasspathIsAnError(t *testing.T) {
	if cp := parseClasspath("BUILD SUCCESSFUL"); len(cp.Compile) != 0 {
		t.Errorf("got %v", cp.Compile)
	}
}

// The exclusion is marked rather than backed up: slim already keeps a
// .pusher-bak of the same file, and two features sharing one backup means
// undoing either undoes both.
func TestExclusionIsAddedAndRemovedExactly(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, Module), 0o755); err != nil {
		t.Fatal(err)
	}

	original := "android {\n    namespace = 'org.firstinspires.ftc.teamcode'\n}\n"
	if err := os.WriteFile(GradleFile(root), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if Excluded(root) {
		t.Fatal("a fresh project reports as excluded")
	}

	if err := Exclude(root); err != nil {
		t.Fatal(err)
	}
	if !Excluded(root) {
		t.Fatal("the exclusion was not detected after adding it")
	}

	after, _ := os.ReadFile(GradleFile(root))
	if !strings.Contains(string(after), TeamPackage) {
		t.Error("the excluded package is not named in the block")
	}
	// The instruction for getting back has to be in the file, not only in a menu.
	if !strings.Contains(string(after), "Remove this block") {
		t.Error("the block does not say how to undo it")
	}

	// Adding twice must not stack.
	if err := Exclude(root); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(mustRead(t, GradleFile(root))), beginMarker); got != 1 {
		t.Errorf("the block appears %d times", got)
	}

	if err := Include(root); err != nil {
		t.Fatal(err)
	}
	if Excluded(root) {
		t.Error("still excluded after going back")
	}

	// And the file has to come back exactly as it was.
	if got := string(mustRead(t, GradleFile(root))); got != original {
		t.Errorf("the file did not come back unchanged:\n%q", got)
	}
}

// Removing what was never added must not damage the file.
func TestIncludeOnAnUntouchedProjectDoesNothing(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, Module), 0o755); err != nil {
		t.Fatal(err)
	}

	original := "dependencies {\n}\n"
	if err := os.WriteFile(GradleFile(root), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Include(root); err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, GradleFile(root))); got != original {
		t.Errorf("got %q", got)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
