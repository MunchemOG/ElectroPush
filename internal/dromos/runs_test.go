package dromos

import (
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MunchemOG/ElectroPush/internal/power"
	"github.com/MunchemOG/ElectroPush/internal/profile"
)

// Written the way RunFile.json writes one: a null for a value the robot could
// not read, and a path end with no heading.
const runFile = `{"format":"electrodromos-run","version":1,"auto":"RedNear","label":"round 2","date":"2026-10-06T15:24:57","full":false,` +
	`"poses":[{"name":"START","designed":[9,60,0],"used":[9,60,0]},{"name":"SCORE","designed":[40,70,0.7854],"used":[40.62,69.4,0.7801]}],` +
	`"paths":[{"from":0,"to":2.5,"segments":[[[9,60],[24,65],[40,70]]],"end":[40,70,null]}],` +
	`"ends":[{"path":0,"stop":true,"dx":0.3,"dy":-0.4,"dh":0.0123,"t":2.51}],` +
	`"arrivals":[{"pose":"SCORE","stop":true,"dx":0.3,"dy":-0.4,"dh":0.0123,"t":2.51}],` +
	`"samples":{"t":[0,1,2,3],"x":[9,20,35,40.3],"y":[60,63,68,69.6],"h":[0,0.3,0.6,0.79],` +
	`"cx":[9,20.5,35,40],"cy":[60,63,68.5,70],"fwd":[0,1,0.6,0],"str":[0,0,0,0],"turn":[0,0.2,0.1,0],` +
	`"v":[12.6,null,12.1,12.4],"path":[0,0,0,0],"seg":[0,0,0,0]}}
`

func TestReadingARunFile(t *testing.T) {
	run, err := ParseRun([]byte(runFile))
	if err != nil {
		t.Fatal(err)
	}
	if run.Auto != "RedNear" || run.Seconds() != 3 {
		t.Errorf("auto %q, %v s", run.Auto, run.Seconds())
	}
	if run.Samples.V[1] != nil {
		t.Error("a null reading has to stay missing, not become 0 V")
	}
	if run.Paths[0].End[2] != nil {
		t.Error("a path end without a heading has to stay without one")
	}

	errs := run.FollowingError()
	if got := *errs[1]; math.Abs(got-0.5) > 1e-9 {
		t.Errorf("off the path by %v at 1 s, want 0.5", got)
	}
	if got := run.Ends[0].Distance(); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("end miss %v, want 0.5", got)
	}

	text := strings.Join(run.Lines(), "\n")
	for _, want := range []string{"RedNear (round 2)", "0.50 in", "SCORE", "12.60V down to 12.10V"} {
		if !strings.Contains(text, want) {
			t.Errorf("summary is missing %q:\n%s", want, text)
		}
	}
}

func TestARunFromSomethingElseIsRefused(t *testing.T) {
	for _, data := range []string{
		`{"format":"pedro-visualizer"}`,
		`{"format":"electrodromos-run","version":9}`,
		`not json`,
	} {
		if _, err := ParseRun([]byte(data)); err == nil {
			t.Errorf("read %q as a run", data)
		}
	}
}

// The stamp is written when the run ends, in the robot's zone, so the start
// is the stamp less the run.
func TestWhenARunStarted(t *testing.T) {
	run, _ := ParseRun([]byte(runFile))
	zone := zoneFromOffset("-0500")
	started, ok := run.Started(zone)
	if !ok {
		t.Fatal("could not read the stamp")
	}
	want := time.Date(2026, 10, 6, 20, 24, 54, 0, time.UTC)
	if !started.Equal(want) {
		t.Errorf("started %v, want %v", started.UTC(), want)
	}

	for _, offset := range []string{"", "EST", "+5"} {
		if zoneFromOffset(offset) != time.Local {
			t.Errorf("%q should fall back to this laptop's zone", offset)
		}
	}
}

// syntheticRun is a 20 s run at 50 Hz whose battery dips twice.
func syntheticRun() Run {
	var r Run
	for i := 0; i <= 1000; i++ {
		t := float64(i) * 0.02
		v := battery(t)
		x, y := t, 0.0
		r.Samples.T = append(r.Samples.T, &t)
		r.Samples.V = append(r.Samples.V, &v)
		r.Samples.X = append(r.Samples.X, &x)
		r.Samples.Y = append(r.Samples.Y, &y)
		r.Samples.CX = append(r.Samples.CX, &x)
		r.Samples.CY = append(r.Samples.CY, &y)
	}
	return r
}

func battery(t float64) float64 {
	return 12.8 - 1.1*math.Exp(-math.Pow(t-5, 2)/0.3) - 0.7*math.Exp(-math.Pow(t-13, 2)/0.5)
}

