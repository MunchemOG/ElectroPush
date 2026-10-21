//go:build windows

package bridge

import (
	"fmt"
	"os/exec"
	"strings"
)

const taskName = "epsh bridge tunnel"

// taskCommand runs the tunnel without a console window: a scheduled task opens
// one for a console program, and a window that closes the tunnel when somebody
// closes it is not one to leave on screen.
func taskCommand(exe string) string {
	return fmt.Sprintf(`powershell.exe -NoProfile -WindowStyle Hidden -Command "& '%s' bridge tunnel"`,
		strings.ReplaceAll(exe, "'", "''"))
}

// installAutostart starts the tunnel now and at every logon, as a scheduled
// task.
func installAutostart(exe string) error {
	_ = exec.Command("schtasks", "/End", "/TN", taskName).Run()
	if out, err := exec.Command("schtasks", "/Create", "/TN", taskName, "/TR", taskCommand(exe),
		"/SC", "ONLOGON", "/RL", "LIMITED", "/F").CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks /Create: %s", strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("schtasks", "/Run", "/TN", taskName).CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks /Run: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func removeAutostart() error {
	_ = exec.Command("schtasks", "/End", "/TN", taskName).Run()
	if out, err := exec.Command("schtasks", "/Delete", "/TN", taskName, "/F").CombinedOutput(); err != nil &&
		!strings.Contains(strings.ToLower(string(out)), "cannot find") {
		return fmt.Errorf("schtasks /Delete: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func autostartInstalled() bool {
	return exec.Command("schtasks", "/Query", "/TN", taskName).Run() == nil
}

func autostartName() string { return "scheduled task \"" + taskName + "\"" }
