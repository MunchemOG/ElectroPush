package cmd

import (
	"fmt"

	"github.com/MunchemOG/ElectroPush/internal/selfupdate"
	"github.com/spf13/cobra"
)

var updateCheckOnly bool

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update epsh to the latest release",
	Long: `Updates epsh itself to the newest published release.

A Homebrew install is handed to 'brew upgrade'. Anything else is replaced in
place, after checking the download against the release's published checksum.

The same thing is available in 'epsh settings' -> Update.`,
	RunE: runUpdate,
}

func init() {
	updateCmd.Flags().BoolVar(&updateCheckOnly, "check", false, "Report what is available without installing it")
}

func runUpdate(cmd *cobra.Command, args []string) error {
	uiHeading("Update", "Epsh release channel")
	install, err := selfupdate.Detect()
	if err != nil {
		return err
	}

	via := install.Method.String()
	if install.Formula != "" {
		via += " (formula " + install.Formula + ")"
	}

	uiStatus("run", fmt.Sprintf("Installed via %s", via))
	uiNote("Location · " + install.Path)
	uiNote("Running  · " + selfupdate.Current())

	release, err := selfupdate.Latest()
	if err != nil {
		return err
	}
	uiStatus("wait", "Latest release · "+release.Tag)

	if !release.Newer() {
		uiStatus("ok", "Already up to date")
		return nil
	}

	if updateCheckOnly {
		uiStatus("warn", fmt.Sprintf("%s is available · run `epsh update` to install", release.Tag))
		return nil
	}

	if install.Method == selfupdate.Homebrew {
		uiStatus("run", "brew upgrade "+install.Formula)

		// Homebrew says plenty and only the end of it is the outcome, which is
		// what somebody watching a one-line command wants to see.
		out, err := selfupdate.UpgradeBrew(install.Formula, release.Version())
		if line := selfupdate.LastLine(out); line != "" {
			uiNote(line)
		}
		if err != nil {
			return err
		}
	} else {
		uiStatus("run", "Installing "+release.Tag)
		if err := selfupdate.Apply(release, install.Path); err != nil {
			return err
		}
	}

	uiStatus("ok", fmt.Sprintf("Updated to %s · run epsh again to use it", release.Tag))
	return nil
}
