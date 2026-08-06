package extreme

import (
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
