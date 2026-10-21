package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MunchemOG/ElectroPush/internal/adb"
)

// This half runs on the bridge itself: a Linux machine with systemd and
// NetworkManager, usually a Raspberry Pi, with one network connection of its
// own and a Wi-Fi adapter to spare for the robot's.
//
// Three services, the same three Relay has:
//
//   - an adb server, run by an ordinary user and bound to the bridge's
//     loopback. Never -a: a server on every interface would hand the robot to
//     anyone on the shop network, and laptops reach it through SSH anyway.
//   - a watchdog that keeps the spare adapter on the robot's Wi-Fi and the
//     server attached to the hub, forever, through power cycles on either end.
//   - a nightly `epsh update`, which only ever moves to a newer release.

// ServerConfigPath is where a bridge keeps what it was set up with. Root's,
// because it holds the robot's Wi-Fi password.
var ServerConfigPath = "/etc/epsh/bridge.json"

// Connection is the NetworkManager profile for the robot's Wi-Fi.
const Connection = "epsh-robot"

// Unit names, so setup, removal and status agree.
const (
	unitADB     = "epsh-bridge-adb.service"
	unitWatch   = "epsh-bridge.service"
	unitUpdate  = "epsh-bridge-update.service"
	timerUpdate = "epsh-bridge-update.timer"
)

var unitDir = "/etc/systemd/system"

// Server is a bridge's own configuration.
type Server struct {
	// User runs the adb server, and is who laptops sign in as.
	User string `json:"user"`
	// Interface is the Wi-Fi adapter given over to the robot.
	Interface string `json:"interface"`
	SSID      string `json:"ssid"`
	Password  string `json:"password"`
	// Robot is the hub's adb address on its own network.
	Robot string `json:"robot"`
	// Port is the adb server's port on this machine's loopback.
	Port int `json:"port"`
	// Host is the address laptops are told to use: Tailscale's, or this
	// machine's on its own network.
	Host string `json:"host"`
}

func (s Server) adbAddr() string { return fmt.Sprintf("127.0.0.1:%d", s.Port) }

// LoadServer reads the bridge's configuration.
func LoadServer() (Server, error) {
	var s Server
	data, err := os.ReadFile(ServerConfigPath)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("%s is not readable: %w", ServerConfigPath, err)
	}
	if s.Port == 0 {
		s.Port = DefaultADBPort
	}
	if s.Robot == "" {
		s.Robot = adb.RobotIP + ":" + adb.RobotPort
	}
	return s, nil
}

// IsBridge reports whether this machine has been set up as one.
func IsBridge() bool {
	_, err := os.Stat(ServerConfigPath)
	return err == nil
}

func (s Server) save() error {
	if err := os.MkdirAll(filepath.Dir(ServerConfigPath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ServerConfigPath, append(data, '\n'), 0o600)
}

// CanHost says why this machine cannot be a bridge, or nil when it can.
func CanHost() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("a bridge is a Linux machine with systemd and NetworkManager, usually a Raspberry Pi; run `epsh bridge join <bridge>` on this one instead")
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("setting up a bridge installs system services: run it with sudo")
	}
	for _, tool := range []string{"systemctl", "nmcli"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("%s not found: a bridge needs systemd and NetworkManager", tool)
		}
	}
	if out, _ := exec.Command("systemctl", "is-active", "NetworkManager").Output(); strings.TrimSpace(string(out)) != "active" {
		return fmt.Errorf("NetworkManager is not running. On Ubuntu Server or Arch it usually has to be installed and enabled first:\n" +
			"    sudo systemctl enable --now NetworkManager\n" +
			"    Do that from a keyboard and screen, not over SSH: the network can drop while it changes over")
	}
	return nil
}

// SSHRunning reports whether laptops will be able to sign in.
func SSHRunning() bool {
	for _, unit := range []string{"ssh", "sshd"} {
		if out, _ := exec.Command("systemctl", "is-active", unit).Output(); strings.TrimSpace(string(out)) == "active" {
			return true
		}
	}
	return false
}

// WifiDevice is one Wi-Fi adapter as NetworkManager sees it.
type WifiDevice struct {
	Name       string
	State      string
	Connection string
}

// WifiDevices is every Wi-Fi adapter on this machine.
func WifiDevices() []WifiDevice {
	out, err := exec.Command("nmcli", "-t", "-f", "DEVICE,TYPE,STATE,CONNECTION", "device").Output()
	if err != nil {
		return nil
	}
	return parseWifiDevices(string(out))
}

