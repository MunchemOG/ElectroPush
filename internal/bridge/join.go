package bridge

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/MunchemOG/ElectroPush/internal/adb"
	"github.com/MunchemOG/ElectroPush/internal/config"
)

// JoinOptions is what a laptop needs to know about a bridge: what `epsh bridge
// setup` printed at the end, or Relay's installer did.
type JoinOptions struct {
	Host string
	User string
	Port int
}

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// keyPath is this laptop's key for one bridge. One per bridge, so leaving one
// never locks the laptop out of another.
func keyPath(host string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "epsh_bridge_"+unsafeChars.ReplaceAllString(host, "_"))
}

// authorizeScript adds the key on standard input to authorized_keys, once.
const authorizeScript = `umask 077; mkdir -p ~/.ssh && touch ~/.ssh/authorized_keys && ` +
	`key="$(cat)" && { grep -qxF "$key" ~/.ssh/authorized_keys || printf '%s\n' "$key" >> ~/.ssh/authorized_keys; }`

// Join points this laptop at a bridge and keeps a tunnel to it open from now
// on, at every login.
func Join(out io.Writer, opt JoinOptions) (Link, error) {
	link := Link{Host: opt.Host, User: opt.User, Port: opt.Port, Robot: adb.RobotIP + ":" + adb.RobotPort}
	if link.Port == 0 {
		link.Port = DefaultADBPort
	}

	ssh, err := exec.LookPath("ssh")
	if err != nil {
		return link, fmt.Errorf("ssh not found: install an OpenSSH client")
	}

	link.Key = keyPath(opt.Host)
	if _, err := os.Stat(link.Key); err != nil {
		keygen, err := exec.LookPath("ssh-keygen")
		if err != nil {
			return link, fmt.Errorf("ssh-keygen not found: install an OpenSSH client")
		}
		if err := os.MkdirAll(filepath.Dir(link.Key), 0o700); err != nil {
			return link, err
		}
		hostname, _ := os.Hostname()
		if output, err := exec.Command(keygen, "-q", "-t", "ed25519", "-N", "", "-C", "epsh bridge "+hostname,
			"-f", link.Key).CombinedOutput(); err != nil {
			return link, fmt.Errorf("ssh-keygen: %s", strings.TrimSpace(string(output)))
		}
		fmt.Fprintf(out, "[OK] Made a key for this bridge: %s\n", link.Key)
	}

	signsIn := func() bool {
		return exec.Command(ssh, "-i", link.Key, "-o", "IdentitiesOnly=yes", "-o", "BatchMode=yes",
			"-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new", link.Target(), "true").Run() == nil
	}

	if !signsIn() {
		public, err := os.ReadFile(link.Key + ".pub")
		if err != nil {
			return link, err
		}

		fmt.Fprintf(out, "[*] Putting this laptop's key on the bridge. Enter %s's password on %s once;\n", orDefault(opt.User, "your"), opt.Host)
		fmt.Fprintln(out, "    after this, the tunnel signs in on its own.")

		cmd := exec.Command(ssh, "-o", "StrictHostKeyChecking=accept-new", "-o", "ConnectTimeout=15",
			link.Target(), authorizeScript)
		cmd.Stdin = bytes.NewReader(public)
		cmd.Stdout, cmd.Stderr = out, out
		if err := cmd.Run(); err != nil {
			return link, fmt.Errorf("could not sign in to %s: %w. Check the address, the user and that the bridge runs an SSH server",
				link.Target(), err)
		}
		if !signsIn() {
			return link, fmt.Errorf("the key was added but %s still refuses it: check ~/.ssh on the bridge belongs to %s",
				link.Target(), orDefault(opt.User, "that user"))
		}
	}
	fmt.Fprintf(out, "[OK] Signed in to %s with the key\n", link.Target())

	if err := config.SetBridge(link.Host, link.User, link.Port, link.Key); err != nil {
		return link, err
	}

	if _, holder := portHolder(DefaultADBPort); holder == "adb" {
		fmt.Fprintf(out, "[*] Stopping this laptop's own adb server, so the tunnel can have port %d.\n", DefaultADBPort)
		fmt.Fprintln(out, "    Android Studio and adb find the bridge's there instead. `epsh bridge leave` undoes it.")
	}

	exe := stableExecutable()
	if err := installAutostart(exe); err != nil {
		return link, fmt.Errorf("joined, but the tunnel will not start on its own: %w\n"+
			"    Run `epsh bridge tunnel` and leave it open instead", err)
	}
	fmt.Fprintf(out, "[OK] The tunnel starts at every login (%s)\n", autostartName())

	deadline := time.Now().Add(20 * time.Second)
	for !tunnelIsUp() {
		if time.Now().After(deadline) {
			return link, fmt.Errorf("the tunnel did not come up: `epsh bridge status` says why, and %s has its log", LogPath())
		}
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Fprintln(out, "[OK] The tunnel is up")

	return link, nil
}

// Leave stops using the bridge. The key stays, so joining again needs no
// password.
func Leave() error {
	if err := removeAutostart(); err != nil {
		return err
	}
	stopTunnel()
	return config.SetBridge("", "", 0, "")
}

// stableExecutable is the path to start epsh by at login.
//
// Not always os.Executable: under Homebrew that is a versioned Cellar path
// that the next upgrade deletes, which would leave a login item pointing at
// nothing. The path on PATH is the one that survives upgrades, so it wins
// whenever it is the same program.
func stableExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		exe = "epsh"
	}
	resolved, _ := filepath.EvalSymlinks(exe)

	if onPath, err := exec.LookPath("epsh"); err == nil {
		if abs, err := filepath.Abs(onPath); err == nil {
			if target, err := filepath.EvalSymlinks(abs); err == nil && target == resolved {
				return abs
			}
		}
	}
	return exe
}

