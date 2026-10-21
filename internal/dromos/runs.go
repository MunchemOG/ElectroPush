package dromos

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MunchemOG/ElectroPush/internal/adb"
)

// A run is one recording of an auto, written by Record Auto or by each round
// of the Path Tuner: where the robot was every loop, where the path said it
// should be, the drive powers and the battery. The docs site's Run Viewer reads
// the same files; epsh reads them so a run can be looked at next to what the
// power monitor and the loop profiler saw during it, on one clock.

// Recording is a run file on the robot.
type Recording struct {
	Name string
	Path string
	// Auto is the auto's name, from the file name.
	Auto string
}

// Runs lists the robot's recordings, newest first.
func Runs(serial string) ([]Recording, error) {
	out, err := adb.Shell(serial, "ls", "-t", RobotRunsDir, "2>/dev/null", "||", "true")
	if err != nil {
		return nil, fmt.Errorf("cannot look in %s: %w", RobotRunsDir, err)
	}

	var found []Recording
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		auto := strings.TrimSuffix(name, ".json")
		// <auto>-yyyyMMdd-HHmmss[-label]
		if i := strings.Index(auto, "-"); i > 0 {
			auto = auto[:i]
		}
		found = append(found, Recording{Name: name, Path: RobotRunsDir + "/" + name, Auto: auto})
	}
	return found, nil
}

// Pull copies one recording off the robot.
func Pull(serial string, rec Recording, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	local := filepath.Join(dir, rec.Name)
	if err := adb.Pull(serial, rec.Path, local); err != nil {
		return "", fmt.Errorf("cannot pull %s: %w", rec.Name, err)
	}
	return local, nil
}

// Run is a recording as RunFile writes it (format "electrodromos-run").
type Run struct {
	Format  string  `json:"format"`
	Version int     `json:"version"`
	Auto    string  `json:"auto"`
	Label   string  `json:"label"`
	Date    string  `json:"date"`
	Full    bool    `json:"full"`
	Poses   []Pose  `json:"poses"`
	Paths   []Path  `json:"paths"`
	Ends    []Miss  `json:"ends"`
	Arrives []Miss  `json:"arrivals"`
	Samples Samples `json:"samples"`
}

// Pose is one of the auto's named poses: as designed, and as the run used it
// after the Path Tuner's corrections.
type Pose struct {
	Name     string     `json:"name"`
	Designed []*float64 `json:"designed"`
	Used     []*float64 `json:"used"`
}

// Path is one planned path, drawn as lines, and the time it was followed.
type Path struct {
	From     float64        `json:"from"`
	To       float64        `json:"to"`
	Segments [][][]*float64 `json:"segments"`
	End      []*float64     `json:"end"`
}

// Miss is how far off a path ended, or a pose was arrived at.
type Miss struct {
	Path int     `json:"path"`
	Pose string  `json:"pose"`
	Stop bool    `json:"stop"`
	DX   float64 `json:"dx"`
	DY   float64 `json:"dy"`
	DH   float64 `json:"dh"`
	T    float64 `json:"t"`
}

// Distance is the miss in inches.
func (m Miss) Distance() float64 { return math.Hypot(m.DX, m.DY) }

// Samples is the recording itself, one column per value. A value the robot
// could not read is null.
type Samples struct {
	T    []*float64 `json:"t"`
	X    []*float64 `json:"x"`
	Y    []*float64 `json:"y"`
	H    []*float64 `json:"h"`
	CX   []*float64 `json:"cx"`
	CY   []*float64 `json:"cy"`
	Fwd  []*float64 `json:"fwd"`
	Str  []*float64 `json:"str"`
	Turn []*float64 `json:"turn"`
	V    []*float64 `json:"v"`
	Path []*float64 `json:"path"`
	Seg  []*float64 `json:"seg"`
}

// ParseRun reads a recording.
func ParseRun(data []byte) (Run, error) {
	var r Run
	if err := json.Unmarshal(data, &r); err != nil {
		return r, fmt.Errorf("not a run recording: %w", err)
	}
	if r.Format != "electrodromos-run" {
		return r, fmt.Errorf("not an ElectroDromos run (format %q)", r.Format)
	}
	if r.Version > 1 {
		return r, fmt.Errorf("written by a newer ElectroDromos (version %d) than this epsh reads: update epsh", r.Version)
	}
	return r, nil
}

