package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/andreibanu/pusher/internal/adb"
	"github.com/andreibanu/pusher/internal/pathtrace"
	"github.com/spf13/cobra"
)

var (
	visFile     string
	visOut      string
	visNoOpen   bool
	visProject  string
	visTopSpeed float64
	visAccel    float64
	visDecel    float64
	visLatAccel float64
)

var visualiseCmd = &cobra.Command{
	Use:     "visualiser [OpMode class name]",
	Aliases: []string{"visualizer", "vis"},
	Short:   "Draw the path an autonomous actually drove, coloured by speed",
	Long: `Pulls a blob path trace off the robot and renders it as an HTML page: the
whole flow of the auto, every curve coloured by modelled speed, and a duration
estimate next to the measured time.

Traces are written by the blob library when BlobParams.recordTrace is on, which
requires the blob-dev artifact. Competition builds cannot record at all.

  pusher visualiser CloseBlue      render the newest trace for that OpMode
  pusher visualiser                render the newest trace on the robot
  pusher visualiser --file t.json  render a trace you already have`,
	RunE: runVisualise,
}

func runVisualise(cmd *cobra.Command, args []string) error {
	name := ""
	if len(args) > 0 {
		name = args[0]
	}

	local := visFile
	if local == "" {
		pulled, err := pullTrace(name)
		if err != nil {
			return err
		}
		local = pulled
	}

	trace, err := pathtrace.Load(local)
	if err != nil {
		return err
	}

	root := visProject
	if root == "" {
		root, _ = os.Getwd()
	}
	trace.Annotate(root)

	limits := pathtrace.DefaultLimits()
	if visTopSpeed > 0 {
		limits.TopSpeed = visTopSpeed
	}
	if visAccel > 0 {
		limits.Accel = visAccel
	}
	if visDecel > 0 {
		limits.Decel = visDecel
	}
	if visLatAccel > 0 {
		limits.LatAccel = visLatAccel
	}
	trace.Profile(limits)

	out := visOut
	if out == "" {
		out = filepath.Join(os.TempDir(), fmt.Sprintf("pusher-%s.html", safe(trace.OpMode)))
	}
	if err := trace.Render(out, limits); err != nil {
		return err
	}

	est, actual := trace.Totals()
	fmt.Printf("%s: %d segments, %.2fs measured, %.2fs estimated\n",
		trace.OpMode, len(trace.Segments), actual, est)
	fmt.Println(out)

	if !visNoOpen {
		openBrowser(out)
	}
	return nil
}

// pullTrace finds the newest matching trace on the robot and copies it locally.
func pullTrace(name string) (string, error) {
	if !adb.IsInstalled() {
		return "", fmt.Errorf("adb not found - install Android SDK Platform-Tools")
	}

	serial, err := pickDevice()
	if err != nil {
		return "", err
	}

	traces, err := adb.ListTraces(serial)
	if err != nil {
		return "", err
	}
	if len(traces) == 0 {
		return "", fmt.Errorf("no traces on the robot in %s\n"+
			"Record one first: depend on blob-dev, set BlobParams.recordTrace = true, "+
			"and call blob.saveTrace() from the OpMode's stop()", adb.TraceDir)
	}

	hits := adb.MatchTraces(traces, name)
	if len(hits) == 0 {
		return "", fmt.Errorf("no trace for %q on the robot\navailable: %s",
			name, strings.Join(adb.OpModeNames(traces), ", "))
	}

	newest := hits[0]
	local := filepath.Join(os.TempDir(), newest.Name)
	if err := adb.Pull(serial, newest.Path, local); err != nil {
		return "", err
	}

	return local, nil
}

// USB wins over Wi-Fi, same preference the deploy path uses.
func pickDevice() (string, error) {
	if dev, ok := adb.FindUSBDevice(); ok {
		return dev.Serial, nil
	}
	if adb.IsConnected() {
		return adb.RobotAddr(), nil
	}
	return "", fmt.Errorf("no robot connected - plug in USB or run `pusher connect`")
}

func safe(s string) string {
	if s == "" {
		return "trace"
	}
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ' ' || r == ':' {
			return '-'
		}
		return r
	}, s)
}

func openBrowser(path string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", path)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		c = exec.Command("xdg-open", path)
	}
	c.Start()
}

func init() {
	visualiseCmd.Flags().StringVar(&visFile, "file", "", "Render a local trace instead of pulling one")
	visualiseCmd.Flags().StringVarP(&visOut, "out", "o", "", "Where to write the HTML")
	visualiseCmd.Flags().BoolVar(&visNoOpen, "no-open", false, "Do not open the result in a browser")
	visualiseCmd.Flags().StringVar(&visProject, "project", "", "Project root used to map segments to source lines")
	visualiseCmd.Flags().Float64Var(&visTopSpeed, "top-speed", 0, "Drivetrain top speed at full power, in/s")
	visualiseCmd.Flags().Float64Var(&visAccel, "accel", 0, "Acceleration limit, in/s^2")
	visualiseCmd.Flags().Float64Var(&visDecel, "decel", 0, "Deceleration limit, in/s^2")
	visualiseCmd.Flags().Float64Var(&visLatAccel, "lat-accel", 0, "Lateral grip limit, in/s^2")
}