// Diagnose says how the laptop's link to its bridge is doing, step by step, so
// the first thing that is wrong is the first thing reported.
func Diagnose(link Link) []Check {
	var checks []Check
	add := func(ok bool, label, detail string) {
		checks = append(checks, Check{OK: ok, Label: label, Detail: detail})
	}

	add(autostartInstalled(), "autostart", autostartName())

	pid, holder := portHolder(DefaultADBPort)
	switch {
	case holder == "ssh":
		add(true, "tunnel", fmt.Sprintf("open to %s (ssh, pid %d)", link.Target(), pid))
	case holder == "adb":
		add(false, "tunnel", "this laptop's own adb server has the port; the tunnel takes it back within a minute")
		return checks
	case holder == "":
		add(false, "tunnel", "not running: "+LogPath()+" says why")
		return checks
	default:
		add(false, "tunnel", fmt.Sprintf("port %d is held by %s (pid %d), so the tunnel cannot open", DefaultADBPort, holder, pid))
		return checks
	}

	server, err := ServerVersion(LocalADB)
	if err != nil {
		add(false, "bridge", "the tunnel is open but nothing answers behind it: is the bridge's adb server running?")
		return checks
	}
	add(true, "bridge", fmt.Sprintf("adb server answering (protocol %d)", server))

	if out, err := exec.Command("adb", "version").Output(); err == nil {
		if local, ok := LocalADBVersion(string(out)); ok {
			if local == server {
				add(true, "adb versions", fmt.Sprintf("match (%d)", local))
			} else {
				add(false, "adb versions", versionMismatch(local, server))
			}
		}
	}

	devices, err := ServerDevices(LocalADB)
	if err != nil {
		add(false, "robot", err.Error())
		return checks
	}
	switch state := StateOf(devices, link.Robot); state {
	case "device":
		add(true, "robot", link.Robot+" attached")
	case "":
		add(false, "robot", "the bridge is not attached to "+link.Robot+": is the hub on, and the bridge in range of it?")
	default:
		add(false, "robot", link.Robot+" is "+state+stateHint(state))
	}

	var open, closed []string
	for _, f := range Forwards {
		if listening(f.Local) {
			open = append(open, fmt.Sprintf("%d", f.Local))
		} else {
			closed = append(closed, fmt.Sprintf("%s (%d)", f.Name, f.Local))
		}
	}
	if len(closed) == 0 {
		add(true, "web tools", "every forward is open")
	} else {
		add(len(open) > 0, "web tools", "not forwarded: "+strings.Join(closed, ", "))
	}

	return checks
}

// versionMismatch explains what a different adb version costs.
func versionMismatch(local, server int) string {
	return fmt.Sprintf("this laptop's adb is protocol %d and the bridge's is %d. A client that finds "+
		"a different server kills it and starts its own, so every adb command from here would restart "+
		"the bridge's server for the whole team. Update one of them to match", local, server)
}

// Problem is why the robot cannot be used through the bridge right now, or nil
// when it can.
func Problem(link Link) error {
	for _, check := range Diagnose(link) {
		if !check.OK && check.Label != "web tools" && check.Label != "autostart" {
			return fmt.Errorf("through the bridge at %s: %s", link.Host, check.Detail)
		}
	}
	return nil
}

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