// Seconds is how long the run lasted.
func (r Run) Seconds() float64 {
	for i := len(r.Samples.T) - 1; i >= 0; i-- {
		if v := r.Samples.T[i]; v != nil {
			return *v
		}
	}
	return 0
}

// FollowingError is how far the robot was from the path, at every sample.
func (r Run) FollowingError() []*float64 {
	s := r.Samples
	out := make([]*float64, len(s.T))
	for i := range out {
		if i < len(s.X) && i < len(s.CX) && s.X[i] != nil && s.Y[i] != nil && s.CX[i] != nil && s.CY[i] != nil {
			e := math.Hypot(*s.X[i]-*s.CX[i], *s.Y[i]-*s.CY[i])
			out[i] = &e
		}
	}
	return out
}

// Started is when the run began, on the robot's clock.
//
// RunFile stamps the file when it is written, right after the run ends, in the
// robot's local time with no zone. So the start is that stamp less the run's
// length, in the zone the robot reports.
func (r Run) Started(zone *time.Location) (time.Time, bool) {
	if zone == nil {
		zone = time.Local
	}
	written, err := time.ParseInLocation("2006-01-02T15:04:05", r.Date, zone)
	if err != nil {
		return time.Time{}, false
	}
	return written.Add(-time.Duration(r.Seconds() * float64(time.Second))), true
}

// RobotZone is the robot's time zone, which its run stamps are written in.
func RobotZone(serial string) *time.Location {
	out, err := adb.Shell(serial, "date", "+%z")
	if err != nil {
		return time.Local
	}
	return zoneFromOffset(strings.TrimSpace(out))
}

func zoneFromOffset(offset string) *time.Location {
	if len(offset) != 5 || (offset[0] != '+' && offset[0] != '-') {
		return time.Local
	}
	var hours, minutes int
	if _, err := fmt.Sscanf(offset[1:], "%02d%02d", &hours, &minutes); err != nil {
		return time.Local
	}
	seconds := hours*3600 + minutes*60
	if offset[0] == '-' {
		seconds = -seconds
	}
	return time.FixedZone(offset, seconds)
}

// Lines is the run as text, for --text and for a terminal.
func (r Run) Lines() []string {
	var out []string
	title := r.Auto
	if r.Label != "" {
		title += " (" + r.Label + ")"
	}
	out = append(out, fmt.Sprintf("%s, %.1fs, recorded %s", title, r.Seconds(), strings.Replace(r.Date, "T", " ", 1)))
	if r.Full {
		out = append(out, "  The recording filled up, so the end of the run is missing.")
	}

	if len(r.Ends) > 0 {
		out = append(out, "", "  path   ended off by      heading     at")
		for _, e := range r.Ends {
			stop := ""
			if e.Stop {
				stop = "  (stopped)"
			}
			out = append(out, fmt.Sprintf("  %4d   %6.2f in  %8.1f°  %6.1fs%s",
				e.Path+1, e.Distance(), e.DH*180/math.Pi, e.T, stop))
		}
	}
	if len(r.Arrives) > 0 {
		out = append(out, "", "  pose                 arrived off by   heading")
		for _, a := range r.Arrives {
			out = append(out, fmt.Sprintf("  %-20s %8.2f in %8.1f°", a.Pose, a.Distance(), a.DH*180/math.Pi))
		}
	}

	worst, mean, n := 0.0, 0.0, 0
	for _, e := range r.FollowingError() {
		if e != nil {
			worst = math.Max(worst, *e)
			mean += *e
			n++
		}
	}
	if n > 0 {
		out = append(out, "", fmt.Sprintf("  Off the path by %.2f in on average, %.2f in at worst.", mean/float64(n), worst))
	}

	low, high := math.Inf(1), math.Inf(-1)
	for _, v := range r.Samples.V {
		if v != nil && *v > 0 {
			low, high = math.Min(low, *v), math.Max(high, *v)
		}
	}
	if !math.IsInf(low, 0) {
		out = append(out, fmt.Sprintf("  Battery %.2fV down to %.2fV.", high, low))
	}
	return out
}
