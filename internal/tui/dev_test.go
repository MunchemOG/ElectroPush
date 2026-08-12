package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// The menu showed "starting" for the whole benchmark because progress was
// collected and never delivered. A quarter of an hour of that is a freeze.
func TestProgressReachesTheMenu(t *testing.T) {
	m := &devModel{height: 40, busy: "starting", started: time.Now()}

	post("epsh, delta transfer (2/3)")

	msg := waitForProgress()
	model, cmd := m.Update(msg)

	if got := model.(*devModel).busy; got != "epsh, delta transfer (2/3)" {
		t.Errorf("got %q", got)
	}
	if cmd == nil {
		t.Error("the menu stopped listening after one update")
	}
}

// The elapsed counter is what tells someone it is alive rather than wedged.
func TestElapsedAdvancesWhileBusy(t *testing.T) {
	m := &devModel{height: 40, busy: "working", started: time.Now().Add(-90 * time.Second)}

	model, cmd := m.Update(devTickMsg(time.Now()))
	if got := model.(*devModel).elapsed; got < 89*time.Second {
		t.Errorf("got %v", got)
	}
	if cmd == nil {
		t.Error("the tick did not re-arm")
	}

	view := model.(*devModel).View()
	if !contains(view, "elapsed") {
		t.Errorf("the view does not show elapsed time:\n%s", view)
	}

	// Idle must not keep ticking.
	m.busy = ""
	if _, cmd := m.Update(devTickMsg(time.Now())); cmd != nil {
		t.Error("the tick kept running with nothing to report")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

var _ tea.Model = (*devModel)(nil)

// A measuring tool must not be able to leave the robot broken. Reloading onto a
// robot whose APK does not match the project puts classes that were kept out of
// the reload in neither place, and the robot then crashes on init resolving
// them. That is exactly what happened: a benchmark run reloaded after the keep
// list changed but before the APK carrying those classes had been installed.
func TestTheBenchmarkRefusesWhenAReloadWouldNotBeValid(t *testing.T) {
	m := &devModel{height: 40}

	// No robot at all is the simplest case of the same rule.
	if cmd := m.benchExtreme(); cmd != nil {
		t.Error("the benchmark started with no robot connected")
	}
	if m.err == nil {
		t.Error("no reason was given")
	}
}
