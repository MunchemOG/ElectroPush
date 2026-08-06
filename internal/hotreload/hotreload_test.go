package hotreload

import (
	"os"
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
func TestTheOpModeCompilesToADex(t *testing.T) {
	tc, err := FindToolchain()
	if err != nil {
		t.Skip(err)
	}
	defer tc.Cleanup()

	work := t.TempDir()

	dex, err := buildDex(tc, work, "Pusher Reload test")
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
