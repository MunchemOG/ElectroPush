package dromos

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MunchemOG/ElectroPush/internal/power"
	"github.com/MunchemOG/ElectroPush/internal/profile"
)

// Three recordings of the same stretch of time, made by three things that do
// not know about each other: ElectroDromos's run, epsh's power monitor and its
// loop profiler. Each is useful alone. Together they answer the question none
// of them can: when the robot came off its path, what were the battery and the
// loop doing at that moment.
//
// All three are stamped by the robot's own clock, so lining them up is
// arithmetic. The run's stamp is the coarse one, to the second and taken once
// the run was over, so where the power monitor recorded the battery too, the
// two battery traces are slid against each other to find the offset at which
// they agree. The profiler's stamps are as fine as the monitor's, so it follows
// whichever offset the monitor's recording got.

// Timeline is one run with whatever else the robot recorded during it.
type Timeline struct {
	Run Run

	Power *power.Report
	// PowerOffset is added to a power sample's time to put it on the run's
	// clock.
	PowerOffset float64

	Profile       *profile.Report
	ProfileOffset float64

	// Aligned says how: "battery" when the two battery traces were matched,
	// "clock" when only the robot's stamps were used.
	Aligned string
}

// alignWindow is how far the battery match may move the clock's answer. The
// run's stamp is to the second and taken after the run was written, so the
// truth is within a couple of seconds of it.
const alignWindow = 2.5

// Attach adds a power recording, when it covers the run.
func (tl *Timeline) Attach(rep power.Report, ended, runStart time.Time) bool {
	if !rep.Series.Any() {
		return false
	}
	started := ended.Add(-time.Duration(rep.Seconds * float64(time.Second)))
	offset := started.Sub(runStart).Seconds()
	if !overlaps(0, tl.Run.Seconds(), offset, offset+rep.Seconds) {
		return false
	}

	tl.Power = &rep
	tl.PowerOffset = offset
	tl.Aligned = "clock"
	if better, ok := matchBattery(tl.Run, rep.Series, offset); ok {
		tl.PowerOffset = better
		tl.Aligned = "battery"
	}
	return true
}

// AttachProfile adds a loop profile, when it covers the run.
func (tl *Timeline) AttachProfile(rep profile.Report, runStart time.Time, powerStart time.Time) bool {
	if rep.Started.IsZero() || len(rep.Timeline) == 0 {
		return false
	}
	offset := rep.Started.Sub(runStart).Seconds()
	// The monitor and the profiler are stamped by the same clock to the
	// millisecond, so once the battery has placed the monitor, the profiler
	// sits where it says relative to that.
	if tl.Power != nil && tl.Aligned == "battery" && !powerStart.IsZero() {
		offset = tl.PowerOffset + rep.Started.Sub(powerStart).Seconds()
	}
	if !overlaps(0, tl.Run.Seconds(), offset, offset+rep.Duration.Seconds()) {
		return false
	}
	tl.Profile = &rep
	tl.ProfileOffset = offset
	if tl.Aligned == "" {
		tl.Aligned = "clock"
	}
	return true
}

func overlaps(a0, a1, b0, b1 float64) bool { return a0 < b1 && b0 < a1 }

