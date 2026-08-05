package cmd

import (
	"fmt"
	"os"

	"github.com/andreibanu/pusher/internal/config"
	"github.com/spf13/cobra"
)

var (
	versionFlag bool
	appVersion  string
)

var rootCmd = &cobra.Command{
	Use:          "pusher",
	Short:        "FTC Robot deployment tool",
	Long:         `Pusher automates connecting to FTC robots and deploying Android Studio projects.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if versionFlag {
			fmt.Printf("Pusher version %s\n", appVersion)
			return nil
		}
		return pushCmd.RunE(cmd, args)
	},
}

func Execute(version string) {
	appVersion = version

	if err := config.Initialize(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize config: %v\n", err)
		os.Exit(1)
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {

	rootCmd.Flags().BoolVarP(&versionFlag, "version", "v", false, "Show version information")

	rootCmd.AddCommand(pushCmd)
	rootCmd.AddCommand(connectCmd)
	rootCmd.AddCommand(disconnectCmd)
	rootCmd.AddCommand(exitCmd)
	rootCmd.AddCommand(prepareCmd)
	rootCmd.AddCommand(settingsCmd)
	rootCmd.AddCommand(slimCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(visualiseCmd)
	rootCmd.AddCommand(helpCmd)
}
