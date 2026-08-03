//go:build !darwin

package wifi

import (
	"errors"
	"time"
)

const RobotSubnet = "192.168.43."

var ErrSSIDUnavailable = errors.New("macOS will not report the current Wi-Fi network")

const LocationHint = "Wi-Fi management is only supported on macOS."

var errUnsupported = errors.New("Wi-Fi management is only supported on macOS")

type Manager struct {
	iface string
}

func NewManager() *Manager {
	return &Manager{iface: ""}
}

func (m *Manager) GetIPv4() (string, error) {
	return "", nil
}

func (m *Manager) IsOnRobotNetwork() (bool, error) {
	return false, nil
}

func (m *Manager) CurrentSSID() (string, error) {
	return "", nil
}

func (m *Manager) PreferredNetworks() ([]string, error) {
	return nil, nil
}

func (m *Manager) MostRecentNetwork(exclude ...string) (string, error) {
	return "", nil
}

func (m *Manager) IsPoweredOn() bool {
	return false
}

func (m *Manager) PowerOn() error {
	return errUnsupported
}

func (m *Manager) Join(ssid, password string) error {
	return errUnsupported
}

func (m *Manager) JoinAndWait(ssid, password, subnet string, timeout time.Duration) (string, error) {
	return "", errUnsupported
}

func (m *Manager) WaitForIP(subnet string, timeout time.Duration) (string, error) {
	return "", errUnsupported
}

func (m *Manager) PowerCycle() error {
	return errUnsupported
}
