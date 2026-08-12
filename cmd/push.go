package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/MunchemOG/ElectroPush/internal/adb"
	"github.com/MunchemOG/ElectroPush/internal/config"
	"github.com/MunchemOG/ElectroPush/internal/gradle"
	"github.com/MunchemOG/ElectroPush/internal/wifi"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const joinTimeout = 45 * time.Second

var pushCmd = &cobra.Command{
	Use:   "push",
	Short: "Build and deploy to the robot",
	Long: `Builds the app and deploys it to the robot.

If a hub is attached over USB, epsh uses it and leaves your Wi-Fi alone.
Otherwise it joins the robot's network, deploys, and puts you back on the
network you started on.`,
	RunE: runPush,
}

func runPush(cmd *cobra.Command, args []string) error {
	uiHeading("Deploy", "Build · connect · install")

	gradlePath, err := gradle.DetectWrapper()
	if err != nil {
		return fmt.Errorf("failed to detect Gradle wrapper: %w", err)
	}
	uiStatus("ok", fmt.Sprintf("Gradle wrapper · %s", gradlePath))

	if !adb.IsInstalled() {
		return fmt.Errorf("adb not found - please install Android SDK Platform-Tools")
	}

	if config.GetPreferUSB() {
		if device, ok := adb.FindUSBDevice(); ok {
			uiStatus("ok", fmt.Sprintf("Hub attached over USB · %s", device.Label()))
			uiNote("Using USB · your Wi-Fi will not be touched")

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
		return fmt.Errorf("no robot profile configured: %w\n\nRun 'epsh settings' to add one", err)
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

	if home != "" {
		uiStatus("run", "Currently on · "+home)
	}

	slimmedFor := ""
	if config.GetAutoSlim() {
		slimmedFor = config.GetHubABI()
		applyAutoSlim()
	}

	if err := buildProject(gradlePath, onRobot); err != nil {
		return err
	}

	if !onRobot {
		uiStatus("run", "Joining robot Wi-Fi · "+profile.SSID)
		ip, err := wifiMgr.JoinAndWait(profile.SSID, profile.Password, wifi.RobotSubnet, joinTimeout)
		if err != nil {
			return fmt.Errorf("failed to join %q: %w", profile.SSID, err)
		}
		uiStatus("ok", fmt.Sprintf("On the robot network · %s", ip))
	} else {
		uiStatus("ok", "Already on the robot network")
	}

	deployErr := deployToRobot(gradlePath, slimmedFor)

	leavingRobot := switchBack && home != ""

	if deployErr == nil || leavingRobot {
		disconnectADB()
	}

	if leavingRobot {
		uiStatus("run", "Returning to "+home)
		if err := wifiMgr.Rejoin(home, robotSSIDs()); err != nil {
			uiStatus("warn", fmt.Sprintf("Could not rejoin %s · %v", home, err))
			uiNote("You will need to switch back manually.")
		} else if _, err := wifiMgr.WaitToLeave(wifi.RobotSubnet, 45*time.Second); err != nil {
			uiStatus("warn", fmt.Sprintf("Could not get back onto %s · %v", home, err))
			uiNote("You will need to switch back manually.")
		} else {
			uiStatus("ok", "Back on "+home)
		}
	}

	return deployErr
}

func disconnectADB() {
	if !adb.IsInstalled() {
		return
	}

	if err := adb.Disconnect(); err != nil {
		uiStatus("warn", fmt.Sprintf("Could not disconnect ADB · %v", err))
		return
	}

	uiStatus("ok", "ADB disconnected")
}

func resolveHomeNetwork(wifiMgr *wifi.Manager, onRobot, switchBack bool, robotSSID string) (string, error) {
	if !switchBack {
		return "", nil
	}

	if saved := config.GetHomeSSID(); saved != "" {
		return saved, nil
	}

	if onRobot {
		fmt.Println("[!] Already on the robot network, so epsh cannot tell where you")
		fmt.Println("    came from and will leave you here when it finishes.")
		fmt.Println("    Set one in 'epsh settings' -> Home Wi-Fi network to change that.")
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
			fmt.Printf("[*] The network name is hidden; assuming you are on %q\n", inferred)
			fmt.Println("    (set it explicitly in 'epsh settings' if that is wrong)")
			return inferred, nil
		}

		fmt.Println("[!] Cannot tell which network you are on, so epsh will leave you")
		fmt.Println("    on the robot's network. Set one in 'epsh settings'.")
		return "", nil
	}

	return "", nil
}

func buildProject(gradlePath string, offline bool) error {
	uiRule()
	uiStatus("run", "Building project")
	if offline {
		uiNote("Offline · using cached dependencies on the robot network")
	}

	start := time.Now()
	if err := gradle.Build(gradlePath, offline, os.Stdout); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	uiRule()
	uiStatus("ok", fmt.Sprintf("Built in %.1fs", time.Since(start).Seconds()))
	return nil
}

func deployToRobot(gradlePath, slimmedFor string) error {
	uiStatus("run", "Connecting to robot via ADB")
	if err := adb.Connect(); err != nil {
		return fmt.Errorf("failed to connect via ADB: %w", err)
	}
	uiStatus("ok", "Connected via ADB")

	warnOnABIMismatch(slimmedFor, rememberHubABI(adb.RobotAddr()))

	return install(gradlePath, adb.RobotAddr())
}

func deploy(gradlePath, serial string, offline bool) error {
	if err := buildProject(gradlePath, offline); err != nil {
		return err
	}
	return install(gradlePath, serial)
}

// install deploys, and reports what tuning that overwrote.
//
// The reading has to be taken here rather than inside either path, because both
// of them put the code's values back.
func install(gradlePath, serial string) error {
	watch := beginDashWatch(serial)

	if err := deployOnce(gradlePath, serial); err != nil {
		return err
	}

	watch.report(gradle.ProjectDir(gradlePath))
	return nil
}

func deployOnce(gradlePath, serial string) error {
	// Reloading replaces the install entirely when it is equivalent, and says
	// why when it is not rather than quietly doing the wrong one.
	if done, err := extremeDeploy(gradlePath, serial); err != nil {
		return err
	} else if done {
		return nil
	}

	apkPath, err := gradle.FindApk(gradle.ProjectDir(gradlePath))
	if err != nil {
		return fmt.Errorf("failed to find APK: %w", err)
	}

	uiRule()
	uiStatus("wait", "APK ready · "+apkPath)

	opt := adb.Options{
		Delta:         config.GetDeltaTransfer(),
		SkipUnchanged: config.GetSkipUnchanged(),
		Stream:        config.GetStreamInstall(),
	}
	if config.GetSplitInstall() {
		opt.Splits = gradle.FindSplits(gradle.ProjectDir(gradlePath))
	}

	start := time.Now()
	plan, err := adb.InstallWith(serial, apkPath, opt)
	if err != nil {
		return fmt.Errorf("install failed: %w", err)
	}

	// The robot now holds this project's non-team-code state, so the next
	// deploy can tell that only team code changed and reload instead.
	recordExtremeState(serial)

	switch {
	case plan.Skipped:
		uiStatus("idle", fmt.Sprintf("Nothing to install · %s (%.1fs)", plan.Reason, time.Since(start).Seconds()))
	case plan.Splits > 0:
		uiStatus("ok", fmt.Sprintf("Deployed %d changed split(s) in %.1fs", plan.Splits, time.Since(start).Seconds()))
	default:
		uiStatus("ok", fmt.Sprintf("Deployed in %.1fs", time.Since(start).Seconds()))
	}

	// The APK just installed has no team code in it, so this is not finished
	// until the reload has run. Skipping it leaves a robot that deployed
	// successfully and has nothing to run.
	return reloadAfterInstall(serial)
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
	fmt.Println("\nWelcome to Epsh!")
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
