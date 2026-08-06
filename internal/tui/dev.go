package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/andreibanu/pusher/internal/adb"
	"github.com/andreibanu/pusher/internal/bench"
	"github.com/andreibanu/pusher/internal/config"
	"github.com/andreibanu/pusher/internal/gradle"
	"github.com/andreibanu/pusher/internal/hotreload"
	tea "github.com/charmbracelet/bubbletea"
)

type devScreen int

const (
	devScreenMain devScreen = iota
	devScreenReport
	devScreenReload
)

// benchRepeats is how many times each configuration is measured. One sample
// cannot tell a real difference from run-to-run variance, and a deploy varies
// by seconds.
const benchRepeats = 3

var devItems = []string{
	"Benchmark the deploy",
	"Hot reload feasibility",
	"Both, with a full report",
	"Try a real hot reload",
	"Remove the hot reload proof",
	"Exit",
}

var devHelp = []string{
	"Deploys the current build with different settings and times each one,\n" +
		"three times over so a difference can be told from noise.\n" +
		"Reinstalls the app repeatedly. Takes about fifteen minutes.",

	"Times pushing a team-code-sized dex to the hub and compiling it there,\n" +
		"to see what a hot reload would have to beat. Installs nothing.",

	"Runs both and writes a report covering what each setting does, plus\n" +
		"Sloth's published figures for context.",

	"Compiles one OpMode here, pushes the dex to the hub and touches the\n" +
		"file the robot controller watches. If it appears on the Driver\n" +
		"Station, an OpMode reached the robot without installing an APK.",

	"Deletes the pushed dex and tells the robot controller to rescan.",

	"",
}

type devModel struct {
	screen devScreen
	cursor int
	height int

	project string
	apk     string
	splits  []string
	serial  string

	busy    string
	reload  *hotreload.Result
	started time.Time
	elapsed time.Duration
	report  string
	saved   string
	summary string

	status string
	err    error
	quit   bool
}

// reloadDoneMsg carries the hot reload attempt back to the menu.
type reloadDoneMsg struct {
	result *hotreload.Result
	err    error
}

type devDoneMsg struct {
	report  string
	summary string
	saved   string
	err     error
}

type devProgressMsg struct{ what string }

// devTickMsg keeps the elapsed counter moving, so a benchmark that takes a
// quarter of an hour does not look like a freeze.
type devTickMsg time.Time

// devProgress carries what the worker is doing back to the menu. Buffered and
// dropped on the floor when full: progress must never block the measurement.
var devProgress = make(chan string, 64)

func waitForProgress() tea.Msg { return devProgressMsg{what: <-devProgress} }

func devTick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return devTickMsg(t) })
}

// RunDev opens the developer menu.
func RunDev(projectRoot, apk string, splits []string) error {
	m := &devModel{
		height:  defaultHeight,
		project: projectRoot,
		apk:     apk,
		splits:  splits,
	}

	if serial, err := adb.Target(); err == nil {
		m.serial = serial
	}

	_, err := tea.NewProgram(m).Run()
	return err
}

// Init satisfies tea.Model.
func (m *devModel) Init() tea.Cmd { return nil }

