package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/andreibanu/pusher/internal/adb"
	"github.com/andreibanu/pusher/internal/config"
	"github.com/andreibanu/pusher/internal/gradle"
	"github.com/andreibanu/pusher/internal/wifi"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const joinTimeout = 45 * time.Second

var pushCmd = &cobra.Command{
	Use:   "push",
	Short: "Build and deploy to the robot",
	Long: `Builds the app and deploys it to the robot.

If a hub is attached over USB, pusher uses it and leaves your Wi-Fi alone.
Otherwise it joins the robot's network, deploys, and puts you back on the
network you started on.`,
	RunE: runPush,
}

func runPush(cmd *cobra.Command, args []string) error {

	gradlePath, err := gradle.DetectWrapper()
	if err != nil {
		return fmt.Errorf("failed to detect Gradle wrapper: %w", err)
	}
	fmt.Printf("[OK] Gradle wrapper: %s\n", gradlePath)

	if !adb.IsInstalled() {
		return fmt.Errorf("adb not found - please install Android SDK Platform-Tools")
	}

	if config.GetPreferUSB() {
		if device, ok := adb.FindUSBDevice(); ok {
			fmt.Printf("[OK] Hub attached over USB: %s\n", device.Label())
			fmt.Println("    Using USB - your Wi-Fi will not be touched.")

			rememberHubABI(device.Serial)
			if config.GetAutoSlim() {
				applyAutoSlim()
			}

			return deploy(gradlePath, device.Serial, false)
		}
	}

	return pushOverWiFi(gradlePath)
}

func pushOverWiFi(gradlePath string) error {
	if err := ensureProfile(); err != nil {
		return err
	}

	profile, err := config.GetDefaultProfile()
	if err != nil {
		return fmt.Errorf("no robot profile configured: %w\n\nRun 'pusher settings' to add one", err)
	}

	wifiMgr := wifi.NewManager()

	onRobot, err := wifiMgr.IsOnRobotNetwork()
	if err != nil {
		return fmt.Errorf("failed to check the current network: %w", err)
	}

	switchBack := config.GetSwitchBack()

	home, err := resolveHomeNetwork(wifiMgr, onRobot, switchBack, profile.SSID)
	if err != nil {
		return err
	}

	// Deliberately not persisted: recomputed every run so moving between home
	// and the lab needs no configuration.
	if home != "" {
		fmt.Printf("[OK] Currently on: %s\n", home)
	}

	// Slimming runs before the build, so it relies on an ABI cached from an
	// earlier connection: the hub is not reachable yet.
	slimmedFor := ""
	if config.GetAutoSlim() {
		slimmedFor = config.GetHubABI()
		applyAutoSlim()
	}

	// Build before switching networks, while real internet is still available.
	// This is why no offline mode is needed on the robot's hotspot.
	if err := buildProject(gradlePath, onRobot); err != nil {
		return err
	}

	if !onRobot {
		fmt.Printf("\n[>] Joining robot Wi-Fi: %s\n", profile.SSID)
		ip, err := wifiMgr.JoinAndWait(profile.SSID, profile.Password, wifi.RobotSubnet, joinTimeout)
		if err != nil {
			return fmt.Errorf("failed to join %q: %w", profile.SSID, err)
		}
		fmt.Printf("[OK] On the robot network (%s)\n", ip)
	} else {
		fmt.Println("[OK] Already on the robot network")
	}

	deployErr := deployToRobot(gradlePath, slimmedFor)

	leavingRobot := switchBack && home != ""

	// Disconnect while the robot is still reachable; after the network hop the
	// address is unroutable. Kept only when a failed deploy leaves us on the
	// robot, where the live connection is what a retry needs.
	if deployErr == nil || leavingRobot {
		disconnectADB()
	}

	if leavingRobot {
		fmt.Printf("\n[<] Returning to %s...\n", home)
		if err := wifiMgr.Join(home, ""); err != nil {
			fmt.Printf("[!] Could not rejoin %s: %v\n", home, err)
			fmt.Println("    You will need to switch back manually.")
		} else if _, err := wifiMgr.WaitForIP("", 30*time.Second); err != nil {
			fmt.Printf("[!] Rejoined %s but no IP address yet: %v\n", home, err)
		} else {
			fmt.Printf("[OK] Back on %s\n", home)
		}
	}

	return deployErr
}

func disconnectADB() {
	if !adb.IsInstalled() {
		return
	}

	if err := adb.Disconnect(); err != nil {
		fmt.Printf("[!] Warning: could not disconnect ADB: %v\n", err)
		return
	}

	fmt.Println("[OK] ADB disconnected")
}

