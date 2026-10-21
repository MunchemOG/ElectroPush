package bridge

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeServer answers the adb host protocol the way a real server does, one
// request per connection.
func fakeServer(t *testing.T, answer func(service string) (string, bool)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				reader := bufio.NewReader(conn)
				header := make([]byte, 4)
				if _, err := io.ReadFull(reader, header); err != nil {
					return
				}
				n, _ := strconv.ParseUint(string(header), 16, 32)
				body := make([]byte, n)
				if _, err := io.ReadFull(reader, body); err != nil {
					return
				}
				payload, ok := answer(string(body))
				status := "OKAY"
				if !ok {
					status = "FAIL"
				}
				fmt.Fprintf(conn, "%s%04x%s", status, len(payload), payload)
			}(conn)
		}
	}()

	return ln.Addr().String()
}

func TestTheProtocolReadsWhatAServerSays(t *testing.T) {
	addr := fakeServer(t, func(service string) (string, bool) {
		switch {
		case service == "host:version":
			return "0029", true
		case service == "host:devices":
			return "192.168.43.1:5555\tdevice\nABC123\tunauthorized\n", true
		case service == "host:connect:192.168.43.1:5555":
			return "already connected to 192.168.43.1:5555", true
		case strings.HasPrefix(service, "host:connect:"):
			return "failed to connect to " + strings.TrimPrefix(service, "host:connect:"), true
		}
		return "unknown host service", false
	})

	version, err := ServerVersion(addr)
	if err != nil || version != 41 {
		t.Errorf("version = %d, %v; want 41", version, err)
	}

	devices, err := ServerDevices(addr)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ serial, want string }{
		{"192.168.43.1:5555", "device"},
		{"ABC123", "unauthorized"},
		{"10.0.0.5:5555", ""},
	} {
		if got := StateOf(devices, tc.serial); got != tc.want {
			t.Errorf("state of %s = %q, want %q", tc.serial, got, tc.want)
		}
	}

	if _, err := ServerConnect(addr, "192.168.43.1:5555"); err != nil {
		t.Errorf("already connected counts as connected: %v", err)
	}
	if _, err := ServerConnect(addr, "10.0.0.9:5555"); err == nil {
		t.Error("a failed connect has to be an error, or the watchdog reports success")
	}

	if _, err := request(addr, "host:nonsense", true); err == nil || !strings.Contains(err.Error(), "unknown host service") {
		t.Errorf("a FAIL has to carry the server's reason, got %v", err)
	}
}

func TestSomethingThatIsNotAnAdbServerIsSaidToBeSo(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			conn.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
			conn.Close()
		}
	}()

	if _, err := ServerVersion(ln.Addr().String()); err == nil || !strings.Contains(err.Error(), "not an adb server") {
		t.Errorf("got %v", err)
	}
}

func TestLocalADBVersion(t *testing.T) {
	for _, tc := range []struct {
		out  string
		want int
		ok   bool
	}{
		{"Android Debug Bridge version 1.0.41\nVersion 35.0.2-12147458\n", 41, true},
		{"Android Debug Bridge version 1.0.39\n", 39, true},
		{"command not found", 0, false},
	} {
		got, ok := LocalADBVersion(tc.out)
		if got != tc.want || ok != tc.ok {
			t.Errorf("LocalADBVersion(%q) = %d, %v; want %d, %v", tc.out, got, ok, tc.want, tc.ok)
		}
	}
}

// Every forward binds this laptop's loopback, or anyone on its network could
// reach the robot through it.
func TestTheTunnelOnlyListensOnThisLaptop(t *testing.T) {
	link := Link{Host: "100.64.0.7", User: "pi", Port: 5037, Key: "/k", Robot: "192.168.43.1:5555"}
	args := link.SSHArgs(map[int]bool{8000: true})

	var forwards []string
	for i, arg := range args {
		if arg == "-L" {
			forwards = append(forwards, args[i+1])
		}
	}

	if len(forwards) == 0 || forwards[0] != "127.0.0.1:5037:localhost:5037" {
		t.Errorf("the first forward has to be the adb server, got %v", forwards)
	}
	for _, f := range forwards {
		if !strings.HasPrefix(f, "127.0.0.1:") {
			t.Errorf("forward %q is not bound to loopback", f)
		}
		if strings.HasPrefix(f, "127.0.0.1:8000:") {
			t.Error("a port something else holds must be left out, not fail the whole tunnel")
		}
	}
	if !contains(forwards, "127.0.0.1:8091:192.168.43.1:8080") {
		t.Errorf("the Robot Controller page goes to 8091, as in Relay: %v", forwards)
	}
	if !contains(forwards, "127.0.0.1:10158:192.168.43.1:10158") {
		t.Errorf("AutoTune is not forwarded: %v", forwards)
	}
	if args[len(args)-1] != "pi@100.64.0.7" {
		t.Errorf("target = %q", args[len(args)-1])
	}
	if !contains(args, "ExitOnForwardFailure=yes") || !contains(args, "BatchMode=yes") {
		t.Error("the tunnel must fail loudly rather than sit half open or wait for a password")
	}
}

