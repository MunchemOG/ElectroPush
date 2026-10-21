//go:build darwin

package bridge

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const agentLabel = "me.munchem.epsh.bridge"

func agentPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", agentLabel+".plist")
}

func agentPlist(exe string) string {
	escape := func(s string) string {
		return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>bridge</string>
		<string>tunnel</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>ThrottleInterval</key>
	<integer>10</integer>
</dict>
</plist>
`, agentLabel, escape(exe))
}

func domain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

// installAutostart starts the tunnel now and at every login, as a LaunchAgent.
func installAutostart(exe string) error {
	if err := os.MkdirAll(filepath.Dir(agentPath()), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(agentPath(), []byte(agentPlist(exe)), 0o644); err != nil {
		return err
	}
	_ = exec.Command("launchctl", "bootout", domain()+"/"+agentLabel).Run()
	if out, err := exec.Command("launchctl", "bootstrap", domain(), agentPath()).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func removeAutostart() error {
	_ = exec.Command("launchctl", "bootout", domain()+"/"+agentLabel).Run()
	if err := os.Remove(agentPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func autostartInstalled() bool {
	_, err := os.Stat(agentPath())
	return err == nil
}

func autostartName() string { return "LaunchAgent " + agentLabel }
