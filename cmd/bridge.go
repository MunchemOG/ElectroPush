package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/MunchemOG/ElectroPush/internal/bridge"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var bridgeCmd = &cobra.Command{
	Use:   "bridge",
	Short: "Share one robot between every laptop, through a computer on its Wi-Fi",
	Long: `A small Linux computer, usually a Raspberry Pi, sits next to the robot on its
Wi-Fi and keeps one adb connection to the hub. Every laptop reaches it through
an SSH tunnel, so Android Studio and epsh on each of them see the hub as an
ordinary device without anybody leaving their own network. With Tailscale on
the bridge and the laptops, that works from anywhere.

On the bridge, once:   sudo epsh bridge setup
On each laptop, once:  epsh bridge join <bridge address> --user <user>

The idea is Relay's (github.com/Bob-41/Relay), and the two work together: an
epsh laptop can join a Relay bridge, and a Relay laptop an epsh bridge.

` + "`epsh relay`" + ` is the other way round, a USB Wi-Fi adapter on the hub so the hub
joins your network. A bridge needs no driver for the hub's kernel, so it works
with any Control Hub OS and any adapter.`,
}

var (
	bridgeSSID      string
	bridgePassword  string
	bridgeInterface string
	bridgeUser      string
	bridgeHost      string
	bridgePort      int
)

var bridgeSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Make this Linux machine the robot's bridge (run with sudo)",
	RunE:  runBridgeSetup,
}

var bridgeRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Stop this machine being a bridge (run with sudo)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := bridge.CanHost(); err != nil {
			return err
		}
		return bridge.Remove(os.Stdout)
	},
}

var bridgeJoinCmd = &cobra.Command{
	Use:   "join <bridge address>",
	Short: "Reach the robot through a bridge from this laptop, from now on",
	Args:  cobra.ExactArgs(1),
	RunE:  runBridgeJoin,
}

var bridgeLeaveCmd = &cobra.Command{
	Use:   "leave",
	Short: "Stop using the bridge, and give this laptop its own adb back",
	RunE:  runBridgeLeave,
}

var bridgeStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Say what is working between here and the robot, and what is not",
	RunE:  runBridgeStatus,
}

// Run by the services, not by people.
var bridgeRunCmd = &cobra.Command{
	Use:    "run",
	Short:  "The bridge's watchdog (run by systemd)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return bridge.Watch(ctx, os.Stdout)
	},
}

var bridgeTunnelCmd = &cobra.Command{
	Use:    "tunnel",
	Short:  "Keep the tunnel to the bridge open (started at login)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		log, err := bridge.OpenLog()
		if err != nil {
			return err
		}
		defer log.Close()

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return bridge.RunTunnel(ctx, log)
	},
}

func init() {
	bridgeSetupCmd.Flags().StringVar(&bridgeSSID, "ssid", "", "The robot's Wi-Fi name")
	bridgeSetupCmd.Flags().StringVar(&bridgePassword, "password", "", "The robot's Wi-Fi password")
	bridgeSetupCmd.Flags().StringVar(&bridgeInterface, "interface", "", "The Wi-Fi adapter to give the robot")
	bridgeSetupCmd.Flags().StringVar(&bridgeUser, "user", "", "The user that runs adb, and that laptops sign in as")
	bridgeSetupCmd.Flags().StringVar(&bridgeHost, "host", "", "The address laptops use (default: Tailscale's, else this machine's)")
	bridgeSetupCmd.Flags().IntVar(&bridgePort, "port", bridge.DefaultADBPort, "The adb server port on the bridge")

	bridgeJoinCmd.Flags().StringVar(&bridgeUser, "user", "", "The user to sign in to the bridge as")
	bridgeJoinCmd.Flags().IntVar(&bridgePort, "port", bridge.DefaultADBPort, "The bridge's adb server port")

	for _, c := range []*cobra.Command{bridgeSetupCmd, bridgeRemoveCmd, bridgeJoinCmd, bridgeLeaveCmd,
		bridgeStatusCmd, bridgeRunCmd, bridgeTunnelCmd} {
		bridgeCmd.AddCommand(c)
	}
}

