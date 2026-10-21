package adb

import (
	"fmt"
	"os/exec"
)

// Shell runs an adb shell command and returns its combined output.
func Shell(serial string, args ...string) (string, error) {
	return run(serial, append([]string{"shell"}, args...)...)
}

// ShellOutput is Shell, keeping what the command printed even when it failed.
//
// Shell throws the output away on a non-zero exit and puts it inside the error
// text, which loses exactly the part worth reading: a tool that refuses says
// why on stderr and then exits non-zero, so "the robot would not load the
// driver" came back with no reason given when insmod had explained itself
// perfectly well.
func ShellOutput(serial string, args ...string) (string, error) {
	full := append([]string{"shell"}, args...)
	if serial != "" {
		full = append([]string{"-s", serial}, full...)
	}

	out, err := exec.Command("adb", full...).CombinedOutput()
	return string(out), err
}

// Pull copies a file off the device.
func Pull(serial, remote, local string) error {
	_, err := run(serial, "pull", remote, local)
	return err
}

// Push copies a file onto the device.
func Push(serial, local, remote string) error {
	_, err := run(serial, "push", local, remote)
	return err
}

// Target picks the robot to talk to, preferring USB the way deploying does.
//
// Both failures are wrapped sentinels rather than plain messages, because what
// a caller should do about them differs: one is worth offering to fix, and the
// other is somebody's afternoon with a package manager.
func Target() (string, error) {
	if !IsInstalled() {
		return "", fmt.Errorf("%w - install Android SDK Platform-Tools", ErrNoADB)
	}
	if dev, ok := FindUSBDevice(); ok {
		return dev.Serial, nil
	}
	if IsConnected() {
		return RobotAddr(), nil
	}
	return "", fmt.Errorf("%w - plug in USB or run `epsh connect`", ErrNoRobot)
}