// Update satisfies tea.Model.
func (m *devModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		return m, nil

	case devProgressMsg:
		if m.busy == "" {
			return m, nil
		}
		m.busy = msg.what
		return m, waitForProgress

	case devTickMsg:
		if m.busy == "" {
			return m, nil
		}
		m.elapsed = time.Since(m.started)
		return m, devTick()

	case reloadDoneMsg:
		m.busy = ""
		m.err = msg.err
		m.reload = msg.result
		if msg.err == nil && msg.result != nil && msg.result.Err == nil {
			m.screen = devScreenReload
		}
		return m, nil

	case devDoneMsg:
		m.busy = ""
		m.err = msg.err
		m.report = msg.report
		m.summary = msg.summary
		m.saved = msg.saved
		if msg.err == nil {
			m.screen = devScreenReport
		}
		return m, nil
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if key.Type == tea.KeyCtrlC {
		m.quit = true
		return m, tea.Quit
	}

	if m.busy != "" {
		return m, nil
	}

	if m.screen == devScreenReport || m.screen == devScreenReload {
		switch key.String() {
		case "esc", "q", "left", "h", "enter":
			m.screen = devScreenMain
		}
		return m, nil
	}

	switch key.String() {
	case "q", "esc":
		m.quit = true
		return m, tea.Quit

	case "up", "k":
		m.cursor = (m.cursor - 1 + len(devItems)) % len(devItems)
	case "down", "j":
		m.cursor = (m.cursor + 1) % len(devItems)

	case "enter", " ", "right", "l":
		m.err = nil
		m.status = ""

		switch m.cursor {
		case 0:
			return m, m.run(true, false)
		case 1:
			return m, m.run(false, true)
		case 2:
			return m, m.run(true, true)
		case 3:
			return m, m.tryReload()
		case 4:
			return m, m.cleanReload()
		case 5:
			m.quit = true
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m *devModel) run(deploy, reload bool) tea.Cmd {
	if m.serial == "" {
		m.err = fmt.Errorf("no robot connected - plug in USB or run `pusher connect`")
		return nil
	}
	if m.apk == "" {
		m.err = fmt.Errorf("no APK built yet - run `pusher` once first")
		return nil
	}

	m.busy = "starting"
	m.started = time.Now()
	m.elapsed = 0

	serial, apk, splits, project := m.serial, m.apk, m.splits, m.project

	work := func() tea.Msg {
		info, err := bench.Inspect(apk)
		if err != nil {
			return devDoneMsg{err: err}
		}

		var runs []bench.Run
		if deploy {
			runs = bench.Deploy(bench.Options{
				Serial:   serial,
				APK:      apk,
				Splits:   splits,
				Repeat:   benchRepeats,
				Progress: post,
			})
		}

		var floor bench.Reload
		if reload {
			post("timing a reload on the hub")
			floor = bench.MeasureReload(serial, apk)
		}

		settings := map[string]bool{
			"delta":     config.GetDeltaTransfer(),
			"skip":      config.GetSkipUnchanged(),
			"stream":    config.GetStreamInstall(),
			"storeLibs": config.GetStoreLibs(),
			"split":     config.GetSplitInstall(),
		}

		body := bench.Report(info, runs, floor, settings)

		saved, err := bench.SaveReport(project, body)
		if err != nil {
			saved = ""
		}

		return devDoneMsg{
			report:  body,
			summary: bench.Summary(runs),
			saved:   saved,
		}
	}

	return tea.Batch(work, waitForProgress, devTick())
}

// post reports progress without ever blocking the measurement.
func post(what string) {
	select {
	case devProgress <- what:
	default:
	}
}

// View satisfies tea.Model.
func (m *devModel) View() string {
	if m.quit {
		return ""
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render("Pusher developer tools"))
	b.WriteString("\n\n")

	switch m.screen {
	case devScreenReport:
		b.WriteString(m.viewDevReport())
	case devScreenReload:
		b.WriteString(m.viewDevReload())
	default:
		b.WriteString(m.viewDevMain())
	}

	switch {
	case m.busy != "":
		status := "  … " + m.busy
		if m.elapsed >= time.Second {
			status += fmt.Sprintf("   %s elapsed", m.elapsed.Round(time.Second))
		}
		b.WriteString("\n" + scrollStyle.Render(status) + "\n")
	case m.err != nil:
		b.WriteString("\n" + errStyle.Render("  ! "+m.err.Error()) + "\n")
	case m.status != "":
		b.WriteString("\n" + okStyle.Render("  ✓ "+m.status) + "\n")
	}

	return b.String()
}

func (m *devModel) viewDevMain() string {
	var b strings.Builder

	b.WriteString("  " + errStyle.Render("These measure by deploying to the robot over and over.") + "\n")
	b.WriteString("  " + helpStyle.Render("If you do not already know why you want this, you do not want it.") + "\n\n")

	robot := "no robot connected"
	if m.serial != "" {
		robot = "robot: " + m.serial
	}
	fmt.Fprintf(&b, "  %s\n", helpStyle.Render(robot))

	apk := "no APK built yet"
	if m.apk != "" {
		apk = m.apk
		if len(m.splits) > 1 {
			apk += fmt.Sprintf("  (+%d split(s))", len(m.splits)-1)
		}
	}
	fmt.Fprintf(&b, "  %s\n\n", helpStyle.Render(apk))

	for i, item := range devItems {
		cursor := "  "
		if i == m.cursor {
			cursor = cursorOn.Render("> ")
		}
		fmt.Fprintf(&b, "%s%s\n", cursor, item)
	}

	if m.cursor < len(devHelp) && devHelp[m.cursor] != "" {
		b.WriteString("\n")
		for _, line := range strings.Split(devHelp[m.cursor], "\n") {
			b.WriteString("  " + helpStyle.Render(line) + "\n")
		}
	}

	b.WriteString("\n" + helpStyle.Render("  enter run · up/down move · q quit") + "\n")
	return b.String()
}

func (m *devModel) viewDevReport() string {
	var b strings.Builder

	b.WriteString("  " + titleStyle.Render("Results") + "\n\n")

	if m.summary != "" {
		b.WriteString(m.summary)
		b.WriteString("\n")
	}

	if m.saved != "" {
		fmt.Fprintf(&b, "  %s\n", okStyle.Render("Full report: "+m.saved))
	} else {
		b.WriteString("  " + helpStyle.Render("The report could not be saved to the project.") + "\n")
	}

	b.WriteString("\n  " + helpStyle.Render("Pusher is not a Sloth replacement. The report says why.") + "\n")
	b.WriteString("\n" + helpStyle.Render("  esc back") + "\n")

	return b.String()
}

// DevTargets resolves what the menu should measure against.
func DevTargets() (project, apk string, splits []string) {
	wrapper, err := gradle.DetectWrapper()
	if err != nil {
		return "", "", nil
	}

	project = gradle.ProjectDir(wrapper)
	if found, err := gradle.FindApk(project); err == nil {
		apk = found
	}

	return project, apk, gradle.FindSplits(project)
}

// tryReload runs the hot reload experiment.
//
// The attempt number goes into the OpMode's name, so a second run proves a
// reload rather than a first load: the entry on the Driver Station has to
// change, not merely be present.
func (m *devModel) tryReload() tea.Cmd {
	if m.serial == "" {
		m.err = fmt.Errorf("no robot connected - plug in USB or run `pusher connect`")
		return nil
	}

	m.busy = "compiling an OpMode"
	m.started = time.Now()
	m.elapsed = 0

	// The marker is the clock, not a counter: `pusher dev` is a fresh process
	// every launch, so a counter restarts at one and two runs look identical
	// on the Driver Station.
	serial, marker := m.serial, time.Now().Format("15:04:05")

	work := func() tea.Msg {
		post("compiling an OpMode")
		result := hotreload.Run(serial, marker)
		if result.Err != nil {
			return reloadDoneMsg{result: result, err: result.Err}
		}
		return reloadDoneMsg{result: result}
	}

	return tea.Batch(work, waitForProgress, devTick())
}

func (m *devModel) cleanReload() tea.Cmd {
	if m.serial == "" {
		m.err = fmt.Errorf("no robot connected")
		return nil
	}

	m.busy = "removing the proof"
	m.started = time.Now()

	serial := m.serial

	return tea.Batch(func() tea.Msg {
		if err := hotreload.Clean(serial); err != nil {
			return reloadDoneMsg{err: err}
		}
		return reloadDoneMsg{result: nil}
	}, devTick())
}

func (m *devModel) viewDevReload() string {
	var b strings.Builder

	r := m.reload
	if r == nil {
		return "  nothing to show\n"
	}

	b.WriteString("  " + titleStyle.Render("Hot reload attempt") + "\n\n")

	for _, step := range r.Steps {
		fmt.Fprintf(&b, "  %s\n", helpStyle.Render(step))
	}

	if d := r.Diagnosis; !d.OK() {
		b.WriteString("\n  " + errStyle.Render("Something is wrong on the robot:") + "\n")
		for _, finding := range d.Findings {
			fmt.Fprintf(&b, "    %s\n", errStyle.Render(finding))
		}
		if d.OutputDir != "" {
			fmt.Fprintf(&b, "\n  %s\n", helpStyle.Render("directory the SDK reads: "+d.OutputDir))
			for _, line := range d.OnHub {
				fmt.Fprintf(&b, "    %s\n", helpStyle.Render(trim(line, 96)))
			}
		}
		if d.Crash != "" {
			b.WriteString("\n  " + helpStyle.Render("Most recent crash:") + "\n")
			for _, line := range strings.Split(d.Crash, "\n") {
				fmt.Fprintf(&b, "    %s\n", helpStyle.Render(trim(line, 96)))
			}
		}
		b.WriteString("\n" + helpStyle.Render("  esc back") + "\n")
		return b.String()
	}

	b.WriteString("\n  " + okStyle.Render("Now look at the Driver Station.") + "\n\n")
	fmt.Fprintf(&b, "  Look for an OpMode called %s in the TeleOp list.\n",
		valueStyle.Render(`"`+r.OpModeName+`"`))
	b.WriteString("  " + helpStyle.Render("It may take a moment, and the list sometimes needs reopening.") + "\n\n")

	b.WriteString("  " + helpStyle.Render("There: an OpMode reached the robot with no APK install.") + "\n")
	b.WriteString("  " + helpStyle.Render("Not there: the dex landed but nothing picked it up.") + "\n")
	b.WriteString("  " + helpStyle.Render("Run it again; the number in the name has to change.") + "\n")

	b.WriteString("\n" + helpStyle.Render("  esc back") + "\n")
	return b.String()
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
