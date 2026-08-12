package cmd

import (
	"fmt"
	"os"

	"github.com/MunchemOG/ElectroPush/internal/config"
	"github.com/MunchemOG/ElectroPush/internal/feature"
	"github.com/MunchemOG/ElectroPush/internal/selfupdate"
	"github.com/spf13/cobra"
)

var (
	versionFlag bool
	appVersion  string
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

	visualiseCmd.Hidden = !feature.Revealed()

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.SetHelpFunc(runCommandHelp)

	rootCmd.Flags().BoolVarP(&versionFlag, "version", "v", false, "Show version information")

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
	rootCmd.AddCommand(devCmd)
	rootCmd.AddCommand(visualiseCmd)
	rootCmd.AddCommand(helpCmd)
}