// The run's stamp is to the second, so the clock alone can put the power lane a
// second off, which is the difference between a spike lining up with the moment
// the robot left its path and not. The battery traces put it right.
func TestTheBatteryLinesUpWhatTheClockCannot(t *testing.T) {
	run := syntheticRun()

	// The monitor started 4 s before the run, and the clock thinks 4.8 s.
	const truth = -4.0
	var s power.Series
	for i := 0; i <= 300; i++ {
		pt := float64(i) * 0.1
		s.Times = append(s.Times, pt)
		// The monitor reads the battery a little higher, through a different path.
		s.Volts = append(s.Volts, battery(pt+truth)+0.15)
	}
	// One row per device, one value per tick.
	s.Names = []string{"shooter"}
	s.Amps = [][]float64{make([]float64, len(s.Times))}

	got, ok := matchBattery(run, s, -4.8)
	if !ok {
		t.Fatal("did not match a battery that moved")
	}
	if math.Abs(got-truth) > 0.05 {
		t.Errorf("offset %.2f, want %.2f", got, truth)
	}

	// A battery that never moved has nothing to match, and the clock stands.
	flat := syntheticRun()
	for i := range flat.Samples.V {
		v := 12.5
		flat.Samples.V[i] = &v
	}
	if got, ok := matchBattery(flat, s, -4.8); ok || got != -4.8 {
		t.Errorf("matched a flat battery: %v, %v", got, ok)
	}
}

func TestOnlyARecordingThatCoversTheRunIsAttached(t *testing.T) {
	runStart := time.Date(2026, 10, 6, 15, 0, 0, 0, time.UTC)
	rep := power.Report{OpMode: "Tuning", Seconds: 30}
	for i := 0; i < 10; i++ {
		rep.Series.Times = append(rep.Series.Times, float64(i))
		rep.Series.Volts = append(rep.Series.Volts, 12)
	}
	rep.Series.Names = []string{"shooter"}
	rep.Series.Amps = [][]float64{make([]float64, len(rep.Series.Times))}

	tl := Timeline{Run: syntheticRun()}
	// Ended an hour before the run: a different session.
	if tl.Attach(rep, runStart.Add(-time.Hour), runStart) {
		t.Error("attached a recording from an hour earlier")
	}
	// Ran from 10 s before the run to 20 s into it.
	if !tl.Attach(rep, runStart.Add(20*time.Second), runStart) {
		t.Fatal("did not attach a recording that covers the run")
	}
	if tl.PowerOffset != -10 || tl.Aligned != "clock" {
		t.Errorf("offset %v aligned %q", tl.PowerOffset, tl.Aligned)
	}
}

// The loop lane names what the team can change: the deepest of their own
// methods the thread was inside, not the SDK call at the bottom of it.
func TestTheLoopLaneNamesTheTeamsOwnCode(t *testing.T) {
	rep := profile.Report{
		OpMode: "Tuning", Period: 10 * time.Millisecond, Started: time.Unix(100, 0),
		Frames: []profile.Frame{
			{ID: 0, Name: "all"},
			{ID: 1, Parent: 0, Name: "org.firstinspires.ftc.teamcode.RedNear.loop"},
			{ID: 2, Parent: 1, Name: "com.qualcomm.hardware.lynx.LynxModule.getBulkData"},
			{ID: 3, Parent: 0, Name: "com.qualcomm.robotcore.eventloop.EventLoopManager.run"},
		},
		Timeline: []profile.Slice{{Frame: 2, Samples: 30}, {Frame: 1, Samples: 20}, {Frame: 3, Samples: 10}},
	}

	lane := profileLane(rep, 1.5)
	if len(lane.Slices) != 2 {
		t.Fatalf("got %+v", lane.Slices)
	}
	if s := lane.Slices[0]; s.Name != "RedNear.loop" || !s.Team || s.From != 1.5 || s.To != 2 {
		t.Errorf("first stretch %+v: the bulk read was inside RedNear.loop, for 0.5 s", s)
	}
	if s := lane.Slices[1]; s.Team || s.Name != "EventLoopManager.run" {
		t.Errorf("second stretch %+v", s)
	}
}

func TestThePageCarriesTheRun(t *testing.T) {
	run, _ := ParseRun([]byte(runFile))
	out := t.TempDir() + "/run.html"
	path, err := Timeline{Run: run}.Render(out)
	if err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<title>RedNear · round 2 - epsh run</title>", `"auto":"RedNear"`, `"err":[0,0.5,`} {
		if !strings.Contains(string(page), want) {
			t.Errorf("page is missing %q", want)
		}
	}
	if strings.Contains(string(page), "<script>alert") {
		t.Error("data escaped into markup")
	}
}