func runBridgeSetup(cmd *cobra.Command, args []string) error {
	uiHeading("Bridge", "Make this machine the robot's bridge")

	if err := bridge.CanHost(); err != nil {
		return err
	}
	if !bridge.SSHRunning() {
		uiStatus("warn", "No SSH server is running, and laptops sign in over SSH")
		uiNote("Debian/Ubuntu: sudo apt install openssh-server · Arch: sudo systemctl enable --now sshd")
	}

	adbPath, err := exec.LookPath("adb")
	if err != nil {
		if !askable() || !confirmDefaultYes("    adb is not installed. Install it now?") {
			return fmt.Errorf("a bridge needs adb")
		}
		if err := bridge.InstallADB(os.Stdout); err != nil {
			return err
		}
		if adbPath, err = exec.LookPath("adb"); err != nil {
			return fmt.Errorf("adb still cannot be found after installing it")
		}
	}
	uiStatus("ok", "adb · "+adbPath)

	s := bridge.Server{Interface: bridgeInterface, SSID: bridgeSSID, Password: bridgePassword,
		User: bridgeUser, Host: bridgeHost, Port: bridgePort}

	// Running it again keeps what it was set up with, so changing one thing
	// does not mean retyping the rest.
	if previous, err := bridge.LoadServer(); err == nil {
		s = keepUnset(s, previous)
	}

	if s.User == "" {
		s.User = os.Getenv("SUDO_USER")
	}
	if s.User == "" || s.User == "root" {
		if s.User = ask("User that runs adb and that laptops sign in as (not root)", ""); s.User == "" || s.User == "root" {
			return fmt.Errorf("name an ordinary user with --user")
		}
	}

	if s.Interface == "" {
		uplink := bridge.DefaultRouteInterface()
		picked, err := bridge.PickInterface(bridge.WifiDevices(), uplink)
		if err != nil {
			return err
		}
		s.Interface = picked
	}
	uiStatus("ok", "Robot Wi-Fi goes on · "+s.Interface)

	if s.SSID == "" {
		if s.SSID = ask("The robot's Wi-Fi name", ""); s.SSID == "" {
			return fmt.Errorf("name the robot's Wi-Fi with --ssid")
		}
	}
	if s.Password == "" && askable() && !cmd.Flags().Changed("password") {
		s.Password = askSecret("The robot's Wi-Fi password")
	}

	if s.Host == "" {
		if ip := bridge.TailscaleIP(); ip != "" {
			s.Host = ip
			uiStatus("ok", "Laptops reach this bridge over Tailscale · "+ip)
		} else if ip := bridge.InterfaceIP(bridge.DefaultRouteInterface()); ip != "" {
			s.Host = ip
			uiStatus("ok", "Laptops reach this bridge on your network · "+ip)
			uiNote("Install Tailscale here and on the laptops to reach it from other networks,")
			uiNote("then run setup again: https://tailscale.com/download/linux")
		}
	}

	if err := bridge.Setup(os.Stdout, s, adbPath, stableExe()); err != nil {
		return err
	}

	uiRule()
	uiStatus("ok", "This machine is the robot's bridge")
	uiNote("It joins " + s.SSID + " whenever the hub is on, through power cycles on either end.")
	uiNote("`epsh bridge status` here says how it is doing.")
	fmt.Println()
	uiStatus("run", "On each laptop, once:")
	join := fmt.Sprintf("epsh bridge join %s --user %s", orPlaceholder(s.Host), s.User)
	if s.Port != bridge.DefaultADBPort {
		join += fmt.Sprintf(" --port %d", s.Port)
	}
	fmt.Println("      " + join)
	uiNote(fmt.Sprintf("Relay's laptop installer works too: host %s, user %s, port %d", orPlaceholder(s.Host), s.User, s.Port))
	return nil
}

// keepUnset fills what this run did not say from the last setup.
func keepUnset(s, previous bridge.Server) bridge.Server {
	if s.Interface == "" {
		s.Interface = previous.Interface
	}
	if s.SSID == "" {
		s.SSID, s.Password = previous.SSID, previous.Password
	}
	if s.User == "" {
		s.User = previous.User
	}
	if s.Host == "" {
		s.Host = previous.Host
	}
	return s
}

