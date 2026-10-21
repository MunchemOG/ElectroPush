// Package bridge shares one robot between every laptop on a team, through a
// small Linux computer that sits on the robot's Wi-Fi.
//
// The idea is Relay's (github.com/Bob-41/Relay): the bridge joins the Control
// Hub's network, keeps one adb server attached to it, and each laptop reaches
// that server through an SSH tunnel. Android Studio on every laptop then sees
// the hub as an ordinary device, and so does epsh, without anybody leaving the
// network they are on. With Tailscale, "the network they are on" can be a
// different building.
//
// It is the other answer to the same question `epsh relay` answers. That one
// puts a second radio on the hub so it joins your network; this one puts a
// second computer on the hub's network. A bridge needs no driver built for the
// hub's kernel, so it works with any Control Hub OS and any Wi-Fi adapter, and
// it is what lets several laptops share the robot at once.
//
// The contract with laptops is deliberately Relay's own: the bridge's adb
// server listens on its loopback only, and a laptop forwards its local adb port
// to it over SSH. So an epsh laptop can join a Relay bridge, and a Relay laptop
// can join an epsh bridge.
package bridge

import (
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/MunchemOG/ElectroPush/internal/adb"
	"github.com/MunchemOG/ElectroPush/internal/config"
)

// DefaultADBPort is the adb server port, on both ends.
const DefaultADBPort = 5037

// LocalADB is where this laptop's adb clients look for a server, and where the
// tunnel puts the bridge's.
//
// The standard port, because that is the whole point: Android Studio, a bare
// `adb` and epsh all find the bridge without being told about it. The cost is
// that a hub plugged into this laptop over USB is invisible while the tunnel is
// up, because the server answering is the bridge's.
var LocalADB = fmt.Sprintf("127.0.0.1:%d", DefaultADBPort)

// Forward is one of the robot's ports carried to this laptop.
type Forward struct {
	Name string
	// Local is the port on this laptop.
	Local int
	// Remote is the port on the robot.
	Remote int
	// Page is whether a browser opens it.
	Page bool
	Note string
}

// URL is where a browser opens it on this laptop.
func (f Forward) URL() string {
	if !f.Page {
		return ""
	}
	return fmt.Sprintf("http://localhost:%d", f.Local)
}

// Forwards is every robot port the tunnel carries.
//
// The local numbers are the robot's own wherever that is possible, because the
// pages work them out from the address they were loaded from: Panels at
// localhost:8001 opens its socket at localhost:8002, and AutoTune at
// localhost:10158 opens its own at 12649. The one exception is the Robot
// Controller's 8080, which is busy on enough laptops that Relay moved it to
// 8091, and keeping Relay's number keeps its instructions true.
var Forwards = []Forward{
	{Name: "Robot Controller", Local: 8091, Remote: 8080, Page: true, Note: "the hub's manage page, FtcDashboard at /dash"},
	{Name: "FtcDashboard", Local: 8000, Remote: 8000, Note: "what `epsh dash diff` reads"},
	{Name: "Panels", Local: 8001, Remote: 8001, Page: true, Note: "tuning and telemetry"},
	{Name: "Panels socket", Local: 8002, Remote: 8002, Note: "what Panels and `epsh dash diff` read"},
	{Name: "Pedro AutoTune", Local: 10158, Remote: 10158, Page: true, Note: "Pedro's tuners, and ElectroDromos's"},
	{Name: "AutoTune socket", Local: 12649, Remote: 12649, Note: "what the AutoTune page reads"},
	{Name: "Limelight", Local: 5801, Remote: 5801, Page: true, Note: "through the Panels proxy"},
	{Name: "Limelight stream", Local: 5800, Remote: 5800, Note: "the camera feed the Limelight page shows"},
	{Name: "Limelight socket", Local: 5805, Remote: 5805, Note: "what the Limelight page reads"},
}

// Local is the port on this laptop that reaches a port on the robot, when the
// tunnel carries it.
func Local(remote int) (int, bool) {
	for _, f := range Forwards {
		if f.Remote == remote {
			return f.Local, true
		}
	}
	return 0, false
}

// Link is a laptop's connection to a bridge, as remembered in the config.
type Link struct {
	Host string
	User string
	// Port is the adb server port on the bridge.
	Port int
	// Key is the private key the tunnel signs in with.
	Key string
	// Robot is the hub's adb address as the bridge sees it.
	Robot string
}

// Joined is the bridge this laptop uses, if it uses one.
func Joined() (Link, bool) {
	host, user, port, key := config.GetBridge()
	if host == "" {
		return Link{}, false
	}
	if port == 0 {
		port = DefaultADBPort
	}
	return Link{Host: host, User: user, Port: port, Key: key, Robot: adb.RobotAddr()}, true
}

// Target is "user@host", for ssh.
func (l Link) Target() string {
	if l.User == "" {
		return l.Host
	}
	return l.User + "@" + l.Host
}

// SSHArgs is the tunnel's command line, without the ssh itself.
//
// Every forward binds to this laptop's loopback, so nothing on the network the
// laptop is on can reach the robot through it. skip leaves out local ports that
// something else already holds: ExitOnForwardFailure is all or nothing, and one
// developer's server on 8000 should cost them the dashboard, not the robot.
func (l Link) SSHArgs(skip map[int]bool) []string {
	args := []string{
		"-N",
		"-o", "BatchMode=yes",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "StrictHostKeyChecking=accept-new",
	}
	if l.Key != "" {
		args = append(args, "-i", l.Key, "-o", "IdentitiesOnly=yes")
	}

	args = append(args, "-L", fmt.Sprintf("%s:localhost:%d", LocalADB, l.Port))

	robotHost, _, err := net.SplitHostPort(l.Robot)
	if err != nil || robotHost == "" {
		robotHost = adb.RobotIP
	}
	for _, f := range Forwards {
		if skip[f.Local] {
			continue
		}
		args = append(args, "-L", fmt.Sprintf("127.0.0.1:%d:%s:%d", f.Local, robotHost, f.Remote))
	}

	return append(args, l.Target())
}

// listening reports whether something on this laptop answers on a port.
func listening(port int) bool {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 300*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
