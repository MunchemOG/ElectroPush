package wifi

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// RobotSubnet is the address range the robot hands out on its own network.
const RobotSubnet = "192.168.43."

// ErrSSIDUnavailable means the OS would not say which network we are on.
var ErrSSIDUnavailable = errors.New("the current Wi-Fi network name is unavailable")

// Manager drives the platform's Wi-Fi tooling.
type Manager struct {
	iface string
}

// NewManager returns a Wi-Fi manager for this platform.
func NewManager() *Manager {
	return &Manager{}
}

func (m *Manager) wifiInterface() string {
	if m.iface == "" {
		m.iface = m.detectInterface()
	}
	return m.iface
}

// GetIPv4 is the machine's current address.
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

// IsOnRobotNetwork reports whether we currently hold a robot address.
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

// MostRecentNetwork guesses where we came from when the name is hidden.
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

// Rejoin returns to a network, leaving the robot's behind.
func (m *Manager) Rejoin(ssid string, leaving []string) error {
	return m.rejoin(ssid, leaving)
}

// WaitToLeave blocks until the machine no longer holds an address in a subnet.
func (m *Manager) WaitToLeave(subnet string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)

	for {
		ip, err := m.GetIPv4()
		if err == nil && ip != "" && !strings.HasPrefix(ip, subnet) {
			return ip, nil
		}

		if time.Now().After(deadline) {
			if ip != "" {
				return "", fmt.Errorf("still on %s after %s", ip, timeout)
			}
			return "", fmt.Errorf("timed out after %s waiting for an IP address", timeout)
		}

		time.Sleep(500 * time.Millisecond)
	}
}

// JoinAndWait joins a network and waits for an address on it.
func (m *Manager) JoinAndWait(ssid, password, subnet string, timeout time.Duration) (string, error) {
	if err := m.Join(ssid, password); err != nil {
		return "", err
	}
	return m.WaitForIP(subnet, timeout)
}

// WaitForIP blocks until the machine has an address in a subnet.
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