func TestTheRobotsPortsMapToThisLaptopsOwn(t *testing.T) {
	for _, tc := range []struct {
		remote, local int
		ok            bool
	}{
		{8080, 8091, true},
		{8000, 8000, true},
		{8002, 8002, true},
		{10158, 10158, true},
		{5805, 5805, true},
		{9999, 0, false},
	} {
		local, ok := Local(tc.remote)
		if local != tc.local || ok != tc.ok {
			t.Errorf("Local(%d) = %d, %v; want %d, %v", tc.remote, local, ok, tc.local, tc.ok)
		}
	}
}

func TestNmcliTerseOutput(t *testing.T) {
	out := "wlan0:wifi:connected:Home\\:5G\neth0:ethernet:connected:Wired\nwlan1:wifi:disconnected:\np2p-dev-wlan0:wifi-p2p:disconnected:\n"
	devices := parseWifiDevices(out)
	if len(devices) != 2 {
		t.Fatalf("got %+v", devices)
	}
	if devices[0].Connection != "Home:5G" {
		t.Errorf("an escaped colon has to stay part of the name, got %q", devices[0].Connection)
	}
	if devices[1].Name != "wlan1" || devices[1].State != "disconnected" {
		t.Errorf("got %+v", devices[1])
	}
}

func TestTheDefaultRoute(t *testing.T) {
	out := "default via 192.168.1.1 dev wlan0 proto dhcp src 192.168.1.40 metric 600\n"
	if got := parseDefaultRoute(out); got != "wlan0" {
		t.Errorf("got %q", got)
	}
	if got := parseDefaultRoute(""); got != "" {
		t.Errorf("no route should be no interface, got %q", got)
	}
}