func parseWifiDevices(out string) []WifiDevice {
	var devices []WifiDevice
	for _, line := range strings.Split(out, "\n") {
		fields := splitTerse(line)
		if len(fields) < 4 || fields[1] != "wifi" {
			continue
		}
		devices = append(devices, WifiDevice{Name: fields[0], State: fields[2], Connection: fields[3]})
	}
	return devices
}

// splitTerse splits one line of nmcli's terse output, where a literal colon in
// a value is written \:.
func splitTerse(line string) []string {
	var fields []string
	var current strings.Builder
	escaped := false
	for _, r := range strings.TrimRight(line, "\r") {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case r == ':':
			fields = append(fields, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	if line == "" {
		return nil
	}
	return append(fields, current.String())
}

// DefaultRouteInterface is the interface this machine reaches the rest of the
// world through. The robot's Wi-Fi must not be it.
func DefaultRouteInterface() string {
	out, err := exec.Command("ip", "route", "show", "default").Output()
	if err != nil {
		return ""
	}
	return parseDefaultRoute(string(out))
}

func parseDefaultRoute(out string) string {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		for i := 0; i+1 < len(fields); i++ {
			if fields[0] == "default" && fields[i] == "dev" {
				return fields[i+1]
			}
		}
	}
	return ""
}

// PickInterface chooses the adapter to give the robot, when there is only one
// sensible answer, and says why when there is not.
func PickInterface(devices []WifiDevice, uplink string) (string, error) {
	var spare []string
	for _, d := range devices {
		if d.Name != uplink && d.State != "unavailable" {
			spare = append(spare, d.Name)
		}
	}

	switch {
	case len(devices) == 0:
		return "", fmt.Errorf("this machine has no Wi-Fi adapter NetworkManager can use")
	case len(spare) == 0:
		return "", fmt.Errorf("the only Wi-Fi adapter, %s, is how this machine reaches your network. "+
			"The robot's Wi-Fi has no route anywhere, so it needs one of its own: plug in a USB Wi-Fi adapter, "+
			"or put this machine on Ethernet", uplink)
	case len(spare) == 1:
		return spare[0], nil
	default:
		return "", fmt.Errorf("more than one adapter is free (%s): name the one for the robot with --interface",
			strings.Join(spare, ", "))
	}
}

// TailscaleIP is this machine's Tailscale address, when it is signed in.
func TailscaleIP() string {
	out, err := exec.Command("tailscale", "ip", "-4").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
}

// InterfaceIP is an interface's IPv4 address.
func InterfaceIP(name string) string {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return ""
	}
	addrs, _ := iface.Addrs()
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return ""
}

// InstallADB installs the platform tools with whichever package manager this
// distro has.
//
// Never `pacman -Sy`: a partial upgrade is unsupported on Arch, and a stale
// mirror fails here loudly rather than half-upgrading the machine.
func InstallADB(out io.Writer) error {
	var cmd *exec.Cmd
	switch {
	case lookPath("apt-get"):
		cmd = exec.Command("apt-get", "install", "-y", "adb")
	case lookPath("pacman"):
		cmd = exec.Command("pacman", "-S", "--needed", "--noconfirm", "android-tools")
	case lookPath("dnf"):
		cmd = exec.Command("dnf", "install", "-y", "android-tools")
	default:
		return fmt.Errorf("no package manager epsh knows: install adb, then run this again")
	}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		if strings.Contains(cmd.Path, "pacman") {
			return fmt.Errorf("pacman could not install android-tools (%v). A 404 means the package "+
				"database is stale: run `sudo pacman -Syu`, then this again", err)
		}
		return err
	}
	return nil
}

func lookPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// Units is every systemd file a bridge installs, by name.
func Units(s Server, adbPath, exe string) map[string]string {
	return map[string]string{
		unitADB: fmt.Sprintf(`[Unit]
Description=epsh bridge: the shared adb server, on this machine's loopback only
After=network.target

[Service]
User=%s
# No -a. With it, adb listens on every interface and anyone on this network
# could reach the robot. Laptops come in over SSH instead.
ExecStart=%s -P %d nodaemon server
Restart=always
RestartSec=2

[Install]
WantedBy=multi-user.target
`, s.User, adbPath, s.Port),

		unitWatch: fmt.Sprintf(`[Unit]
Description=epsh bridge: keeps %s on the robot's Wi-Fi and adb attached to the hub
After=NetworkManager.service %s
Wants=%s

[Service]
ExecStart=%s bridge run
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`, s.Interface, unitADB, unitADB, exe),

		unitUpdate: fmt.Sprintf(`[Unit]
Description=epsh bridge: install a newer epsh, if there is one
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=%s update
# The watchdog is still running the old binary until it restarts.
ExecStartPost=/bin/systemctl try-restart %s
`, exe, unitWatch),

		timerUpdate: `[Unit]
Description=epsh bridge: look for a newer epsh overnight

[Timer]
OnCalendar=*-*-* 03:00:00
RandomizedDelaySec=30min
Persistent=true

[Install]
WantedBy=timers.target
`,
	}
}

