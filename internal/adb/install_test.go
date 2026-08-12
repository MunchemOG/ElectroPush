package adb

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionIDIsPulledOutOfWhatThePackageManagerSays(t *testing.T) {
	for _, tc := range []struct{ out, want string }{
		{"Success: created install session [99]", "99"},
		{"Success: created install session [1234567]\n", "1234567"},
		{"Failure [INSTALL_FAILED_INVALID_APK]", ""},
		{"", ""},
		{"no brackets here", ""},
	} {
		if got := sessionID(tc.out); got != tc.want {
			t.Errorf("sessionID(%q) = %q, want %q", tc.out, got, tc.want)
		}
	}
}

func TestSplitNaming(t *testing.T) {
	apks := []string{
		"/x/TeamCode-debug.apk",
		"/x/OpModes-debug.apk",
		"/x/Extra-debug.apk",
	}

	if got := splitName(apks, 0); got != "base" {
		t.Errorf("the application module was filed as %q, want base", got)
	}
	if got := splitName(apks, 1); got != "OpModes-debug" {
		t.Errorf("got %q", got)
	}
	if got := splitName(apks, 2); got != "Extra-debug" {
		t.Errorf("got %q", got)
	}

	if got := splitName([]string{"/x/anything.apk"}, 0); got != "base" {
		t.Errorf("got %q", got)
	}
}

func TestFingerprintTracksContent(t *testing.T) {
	dir := t.TempDir()

	a := filepath.Join(dir, "a.apk")
	b := filepath.Join(dir, "b.apk")

	if err := os.WriteFile(a, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}

	sumA, err := APKFingerprint(a)
	if err != nil {
		t.Fatal(err)
	}
	sumB, err := APKFingerprint(b)
	if err != nil {
		t.Fatal(err)
	}
	if sumA != sumB {
		t.Error("identical files fingerprinted differently")
	}

	if err := os.WriteFile(b, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	sumB, _ = APKFingerprint(b)
	if sumA == sumB {
		t.Error("different files fingerprinted the same")
	}

	if _, err := APKFingerprint(filepath.Join(dir, "missing.apk")); err == nil {
		t.Error("a missing file fingerprinted without complaint")
	}
}

func TestInstallPathsAgainstADevice(t *testing.T) {
	serial := os.Getenv("EPSH_TEST_DEVICE")
	apk := os.Getenv("EPSH_TEST_APK")

	if serial == "" || apk == "" {
		t.Skip("set EPSH_TEST_DEVICE and EPSH_TEST_APK to run this")
	}

	forgetInstalled(serial)

	plan, err := InstallWith(serial, apk, Options{Stream: true, SkipUnchanged: true})
	if err != nil {
		t.Fatalf("streaming install failed: %v", err)
	}
	if !plan.Streamed {
		t.Error("the install did not stream")
	}
	if plan.Skipped {
		t.Error("the first install was skipped")
	}

	plan, err = InstallWith(serial, apk, Options{Stream: true, SkipUnchanged: true})
	if err != nil {
		t.Fatalf("second install failed: %v", err)
	}
	if !plan.Skipped {
		t.Error("an unchanged APK was installed again")
	}
}

// Streaming used to be tried first with the local APK, so whenever both were on
// the whole file was sent and delta never ran. Both are on by default, which
// made delta dead for everyone who had not changed a setting.
func TestDeltaAndStreamingCompose(t *testing.T) {
	serial := os.Getenv("EPSH_TEST_DEVICE")
	apk := os.Getenv("EPSH_TEST_APK")

	if serial == "" || apk == "" {
		t.Skip("set EPSH_TEST_DEVICE and EPSH_TEST_APK to run this")
	}

	forgetInstalled(serial)

	plan, err := InstallWith(serial, apk, Options{Delta: true, Stream: true})
	if err != nil {
		t.Fatalf("install failed: %v", err)
	}

	if !plan.Delta {
		t.Error("delta was asked for and did not run")
	}
	if !plan.Streamed {
		t.Error("streaming was asked for and did not run")
	}
}

// The record of what is installed is not only for skipping. Epsh Extreme
// reads it to decide whether a reload is equivalent to an install, and when it
// was only written with skipping enabled, turning that setting off made extreme
// silently never activate.
func TestTheInstalledRecordIsKeptRegardlessOfSkipping(t *testing.T) {
	serial := os.Getenv("EPSH_TEST_DEVICE")
	apk := os.Getenv("EPSH_TEST_APK")

	if serial == "" || apk == "" {
		t.Skip("set EPSH_TEST_DEVICE and EPSH_TEST_APK to run this")
	}

	forgetInstalled(serial)

	// Skipping off on purpose.
	if _, err := InstallWith(serial, apk, Options{Stream: true, SkipUnchanged: false}); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	if InstalledFingerprint(serial) == "" {
		t.Error("nothing was recorded, so extreme could never tell what the robot holds")
	}
}
