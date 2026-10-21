package cmd

import (
	"fmt"

	"github.com/MunchemOG/ElectroPush/internal/adb"
	"github.com/MunchemOG/ElectroPush/internal/bridge"
	"github.com/spf13/cobra"
)

var disconnectCmd = &cobra.Command{
	Use:     "disconnect",
	Aliases: []string{"dc"},
	Short:   "Disconnect ADB connection",
	Long:    `Disconnects the ADB connection to the robot without changing Wi-Fi.`,
	RunE:    runDisconnect,
}

func runDisconnect(cmd *cobra.Command, args []string) error {
	uiHeading("Connection", "Disconnect ADB")

	// `adb disconnect` here would reach the bridge's server and drop the robot
	// for every laptop on the team, not just this one.
	if link, ok := bridge.Joined(); ok {
		return fmt.Errorf("this laptop reaches the robot through the bridge at %s, and that connection is "+
			"shared with the team: `epsh bridge leave` stops this laptop using it", link.Host)
	}
	uiStatus("run", "Disconnecting ADB")

	if !adb.IsInstalled() {
		return fmt.Errorf("adb not found")
	}

	if err := adb.Disconnect(); err != nil {
		return fmt.Errorf("failed to disconnect: %w", err)
	}

	uiStatus("ok", "ADB disconnected")
	return nil
}
