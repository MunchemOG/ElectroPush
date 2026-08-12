//go:build linux

package wifi

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const tracksRecency = true

// LocationHint explains the permission this platform needs to read the network name.
const LocationHint = `Wi-Fi control needs NetworkManager, which is standard on Debian and Ubuntu
desktops. epsh drives it through nmcli.

If nmcli is missing:
  sudo apt install network-manager

On a machine managed by ifupdown or systemd-networkd instead, epsh cannot
switch networks. Connect to the robot yourself and epsh will deploy over the
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

// CurrentSSID is the network the machine is on.
func (m *Manager) CurrentSSID() (string, error) {
	out, err := nmcli("-t", "-f", "ACTIVE,SSID", "device", "wifi")
	if err != nil {
		return "", err
	}
	return parseNmcliActiveSSID(out), nil
}

// PreferredNetworks lists the saved networks, most recent first.
func (m *Manager) PreferredNetworks() ([]string, error) {
	out, err := nmcli("-t", "-f", "NAME,TYPE,TIMESTAMP", "connection", "show")
	if err != nil {
		return nil, err
	}
	return parseNmcliSavedNetworks(out), nil
}

// IsPoweredOn reports whether the Wi-Fi radio is on.
func (m *Manager) IsPoweredOn() bool {
	out, err := nmcli("radio", "wifi")
	if err != nil {
		return false
	}
	return parseNmcliRadio(out)
}

// PowerOn turns the Wi-Fi radio on.
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

// Join connects to a network.
func (m *Manager) Join(ssid, password string) error {
	if ssid == "" {
		return fmt.Errorf("no SSID given")
	}

	if err := m.PowerOn(); err != nil {
		return err
	}

	if password != "" {

		args := []string{"device", "wifi", "connect", ssid, "password", password}
		if iface := m.wifiInterface(); iface != "" {
			args = append(args, "ifname", iface)
		}
		if _, err := nmcli(args...); err != nil {
			return fmt.Errorf("failed to join %q: %w", ssid, err)
		}
		return nil
	}

	if _, err := nmcli("connection", "up", "id", ssid); err == nil {
		return nil
	}

	if _, err := nmcli("device", "wifi", "connect", ssid); err != nil {
		return fmt.Errorf("failed to join %q: %w", ssid, err)
	}
	return nil
}

// PowerCycle turns the radio off and on again.
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

func (m *Manager) rejoin(ssid string, _ []string) error {
	return m.Join(ssid, "")
}
