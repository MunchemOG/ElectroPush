//go:build !darwin && !linux && !windows

package bridge

import "fmt"

func installAutostart(exe string) error {
	return fmt.Errorf("epsh cannot start the tunnel at login on this system: run `epsh bridge tunnel` yourself")
}

func removeAutostart() error { return nil }

func autostartInstalled() bool { return false }

func autostartName() string { return "nothing" }
