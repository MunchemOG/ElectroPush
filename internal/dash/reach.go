package dash

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/andreibanu/pusher/internal/adb"
)

// The dashboard listens on the robot's own network interface, so over Wi-Fi it
// is simply reachable. Over USB it is not, and adb forwards a local port to it
// instead. That is the difference between this working when plugged in and only
// working when on the robot's network.

// forwardPort is the local end of a USB forward. High enough to be free, and
// fixed so a leaked forward is reused rather than accumulating.
const forwardPort = 28000

// Reach is an open route to the dashboard.
type Reach struct {
	// Addr is host:port to connect to.
	Addr string

	forwarded bool
	serial    string
}

// Close takes down a forward, if one was set up.
func (r Reach) Close() {
	if r.forwarded {
		_ = exec.Command("adb", "-s", r.serial, "forward", "--remove",
			"tcp:"+strconv.Itoa(forwardPort)).Run()
	}
}

// Open works out how to reach the dashboard on the connected robot.
func Open(serial string) (Reach, error) {
	if serial == "" {
		return Reach{}, fmt.Errorf("no robot connected")
	}

	// A Wi-Fi serial is already an address, and the dashboard is on the same
	// host at its own port.
	if host, _, found := strings.Cut(serial, ":"); found && host != "" {
		return Reach{Addr: fmt.Sprintf("%s:%d", host, Port)}, nil
	}

	local := "tcp:" + strconv.Itoa(forwardPort)
	out, err := exec.Command("adb", "-s", serial, "forward", local,
		"tcp:"+strconv.Itoa(Port)).CombinedOutput()
	if err != nil {
		return Reach{}, fmt.Errorf("cannot forward a port to the dashboard: %s",
			strings.TrimSpace(string(out)))
	}

	return Reach{
		Addr:      fmt.Sprintf("127.0.0.1:%d", forwardPort),
		forwarded: true,
		serial:    serial,
	}, nil
}

// Read opens a route to the connected robot and fetches what the dashboard
// holds.
func Read(serial string) (Values, error) {
	route, err := Open(serial)
	if err != nil {
		return nil, err
	}
	defer route.Close()

	values, err := Fetch(route.Addr)
	if err != nil {
		return nil, fmt.Errorf("%w\n    Is FtcDashboard running? It needs the robot app started", err)
	}
	return values, nil
}

// Robot is the connected robot, or an error saying there is not one.
func Robot() (string, error) { return adb.Target() }
