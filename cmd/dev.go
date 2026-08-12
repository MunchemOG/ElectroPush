package cmd

import (
	"github.com/MunchemOG/ElectroPush/internal/tui"
	"github.com/spf13/cobra"
)

var devCmd = &cobra.Command{
	Use:   "dev",
	Short: "Developer tools: measure what a deploy actually costs",
	Long: `Measuring tools for working on epsh itself.

Nothing here makes a deploy faster. It deploys to the robot over and over with
different settings, times each one, and writes a report saying what each switch
in ` + "`epsh settings` -> Deploy speed" + ` is worth on your hub.

Do not run this if you are not sure why you want it. It reinstalls the robot
controller app several times in a row and takes a few minutes.

  Benchmark the deploy       time every deploy configuration against the
                             Android Studio equivalent
  Hot reload feasibility     time pushing and compiling a team-code-sized dex
                             on the hub, to see what a reload would have to beat
  Both, with a full report    both, written to epsh-reports/ in your project`,
	RunE: func(cmd *cobra.Command, args []string) error {
		project, apk, splits := tui.DevTargets()
		return tui.RunDev(project, apk, splits)
	},
}
