package wifi

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

const RobotSubnet = "192.168.43."

// Returned when the OS refuses to name the current network. Only macOS does
// this, but every platform shares the sentinel so callers stay portable.
var ErrSSIDUnavailable = errors.New("the current Wi-Fi network name is unavailable")

type Manager struct {
	iface string
}

func NewManager() *Manager {
	return &Manager{}
}

// wifiInterface resolves the wireless interface name lazily, since discovering
// it costs a subprocess on Linux and Windows.
func (m *Manager) wifiInterface() string {
	if m.iface == "" {
		m.iface = m.detectInterface()
	}
	return m.iface
}

func (m *Manager) GetIPv4() (string, error) {
	name := m.wifiInterface()
	if name == "" {
		return "", fmt.Errorf("no Wi-Fi interface found")
	}

	iface, err := net.InterfaceByName(name)
	if err != nil {
		return "", fmt.Errorf("failed to get interface %s: %w", name, err)
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return "", fmt.Errorf("failed to get addresses for %s: %w", name, err)
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok {
			if ip4 := ipNet.IP.To4(); ip4 != nil {
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

// MostRecentNetwork reports the network most recently connected to, skipping
// any SSID in exclude. The robot's own networks must be excluded: joining the
// robot makes it the most recent, and returning it as "home" would strand the
// user on the hotspot.
//
// Returns "" where the OS keeps no usable history (Windows), rather than
// guessing.
func (m *Manager) MostRecentNetwork(exclude ...string) (string, error) {
	if !tracksRecency {
		return "", nil
	}

	networks, err := m.PreferredNetworks()
	if err != nil {
		return "", err
	}

	return firstNotIn(networks, exclude), nil
}

func (m *Manager) JoinAndWait(ssid, password, subnet string, timeout time.Duration) (string, error) {
	if err := m.Join(ssid, password); err != nil {
		return "", err
	}
	return m.WaitForIP(subnet, timeout)
}

// WaitForIP blocks until the interface holds an IPv4 address. A non-empty
// subnet additionally requires that address to carry the given prefix, which is
// how landing on the robot rather than some other remembered network is
// confirmed.
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
