package tui

import (
	"fmt"
	"strings"

	"github.com/andreibanu/pusher/internal/adb"
	"github.com/andreibanu/pusher/internal/blobdep"
	"github.com/andreibanu/pusher/internal/pathtrace"
	"github.com/andreibanu/pusher/internal/visual"
	tea "github.com/charmbracelet/bubbletea"
)

// blobState is everything the blob screens need, refreshed on entry.
type blobState struct {
	// pickerOnly is set when the runs list was opened directly by
	// `pusher visualiser`, so esc leaves the program instead of walking up into
	// settings the user never asked for.
	pickerOnly bool

	dep     *blobdep.Dep
	latest  string
	traces  []adb.RemoteTrace
	serial  string
	tracErr error

	// limits the renderer runs with. Defaults unless `pusher visualiser` was
	// given tuning flags to pass down.
	limits pathtrace.Limits
}

// Menu rows when blob is installed.
var blobItems = []string{
	"Build variant",
	"Version",
	"Recorded runs",
	"Back",
}

// Menu rows when it is not.
var blobMissingItems = []string{
	"Add blob to build.gradle",
	"Back",
}

// RunTracePicker opens the recorded-runs list on its own, for `pusher visualiser`
// with no arguments. projectRoot and lim carry the command's flags, which would
// otherwise be dropped on the way into the picker.
func RunTracePicker(projectRoot string, lim pathtrace.Limits) error {
	m, err := NewSettingsModel()
	if err != nil {
		return err
	}
	if projectRoot != "" {
		m.root = projectRoot
	}
	m.blob.limits = lim

	m.loadTraces()
	m.blob.pickerOnly = true
	m.screen = screenBlobRuns

	_, err = tea.NewProgram(m).Run()
	return err
}

func (m *SettingsModel) refreshBlob() {
	dep, err := blobdep.Detect(m.projectRoot())
	if err != nil {
		// Not an FTC project, or no TeamCode/build.gradle. Treat as "not installed"
		// rather than an error: the menu still offers to add it.
		m.blob.dep = nil
		return
	}
	m.blob.dep = dep
}

func (m *SettingsModel) blobMenuItems() []string {
	if m.blob.dep == nil {
		return blobMissingItems
	}
	return blobItems
}

// blobLabel is the value shown next to "blob library" on the main menu.
func (m *SettingsModel) blobLabel() string {
	if m.blob.dep == nil {
		return "not installed"
	}
	variant := "comp"
	if m.blob.dep.IsDev() {
		variant = "dev"
	}
	return m.blob.dep.Version + " (" + variant + ")"
}

func (m *SettingsModel) updateBlob(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.blobMenuItems()

	switch key.String() {
	case "esc", "q", "left", "h":
		m.goTo(screenMain, 0)
		m.status = ""

	case "up", "k":
		m.moveCursor(-1, len(items))
	case "down", "j":
		m.moveCursor(1, len(items))

	case "enter", " ", "right", "l":
		m.status = ""
		m.err = nil

		if m.blob.dep == nil {
			switch m.cursor {
			case 0:
				m.addBlob()
			case 1:
				m.goTo(screenMain, 0)
			}
			return m, nil
		}

		switch m.cursor {
		case 0:
			m.toggleBlobVariant()
		case 1:
			m.updateBlobVersion()
		case 2:
			m.loadTraces()
			m.goTo(screenBlobRuns, 0)
		case 3:
			m.goTo(screenMain, 0)
		}
	}

	return m, nil
}

func (m *SettingsModel) toggleBlobVariant() {
	target := blobdep.ArtifactDev
	if m.blob.dep.IsDev() {
		target = blobdep.ArtifactComp
	}

	if err := blobdep.SetArtifact(m.projectRoot(), target); err != nil {
		m.err = err
		return
	}
	m.refreshBlob()

	if target == blobdep.ArtifactDev {
		m.status = "Switched to dev. Records traces. Do not take this to a match. Gradle sync + redeploy."
	} else {
		m.status = "Switched to competition. No logging code in the APK. Gradle sync + redeploy."
	}
}

func (m *SettingsModel) updateBlobVersion() {
	latest, err := blobdep.LatestVersion()
	if err != nil {
		m.err = err
		return
	}
	m.blob.latest = latest

	if latest == m.blob.dep.Version {
		m.status = "Already on " + latest
		return
	}

	previous := m.blob.dep.Version
	if err := blobdep.SetVersion(m.projectRoot(), latest); err != nil {
		m.err = err
		return
	}
	m.refreshBlob()
	m.status = fmt.Sprintf("Updated %s to %s. Gradle sync to pick it up.", previous, latest)
}

