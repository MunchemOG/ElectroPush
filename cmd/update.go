package cmd

import (
	"fmt"

	"github.com/andreibanu/pusher/internal/selfupdate"
	"github.com/spf13/cobra"
)

var updateCheckOnly bool

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update pusher to the latest release",
	Long: `Updates pusher itself to the newest published release.

A Homebrew install is handed to 'brew upgrade'. Anything else is replaced in
place, after checking the download against the release's published checksum.

The same thing is available in 'pusher settings' -> Update.`,
	RunE: runUpdate,
}

func init() {
	updateCmd.Flags().BoolVar(&updateCheckOnly, "check", false, "Report what is available without installing it")
}

func runUpdate(cmd *cobra.Command, args []string) error {
	install, err := selfupdate.Detect()
	if err != nil {
		return err
	}

	via := install.Method.String()
	if install.Formula != "" {
		via += " (formula " + install.Formula + ")"
	}

	fmt.Printf("[*] Installed via %s\n", via)
	fmt.Printf("[*] Location: %s\n", install.Path)
	fmt.Printf("[*] Running:  %s\n", selfupdate.Current())

	release, err := selfupdate.Latest()
	if err != nil {
		return err
	}
	fmt.Printf("[*] Latest:   %s\n", release.Tag)

	if !release.Newer() {
		fmt.Println("\n[OK] Already up to date.")
		return nil
	}

	if updateCheckOnly {
		fmt.Printf("\n[!] %s is available. Run 'pusher update' to install it.\n", release.Tag)
		return nil
	}

	if install.Method == selfupdate.Homebrew {
		fmt.Printf("\n[>] brew upgrade %s\n", install.Formula)

		// Homebrew says plenty and only the end of it is the outcome, which is
		// what somebody watching a one-line command wants to see.
		out, err := selfupdate.UpgradeBrew(install.Formula)
		if line := selfupdate.LastLine(out); line != "" {
			fmt.Printf("    %s\n", line)
		}
		if err != nil {
			return err
		}
	} else {
		fmt.Printf("\n[>] Replacing this binary with %s\n", release.Tag)
		if err := selfupdate.Apply(release, install.Path); err != nil {
			return err
		}
	}

	fmt.Printf("\n[OK] Updated to %s. Run pusher again to use it.\n", release.Tag)
	return nil
}
