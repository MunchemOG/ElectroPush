package cmd

import (
	"fmt"
	"os"

	"github.com/MunchemOG/ElectroPush/internal/gradle"
	"github.com/spf13/cobra"
)

var prepareCmd = &cobra.Command{
	Use:   "prepare",
	Short: "Warm the Gradle cache while you have internet",
	Long: `Runs a full online build so every dependency is cached locally.

epsh already builds before it switches to the robot's Wi-Fi, so this is not
required. It is worth running before an event, where internet may be unreliable
and you want the build to succeed from cache alone.`,
	RunE: runPrepare,
}

func runPrepare(cmd *cobra.Command, args []string) error {
	uiHeading("Prepare", "Warm your offline build cache")
	uiStatus("run", "Detecting Gradle wrapper")
	wrapper, err := gradle.DetectWrapper()
	if err != nil {
		return fmt.Errorf("failed to detect Gradle wrapper: %w", err)
	}
	uiStatus("ok", fmt.Sprintf("Gradle wrapper · %s", wrapper))

	uiRule()
	uiStatus("run", "Preparing Gradle cache · online build")

	if err := gradle.Build(wrapper, false, os.Stdout); err != nil {
		return fmt.Errorf("prepare failed: %w", err)
	}

	uiRule()
	uiStatus("ok", "Gradle dependencies cached · builds can now run offline")

	return nil
}