// matchBattery slides the monitor's battery trace against the run's and
// returns the offset at which they agree best.
//
// Compared with each trace's own mean taken away, because the two read the
// battery through different paths and need not agree on its level, only on
// its shape. A battery that never moved has no shape to match, and the
// clock's answer stands.
func matchBattery(run Run, s power.Series, clock float64) (float64, bool) {
	var ts, vs []float64
	for i := range run.Samples.T {
		if i < len(run.Samples.V) && run.Samples.T[i] != nil && run.Samples.V[i] != nil && *run.Samples.V[i] > 0 {
			ts = append(ts, *run.Samples.T[i])
			vs = append(vs, *run.Samples.V[i])
		}
	}
	if len(ts) < 20 || len(s.Times) < 20 || len(s.Volts) != len(s.Times) || spread(vs) < 0.05 {
		return clock, false
	}

	best, bestErr := clock, math.Inf(1)
	for offset := clock - alignWindow; offset <= clock+alignWindow; offset += 0.02 {
		var a, b []float64
		for i, t := range ts {
			if v, ok := interpolate(s.Times, s.Volts, t-offset); ok {
				a = append(a, vs[i])
				b = append(b, v)
			}
		}
		if len(a) < 20 {
			continue
		}
		ma, mb := mean(a), mean(b)
		sum := 0.0
		for i := range a {
			d := (a[i] - ma) - (b[i] - mb)
			sum += d * d
		}
		if e := sum / float64(len(a)); e < bestErr {
			best, bestErr = offset, e
		}
	}
	if math.IsInf(bestErr, 1) {
		return clock, false
	}
	return best, true
}

func interpolate(xs, ys []float64, x float64) (float64, bool) {
	if len(xs) == 0 || x < xs[0] || x > xs[len(xs)-1] {
		return 0, false
	}
	lo, hi := 0, len(xs)-1
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if xs[mid] <= x {
			lo = mid
		} else {
			hi = mid
		}
	}
	if xs[hi] == xs[lo] {
		return ys[lo], true
	}
	f := (x - xs[lo]) / (xs[hi] - xs[lo])
	return ys[lo] + f*(ys[hi]-ys[lo]), true
}

