//go:build !darwin && !linux && !windows

package wifi

import "errors"

const tracksRecency = false

const LocationHint = "Wi-Fi management is only supported on macOS, Linux and Windows."

var errUnsupported = errors.New("Wi-Fi management is not supported on this platform")

func (m *Manager) detectInterface() string { return "" }

func (m *Manager) CurrentSSID() (string, error) { return "", nil }

func (m *Manager) PreferredNetworks() ([]string, error) { return nil, nil }

func (m *Manager) IsPoweredOn() bool { return false }

func (m *Manager) PowerOn() error { return errUnsupported }

func (m *Manager) Join(ssid, password string) error { return errUnsupported }

func (m *Manager) PowerCycle() error { return errUnsupported }
