package cmd

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MunchemOG/ElectroPush/internal/adb"
	"github.com/MunchemOG/ElectroPush/internal/dromos"
	"github.com/MunchemOG/ElectroPush/internal/power"
	"github.com/MunchemOG/ElectroPush/internal/profile"
	"github.com/spf13/cobra"
)

var (
	runsList  bool
	runsText  bool
	runsPull  string
	runsOut   string
	runsClear bool
)

var runsCmd = &cobra.Command{
	Use:   "runs [name or number]",
	Short: "Look at an ElectroDromos run, with the battery and the loop on the same clock",
	Long: `Record Auto and every round of the Path Tuner leave a recording on the robot:
where the robot was each loop, where its path said it should be, the drive
powers and the battery.

This opens one as a page: the driven path over the planned one on the field,
coloured by how far off it was, and under it, on one timeline you can scrub and
zoom, how far off the path it was, the battery, the drive powers, and, when the
power monitor and loop profiler were installed during the run, every motor's
current and what the loop thread was doing at that moment.

With no argument it opens the newest. --list shows them all; a number from that
list or part of a file name picks one. --pull copies the recordings off the
robot, for the docs site's Run Viewer.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRuns,
}

func init() {
	runsCmd.Flags().BoolVar(&runsList, "list", false, "List the robot's recordings, newest first")
	runsCmd.Flags().BoolVar(&runsText, "text", false, "Print the run instead of opening a page")
	runsCmd.Flags().StringVar(&runsPull, "pull", "", "Copy every recording into this folder")
	runsCmd.Flags().StringVarP(&runsOut, "out", "o", "", "Where to write the page")
	runsCmd.Flags().BoolVar(&runsClear, "clear", false, "Delete the recordings from the robot")
}

func runRuns(cmd *cobra.Command, args []string) error {
	serial, err := requireRobot()
	if err != nil {
		return err
	}

	if runsClear {
		if _, err := adb.Shell(serial, "rm", "-rf", dromos.RobotRunsDir); err != nil {
			return err
		}
		fmt.Println("[OK] The robot's run recordings are gone.")
		return nil
	}

	recs, err := dromos.Runs(serial)
	if err != nil {
		return err
	}
	if len(recs) == 0 {
		fmt.Println("[*] The robot has no ElectroDromos runs recorded.")
		fmt.Println("    Record Auto and the Path Tuner write one each time they run an auto.")
		return nil
	}

	if runsPull != "" {
		for _, rec := range recs {
			if _, err := dromos.Pull(serial, rec, runsPull); err != nil {
				return err
			}
		}
		fmt.Printf("[OK] Copied %d recording(s) into %s\n", len(recs), runsPull)
		fmt.Println("     Drop them on the Run Viewer page of the ElectroDromos docs to compare them.")
		return nil
	}

	if runsList {
		for i, rec := range recs {
			fmt.Printf("  %2d  %s\n", i+1, rec.Name)
		}
		fmt.Println("\n  `epsh runs <number>` opens one.")
		return nil
	}

	rec, err := pickRun(recs, args)
	if err != nil {
		return err
	}

	dir, err := os.MkdirTemp("", "epsh-runs-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	local, err := dromos.Pull(serial, rec, dir)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(local)
	if err != nil {
		return err
	}
	run, err := dromos.ParseRun(data)
	if err != nil {
		return fmt.Errorf("%s: %w", rec.Name, err)
	}

	timeline := dromos.Timeline{Run: run}
	if started, ok := run.Started(dromos.RobotZone(serial)); ok {
		attachRecordings(serial, &timeline, started)
	}

	if runsText {
		for _, line := range run.Lines() {
			fmt.Println(line)
		}
		if timeline.Power != nil {
			fmt.Printf("\n  The power monitor was recording too (%s), lined up by %s.\n", timeline.Power.OpMode, timeline.Aligned)
		}
		if timeline.Profile != nil {
			fmt.Printf("  So was the loop profiler (%s).\n", timeline.Profile.OpMode)
		}
		return nil
	}

	page, err := timeline.Render(runsOut)
	if err != nil {
		return err
	}
	fmt.Printf("[OK] %s\n", rec.Name)
	switch {
	case timeline.Power != nil && timeline.Profile != nil:
		fmt.Println("     With the power monitor's and the loop profiler's recordings on the same clock.")
	case timeline.Power != nil:
		fmt.Println("     With the power monitor's recording on the same clock.")
	case timeline.Profile != nil:
		fmt.Println("     With the loop profiler's recording on the same clock.")
	}
	fmt.Printf("     %s\n", page)
	power.Open(page)
	return nil
}

// pickRun is the recording the argument names: a number from --list, or part
// of a file name, newest first when several match.
func pickRun(recs []dromos.Recording, args []string) (dromos.Recording, error) {
	if len(args) == 0 {
		return recs[0], nil
	}
	if n, err := strconv.Atoi(args[0]); err == nil {
		if n < 1 || n > len(recs) {
			return dromos.Recording{}, fmt.Errorf("there are %d recordings: pick 1 to %d", len(recs), len(recs))
		}
		return recs[n-1], nil
	}
	for _, rec := range recs {
		if strings.Contains(strings.ToLower(rec.Name), strings.ToLower(args[0])) {
			return rec, nil
		}
	}
	return dromos.Recording{}, fmt.Errorf("no recording matches %q: `epsh runs --list` shows them", args[0])
}

// attachRecordings finds what the power monitor and the loop profiler recorded
// during the run, if either was installed.
//
// Each of them writes its file when its OpMode stops, so the one that covers
// the run is among those written after the run ended, and the earliest of those
// is the likeliest. A few are tried, newest last, rather than every file on the
// robot.
func attachRecordings(serial string, tl *dromos.Timeline, runStart time.Time) {
	runEnd := runStart.Add(time.Duration(tl.Run.Seconds() * float64(time.Second)))

	var powerStart time.Time
	if recs, err := power.List(serial); err == nil {
		for _, rec := range after(recs, runEnd, func(r power.Recording) time.Time { return r.When }, func(r power.Recording) bool { return r.Problem }) {
			rep, err := power.Read(serial, rec)
			if err != nil {
				continue
			}
			if tl.Attach(rep, rec.When, runStart) {
				powerStart = rec.When.Add(-time.Duration(rep.Seconds * float64(time.Second)))
				break
			}
		}
	}

	if recs, err := profile.List(serial); err == nil {
		for _, rec := range after(recs, runEnd, func(r profile.Recording) time.Time { return r.When }, func(r profile.Recording) bool { return r.Problem }) {
			rep, err := profile.Read(serial, rec)
			if err != nil {
				continue
			}
			if tl.AttachProfile(rep, runStart, powerStart) {
				break
			}
		}
	}
}

// after is the recordings stamped after a moment, earliest first, at most a
// few of them.
func after[R any](recs []R, moment time.Time, when func(R) time.Time, problem func(R) bool) []R {
	var out []R
	for _, r := range recs {
		// A little slack: the run's stamp is to the second.
		if !problem(r) && !when(r).IsZero() && when(r).After(moment.Add(-3*time.Second)) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return when(out[i]).Before(when(out[j])) })
	if len(out) > 4 {
		out = out[:4]
	}
	return out
}
