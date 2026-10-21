package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/MunchemOG/ElectroPush/internal/adb"
	"github.com/MunchemOG/ElectroPush/internal/config"
	"github.com/MunchemOG/ElectroPush/internal/selfupdate"
	"github.com/MunchemOG/ElectroPush/internal/updates"
	"github.com/spf13/cobra"
)

var (
	versionFlag bool
	appVersion  string

	// ignoreWarnings carries on past a check that would otherwise stop the
	// command. Persistent, so `epsh` and `epsh slim` both take it.
	ignoreWarnings bool
)

var rootCmd = &cobra.Command{
	Use:          "epsh",
	Short:        "FTC Robot deployment tool",
	Long:         `Epsh automates connecting to FTC robots and deploying Android Studio projects.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if versionFlag {
			uiHeading("Epsh", "Version information")
			uiStatus("ok", fmt.Sprintf("Version %s", appVersion))
			return nil
		}
		return pushCmd.RunE(cmd, args)
	},
}

// Execute runs the CLI.
func Execute(version string) {
	appVersion = version
	selfupdate.SetCurrent(version)

	if err := config.Initialize(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize config: %v\n", err)
		os.Exit(1)
	}

	// A robot that was found somewhere other than its own access point is still
	// there next time, and every command needs to know before it asks adb
	// anything. Cheap: one string out of the config, no sockets.
	//
	// Not with a bridge, though: the bridge reaches the hub on the hub's own
	// access point, whatever this laptop once found it at.
	if host, _, _, _ := config.GetBridge(); host != "" {
		adb.UseOwnAccessPoint()
	} else if config.GetRelay() {
		if addr := config.GetRobotAddress(); addr != "" {
			adb.UseAddress(addr)
		}
	}

	// Started here so its request overlaps the command rather than following
	// it, and finished after it so a short command still gets one. It cannot
	// fail the run.
	newer := updates.Watch()

	err := rootCmd.Execute()

	newer.Finish(updateWait)

	if err != nil {
		os.Exit(1)
	}
}

// updateWait is how long the update check may hold up an exit. Most commands
// outlast the request several times over, so this is usually not a wait at all.
const updateWait = 1500 * time.Millisecond

func init() {
	rootCmd.SetHelpFunc(runCommandHelp)

	rootCmd.Flags().BoolVarP(&versionFlag, "version", "v", false, "Show version information")
	rootCmd.PersistentFlags().BoolVar(&ignoreWarnings, "ignore-warnings", false,
		"Carry on past a check that would otherwise stop the command")

	rootCmd.AddCommand(pushCmd)
	rootCmd.AddCommand(connectCmd)
	rootCmd.AddCommand(disconnectCmd)
	rootCmd.AddCommand(exitCmd)
	rootCmd.AddCommand(prepareCmd)
	rootCmd.AddCommand(settingsCmd)
	rootCmd.AddCommand(slimCmd)
	rootCmd.AddCommand(hwconfigCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(dashCmd)
	rootCmd.AddCommand(powerCmd)
	rootCmd.AddCommand(profileCmd)
	rootCmd.AddCommand(ipCmd)
	rootCmd.AddCommand(relayCmd)
	rootCmd.AddCommand(bridgeCmd)
	rootCmd.AddCommand(openCmd)
	rootCmd.AddCommand(tuneCmd)
	rootCmd.AddCommand(runsCmd)
	rootCmd.AddCommand(devCmd)
	rootCmd.AddCommand(helpCmd)
}
