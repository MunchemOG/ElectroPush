package cmd

import (
	"github.com/andreibanu/pusher/internal/tui"
	"github.com/spf13/cobra"
)

var settingsCmd = &cobra.Command{
	Use:     "settings",
	Aliases: []string{"config"},
	Short:   "Open the interactive settings menu",
	Long: `Opens a menu for everything pusher remembers: robot profiles, which
network to return to after deploying, whether to use USB when it is attached,
and how many threads Gradle may use.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return tui.RunSettings()
	},
}
