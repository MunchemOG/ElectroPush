//go:build linux

package wifi

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// NetworkManager records a real connection.timestamp per profile, so recency
// here is stored data rather than the list-ordering inference macOS forces.
const tracksRecency = true

const LocationHint = `Wi-Fi control needs NetworkManager, which is standard on Debian and Ubuntu
desktops. pusher drives it through nmcli.

If nmcli is missing:
  sudo apt install network-manager

On a machine managed by ifupdown or systemd-networkd instead, pusher cannot
switch networks. Connect to the robot yourself and pusher will deploy over the
existing connection.`

func nmcli(args ...string) (string, error) {
	out, err := exec.Command("nmcli", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("nmcli %s: %w (output: %s)",
			strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func (m *Manager) detectInterface() string {
	out, err := nmcli("-t", "-f", "DEVICE,TYPE", "device")
	if err != nil {
		return ""
	}
	return parseNmcliWiFiDevice(out)
}

// Linux reports the SSID freely, so ErrSSIDUnavailable never arises here.
func (m *Manager) CurrentSSID() (string, error) {
	out, err := nmcli("-t", "-f", "ACTIVE,SSID", "device", "wifi")
	if err != nil {
		return "", err
	}
	return parseNmcliActiveSSID(out), nil
}

// Ordered most-recently-connected first, from NetworkManager's own timestamps.
func (m *Manager) PreferredNetworks() ([]string, error) {
	out, err := nmcli("-t", "-f", "NAME,TYPE,TIMESTAMP", "connection", "show")
	if err != nil {
		return nil, err
	}
	return parseNmcliSavedNetworks(out), nil
}

func (m *Manager) IsPoweredOn() bool {
	out, err := nmcli("radio", "wifi")
	if err != nil {
		return false
	}
	return parseNmcliRadio(out)
}

func (m *Manager) PowerOn() error {
	if m.IsPoweredOn() {
		return nil
	}

	if _, err := nmcli("radio", "wifi", "on"); err != nil {
		return fmt.Errorf("failed to turn Wi-Fi on: %w", err)
	}

	time.Sleep(2 * time.Second)
	return nil
}

func (m *Manager) Join(ssid, password string) error {
	if ssid == "" {
		return fmt.Errorf("no SSID given")
	}

	if err := m.PowerOn(); err != nil {
		return err
	}

	if password != "" {
		// Creates the profile if absent and updates the key if it changed.
		args := []string{"device", "wifi", "connect", ssid, "password", password}
		if iface := m.wifiInterface(); iface != "" {
			args = append(args, "ifname", iface)
		}
		if _, err := nmcli(args...); err != nil {
			return fmt.Errorf("failed to join %q: %w", ssid, err)
		}
		return nil
	}

	// No password: prefer bringing up the saved profile, which reuses the
	// stored key. Fall back to a scan-and-connect for open networks.
	if _, err := nmcli("connection", "up", "id", ssid); err == nil {
		return nil
	}

	if _, err := nmcli("device", "wifi", "connect", ssid); err != nil {
		return fmt.Errorf("failed to join %q: %w", ssid, err)
	}
	return nil
}

func (m *Manager) PowerCycle() error {
	if _, err := nmcli("radio", "wifi", "off"); err != nil {
		return fmt.Errorf("failed to turn Wi-Fi off: %w", err)
	}

	time.Sleep(2 * time.Second)

	if _, err := nmcli("radio", "wifi", "on"); err != nil {
		return fmt.Errorf("failed to turn Wi-Fi on: %w", err)
	}

	return nil
}

// NetworkManager brings up a saved profile with its stored key, so leaving
// needs no special handling.
func (m *Manager) rejoin(ssid string, _ []string) error {
	return m.Join(ssid, "")
}
