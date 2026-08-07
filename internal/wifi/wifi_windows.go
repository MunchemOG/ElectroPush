//go:build windows

package wifi

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const tracksRecency = false

// LocationHint explains the permission this platform needs to read the network name.
const LocationHint = `Windows reports the current network name freely, so no permission is needed.

Because Windows keeps no record of when each saved network was last used,
'pusher exit' on its own cannot tell where you came from. Set the network to
return to explicitly:
  pusher settings -> Home Wi-Fi network`

func netsh(args ...string) (string, error) {
	out, err := exec.Command("netsh", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("netsh %s: %w (output: %s)",
			strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func powershell(script string) (string, error) {
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (m *Manager) detectInterface() string {
	name, err := powershell(
		"(Get-NetAdapter -Physical | Where-Object { $_.PhysicalMediaType -match '802.11' } | Select-Object -First 1).Name")
	if err == nil && name != "" {
		return name
	}

	if out, err := netsh("wlan", "show", "interfaces"); err == nil {
		if iface := netshField(out, "Name"); iface != "" {
			return iface
		}
	}

	return "Wi-Fi"
}

// CurrentSSID is the network the machine is on.
func (m *Manager) CurrentSSID() (string, error) {

	name, err := powershell(fmt.Sprintf(
		"(Get-NetConnectionProfile -InterfaceAlias '%s').Name", escapeSingleQuotes(m.wifiInterface())))
	if err == nil && name != "" {
		return name, nil
	}

	out, err := netsh("wlan", "show", "interfaces")
	if err != nil {
		return "", err
	}

	return netshField(out, "SSID"), nil
}

// PreferredNetworks lists the saved networks, most recent first.
func (m *Manager) PreferredNetworks() ([]string, error) {
	out, err := netsh("wlan", "show", "profiles")
	if err != nil {
		return nil, err
	}
	return parseNetshProfiles(out), nil
}

// IsPoweredOn reports whether the Wi-Fi radio is on.
func (m *Manager) IsPoweredOn() bool {
	status, err := powershell(
		"(Get-NetAdapter -Physical | Where-Object { $_.PhysicalMediaType -match '802.11' } | Select-Object -First 1).Status")
	if err != nil {

		return true
	}
	return !strings.EqualFold(status, "Disabled")
}

// PowerOn turns the Wi-Fi radio on.
func (m *Manager) PowerOn() error {
	if m.IsPoweredOn() {
		return nil
	}
	return fmt.Errorf("the Wi-Fi adapter is disabled - enable it in Windows network settings")
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
		if err := m.addProfile(ssid, password); err != nil {
			return err
		}
	}

	out, err := netsh("wlan", "connect",
		"name="+ssid, "ssid="+ssid, "interface="+m.wifiInterface())
	if err != nil {
		return fmt.Errorf("failed to join %q: %w", ssid, err)
	}

	if lower := strings.ToLower(out); strings.Contains(lower, "not find") ||
		strings.Contains(lower, "no profile") {
		return fmt.Errorf("failed to join %q: %s", ssid, strings.TrimSpace(out))
	}

	return nil
}

func (m *Manager) addProfile(ssid, password string) error {
	profile, err := wlanProfileXML(ssid, password)
	if err != nil {
		return err
	}

	file, err := os.CreateTemp("", "pusher-wlan-*.xml")
	if err != nil {
		return fmt.Errorf("cannot create Wi-Fi profile: %w", err)
	}
	path := file.Name()

	defer os.Remove(path)

	if _, err := file.WriteString(profile); err != nil {
		file.Close()
		return fmt.Errorf("cannot write Wi-Fi profile: %w", err)
	}
	file.Close()

	if _, err := netsh("wlan", "add", "profile",
		"filename="+filepath.Clean(path), "user=current"); err != nil {
		return fmt.Errorf("cannot import Wi-Fi profile for %q: %w", ssid, err)
	}

	return nil
}

// PowerCycle turns the radio off and on again.
func (m *Manager) PowerCycle() error {
	if _, err := netsh("wlan", "disconnect", "interface="+m.wifiInterface()); err != nil {
		return fmt.Errorf("failed to disconnect Wi-Fi: %w", err)
	}

	time.Sleep(2 * time.Second)
	return nil
}

func (m *Manager) rejoin(ssid string, _ []string) error {
	return m.Join(ssid, "")
}
