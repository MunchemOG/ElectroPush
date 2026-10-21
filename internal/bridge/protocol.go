package bridge

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// The adb server speaks a small text protocol on its port: a four-digit hex
// length, the request, then OKAY or FAIL and, for most requests, a hex-length
// payload. Speaking it directly rather than running the adb binary matters for
// two reasons here.
//
// The binary starts a server of its own when it cannot reach one, so a
// watchdog that ran `adb connect` while the shared server was restarting would
// leave a second server behind, owned by root, holding the port. And on a
// laptop the question "is the bridge answering" has to be asked without the
// local adb deciding that a version mismatch means it should kill the server it
// finds, which is the bridge's.

// dialTimeout bounds every request, so a tunnel that is half up reads as down
// rather than as a hang.
const dialTimeout = 3 * time.Second

// request sends one host request and returns its payload.
func request(addr, service string, payload bool) (string, error) {
	conn, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	if _, err := fmt.Fprintf(conn, "%04x%s", len(service), service); err != nil {
		return "", err
	}

	reader := bufio.NewReader(conn)
	status := make([]byte, 4)
	if _, err := io.ReadFull(reader, status); err != nil {
		return "", fmt.Errorf("no answer from the adb server: %w", err)
	}

	switch string(status) {
	case "OKAY":
		if !payload {
			return "", nil
		}
		return readBlock(reader)
	case "FAIL":
		message, err := readBlock(reader)
		if err != nil {
			return "", fmt.Errorf("the adb server refused %s", service)
		}
		return "", fmt.Errorf("%s", message)
	default:
		return "", fmt.Errorf("%q is not an adb server", addr)
	}
}

func readBlock(reader *bufio.Reader) (string, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil {
		return "", err
	}
	length, err := strconv.ParseUint(string(header), 16, 32)
	if err != nil {
		return "", fmt.Errorf("bad length %q", header)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(reader, body); err != nil {
		return "", err
	}
	return string(body), nil
}

// ServerVersion is the adb server's protocol version, the number that has to
// match between every client and the server it talks to.
func ServerVersion(addr string) (int, error) {
	out, err := request(addr, "host:version", true)
	if err != nil {
		return 0, err
	}
	version, err := strconv.ParseInt(strings.TrimSpace(out), 16, 32)
	if err != nil {
		return 0, fmt.Errorf("bad version %q", out)
	}
	return int(version), nil
}

// Device is one line of the server's device list.
type Device struct {
	Serial string
	State  string
}

// ServerDevices is what the adb server is holding.
func ServerDevices(addr string) ([]Device, error) {
	out, err := request(addr, "host:devices", true)
	if err != nil {
		return nil, err
	}
	return parseDeviceList(out), nil
}

func parseDeviceList(out string) []Device {
	var devices []Device
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			devices = append(devices, Device{Serial: fields[0], State: fields[1]})
		}
	}
	return devices
}

// StateOf is the state the server reports for one device, empty when it does
// not hold it at all.
func StateOf(devices []Device, serial string) string {
	for _, d := range devices {
		if d.Serial == serial {
			return d.State
		}
	}
	return ""
}

// ServerConnect asks the server to open a network connection to a device, the
// way `adb connect` does, and returns what the server said.
func ServerConnect(addr, device string) (string, error) {
	out, err := request(addr, "host:connect:"+device, true)
	if err != nil {
		return "", err
	}
	out = strings.TrimSpace(out)
	if !strings.Contains(out, "connected to") {
		return out, fmt.Errorf("%s", out)
	}
	return out, nil
}

// LocalADBVersion is the protocol version of the adb binary on this machine,
// read from `adb version`'s "Android Debug Bridge version 1.0.41".
func LocalADBVersion(output string) (int, bool) {
	for _, line := range strings.Split(output, "\n") {
		const marker = "Android Debug Bridge version "
		if i := strings.Index(line, marker); i >= 0 {
			parts := strings.Split(strings.TrimSpace(line[i+len(marker):]), ".")
			if len(parts) == 3 {
				if n, err := strconv.Atoi(parts[2]); err == nil {
					return n, true
				}
			}
		}
	}
	return 0, false
}
