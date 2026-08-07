//go:build !darwin && !linux && !windows

package wifi

import "errors"

const tracksRecency = false

// LocationHint explains the permission this platform needs to read the network name.
const LocationHint = "Wi-Fi management is only supported on macOS, Linux and Windows."

var errUnsupported = errors.New("Wi-Fi management is not supported on this platform")

func (m *Manager) detectInterface() string { return "" }

// CurrentSSID is the network the machine is on.
func (m *Manager) CurrentSSID() (string, error) { return "", nil }

// PreferredNetworks lists the saved networks, most recent first.
func (m *Manager) PreferredNetworks() ([]string, error) { return nil, nil }

// IsPoweredOn reports whether the Wi-Fi radio is on.
func (m *Manager) IsPoweredOn() bool { return false }

// PowerOn turns the Wi-Fi radio on.
func (m *Manager) PowerOn() error { return errUnsupported }

// Join connects to a network.
func (m *Manager) Join(ssid, password string) error { return errUnsupported }

// PowerCycle turns the radio off and on again.
func (m *Manager) PowerCycle() error { return errUnsupported }

func (m *Manager) rejoin(ssid string, _ []string) error {
	return m.Join(ssid, "")
}