func mean(xs []float64) float64 {
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func spread(xs []float64) float64 {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, x := range xs {
		lo, hi = math.Min(lo, x), math.Max(hi, x)
	}
	return hi - lo
}

// The page draws itself from this, in the browser, for the same reason the
// power page does: a graph that zooms and scrubs answers the follow-up
// question, and a picture drawn before it was asked does not.
type pageData struct {
	Auto    string                `json:"auto"`
	Label   string                `json:"label"`
	Date    string                `json:"date"`
	Seconds float64               `json:"seconds"`
	Full    bool                  `json:"full"`
	Field   float64               `json:"field"`
	Samples map[string][]*float64 `json:"s"`
	Paths   []Path                `json:"paths"`
	Poses   []Pose                `json:"poses"`
	Ends    []Miss                `json:"ends"`
	Arrives []Miss                `json:"arrivals"`
	Power   *powerLane            `json:"power,omitempty"`
	Loop    *loopLane             `json:"loop,omitempty"`
	Aligned string                `json:"aligned"`
}

type powerLane struct {
	Names  []string    `json:"names"`
	Times  []float64   `json:"t"`
	Amps   [][]float64 `json:"a"`
	Volts  []float64   `json:"v"`
	Total  []float64   `json:"total"`
	OpMode string      `json:"opmode"`
}

type loopLane struct {
	OpMode string      `json:"opmode"`
	Period float64     `json:"period"`
	Slices []loopSlice `json:"slices"`
}

type loopSlice struct {
	From float64 `json:"a"`
	To   float64 `json:"b"`
	Name string  `json:"n"`
	Team bool    `json:"team"`
}

// fieldSize is Pedro's field, which the Run Viewer and the Pedro Visualizer
// both draw.
const fieldSize = 144.0

func (tl Timeline) data() pageData {
	r := tl.Run
	d := pageData{
		Auto: r.Auto, Label: r.Label, Date: strings.Replace(r.Date, "T", " ", 1),
		Seconds: r.Seconds(), Full: r.Full, Field: fieldSize,
		Paths: r.Paths, Poses: r.Poses, Ends: r.Ends, Arrives: r.Arrives,
		Aligned: tl.Aligned,
		Samples: map[string][]*float64{
			"t": r.Samples.T, "x": r.Samples.X, "y": r.Samples.Y, "h": r.Samples.H,
			"cx": r.Samples.CX, "cy": r.Samples.CY, "err": r.FollowingError(),
			"fwd": r.Samples.Fwd, "str": r.Samples.Str, "turn": r.Samples.Turn,
			"v": r.Samples.V, "path": r.Samples.Path,
		},
	}

	if p := tl.Power; p != nil {
		lane := &powerLane{OpMode: p.OpMode}
		for _, t := range p.Series.Times {
			lane.Times = append(lane.Times, round(t+tl.PowerOffset, 3))
		}
		lane.Volts = roundAll(p.Series.Volts)
		motors := p.Motors()
		lane.Total = roundAll(p.Series.Combined(motors))
		for _, m := range motors {
			if series := p.Series.Of(m.Name); series != nil {
				lane.Names = append(lane.Names, m.Name)
				lane.Amps = append(lane.Amps, roundAll(series))
			}
		}
		d.Power = lane
	}

	if p := tl.Profile; p != nil {
		d.Loop = profileLane(*p, tl.ProfileOffset)
	}

	return d
}

// profileLane turns the profiler's run of samples into stretches of time, each
// named after the team's own method it was inside, which is the name somebody
// can go and change.
func profileLane(p profile.Report, offset float64) *loopLane {
	at := map[int]int{}
	for i, f := range p.Frames {
		at[f.ID] = i
	}

	label := map[int]loopSlice{}
	name := func(id int) loopSlice {
		if s, ok := label[id]; ok {
			return s
		}
		leaf := ""
		for depth, cur := 0, id; depth < 512; depth++ {
			i, ok := at[cur]
			if !ok {
				break
			}
			f := p.Frames[i]
			if leaf == "" {
				leaf = f.Short()
			}
			if f.Team() {
				s := loopSlice{Name: f.Short(), Team: true}
				label[id] = s
				return s
			}
			if f.ID == 0 {
				break
			}
			cur = f.Parent
		}
		s := loopSlice{Name: leaf}
		label[id] = s
		return s
	}

	lane := &loopLane{OpMode: p.OpMode, Period: p.Period.Seconds()}
	t := offset
	for _, slice := range p.Timeline {
		length := p.Seconds(slice.Samples)
		s := name(slice.Frame)
		s.From, s.To = round(t, 3), round(t+length, 3)
		// Neighbours in the same team method are one stretch, which is what a
		// lane can actually draw at this width.
		if n := len(lane.Slices); n > 0 && lane.Slices[n-1].Name == s.Name && lane.Slices[n-1].Team == s.Team {
			lane.Slices[n-1].To = s.To
		} else {
			lane.Slices = append(lane.Slices, s)
		}
		t += length
	}
	return lane
}

func round(x float64, places int) float64 {
	scale := math.Pow(10, float64(places))
	return math.Round(x*scale) / scale
}

func roundAll(xs []float64) []float64 {
	out := make([]float64, len(xs))
	for i, x := range xs {
		out[i] = round(x, 2)
	}
	return out
}

// Render writes the page and returns where it went.
func (tl Timeline) Render(out string) (string, error) {
	if out == "" {
		name := strings.Map(func(r rune) rune {
			if r == '/' || r == '\\' || r == ' ' {
				return '-'
			}
			return r
		}, tl.Run.Auto)
		out = filepath.Join(os.TempDir(), fmt.Sprintf("epsh-run-%s.html", name))
	}

	data, err := json.Marshal(tl.data())
	if err != nil {
		return "", err
	}

	tmpl, err := template.New("run").Parse(runPage)
	if err != nil {
		return "", err
	}
	f, err := os.Create(out)
	if err != nil {
		return "", err
	}
	defer f.Close()

	title := tl.Run.Auto
	if tl.Run.Label != "" {
		title += " · " + tl.Run.Label
	}
	err = tmpl.Execute(f, struct {
		Title string
		Data  template.JS
	}{title, template.JS(data)})
	return out, err
}
