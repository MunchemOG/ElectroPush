//go:build darwin

package wifi

import (
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"
)

const RobotSubnet = "192.168.43."

var ErrSSIDUnavailable = errors.New("macOS will not report the current Wi-Fi network")

const LocationHint = `macOS treats "which Wi-Fi am I on?" as a location lookup and hides the network
name from command-line tools. The terminal cannot be added to Location Services
by hand, because macOS only lists apps that have already asked, and command-line
tools have not been able to ask since macOS 13.

pusher works around this by reading the saved-network list instead, which macOS
keeps in most-recently-joined order, so the network you are on is the first
entry. That needs no permission at all.

If it ever guesses wrong, set the network to return to explicitly:
  pusher settings -> Home Wi-Fi network`

type Manager struct {
	iface string
}

func NewManager() *Manager {
	return &Manager{iface: "en0"}
}

func (m *Manager) GetIPv4() (string, error) {
	iface, err := net.InterfaceByName(m.iface)
	if err != nil {
		return "", fmt.Errorf("failed to get interface %s: %w", m.iface, err)
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return "", fmt.Errorf("failed to get addresses for %s: %w", m.iface, err)
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok {
			ip4 := ipNet.IP.To4()
			if ip4 != nil {
				return ip4.String(), nil
			}
		}
	}

	return "", nil
}

func (m *Manager) IsOnRobotNetwork() (bool, error) {
	ip, err := m.GetIPv4()
	if err != nil {
		return false, err
	}
	if ip == "" {
		return false, nil
	}
	return strings.HasPrefix(ip, RobotSubnet), nil
}

// Returns ("", nil) when genuinely not associated, and ErrSSIDUnavailable when
// associated but macOS is withholding the name. Both produce the same
// "not associated" output, so the IPv4 lease is what tells them apart.
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
	out, err := exec.Command("networksetup", "-getairportnetwork", m.iface).Output()
	if err != nil {
		return ""
	}

	return parseNetworksetupSSID(string(out))
}

func parseNetworksetupSSID(output string) string {
	line := strings.TrimSpace(output)

	_, ssid, found := strings.Cut(line, ":")
	if !found {
		return ""
	}

	return strings.TrimSpace(ssid)
}

func (m *Manager) ssidFromIPConfig() string {
	out, err := exec.Command("ipconfig", "getsummary", m.iface).Output()
	if err != nil {
		return ""
	}

	for _, line := range strings.Split(string(out), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "SSID ") && !strings.HasPrefix(trimmed, "SSID:") {
			continue
		}
		_, value, found := strings.Cut(trimmed, ":")
		if !found {
			continue
		}
		return strings.TrimSpace(value)
	}

	return ""
}

func isRedacted(ssid string) bool {
	lower := strings.ToLower(ssid)
	return strings.Contains(lower, "redacted") ||
		strings.Contains(lower, "not associated") ||
		lower == "<unknown>"
}

// macOS keeps saved networks most-recently-joined first, so the first entry is
// the one in use. Needs no permission, unlike reading the SSID directly.
// exclude must carry the robot's networks: joining the robot puts it at the
// top, and returning it as "home" would strand the user on the hotspot.
func (m *Manager) MostRecentNetwork(exclude ...string) (string, error) {
	networks, err := m.PreferredNetworks()
	if err != nil {
		return "", err
	}

	return firstNotIn(networks, exclude), nil
}

func firstNotIn(networks, exclude []string) string {
	skip := make(map[string]bool, len(exclude))
	for _, ssid := range exclude {
		if ssid != "" {
			skip[ssid] = true
		}
	}

	for _, ssid := range networks {
		if !skip[ssid] {
			return ssid
		}
	}

	return ""
}

func (m *Manager) PreferredNetworks() ([]string, error) {
	out, err := exec.Command("networksetup", "-listpreferredwirelessnetworks", m.iface).Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list preferred networks: %w", err)
	}

	var networks []string
	for _, line := range strings.Split(string(out), "\n") {

		if !strings.HasPrefix(line, "\t") {
			continue
		}
		if name := strings.TrimSpace(line); name != "" {
			networks = append(networks, name)
		}
	}

	return networks, nil
}

func (m *Manager) IsPoweredOn() bool {
	out, err := exec.Command("networksetup", "-getairportpower", m.iface).Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), ": on")
}

func (m *Manager) PowerOn() error {
	if m.IsPoweredOn() {
		return nil
	}

	out, err := exec.Command("networksetup", "-setairportpower", m.iface, "on").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to turn Wi-Fi on: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}

	time.Sleep(2 * time.Second)
	return nil
}

func (m *Manager) Join(ssid, password string) error {
	if ssid == "" {
		return errors.New("no SSID given")
	}

	if err := m.PowerOn(); err != nil {
		return err
	}

	args := []string{"-setairportnetwork", m.iface, ssid}
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

func (m *Manager) JoinAndWait(ssid, password, subnet string, timeout time.Duration) (string, error) {
	if err := m.Join(ssid, password); err != nil {
		return "", err
	}
	return m.WaitForIP(subnet, timeout)
}

func (m *Manager) WaitForIP(subnet string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)

	for {
		ip, err := m.GetIPv4()
		if err == nil && ip != "" && (subnet == "" || strings.HasPrefix(ip, subnet)) {
			return ip, nil
		}

		if time.Now().After(deadline) {
			if subnet != "" && ip != "" {
				return "", fmt.Errorf("joined the network but got %s, expected an address in %sx (is this the robot's Wi-Fi?)", ip, subnet)
			}
			return "", fmt.Errorf("timed out after %s waiting for an IP address", timeout)
		}

		time.Sleep(500 * time.Millisecond)
	}
}

func (m *Manager) PowerCycle() error {
	offCmd := exec.Command("networksetup", "-setairportpower", m.iface, "off")
	if output, err := offCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to turn Wi-Fi off: %w (output: %s)", err, string(output))
	}

	time.Sleep(2 * time.Second)

	onCmd := exec.Command("networksetup", "-setairportpower", m.iface, "on")
	if output, err := onCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to turn Wi-Fi on: %w (output: %s)", err, string(output))
	}

	return nil
}
