package bridge

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MunchemOG/ElectroPush/internal/config"
)

// The tunnel is one ssh, kept alive by epsh rather than by launchd, systemd or
// the Task Scheduler directly. Those only know how to restart a process; this
// also knows what to do about the port before each start, which is where the
// hard part is.
//
// Port 5037 is contested. Android Studio and a bare `adb` both start a local
// adb server there whenever they find nothing, so between tunnels it is
// routinely taken. If what holds it is this laptop's own adb, it is told to
// shut down and the tunnel takes its place. If it is an ssh this supervisor
// started and lost track of, it is stopped. Anything else is left alone and
// named in the log: in particular, `adb kill-server` is never sent to a port an
// ssh holds, because the server behind that ssh is the bridge's, and killing it
// takes the robot away from every laptop on the team.

func tunnelPidFile() string     { return filepath.Join(config.Dir(), "bridge-ssh.pid") }
func supervisorPidFile() string { return filepath.Join(config.Dir(), "bridge-tunnel.pid") }

// LogPath is where the tunnel says what it is doing.
func LogPath() string { return filepath.Join(config.Dir(), "bridge-tunnel.log") }

// OpenLog opens the tunnel's log for appending, starting it again once it is
// big enough that nobody will read the start of it.
func OpenLog() (*os.File, error) {
	if info, err := os.Stat(LogPath()); err == nil && info.Size() > 1<<20 {
		_ = os.Remove(LogPath())
	}
	return os.OpenFile(LogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
}

// RunTunnel keeps the tunnel to the bridge up until ctx ends.
func RunTunnel(ctx context.Context, out io.Writer) error {
	link, ok := Joined()
	if !ok {
		return fmt.Errorf("this laptop has not joined a bridge: `epsh bridge join <bridge>`")
	}
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		return fmt.Errorf("ssh not found")
	}

	writePid(supervisorPidFile(), os.Getpid())
	defer os.Remove(supervisorPidFile())

	log := func(format string, args ...any) {
		fmt.Fprintf(out, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
	}

	backoff := 2 * time.Second
	for ctx.Err() == nil {
		if err := clearLocalADB(); err != nil {
			log("%v", err)
		} else {
			skip := map[int]bool{}
			for _, f := range Forwards {
				if listening(f.Local) {
					skip[f.Local] = true
					_, name := portHolder(f.Local)
					log("Not forwarding %s: port %d is already taken by %s", f.Name, f.Local, orUnknown(name))
				}
			}

			cmd := exec.CommandContext(ctx, ssh, link.SSHArgs(skip)...)
			cmd.Stdout, cmd.Stderr = out, out
			log("Opening the tunnel to %s", link.Target())

			started := time.Now()
			if err := cmd.Start(); err != nil {
				log("Could not start ssh: %v", err)
			} else {
				writePid(tunnelPidFile(), cmd.Process.Pid)
				err := cmd.Wait()
				os.Remove(tunnelPidFile())
				if ctx.Err() != nil {
					return nil
				}
				log("The tunnel closed after %s: %v", time.Since(started).Round(time.Second), err)
			}

			// A tunnel that held for a while was working, and the next one
			// probably will too. One that dies at once is a bridge that is
			// off or a key it refuses, and asking every two seconds helps
			// nobody.
			if time.Since(started) > time.Minute {
				backoff = 2 * time.Second
			}
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
	return nil
}

// clearLocalADB makes the local adb port free for the tunnel, if it safely can.
func clearLocalADB() error {
	if !listening(DefaultADBPort) {
		return nil
	}

	pid, name := portHolder(DefaultADBPort)
	switch {
	case name == "adb":
		// This laptop's own server. host:kill is what `adb kill-server`
		// sends, and only this server can be listening as adb here.
		_, _ = request(LocalADB, "host:kill", false)
	case name == "ssh" && pid != 0 && pid == readPid(tunnelPidFile()):
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill()
		}
	case name == "ssh":
		return fmt.Errorf("port %d is held by another ssh (pid %d), probably a Relay tunnel to a bridge: "+
			"leaving it alone. Remove that tunnel for this one to start", DefaultADBPort, pid)
	default:
		return fmt.Errorf("port %d is held by %s (pid %d), which is not adb: leaving it alone",
			DefaultADBPort, orUnknown(name), pid)
	}

	deadline := time.Now().Add(5 * time.Second)
	for listening(DefaultADBPort) {
		if time.Now().After(deadline) {
			return fmt.Errorf("port %d is still held after asking %s to let go", DefaultADBPort, orUnknown(name))
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil
}

// tunnelIsUp reports whether the local adb port is this laptop's tunnel, and
// the server behind it answers.
func tunnelIsUp() bool {
	if _, name := portHolder(DefaultADBPort); name != "ssh" {
		return false
	}
	_, err := ServerVersion(LocalADB)
	return err == nil
}

// stopTunnel stops the supervisor and its ssh, if they are running.
func stopTunnel() {
	for _, file := range []string{supervisorPidFile(), tunnelPidFile()} {
		if pid := readPid(file); pid != 0 {
			if p, err := os.FindProcess(pid); err == nil {
				_ = p.Kill()
			}
			os.Remove(file)
		}
	}
}

func writePid(path string, pid int) {
	_ = os.WriteFile(path, []byte(strconv.Itoa(pid)), 0o644)
}

func readPid(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid
}

func orUnknown(name string) string {
	if name == "" {
		return "something epsh cannot identify"
	}
	return name
}
