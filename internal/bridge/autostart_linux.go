//go:build linux

package bridge

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const userUnit = "epsh-bridge-tunnel.service"

func userUnitPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user", userUnit)
}

func tunnelUnit(exe string) string {
	return fmt.Sprintf(`[Unit]
Description=epsh: the tunnel to the robot's bridge
After=network-online.target

[Service]
ExecStart=%s bridge tunnel
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
`, exe)
}

// installAutostart starts the tunnel now and at every login, as a systemd user
// service.
func installAutostart(exe string) error {
	if err := os.MkdirAll(filepath.Dir(userUnitPath()), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(userUnitPath(), []byte(tunnelUnit(exe)), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{{"--user", "daemon-reload"}, {"--user", "enable", userUnit}, {"--user", "restart", userUnit}} {
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func removeAutostart() error {
	_ = exec.Command("systemctl", "--user", "disable", "--now", userUnit).Run()
	if err := os.Remove(userUnitPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	return nil
}

func autostartInstalled() bool {
	_, err := os.Stat(userUnitPath())
	return err == nil
}

func autostartName() string { return "systemd user service " + userUnit }
