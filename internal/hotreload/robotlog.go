package hotreload

import (
	"fmt"
	"os"
	"strings"

	"github.com/andreibanu/pusher/internal/adb"
)

// The robot controller runs its own logcat into a file on /sdcard, rotating
// four of them. Those survive the app dying and the hub rebooting, which
// `adb logcat -d` does not: a reboot empties that buffer and takes the reason
// for the reboot with it.
//
// So when something crashes hard enough to lose the live log, this is where the
// answer still is.

// robotLogFiles are the SDK's own logs, oldest last.
var robotLogFiles = []string{
	"/sdcard/robotControllerLog.txt",
	"/sdcard/robotControllerLog.txt.1",
	"/sdcard/robotControllerLog.txt.2",
	"/sdcard/robotControllerLog.txt.3",
}

// crashMarkers are what a hard failure leaves behind.
var crashMarkers = []string{
	"FATAL EXCEPTION",
	"AndroidRuntime",
	"beginning of crash",
	"Process: com.qualcomm",
	"signal 11",
	"signal 6",
	"tombstone",
	"ANR in",
	"Force finishing",
	"has died",
	"Scheduling restart",
}

// CollectRobotLog pulls the robot controller's own logs and saves them.
//
// Returns where they were saved and the first crash found, which is empty when
// there is not one.
func CollectRobotLog(serial string) (path, crash string, err error) {
	var whole strings.Builder

	found := 0
	for i := len(robotLogFiles) - 1; i >= 0; i-- {
		remote := robotLogFiles[i]

		local, err := os.CreateTemp("", "pusher-rclog-*")
		if err != nil {
			continue
		}
		local.Close()

		if adb.Pull(serial, remote, local.Name()) != nil {
			os.Remove(local.Name())
			continue
		}

		data, err := os.ReadFile(local.Name())
		os.Remove(local.Name())
		if err != nil || len(data) == 0 {
			continue
		}

		found++
		fmt.Fprintf(&whole, "----- %s -----\n", remote)
		whole.Write(data)
		whole.WriteString("\n")
	}

	if found == 0 {
		return "", "", fmt.Errorf("no robot controller logs on the hub")
	}

	saved, err := os.CreateTemp("", "pusher-robot-log-*.txt")
	if err != nil {
		return "", "", err
	}
	defer saved.Close()

	if _, err := saved.WriteString(whole.String()); err != nil {
		return "", "", err
	}

	return saved.Name(), lastCrash(whole.String()), nil
}

// lastCrash returns the most recent crash in the log, with the lines around it.
//
// The most recent, not the first: these files hold hours of history and the
// interesting failure is the one that just happened.
func lastCrash(log string) string {
	lines := strings.Split(log, "\n")

	at := -1
	for i, line := range lines {
		for _, marker := range crashMarkers {
			if strings.Contains(line, marker) {
				at = i
				break
			}
		}
	}

	if at < 0 {
		return ""
	}

	start := at - 4
	if start < 0 {
		start = 0
	}
	end := at + 40
	if end > len(lines) {
		end = len(lines)
	}

	var out []string
	for _, line := range lines[start:end] {
		if line = strings.TrimRight(line, "\r"); line != "" {
			out = append(out, line)
		}
	}

	return strings.Join(out, "\n")
}
