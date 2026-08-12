package cmd

import (
	"fmt"
	"time"

	"github.com/MunchemOG/ElectroPush/internal/adb"
	"github.com/MunchemOG/ElectroPush/internal/config"
	"github.com/MunchemOG/ElectroPush/internal/wifi"
	"github.com/spf13/cobra"
)

var exitCmd = &cobra.Command{
	Use:   "exit",
	Short: "Disconnect from the robot and go back to your usual Wi-Fi",
	Long:  `Drops the ADB connection and rejoins the network you were on before deploying.`,
	RunE:  runExit,
}

func runExit(cmd *cobra.Command, args []string) error {
	uiHeading("Connection", "Return to your network")
	uiStatus("run", "Disconnecting ADB")
	if adb.IsInstalled() {
		if err := adb.Disconnect(); err != nil {
			uiStatus("warn", fmt.Sprintf("Could not disconnect ADB · %v", err))
		} else {
			uiStatus("ok", "ADB disconnected")
		}
	}

	wifiMgr := wifi.NewManager()

	onRobot, err := wifiMgr.IsOnRobotNetwork()
	if err != nil {
		return fmt.Errorf("failed to check the current network: %w", err)
	}
	if !onRobot {
		uiStatus("ok", "Not on the robot network · leaving Wi-Fi alone")
		return nil
	}

	home := config.GetHomeSSID()
	if home == "" {

		if inferred, err := wifiMgr.MostRecentNetwork(robotSSIDs()...); err == nil && inferred != "" {
			home = inferred
			uiStatus("wait", fmt.Sprintf("Assuming you came from %q", home))
		}
	}

	if home == "" {

		uiStatus("run", "No home network known · cycling Wi-Fi")
		if err := wifiMgr.PowerCycle(); err != nil {
			uiStatus("warn", fmt.Sprintf("Could not cycle Wi-Fi · %v", err))
			uiNote("You may need to switch networks manually.")
			return nil
		}
		uiStatus("ok", "Wi-Fi cycled · your system should auto-join its usual network")
		uiNote("Tip · set a home network in `epsh settings` for a clean switch back")
		return nil
	}

	uiStatus("run", "Returning to "+home)
	if err := wifiMgr.Join(home, ""); err != nil {
		uiStatus("warn", fmt.Sprintf("Could not rejoin %s · %v", home, err))
		uiNote("You will need to switch back manually.")
		return nil
	}

	if _, err := wifiMgr.WaitForIP("", 30*time.Second); err != nil {
		uiStatus("warn", fmt.Sprintf("Rejoined %s but no IP address yet · %v", home, err))
		return nil
	}

	uiStatus("ok", "Back on "+home)
	return nil
}
