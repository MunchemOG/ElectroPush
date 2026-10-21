package bridge

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// Check is one line of a status report.
type Check struct {
	OK     bool
	Label  string
	Detail string
}

// The watchdog's job is to make the robot reachable without anybody being
// there: the hub gets switched off and on all day, and the bridge has to rejoin
// its Wi-Fi and reattach adb each time, on its own.
//
// It also has to say so when it cannot. A loop that keeps trying in silence
// looks exactly like one that is working until somebody needs the robot, so
// every change of state is logged, and so is a long wait, with the most likely
// reason.

// watchInterval is how often the watchdog looks.
const watchInterval = 5 * time.Second

// scanPatience is how long the hub's network can be missing before the log
// says what usually causes that.
const scanPatience = 2 * time.Minute

// heartbeatEvery is how often a healthy bridge says it is still healthy, so
// "working" and "stuck since this morning" can be told apart in the log.
const heartbeatEvery = time.Hour

// ops is everything the watchdog asks of the machine, so the decisions can be
// tested without one.
type ops interface {
	// Joined is whether the robot's Wi-Fi is up on the bridge's adapter.
	Joined() bool
	// Visible is whether the hub's network is in range at all.
	Visible() bool
	// Managed is whether NetworkManager controls the adapter.
	Managed() bool
	Up() error
	// State is the hub's adb state, empty when the server does not hold it,
	// and an error when the server itself is not answering.
	State() (string, error)
	Connect() (string, error)
}

type watchdog struct {
	ops  ops
	log  func(format string, args ...any)
	ssid string

	missing   time.Duration
	lastHint  time.Duration
	lastState string
	lastFail  string
	healthy   time.Duration
}

// step looks once and does what is needed.
func (w *watchdog) step(elapsed time.Duration) {
	if !w.ops.Joined() {
		w.healthy = 0
		if !w.ops.Visible() {
			w.missing += elapsed
			if w.missing-w.lastHint >= 30*time.Second || w.lastHint == 0 {
				w.lastHint = w.missing
				w.log("Looking for %s (%s so far). Is the Control Hub on?", w.ssid, w.missing.Round(time.Second))
				if w.missing >= scanPatience {
					if !w.ops.Managed() {
						w.log("The adapter is not managed by NetworkManager, so it can never see the hub. " +
							"On a netplan system, give it to NetworkManager (renderer: NetworkManager) and run setup again.")
					} else {
						w.log("The adapter is working and the hub's network is not there: check the hub is on, " +
							"and that the adapter can do the hub's band (5 GHz needs a 5 GHz radio).")
					}
				}
			}
			return
		}

		w.missing, w.lastHint = 0, 0
		w.log("Joining %s", w.ssid)
		if err := w.ops.Up(); err != nil {
			w.log("Could not join %s: %v. A wrong password and a 2.4 GHz-only adapter are the usual reasons.", w.ssid, err)
			return
		}
		w.log("Joined %s", w.ssid)
	}

	state, err := w.ops.State()
	if err != nil {
		if w.lastState != "server-down" {
			w.log("The adb server is not answering (%v); systemd restarts it.", err)
			w.lastState = "server-down"
		}
		return
	}

	if state == "device" {
		if w.lastState != "device" {
			w.log("Robot attached. Laptops can deploy.")
			w.lastState = "device"
			w.healthy = 0
		}
		w.healthy += elapsed
		if w.healthy >= heartbeatEvery {
			w.healthy = 0
			w.log("Still attached.")
		}
		return
	}

	w.healthy = 0
	if state != "" && state != w.lastState {
		w.log("The robot is %s%s", state, stateHint(state))
	}
	w.lastState = state

	message, err := w.ops.Connect()
	if err != nil {
		// Said once per reason, not every five seconds for as long as a hub
		// that is booting takes to start answering.
		if err.Error() != w.lastFail {
			w.log("adb could not reach the robot: %v", err)
			w.lastFail = err.Error()
		}
		return
	}
	w.lastFail = ""
	if !strings.Contains(message, "already") {
		w.log("%s", message)
	}
}

// stateHint says what a non-ready adb state usually means on a hub.
func stateHint(state string) string {
	switch state {
	case "unauthorized":
		return ": accept the debugging prompt on whatever screen the hub shows it"
	case "offline":
		return ": the link is up but the hub is not answering yet"
	}
	return ""
}

// Watch runs the watchdog until ctx ends.
func Watch(ctx context.Context, out io.Writer) error {
	s, err := LoadServer()
	if err != nil {
		return fmt.Errorf("this machine is not set up as a bridge: %w", err)
	}

	w := &watchdog{
		ops:  realOps{s},
		ssid: s.SSID,
		log: func(format string, args ...any) {
			fmt.Fprintf(out, format+"\n", args...)
		},
	}
	w.log("Keeping %s on %s and adb attached to %s", s.Interface, s.SSID, s.Robot)

	ticker := time.NewTicker(watchInterval)
	defer ticker.Stop()
	w.step(0)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			w.step(watchInterval)
		}
	}
}

// realOps is the watchdog's view of an actual bridge.
type realOps struct{ s Server }

func (o realOps) Joined() bool {
	out, err := exec.Command("nmcli", "-t", "-f", "NAME,DEVICE", "connection", "show", "--active").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if f := splitTerse(line); len(f) >= 2 && f[0] == Connection && f[1] == o.s.Interface {
			return true
		}
	}
	return false
}

func (o realOps) Visible() bool {
	out, err := exec.Command("nmcli", "-t", "-f", "SSID", "device", "wifi", "list", "ifname", o.s.Interface).Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if f := splitTerse(line); len(f) > 0 && f[0] == o.s.SSID {
			return true
		}
	}
	return false
}

func (o realOps) Managed() bool {
	out, err := exec.Command("nmcli", "-t", "-f", "DEVICE,STATE", "device").Output()
	if err != nil {
		return true
	}
	for _, line := range strings.Split(string(out), "\n") {
		if f := splitTerse(line); len(f) >= 2 && f[0] == o.s.Interface {
			return f[1] != "unmanaged"
		}
	}
	return true
}

func (o realOps) Up() error {
	out, err := exec.Command("nmcli", "connection", "up", Connection, "ifname", o.s.Interface).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

func (o realOps) State() (string, error) {
	devices, err := ServerDevices(o.s.adbAddr())
	if err != nil {
		return "", err
	}
	return StateOf(devices, o.s.Robot), nil
}

func (o realOps) Connect() (string, error) {
	return ServerConnect(o.s.adbAddr(), o.s.Robot)
}