func orPlaceholder(host string) string {
	if host == "" {
		return "<this machine's address>"
	}
	return host
}

func runBridgeJoin(cmd *cobra.Command, args []string) error {
	uiHeading("Bridge", "Reach the robot through "+args[0])

	if _, err := bridge.Join(os.Stdout, bridge.JoinOptions{Host: args[0], User: bridgeUser, Port: bridgePort}); err != nil {
		return err
	}

	link, _ := bridge.Joined()
	reportBridge(link)

	uiRule()
	uiNote("Restart Android Studio and the hub is in its device list.")
	uiNote("`epsh` deploys through the bridge now, without touching your Wi-Fi.")
	return nil
}

func runBridgeLeave(cmd *cobra.Command, args []string) error {
	uiHeading("Bridge", "Stop using the bridge")
	if _, ok := bridge.Joined(); !ok {
		uiStatus("ok", "This laptop is not using a bridge")
		return nil
	}
	if err := bridge.Leave(); err != nil {
		return err
	}
	uiStatus("ok", "Tunnel closed · this laptop's adb is its own again")
	uiNote("The key stays in ~/.ssh, so joining again needs no password.")
	return nil
}

func runBridgeStatus(cmd *cobra.Command, args []string) error {
	shown := false

	if bridge.IsBridge() {
		uiHeading("Bridge", "This machine is the robot's bridge")
		s, err := bridge.LoadServer()
		if err != nil {
			return err
		}
		printChecks(bridge.ServerStatus(s))
		uiNote("Log · journalctl -u epsh-bridge -u epsh-bridge-adb -f")
		shown = true
	}

	if link, ok := bridge.Joined(); ok {
		uiHeading("Bridge", "This laptop reaches the robot through "+link.Host)
		reportBridge(link)
		uiNote("Log · " + bridge.LogPath())
		shown = true
	}

	if !shown {
		uiHeading("Bridge", "Not in use")
		uiNote("On the bridge: sudo epsh bridge setup")
		uiNote("On a laptop:   epsh bridge join <bridge address> --user <user>")
	}
	return nil
}

// reportBridge prints a laptop's link, then where each robot page is.
func reportBridge(link bridge.Link) {
	checks := bridge.Diagnose(link)
	printChecks(checks)

	fmt.Println()
	for _, f := range bridge.Forwards {
		if url := f.URL(); url != "" {
			fmt.Printf("    %-18s %-26s %s\n", f.Name, url, uiMutedStyle.Render(f.Note))
		}
	}
}

func printChecks(checks []bridge.Check) {
	for _, c := range checks {
		kind := "ok"
		if !c.OK {
			kind = "warn"
		}
		uiStatus(kind, fmt.Sprintf("%-14s %s", c.Label, c.Detail))
	}
}

// bridgeProblem is why a laptop that uses a bridge cannot reach the robot
// through it right now, or nil when it can, or when there is no bridge.
func bridgeProblem() (bool, error) {
	link, ok := bridge.Joined()
	if !ok {
		return false, nil
	}
	return true, bridge.Problem(link)
}

func ask(question, fallback string) string {
	if !askable() {
		return fallback
	}
	fmt.Printf("    %s: ", question)
	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return fallback
	}
	if answer = strings.TrimSpace(answer); answer == "" {
		return fallback
	}
	return answer
}

func askSecret(question string) string {
	fmt.Printf("    %s (empty for an open network): ", question)
	secret, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return ""
	}
	return string(secret)
}

// stableExe is the path systemd should start epsh by.
func stableExe() string {
	exe, err := os.Executable()
	if err != nil {
		return "/usr/local/bin/epsh"
	}
	if strings.HasPrefix(exe, os.TempDir()) {
		uiStatus("warn", "epsh is running from a temporary folder, which the services cannot rely on")
		uiNote("Install it somewhere permanent, like /usr/local/bin, and run setup again.")
	}
	return exe
}
