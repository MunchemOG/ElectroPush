package cmd

import (
	"errors"
	"fmt"

	"github.com/MunchemOG/ElectroPush/internal/adb"
	"github.com/MunchemOG/ElectroPush/internal/config"
	"github.com/MunchemOG/ElectroPush/internal/wifi"
	"github.com/spf13/cobra"
)

var connectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Join the robot's Wi-Fi and connect ADB",
	Long:  `Joins the robot's Wi-Fi network and establishes an ADB connection, without building or deploying.`,
	RunE:  runConnect,
}

func runConnect(cmd *cobra.Command, args []string) error {
	if !adb.IsInstalled() {
		return fmt.Errorf("adb not found - please install Android SDK Platform-Tools")
	}

	if device, ok := adb.FindUSBDevice(); ok {
		uiHeading("Connection", "Robot link")
		uiStatus("ok", fmt.Sprintf("Hub attached over USB · %s", device.Label()))
		uiNote("Run `epsh` to build and deploy.")
		return nil
	}

	wifiMgr := wifi.NewManager()

	onRobot, err := wifiMgr.IsOnRobotNetwork()
	if err != nil {
		return fmt.Errorf("failed to check the current network: %w", err)
	}

	if onRobot {
		uiHeading("Connection", "Robot link")
		uiStatus("ok", "Already on the robot network")
	} else {
		if err := ensureProfile(); err != nil {
			return err
		}

		profile, err := config.GetDefaultProfile()
		if err != nil {
			return fmt.Errorf("no robot profile configured: %w\n\nRun 'epsh settings' to add one", err)
		}

		ssid, ssidErr := wifiMgr.CurrentSSID()
		switch {
		case ssidErr == nil && ssid != "":
			uiStatus("run", fmt.Sprintf("Currently on %s", ssid))
		case errors.Is(ssidErr, wifi.ErrSSIDUnavailable):
			if inferred, err := wifiMgr.MostRecentNetwork(robotSSIDs()...); err == nil && inferred != "" {
				uiStatus("wait", fmt.Sprintf("Network name hidden · assuming %q", inferred))
			}
		}

		uiStatus("run", fmt.Sprintf("Joining robot Wi-Fi · %s", profile.SSID))
		ip, err := wifiMgr.JoinAndWait(profile.SSID, profile.Password, wifi.RobotSubnet, joinTimeout)
		if err != nil {
			return fmt.Errorf("failed to join %q: %w", profile.SSID, err)
		}
		uiStatus("ok", fmt.Sprintf("On the robot network · %s", ip))
	}

	uiRule()
	uiStatus("run", "Connecting to robot via ADB")
	if err := adb.Connect(); err != nil {
		return fmt.Errorf("failed to connect via ADB: %w", err)
	}

	uiStatus("ok", "Connected via ADB")
	uiNote("Run `epsh` to build and deploy, or `epsh exit` when you're done.")

	return nil
}
