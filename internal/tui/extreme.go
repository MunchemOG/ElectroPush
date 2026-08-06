package tui

import (
	"fmt"
	"strings"

	"github.com/andreibanu/pusher/internal/adb"
	"github.com/andreibanu/pusher/internal/config"
	"github.com/andreibanu/pusher/internal/extreme"
	"github.com/andreibanu/pusher/internal/gradle"
	tea "github.com/charmbracelet/bubbletea"
)

// Pusher Extreme changes the shape of a team's project, so the menu has to be
// honest about what it did and how to get out. Everything reversible is said to
// be reversible, in the place where somebody would look for it.

var extremeItems = []string{
	"Set up this project",
	"Undo the setup",
	"Use it when deploying",
	"Back",
}

var extremeHelp = []string{
	"Stops team code being packaged into the APK, so it can be reloaded onto\n" +
		"a running robot instead of installed. One marked block is added to\n" +
		"TeamCode/build.gradle and nothing else in the project is touched.",

	"Puts team code back in the APK and removes the block. Deploy once\n" +
		"afterwards, or the robot keeps running whatever was last reloaded.",

	"When on, a deploy compiles team code and reloads it instead of\n" +
		"installing, but only when that is equivalent. Anything else changing\n" +
		"falls back to a normal install.",

	"",
}

type extremeState struct {
	root     string
	set      bool
	status   extreme.State
	haveRoot bool
}

func (m *SettingsModel) refreshExtreme() {
	m.extreme = extremeState{}

	project, err := extreme.FindProject()
	if err != nil {
		return
	}

	m.extreme.haveRoot = true
	m.extreme.root = project.Root
	m.extreme.set = extreme.Excluded(project.Root)

	serial := ""
	if s, err := adb.Target(); err == nil {
		serial = s
	}

	apk, _ := gradle.FindApk(project.Root)
	m.extreme.status = extreme.Status(project.Root, serial, apk)
}

func (m *SettingsModel) extremeLabel() string {
	if !config.GetExtreme() {
		return "off"
	}
	if m.extreme.set {
		return "on"
	}
	return "on (project not set up)"
}

func (m *SettingsModel) updateExtreme(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "q":
		m.quit = true
		return m, tea.Quit

	case "esc", "left", "h":
		m.goTo(screenMain, 9)
		return m, nil

	case "up", "k":
		m.moveCursor(-1, len(extremeItems))
	case "down", "j":
		m.moveCursor(1, len(extremeItems))

	case "enter", " ", "right", "l":
		m.err = nil
		m.status = ""

		switch m.cursor {
		case 0:
			m.setUpExtreme()
		case 1:
			m.undoExtreme()
		case 2:
			m.setStatus(config.SetExtreme(!config.GetExtreme()), "Pusher Extreme updated")
		case 3:
			m.goTo(screenMain, 9)
		}
	}

	return m, nil
}

func (m *SettingsModel) setUpExtreme() {
	if !m.extreme.haveRoot {
		m.err = fmt.Errorf("run pusher from your FTC project to set this up")
		return
	}

	if err := extreme.Exclude(m.extreme.root); err != nil {
		m.err = err
		return
	}
	if err := config.SetExtreme(true); err != nil {
		m.err = err
		return
	}

	m.refreshExtreme()
	m.status = "Set up. Deploy once to install the APK without team code."
}

func (m *SettingsModel) undoExtreme() {
	if !m.extreme.haveRoot {
		m.err = fmt.Errorf("run pusher from your FTC project to undo this")
		return
	}

	if err := extreme.Include(m.extreme.root); err != nil {
		m.err = err
		return
	}
	if err := config.SetExtreme(false); err != nil {
		m.err = err
		return
	}

	m.refreshExtreme()
	m.status = "Undone. Deploy once so the robot gets an APK with team code in it."
}

func (m *SettingsModel) viewExtreme() string {
	var b strings.Builder

	b.WriteString(helpStyle.Render("  Pusher Extreme") + "\n\n")

	b.WriteString("  " + helpStyle.Render("Reloads your OpModes onto a running robot instead of installing an") + "\n")
	b.WriteString("  " + helpStyle.Render("APK. Under a second rather than around forty.") + "\n\n")

	b.WriteString(m.extremeStatusLines())

	values := []string{
		onOff(m.extreme.set),
		"",
		onOff(config.GetExtreme()),
		"",
	}

	b.WriteString(m.renderList(len(extremeItems), func(i int) string {
		return renderRow(i == m.cursor, extremeItems[i], values[i], 24)
	}))

	if m.cursor < len(extremeHelp) && extremeHelp[m.cursor] != "" {
		b.WriteString("\n")
		for _, line := range strings.Split(extremeHelp[m.cursor], "\n") {
			b.WriteString("  " + helpStyle.Render(line) + "\n")
		}
	}

	b.WriteString("\n" + errStyle.Render("  Team code stops being part of the APK while this is set up.") + "\n")
	b.WriteString("  " + helpStyle.Render("Anyone deploying this project from Android Studio gets a robot with") + "\n")
	b.WriteString("  " + helpStyle.Render("no OpModes until pusher reloads them. Undo above puts it back.") + "\n")

	b.WriteString("\n" + helpStyle.Render("  enter choose · esc back") + "\n")
	return b.String()
}

// extremeStatusLines says what would happen on the next deploy, which is the
// question somebody actually has.
func (m *SettingsModel) extremeStatusLines() string {
	var b strings.Builder

	if !m.extreme.haveRoot {
		b.WriteString("  " + unsetStyle.Render("No FTC project here, so there is nothing to set up.") + "\n\n")
		return b.String()
	}

	fmt.Fprintf(&b, "  %s\n", helpStyle.Render(m.extreme.root))

	switch {
	case !m.extreme.set:
		b.WriteString("  " + unsetStyle.Render("Not set up: team code is packaged in the APK as usual.") + "\n")
	case m.extreme.status.Usable():
		b.WriteString("  " + okStyle.Render("Ready: the next deploy will reload instead of installing.") + "\n")
	default:
		fmt.Fprintf(&b, "  %s\n", scrollStyle.Render("Set up, but the next deploy will install: "+m.extreme.status.Reason))
	}

	b.WriteString("\n")
	return b.String()
}
