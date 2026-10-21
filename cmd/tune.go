package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/MunchemOG/ElectroPush/internal/dromos"
	"github.com/MunchemOG/ElectroPush/internal/gradle"
	"github.com/spf13/cobra"
)

var (
	tuneDeploy bool
	tuneDryRun bool
)

var tuneCmd = &cobra.Command{
	Use:   "tune",
	Short: "ElectroDromos's tuning results: what the robot has that the project does not",
	Long: `ElectroDromos's tuners write their results as Java files on the robot, in
/sdcard/FIRST/electrodromos/tuning: DromosTuning.java for the robot, and one
<Auto>Tuning.java per auto the Path Tuner has corrected. The robot never reads
them back. They only count once they are in the project and deployed.

With no subcommand this says which of them the project is missing, value by
value. ` + "`epsh tune pull`" + ` brings them in.`,
	RunE: runTuneStatus,
}

var tunePullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Copy the tuners' results into the project, and say what changed",
	Long: `Copies every generated tuning file off the robot into the package it names,
the way ElectroDromos's own adb pull command does, and lists each value that
changed with the tuner's note on how it was measured.

A file in the project that ElectroDromos did not generate is never overwritten.

Then it offers to deploy, because the robot runs only what was compiled into
it. With Epsh Extreme set up that is a reload of a few seconds.`,
	RunE: runTunePull,
}

func init() {
	tunePullCmd.Flags().BoolVar(&tuneDeploy, "deploy", false, "Deploy straight after, without asking")
	tunePullCmd.Flags().BoolVar(&tuneDryRun, "dry-run", false, "Say what would change, and change nothing")
	tuneCmd.AddCommand(tunePullCmd)
}

func runTuneStatus(cmd *cobra.Command, args []string) error {
	uiHeading("Tune", "ElectroDromos tuning on the robot")

	root, serial, err := tuneTarget()
	if err != nil {
		return err
	}

	files, err := dromos.Fetch(serial, root)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		uiStatus("idle", "The robot has no tuning results yet")
		uiNote("Run ElectroDromos's tuners from the AutoTune page: `epsh open autotune`.")
		return nil
	}

	pending := dromos.Pending(files)
	for _, f := range files {
		if !f.Changed() {
			uiStatus("ok", f.Name+" · the project has it")
		}
	}
	if len(pending) == 0 {
		return nil
	}

	showTuning(root, pending)
	fmt.Println()
	uiNote("`epsh tune pull` brings them into the project.")
	return nil
}

func runTunePull(cmd *cobra.Command, args []string) error {
	uiHeading("Tune", "Bring the tuners' results into the project")

	root, serial, err := tuneTarget()
	if err != nil {
		return err
	}

	files, err := dromos.Fetch(serial, root)
	if err != nil {
		return err
	}
	pending := dromos.Pending(files)
	if len(pending) == 0 {
		if len(files) == 0 {
			uiStatus("idle", "The robot has no tuning results yet")
		} else {
			uiStatus("ok", "The project already has every result the robot holds")
		}
		return nil
	}

	showTuning(root, pending)

	if tuneDryRun {
		fmt.Println()
		uiNote("Dry run · nothing was written.")
		return nil
	}

	written, err := dromos.Write(pending)
	for _, path := range written {
		uiStatus("ok", "Wrote "+relTo(root, path))
	}
	if err != nil {
		return err
	}

	uiNote("They are ordinary files in the project now, so git shows exactly which numbers moved.")

	if !tuneDeploy && !(askable() && confirmDefaultYes("\n    The robot only runs what it was built with. Deploy now?")) {
		uiNote("Deploy when ready: `epsh`.")
		return nil
	}

	wrapper, err := gradle.DetectWrapper()
	if err != nil {
		return fmt.Errorf("failed to detect Gradle wrapper: %w", err)
	}
	fmt.Println()
	return push(wrapper)
}

// tuneTarget is the project and the robot a tuning command works between.
func tuneTarget() (string, string, error) {
	wrapper, err := gradle.DetectWrapper()
	if err != nil {
		return "", "", fmt.Errorf("run this from inside the robot project: %w", err)
	}
	serial, err := requireRobot()
	if err != nil {
		return "", "", err
	}
	return gradle.ProjectDir(wrapper), serial, nil
}

// showTuning lists what pulling would change, value by value.
func showTuning(root string, pending []dromos.TuningFile) {
	for _, f := range pending {
		verb := "changed"
		if f.Local == nil {
			verb = "new"
		}
		uiStatus("wait", fmt.Sprintf("%s · %s · %s", f.Name, verb, relTo(root, f.Dest)))

		changes := f.Changes()
		if len(changes) == 0 {
			uiNote("  differs from the project's copy")
			continue
		}
		for _, c := range changes {
			line := fmt.Sprintf("  %-22s %s → %s", c.Name, c.Old, c.New)
			if c.Note != "" {
				line += "   " + c.Note
			}
			uiNote(line)
		}
	}
}

func relTo(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil {
		return rel
	}
	return path
}

// offerTuning is the deploy's check for tuning left on the robot.
//
// A tuner's result does nothing until it is compiled in, and the robot keeps
// the file whether or not anybody pulls it. Deploying without it is the easy
// mistake: the tuner said "tuned", the robot was redeployed, and it drives
// exactly as it did before. So the deploy looks first, and when it finds
// results the project lacks it offers to bring them in, which means building
// again with them.
//
// It reports whether it pulled anything. Quiet and harmless when it cannot
// look: a deploy must not fail over a check about tuning.
func offerTuning(gradlePath, serial string) bool {
	root := gradle.ProjectDir(gradlePath)
	if !dromos.UsesDromos(root) {
		return false
	}

	files, err := dromos.Fetch(serial, root)
	if err != nil {
		return false
	}
	pending := dromos.Pending(files)
	if len(pending) == 0 {
		return false
	}

	fmt.Println()
	uiStatus("warn", "The robot has ElectroDromos tuning results this project does not")
	showTuning(root, pending)

	if !askable() || !confirmDefaultYes("    Pull them in and build with them?") {
		uiNote("Deploying without them: the robot keeps driving on the values in your code.")
		uiNote("`epsh tune pull` brings them in later.")
		fmt.Println()
		return false
	}

	written, err := dromos.Write(pending)
	for _, path := range written {
		uiStatus("ok", "Wrote "+relTo(root, path))
	}
	if err != nil {
		uiStatus("warn", err.Error())
	}
	return len(written) > 0
}