// Setup makes this machine a bridge.
func Setup(out io.Writer, s Server, adbPath, exe string) error {
	if s.Port == 0 {
		s.Port = DefaultADBPort
	}
	if s.Robot == "" {
		s.Robot = adb.RobotIP + ":" + adb.RobotPort
	}

	step := func(format string, args ...any) { fmt.Fprintf(out, "[*] "+format+"\n", args...) }

	step("Saving the robot's Wi-Fi as a NetworkManager connection on %s", s.Interface)
	_ = exec.Command("nmcli", "connection", "delete", Connection).Run()
	add := []string{"connection", "add", "type", "wifi", "ifname", s.Interface,
		"con-name", Connection, "ssid", s.SSID,
		// The robot's network goes nowhere. Without never-default it can
		// become this machine's default route and take its uplink away.
		"ipv4.method", "auto", "ipv4.never-default", "yes", "ipv6.method", "ignore",
		"connection.autoconnect", "yes"}
	if s.Password != "" {
		add = append(add, "wifi-sec.key-mgmt", "wpa-psk", "wifi-sec.psk", s.Password)
	}
	if output, err := exec.Command("nmcli", add...).CombinedOutput(); err != nil {
		return fmt.Errorf("NetworkManager would not save the connection: %s", strings.TrimSpace(string(output)))
	}

	if err := s.save(); err != nil {
		return fmt.Errorf("cannot write %s: %w", ServerConfigPath, err)
	}

	step("Installing the services")
	units := Units(s, adbPath, exe)
	names := make([]string, 0, len(units))
	for name := range units {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(unitDir, name), []byte(units[name]), 0o644); err != nil {
			return fmt.Errorf("cannot write %s: %w", name, err)
		}
	}

	// Restarted rather than only started, so running setup again picks up a
	// changed port or adapter rather than leaving the old services running.
	for _, args := range [][]string{
		{"daemon-reload"},
		{"enable", unitADB, unitWatch, timerUpdate},
		{"restart", unitADB, unitWatch},
		{"start", timerUpdate},
	} {
		if output, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl %s: %s", strings.Join(args, " "), strings.TrimSpace(string(output)))
		}
	}

	step("Waiting for the adb server")
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := ServerVersion(s.adbAddr()); err == nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the adb server never came up on port %d: `journalctl -u %s` says why", s.Port, unitADB)
		}
		time.Sleep(500 * time.Millisecond)
	}

	return nil
}

// Remove undoes Setup, and leaves the machine as it was.
func Remove(out io.Writer) error {
	units := []string{unitWatch, unitADB, timerUpdate, unitUpdate}
	_ = exec.Command("systemctl", append([]string{"disable", "--now"}, units...)...).Run()
	for _, name := range units {
		_ = os.Remove(filepath.Join(unitDir, name))
	}
	_ = exec.Command("systemctl", "daemon-reload").Run()
	_ = exec.Command("nmcli", "connection", "delete", Connection).Run()

	if err := os.Remove(ServerConfigPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	fmt.Fprintln(out, "[OK] This machine is no longer a bridge.")
	return nil
}

// ServerStatus is how a bridge is doing, from the bridge itself.
func ServerStatus(s Server) []Check {
	var checks []Check

	for _, unit := range []string{unitADB, unitWatch, timerUpdate} {
		out, _ := exec.Command("systemctl", "is-active", unit).Output()
		state := strings.TrimSpace(string(out))
		checks = append(checks, Check{OK: state == "active", Label: unit, Detail: state})
	}

	ops := realOps{s}
	joined := ops.Joined()
	detail := "not joined"
	if joined {
		detail = "joined on " + s.Interface
	}
	checks = append(checks, Check{OK: joined, Label: s.SSID, Detail: detail})

	state, err := ops.State()
	switch {
	case err != nil:
		checks = append(checks, Check{Label: "robot", Detail: "the adb server is not answering on port " + strconv.Itoa(s.Port)})
	case state == "device":
		checks = append(checks, Check{OK: true, Label: "robot", Detail: s.Robot + " attached"})
	case state == "":
		checks = append(checks, Check{Label: "robot", Detail: s.Robot + " not attached"})
	default:
		checks = append(checks, Check{Label: "robot", Detail: s.Robot + " is " + state + stateHint(state)})
	}

	return checks
}
