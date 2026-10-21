//go:build darwin && !cgo

package wifi

const scanSupported = false

// ScanNote explains what epsh does about networks it cannot see yet.
//
// Looking for a network by name means asking CoreWLAN, which means cgo. A macOS
// build made with CGO_ENABLED=0 cannot do it, so it does not pretend to.
const ScanNote = `This build cannot look for networks: it was compiled without cgo, and reading
the Wi-Fi around you on macOS needs CoreWLAN. Joining the robot will still work;
it just cannot be told in advance whether the hub is broadcasting.

Reinstall with 'brew install MunchemOG/homebrew-epsh/epsh', which builds from
source with cgo on.`

func (m *Manager) visible(ssid string) (bool, error) {
	return false, ErrScanUnsupported
}