func (m *SettingsModel) addBlob() {
	version, err := blobdep.LatestVersion()
	if err != nil {
		// Offline is not a reason to refuse: pin something sane and say so.
		version = "2.0.0"
	}

	warning, addErr := blobdep.Add(m.projectRoot(), blobdep.ArtifactComp, version)
	if addErr != nil {
		m.err = addErr
		return
	}
	m.refreshBlob()

	m.status = fmt.Sprintf("Added blob %s (competition build). Gradle sync to pick it up.", version)
	if warning != "" {
		m.status += " " + warning
	}
}

func (m *SettingsModel) loadTraces() {
	serial, traces, err := visual.List()
	m.blob.serial = serial
	m.blob.traces = traces
	m.blob.tracErr = err
}

func (m *SettingsModel) updateBlobRuns(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc", "q", "left", "h":
		if m.blob.pickerOnly {
			m.quit = true
			return m, tea.Quit
		}
		m.goTo(screenBlob, 2)
		m.status = ""

	case "r":
		m.loadTraces()
		m.cursor = 0

	case "up", "k":
		m.moveCursor(-1, len(m.blob.traces))
	case "down", "j":
		m.moveCursor(1, len(m.blob.traces))

	case "enter", " ":
		if len(m.blob.traces) == 0 {
			return m, nil
		}

		trace := m.blob.traces[m.cursor]
		out, err := visual.Render(m.blob.serial, trace, m.projectRoot(), "", m.blob.limits)
		if err != nil {
			m.err = err
			return m, nil
		}
		visual.Open(out)
		m.status = "Opened " + out
	}

	return m, nil
}

func (m *SettingsModel) viewBlob() string {
	var b strings.Builder

	if m.blob.dep == nil {
		b.WriteString(helpStyle.Render("  blob is not in TeamCode/build.gradle.") + "\n\n")
		b.WriteString(m.renderList(len(blobMissingItems), func(i int) string {
			return renderRow(i == m.cursor, blobMissingItems[i], "", 29)
		}))
		b.WriteString("\n" + helpStyle.Render("  Adds the competition build, which carries no logging code.") + "\n")
		b.WriteString(helpStyle.Render("  ↑/↓ move · enter select · esc back") + "\n")
		return b.String()
	}

	latest := m.blob.latest
	if latest == "" {
		latest = "press enter to check"
	} else if latest == m.blob.dep.Version {
		latest = m.blob.dep.Version + " (latest)"
	} else {
		latest = m.blob.dep.Version + " -> " + latest
	}

	values := []string{
		m.blob.dep.VariantName(),
		latest,
		"",
		"",
	}

	b.WriteString(m.renderList(len(blobItems), func(i int) string {
		return renderRow(i == m.cursor, blobItems[i], values[i], 29)
	}))

	b.WriteString("\n")
	switch m.cursor {
	case 0:
		b.WriteString(helpStyle.Render("  competition: recorder methods are empty, no file IO ships.") + "\n")
		b.WriteString(helpStyle.Render("  dev: records path traces for the visualiser. Practice only.") + "\n")
		b.WriteString(helpStyle.Render("  Enter swaps them. Gradle sync and redeploy afterwards.") + "\n")
	case 1:
		b.WriteString(helpStyle.Render("  Enter checks GitHub and bumps every blob line to the newest tag.") + "\n")
	case 2:
		b.WriteString(helpStyle.Render("  Lists runs recorded on the robot and opens one in your browser.") + "\n")
	}

	b.WriteString(helpStyle.Render("  ↑/↓ move · enter select · esc back") + "\n")
	return b.String()
}

func (m *SettingsModel) viewBlobRuns() string {
	var b strings.Builder

	if m.blob.tracErr != nil {
		for _, line := range strings.Split(m.blob.tracErr.Error(), "\n") {
			b.WriteString(helpStyle.Render("  "+line) + "\n")
		}
		b.WriteString("\n" + helpStyle.Render("  r retry · esc back") + "\n")
		return b.String()
	}

	if len(m.blob.traces) == 0 {
		b.WriteString(helpStyle.Render("  No recorded runs on the robot.") + "\n")
		b.WriteString("\n" + helpStyle.Render("  r refresh · esc back") + "\n")
		return b.String()
	}

	b.WriteString(m.renderList(len(m.blob.traces), func(i int) string {
		t := m.blob.traces[i]
		note := ""
		if i == 0 {
			note = "newest"
		}
		return renderRow(i == m.cursor, t.OpMode, note, 29)
	}))

	b.WriteString("\n" + helpStyle.Render("  enter opens the visualiser · r refresh · esc back") + "\n")
	return b.String()
}
