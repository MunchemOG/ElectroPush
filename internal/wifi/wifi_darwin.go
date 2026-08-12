//go:build darwin

package wifi

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const tracksRecency = true

// LocationHint explains the permission this platform needs to read the network name.
const LocationHint = `macOS treats "which Wi-Fi am I on?" as a location lookup and hides the network
name from command-line tools. The terminal cannot be added to Location Services
by hand, because macOS only lists apps that have already asked, and command-line
tools have not been able to ask since macOS 13.

epsh works around this by reading the saved-network list instead, which macOS
keeps in most-recently-joined order, so the network you are on is the first
entry. That needs no permission at all.

If it ever guesses wrong, set the network to return to explicitly:
  epsh settings -> Home Wi-Fi network`

func (m *Manager) detectInterface() string {
	return "en0"
}

// CurrentSSID is the network the machine is on.
func (m *Manager) CurrentSSID() (string, error) {
	ssid := m.ssidFromNetworksetup()
	if ssid == "" {
		ssid = m.ssidFromIPConfig()
	}

	if ssid != "" && !isRedacted(ssid) {
		return ssid, nil
	}

	ip, err := m.GetIPv4()
	if err != nil {
		return "", err
	}
	if ip != "" {
		return "", ErrSSIDUnavailable
	}

	return "", nil
}

func (m *Manager) ssidFromNetworksetup() string {
	out, err := exec.Command("networksetup", "-getairportnetwork", m.wifiInterface()).Output()
	if err != nil {
		return ""
	}
	return parseNetworksetupSSID(string(out))
}

func (m *Manager) ssidFromIPConfig() string {
	out, err := exec.Command("ipconfig", "getsummary", m.wifiInterface()).Output()
	if err != nil {
		return ""
	}

	for _, line := range strings.Split(string(out), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "SSID ") && !strings.HasPrefix(trimmed, "SSID:") {
			continue
		}
		if _, value, found := strings.Cut(trimmed, ":"); found {
			return strings.TrimSpace(value)
		}
	}

	return ""
}

// PreferredNetworks lists the saved networks, most recent first.
func (m *Manager) PreferredNetworks() ([]string, error) {
	out, err := exec.Command("networksetup", "-listpreferredwirelessnetworks", m.wifiInterface()).Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list preferred networks: %w", err)
	}
	return parseDarwinPreferred(string(out)), nil
}

// IsPoweredOn reports whether the Wi-Fi radio is on.
func (m *Manager) IsPoweredOn() bool {
	out, err := exec.Command("networksetup", "-getairportpower", m.wifiInterface()).Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), ": on")
}

// PowerOn turns the Wi-Fi radio on.
func (m *Manager) PowerOn() error {
	if m.IsPoweredOn() {
		return nil
	}

	out, err := exec.Command("networksetup", "-setairportpower", m.wifiInterface(), "on").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to turn Wi-Fi on: %w (output: %s)", err, strings.TrimSpace(string(out)))
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

	args := []string{"-setairportnetwork", m.wifiInterface(), ssid}
	if password != "" {
		args = append(args, password)
	}

	out, err := exec.Command("networksetup", args...).CombinedOutput()
	result := strings.TrimSpace(string(out))
	if err != nil {
		return fmt.Errorf("failed to join %q: %w (output: %s)", ssid, err, result)
	}

	if result != "" {
		lower := strings.ToLower(result)
		if strings.Contains(lower, "could not find") ||
			strings.Contains(lower, "failed to join") ||
			strings.Contains(lower, "error") {
			return fmt.Errorf("failed to join %q: %s", ssid, result)
		}
	}

	return nil
}

// networksetup refuses a credential-less join with -3900, so the robot's
// networks are dropped from the preferred list and the radio power-cycled.
func (m *Manager) rejoin(ssid string, leaving []string) error {
	iface := m.wifiInterface()

	for _, network := range leaving {
		if network == "" || network == ssid {
			continue
		}

		_ = exec.Command("networksetup", "-removepreferredwirelessnetwork", iface, network).Run()
	}

	return m.PowerCycle()
}

// PowerCycle turns the radio off and on again.
func (m *Manager) PowerCycle() error {
	iface := m.wifiInterface()

	if output, err := exec.Command("networksetup", "-setairportpower", iface, "off").CombinedOutput(); err != nil {
		return fmt.Errorf("failed to turn Wi-Fi off: %w (output: %s)", err, string(output))
	}

	time.Sleep(2 * time.Second)

	if output, err := exec.Command("networksetup", "-setairportpower", iface, "on").CombinedOutput(); err != nil {
		return fmt.Errorf("failed to turn Wi-Fi on: %w (output: %s)", err, string(output))
	}

	return nil
}