// Precedence: an explicit setting, then the live SSID, then inference from the
// saved-network order. Must run before joining the robot, which destroys every
// signal of where we came from.
func resolveHomeNetwork(wifiMgr *wifi.Manager, onRobot, switchBack bool, robotSSID string) (string, error) {
	if !switchBack {
		return "", nil
	}

	if saved := config.GetHomeSSID(); saved != "" {
		return saved, nil
	}

	if onRobot {
		fmt.Println("[!] Already on the robot network, so pusher cannot tell where you")
		fmt.Println("    came from and will leave you here when it finishes.")
		fmt.Println("    Set one in 'pusher settings' -> Home Wi-Fi network to change that.")
		return "", nil
	}

	ssid, err := wifiMgr.CurrentSSID()
	if err == nil && ssid != "" {
		return ssid, nil
	}

	if err != nil && !errors.Is(err, wifi.ErrSSIDUnavailable) {
		return "", fmt.Errorf("failed to read the current network: %w", err)
	}

	if errors.Is(err, wifi.ErrSSIDUnavailable) {

		inferred, inferErr := wifiMgr.MostRecentNetwork(robotSSID)
		if inferErr == nil && inferred != "" {
			fmt.Printf("[*] macOS hides the network name; assuming you are on %q\n", inferred)
			fmt.Println("    (set it explicitly in 'pusher settings' if that is wrong)")
			return inferred, nil
		}

		fmt.Println("[!] Cannot tell which network you are on, so pusher will leave you")
		fmt.Println("    on the robot's network. Set one in 'pusher settings'.")
		return "", nil
	}

	return "", nil
}

func buildProject(gradlePath string, offline bool) error {
	fmt.Println("\n[#] Building...")
	if offline {
		fmt.Println("    (offline - on the robot network, using cached dependencies)")
	}
	fmt.Println("─────────────────────────────────────────")

	start := time.Now()
	if err := gradle.Build(gradlePath, offline, os.Stdout); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	fmt.Println("─────────────────────────────────────────")
	fmt.Printf("[OK] Built in %.1fs\n", time.Since(start).Seconds())
	return nil
}

func deployToRobot(gradlePath, slimmedFor string) error {
	fmt.Println("\n[+] Connecting to robot via ADB...")
	if err := adb.Connect(); err != nil {
		return fmt.Errorf("failed to connect via ADB: %w", err)
	}
	fmt.Println("[OK] Connected via ADB")

	warnOnABIMismatch(slimmedFor, rememberHubABI(adb.RobotAddr()))

	return install(gradlePath, adb.RobotAddr())
}

func deploy(gradlePath, serial string, offline bool) error {
	if err := buildProject(gradlePath, offline); err != nil {
		return err
	}
	return install(gradlePath, serial)
}

func install(gradlePath, serial string) error {
	apkPath, err := gradle.FindApk(gradle.ProjectDir(gradlePath))
	if err != nil {
		return fmt.Errorf("failed to find APK: %w", err)
	}

	fmt.Printf("\n[*] APK: %s\n", apkPath)

	start := time.Now()
	if err := adb.Install(serial, apkPath, config.GetDeltaTransfer()); err != nil {
		return fmt.Errorf("install failed: %w", err)
	}

	fmt.Printf("\n[OK] Deployed in %.1fs\n", time.Since(start).Seconds())
	return nil
}

func robotSSIDs() []string {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}

	ssids := make([]string, 0, len(cfg.Profiles))
	for _, profile := range cfg.Profiles {
		if profile != nil && profile.SSID != "" {
			ssids = append(ssids, profile.SSID)
		}
	}

	return ssids
}

func ensureProfile() error {
	has, err := config.HasProfiles()
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}
	if has {
		return nil
	}
	return firstRunSetup()
}

func firstRunSetup() error {
	fmt.Println("\nWelcome to Pusher!")
	fmt.Println("No robot profiles found. Let's set one up.")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Robot Wi-Fi SSID: ")
	ssid, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read SSID: %w", err)
	}
	ssid = strings.TrimSpace(ssid)
	if ssid == "" {
		return fmt.Errorf("SSID cannot be empty")
	}

	fmt.Print("Robot Wi-Fi Password: ")
	passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return fmt.Errorf("failed to read password: %w", err)
	}
	fmt.Println()

	if err := config.AddProfile("default", ssid, string(passwordBytes)); err != nil {
		return fmt.Errorf("failed to save profile: %w", err)
	}

	fmt.Println("\n[OK] Profile saved as 'default'")
	return nil
}
