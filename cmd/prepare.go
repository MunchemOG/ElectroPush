package cmd

import (
	"fmt"
	"os"

	"github.com/andreibanu/pusher/internal/gradle"
	"github.com/spf13/cobra"
)

var prepareCmd = &cobra.Command{
	Use:   "prepare",
	Short: "Warm the Gradle cache while you have internet",
	Long: `Runs a full online build so every dependency is cached locally.

pusher already builds before it switches to the robot's Wi-Fi, so this is not
required. It is worth running before an event, where internet may be unreliable
and you want the build to succeed from cache alone.`,
	RunE: runPrepare,
}

func runPrepare(cmd *cobra.Command, args []string) error {
	fmt.Println("[*] Detecting Gradle wrapper...")
	wrapper, err := gradle.DetectWrapper()
	if err != nil {
		return fmt.Errorf("failed to detect Gradle wrapper: %w", err)
	}
	fmt.Printf("[OK] Found Gradle wrapper: %s\n", wrapper)

	fmt.Println("\n[#] Preparing Gradle cache (online build)...")
	fmt.Println("─────────────────────────────────────────")

	if err := gradle.Build(wrapper, false, os.Stdout); err != nil {
		return fmt.Errorf("prepare failed: %w", err)
	}

	fmt.Println("─────────────────────────────────────────")
	fmt.Println("\n[OK] Gradle dependencies cached. Builds will now work without internet.")

	return nil
}
