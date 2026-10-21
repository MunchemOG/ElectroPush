package cmd

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/MunchemOG/ElectroPush/internal/adb"
	"github.com/MunchemOG/ElectroPush/internal/config"
	"github.com/MunchemOG/ElectroPush/internal/extreme"
	"github.com/MunchemOG/ElectroPush/internal/ftcproject"
	"github.com/MunchemOG/ElectroPush/internal/gradle"
	"github.com/MunchemOG/ElectroPush/internal/robot"
	"github.com/MunchemOG/ElectroPush/internal/wifi"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check that everything epsh needs is working",
	Long: `Reports what epsh can and cannot see: whether the system will name your
Wi-Fi network, whether adb is installed and what it is talking to, and how
large the APK your project builds is.

Run this first when something is not behaving.`,
	RunE: runDoctor,
}

func runDoctor(cmd *cobra.Command, args []string) error {
	uiHeading("Doctor", "System and robot readiness")
	uiNote(fmt.Sprintf("Platform · %s/%s · Wi-Fi via %s", runtime.GOOS, runtime.GOARCH, wifiBackend()))

	locationOK := reportWiFi()
	fmt.Println()
	reportHubAP()
	fmt.Println()
	reportADB()
	fmt.Println()
	reportProject()

	uiRule()
	if !locationOK {
		uiStatus("warn", "epsh cannot identify your current network")
		uiNote("Set a fallback in `epsh settings` → Home Wi-Fi network.")
	} else {
		uiStatus("ok", "No problems detected")
	}

	return nil
}

func wifiBackend() string {
	switch runtime.GOOS {
	case "darwin":
		return "networksetup"
	case "linux":
		return "nmcli/NetworkManager"
	case "windows":
		return "netsh/PowerShell"
	default:
		return "unsupported"
	}
}

func reportWiFi() bool {
	uiHeading("Wi-Fi", "Network state")

	wifiMgr := wifi.NewManager()

	fmt.Printf("  Radio powered on   : %v\n", wifiMgr.IsPoweredOn())

	ip, err := wifiMgr.GetIPv4()
	switch {
	case err != nil:
		fmt.Printf("  IPv4 address       : error: %v\n", err)
	case ip == "":
		fmt.Println("  IPv4 address       : none (not connected)")
	default:
		fmt.Printf("  IPv4 address       : %s\n", ip)
	}

	onRobot, _ := wifiMgr.IsOnRobotNetwork()
	fmt.Printf("  On robot network   : %v\n", onRobot)

	ssid, ssidErr := wifiMgr.CurrentSSID()
	switch {
	case errors.Is(ssidErr, wifi.ErrSSIDUnavailable):
		fmt.Println("  Current network    : hidden by the OS")

		inferred, _ := wifiMgr.MostRecentNetwork(robot.SSIDs()...)
		if inferred != "" {
			fmt.Printf("  Inferred as        : %s\n", inferred)
		} else {
			fmt.Println("  Inferred as        : could not tell")
		}

		if saved := config.GetHomeSSID(); saved != "" {
			fmt.Printf("  Overridden to      : %s (from settings)\n", saved)
		}

		fmt.Println()
		for _, line := range strings.Split(wifi.LocationHint, "\n") {
			fmt.Println("  " + line)
		}

		return inferred != "" || config.GetHomeSSID() != ""
	case ssidErr != nil:
		fmt.Printf("  Current network    : error: %v\n", ssidErr)
		return false
	case ssid == "":
		fmt.Println("  Current network    : not associated")
	default:
		fmt.Printf("  Current network    : %s\n", ssid)
	}

	if networks, err := wifiMgr.PreferredNetworks(); err == nil {
		fmt.Printf("  Saved networks     : %d\n", len(networks))
	}

	return true
}