func TestPickingTheAdapterForTheRobot(t *testing.T) {
	for _, tc := range []struct {
		name    string
		devices []WifiDevice
		uplink  string
		want    string
		wantErr string
	}{
		{"a Pi on Ethernet with its own radio", []WifiDevice{{Name: "wlan0"}}, "eth0", "wlan0", ""},
		{"a Pi on Wi-Fi with a USB adapter", []WifiDevice{{Name: "wlan0"}, {Name: "wlan1"}}, "wlan0", "wlan1", ""},
		{"one radio that is also the uplink", []WifiDevice{{Name: "wlan0"}}, "wlan0", "", "only Wi-Fi adapter"},
		{"two spare radios", []WifiDevice{{Name: "wlan0"}, {Name: "wlan1"}}, "eth0", "", "--interface"},
		{"no radio at all", nil, "eth0", "", "no Wi-Fi adapter"},
		{"a radio that is switched off", []WifiDevice{{Name: "wlan0", State: "unavailable"}, {Name: "wlan1"}}, "eth0", "wlan1", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PickInterface(tc.devices, tc.uplink)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("err = %v, want one mentioning %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestTheServicesKeepAdbOffTheNetwork(t *testing.T) {
	units := Units(Server{User: "pi", Interface: "wlan1", Port: 5037}, "/usr/bin/adb", "/usr/local/bin/epsh")

	adb := units[unitADB]
	if !strings.Contains(adb, "ExecStart=/usr/bin/adb -P 5037 nodaemon server") {
		t.Errorf("adb unit:\n%s", adb)
	}
	if strings.Contains(adb, " -a ") {
		t.Error("the adb server must not listen on every interface")
	}
	if !strings.Contains(adb, "User=pi") {
		t.Error("the adb server should run as the ordinary user")
	}
	if !strings.Contains(units[unitWatch], "ExecStart=/usr/local/bin/epsh bridge run") {
		t.Errorf("watch unit:\n%s", units[unitWatch])
	}
	if !strings.Contains(units[unitUpdate], "ExecStart=/usr/local/bin/epsh update") ||
		!strings.Contains(units[unitUpdate], "try-restart "+unitWatch) {
		t.Errorf("an update has to restart the watchdog onto the new binary:\n%s", units[unitUpdate])
	}
	if !strings.Contains(units[timerUpdate], "OnCalendar=") {
		t.Error("the update timer has no schedule")
	}
}

func TestTheBridgeKeepsItsPasswordToItself(t *testing.T) {
	restore := ServerConfigPath
	ServerConfigPath = filepath.Join(t.TempDir(), "epsh", "bridge.json")
	defer func() { ServerConfigPath = restore }()

	want := Server{User: "pi", Interface: "wlan1", SSID: "14270-RC", Password: "secret", Port: 5037, Host: "100.64.0.7"}
	if err := want.save(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(ServerConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Errorf("mode = %v: the Wi-Fi password would be readable by every user", info.Mode().Perm())
	}

	got, err := LoadServer()
	if err != nil {
		t.Fatal(err)
	}
	want.Robot = "192.168.43.1:5555"
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// fakeOps is a bridge the watchdog can be pointed at.
type fakeOps struct {
	joined, visible, managed bool
	state                    string
	serverDown, refuse       bool
	ups, connects            int
}

func (f *fakeOps) Joined() bool  { return f.joined }
func (f *fakeOps) Visible() bool { return f.visible }
func (f *fakeOps) Managed() bool { return f.managed }
func (f *fakeOps) Up() error     { f.ups++; f.joined = true; return nil }
func (f *fakeOps) State() (string, error) {
	if f.serverDown {
		return "", fmt.Errorf("connection refused")
	}
	return f.state, nil
}
func (f *fakeOps) Connect() (string, error) {
	f.connects++
	if f.refuse {
		return "failed to connect to 192.168.43.1:5555", fmt.Errorf("failed to connect to 192.168.43.1:5555")
	}
	return "connected to 192.168.43.1:5555", nil
}

func newWatchdog(f *fakeOps) (*watchdog, *[]string) {
	var lines []string
	w := &watchdog{ops: f, ssid: "14270-RC", log: func(format string, args ...any) {
		lines = append(lines, fmt.Sprintf(format, args...))
	}}
	return w, &lines
}

func TestTheWatchdog(t *testing.T) {
	for _, tc := range []struct {
		name         string
		ops          fakeOps
		ups          int
		connects     int
		mentions     string
		steps        int
		stepDuration time.Duration
	}{
		{name: "hub switched off", ops: fakeOps{managed: true}, mentions: "Is the Control Hub on?"},
		{name: "hub came into range", ops: fakeOps{visible: true, state: ""}, ups: 1, connects: 1, mentions: "Joined"},
		{name: "joined but adb dropped", ops: fakeOps{joined: true, state: ""}, connects: 1},
		{name: "everything is fine", ops: fakeOps{joined: true, state: "device"}, mentions: "Robot attached"},
		{name: "hub needs the prompt accepted", ops: fakeOps{joined: true, state: "unauthorized"}, connects: 1, mentions: "debugging prompt"},
		{name: "adb server restarting", ops: fakeOps{joined: true, serverDown: true}, mentions: "systemd restarts it"},
		{
			name: "adapter NetworkManager does not manage", ops: fakeOps{managed: false},
			steps: 30, stepDuration: watchInterval, mentions: "not managed by NetworkManager",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ops := tc.ops
			w, lines := newWatchdog(&ops)
			steps := tc.steps
			if steps == 0 {
				steps = 1
			}
			for i := 0; i < steps; i++ {
				w.step(tc.stepDuration)
			}
			if ops.ups != tc.ups {
				t.Errorf("joined %d times, want %d", ops.ups, tc.ups)
			}
			if ops.connects != tc.connects {
				t.Errorf("connected %d times, want %d", ops.connects, tc.connects)
			}
			if tc.mentions != "" && !strings.Contains(strings.Join(*lines, "\n"), tc.mentions) {
				t.Errorf("log does not say %q:\n%s", tc.mentions, strings.Join(*lines, "\n"))
			}
		})
	}
}

// A log that repeats the same line every five seconds buries the one line that
// says what changed.
func TestTheWatchdogOnlySaysWhatChanged(t *testing.T) {
	ops := fakeOps{joined: true, state: "device"}
	w, lines := newWatchdog(&ops)
	for i := 0; i < 100; i++ {
		w.step(watchInterval)
	}
	if len(*lines) != 1 {
		t.Errorf("a healthy bridge logged %d lines in 8 minutes:\n%s", len(*lines), strings.Join(*lines, "\n"))
	}

	ops.serverDown = true
	for i := 0; i < 10; i++ {
		w.step(watchInterval)
	}
	if len(*lines) != 2 {
		t.Errorf("a server that stays down should be reported once, got:\n%s", strings.Join(*lines, "\n"))
	}

	// A hub that is booting refuses adb for a while.
	ops.serverDown, ops.state, ops.refuse = false, "", true
	for i := 0; i < 10; i++ {
		w.step(watchInterval)
	}
	if len(*lines) != 3 {
		t.Errorf("a hub that keeps refusing should be reported once, got:\n%s", strings.Join(*lines, "\n"))
	}
	if ops.connects != 10 {
		t.Errorf("tried %d times, but has to keep trying every step", ops.connects)
	}
}