// reportHubAP answers the question a failed join leaves open: was the hub even
// broadcasting? Its own section because the Wi-Fi one gives up early on a macOS
// that will not name the current network, which is every macOS since 15.
func reportHubAP() {
	fmt.Println("Robot Wi-Fi")
	fmt.Println("─────────────────────────────────────────")

	profile, err := config.GetDefaultProfile()
	if err != nil || profile.SSID == "" {
		fmt.Println("  Network            : none configured")
		fmt.Println("  Add one with 'epsh settings' -> Robot profiles.")
		return
	}

	fmt.Printf("  Network            : %s\n", profile.SSID)

	if !wifi.ScanningEnabled() {
		fmt.Println("  Broadcasting       : cannot look")
		fmt.Println()
		for _, line := range strings.Split(wifi.ScanNote, "\n") {
			fmt.Println("  " + line)
		}
		return
	}

	// The label goes out before the scan, which takes a few seconds, so the
	// pause reads as epsh working rather than epsh hanging.
	fmt.Print("  Broadcasting       : ")

	switch present, err := wifi.NewManager().Visible(profile.SSID); {
	case err != nil:
		fmt.Printf("could not tell: %v\n", err)
	case present:
		fmt.Println("yes, in range now")
	default:
		fmt.Println("no")
		fmt.Println("  The hub is switched off, still starting up, or out of range.")
	}
}

func reportADB() {
	uiHeading("ADB", "Android Debug Bridge")

	if !adb.IsInstalled() {
		fmt.Println("  adb                : NOT FOUND")
		fmt.Println("  Install Android SDK Platform-Tools, or: brew install android-platform-tools")
		return
	}
	fmt.Println("  adb                : found")

	devices, err := adb.Devices()
	if err != nil {
		fmt.Printf("  Devices            : error: %v\n", err)
		return
	}

	if len(devices) == 0 {
		fmt.Println("  Devices            : none attached")
		return
	}

	for _, device := range devices {
		fmt.Printf("  %-8s %-9s %s\n", device.Transport, device.State, device.Label())
		if !device.IsOnline() {
			continue
		}
		if abis, err := adb.ABIList(device.Serial); err == nil {
			fmt.Printf("           ABIs      %s\n", strings.Join(abis, ", "))
		}
	}
}

func reportProject() {
	uiHeading("Project", "FTC project readiness")

	wrapper, err := gradle.DetectWrapper()
	if err != nil {
		fmt.Println("  Gradle wrapper     : not found in this directory")
		fmt.Println("  Run epsh from inside your FTC project.")
		return
	}
	fmt.Printf("  Gradle wrapper     : %s\n", wrapper)

	root := gradle.ProjectDir(wrapper)

	project, err := ftcproject.Detect(root)
	if err != nil {
		fmt.Printf("  FTC project        : %v\n", err)
		return
	}

	// The pairing that empties the OpMode list. Setup is refused now, but a
	// project set up before that check existed still has the block in its
	// gradle file and a dex on its robot, and neither goes away on its own.
	if extreme.UsesSloth(root) {
		fmt.Println("  Hot reload         : Sloth")
		if extreme.Excluded(root) {
			fmt.Println("                       ^ Epsh Extreme is also set up here, and the two")
			fmt.Println("                         cannot share a robot: both put team code on it and")
			fmt.Println("                         the robot loads whichever copy it finds first,")
			fmt.Println("                         which is how an OpMode list comes up empty.")
			fmt.Println("                         Undo it in `epsh settings` with the robot")
			fmt.Println("                         connected, so the dex comes off the robot too.")
		}
	}

	if analysis, err := project.Analyze(); err == nil {
		fmt.Printf("  Packaged ABIs      : %s\n", strings.Join(analysis.ABIs, ", "))
		if len(analysis.ABIs) > 1 {
			fmt.Println("                       ^ the hub runs one of these; 'epsh slim' drops the rest")
		}
		fmt.Printf("  Slim backups       : %v\n", analysis.HasBackups)
	}

	apkPath, err := gradle.FindApk(root)
	if err != nil {
		fmt.Println("  Built APK          : none yet")
		return
	}

	info, err := os.Stat(apkPath)
	if err != nil {
		return
	}
	fmt.Printf("  Built APK          : %.1f MB\n", float64(info.Size())/(1024*1024))
}
