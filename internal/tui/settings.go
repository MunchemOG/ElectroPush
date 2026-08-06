package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/andreibanu/pusher/internal/config"
	"github.com/andreibanu/pusher/internal/feature"
	"github.com/andreibanu/pusher/internal/pathtrace"
	"github.com/andreibanu/pusher/internal/wifi"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	valueStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	unsetStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	scrollStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	cursorOn    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
)

type screen int

const (
	screenMain screen = iota
	screenProfiles
	screenAddProfile
	screenHomeNetwork
	screenThreads
	screenBlob
	screenBlobRuns
	screenBlobToken
	screenUpdate
	screenDeploy
	screenExtreme
)

type addStep int

const (
	stepName addStep = iota
	stepSSID
	stepPassword
)

const defaultHeight = 24

const minVisibleRows = 3

// SettingsModel is the settings menu.
type SettingsModel struct {
	screen screen
	cursor int

	offset int

	height int

	cfg      *config.Config
	profiles []string
	networks []string

	step               addStep
	newName            string
	newSSID            string
	input              string
	maskInput          bool
	confirmDeleteIndex int

	blob     blobState
	extreme  extremeState
	root     string
	gateStep int
	update   updateState

	status string
	err    error
	quit   bool
}

func (m *SettingsModel) projectRoot() string {
	if m.root == "" {
		m.root, _ = os.Getwd()
	}
	return m.root
}

// NewSettingsModel builds the settings menu.
func NewSettingsModel() (*SettingsModel, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	m := &SettingsModel{cfg: cfg, confirmDeleteIndex: -1, height: defaultHeight}
	m.blob.limits = pathtrace.DefaultLimits()
	m.refreshProfiles()
	m.refreshBlob()
	return m, nil
}

// RunSettings opens the settings menu.
func RunSettings() error {
	model, err := NewSettingsModel()
	if err != nil {
		return err
	}

	_, err = tea.NewProgram(model).Run()
	return err
}

func (m *SettingsModel) refreshProfiles() {
	cfg, err := config.Load()
	if err != nil {
		m.err = err
		return
	}
	m.cfg = cfg

	m.profiles = m.profiles[:0]
	for name := range cfg.Profiles {
		m.profiles = append(m.profiles, name)
	}

	sortStrings(m.profiles)
}

func sortStrings(items []string) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j] < items[j-1]; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

// Init satisfies tea.Model.
func (m *SettingsModel) Init() tea.Cmd { return nil }

var mainItems = []string{
	"Robot profiles",
	"Home Wi-Fi network",
	"Return to previous Wi-Fi",
	"Prefer USB when attached",
	"Slim APK before every push",
	"Send only changed parts",
	"Gradle threads",
	"blob library",
	"Deploy speed",
	"Pusher Extreme",
	"Update pusher",
	"Exit",
}

// Update satisfies tea.Model.
func (m *SettingsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.height = size.Height

		m.offset = clampOffset(m.offset, m.cursor, m.visibleRows(), m.listLength())
		return m, nil
	}

	switch msg := msg.(type) {
	case releaseFoundMsg:
		m.update.checking = false
		m.update.release = msg.release
		m.update.err = msg.err
		return m, nil

	case updateAppliedMsg:
		m.update.busy = false
		m.update.done = msg.err == nil
		m.update.result = msg.result
		m.update.err = msg.err
		return m, nil

	case blobAuthMsg:
		m.blob.checking = false
		m.blob.busy = false
		m.blob.auth = msg.status
		m.blob.creds = msg.creds
		m.cursor, m.offset = 0, 0
		return m, nil

	case blobOpMsg:
		m.blob.busy = false
		m.err = msg.err
		m.status = msg.status
		m.refreshBlob()
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

	switch m.screen {
	case screenMain:
		return m.updateMain(key)
	case screenProfiles:
		return m.updateProfiles(key)
	case screenAddProfile:
		return m.updateAddProfile(key)
	case screenHomeNetwork:
		return m.updateHomeNetwork(key)
	case screenThreads:
		return m.updateThreads(key)
	case screenBlob:
		return m.updateBlob(key)
	case screenBlobRuns:
		return m.updateBlobRuns(key)
	case screenBlobToken:
		return m.updateBlobToken(key)
	case screenUpdate:
		return m.updateUpdate(key)
	case screenDeploy:
		return m.updateDeploy(key)
	case screenExtreme:
		return m.updateExtreme(key)
	}

	return m, nil
}

// Position in mainItems of the entry an install only shows once turned on.
// rows() exists because the indexes stop lining up otherwise.
const optionalRow = 7

func (m *SettingsModel) rows() []int {
	enabled := feature.Revealed()

	out := make([]int, 0, len(mainItems))
	for i := range mainItems {
		if i == optionalRow && !enabled {
			continue
		}
		out = append(out, i)
	}
	return out
}

func (m *SettingsModel) checkGate(key tea.KeyMsg) bool {
	if feature.Revealed() {
		return false
	}

	name := key.String()

	next, done := feature.Match(m.gateStep, name)
	m.gateStep = next

	switch {
	case done:
		m.gateStep = 0
		m.err = feature.Grant()
		m.cursor, m.offset = 0, 0
		return true

	case next > 0 && name == "right":
		return true
	}

	return false
}

func (m *SettingsModel) updateMain(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.checkGate(key) {
		return m, nil
	}

	rows := m.rows()

	switch key.String() {
	case "q", "esc":
		m.quit = true
		return m, tea.Quit

	case "up", "k":
		m.moveCursor(-1, len(rows))
	case "down", "j":
		m.moveCursor(1, len(rows))

	case "enter", " ", "right", "l":
		m.status = ""
		m.err = nil

		switch rows[m.cursor] {
		case 0:
			m.confirmDeleteIndex = -1
			m.refreshProfiles()
			m.goTo(screenProfiles, 0)
		case 1:
			m.loadNetworks()
			m.goTo(screenHomeNetwork, 0)
		case 2:
			m.setStatus(config.SetSwitchBack(!config.GetSwitchBack()), "Return-to-Wi-Fi updated")
		case 3:
			m.setStatus(config.SetPreferUSB(!config.GetPreferUSB()), "USB preference updated")
		case 4:
			m.toggleAutoSlim()
		case 5:
			m.setStatus(config.SetDeltaTransfer(!config.GetDeltaTransfer()), "Delta transfer updated")
		case 6:
			m.input = strconv.Itoa(config.GetThreads())
			m.goTo(screenThreads, 0)
		case 7:
			return m, m.enterBlob()
		case 8:
			m.goTo(screenDeploy, 0)
		case 9:
			m.refreshExtreme()
			m.goTo(screenExtreme, 0)
		case 10:
			return m, m.enterUpdate()
		case 11:
			m.quit = true
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m *SettingsModel) updateProfiles(key tea.KeyMsg) (tea.Model, tea.Cmd) {

	if m.confirmDeleteIndex >= 0 {
		if key.String() == "y" {
			name := m.profiles[m.confirmDeleteIndex]
			m.setStatus(config.DeleteProfile(name), fmt.Sprintf("Deleted %q", name))
			m.refreshProfiles()
			if m.cursor >= len(m.profiles) && m.cursor > 0 {
				m.cursor = len(m.profiles) - 1
			}
		} else {
			m.status = "Delete cancelled"
		}
		m.confirmDeleteIndex = -1
		return m, nil
	}

	switch key.String() {
	case "esc", "q", "left", "h":
		m.goTo(screenMain, 0)
		m.status = ""

	case "up", "k":
		m.moveCursor(-1, len(m.profiles))
	case "down", "j":
		m.moveCursor(1, len(m.profiles))

	case "a":
		m.step = stepName
		m.input = ""
		m.maskInput = false
		m.newName, m.newSSID = "", ""
		m.goTo(screenAddProfile, 0)
		m.status = ""

	case "d":
		if len(m.profiles) > 0 {
			m.confirmDeleteIndex = m.cursor
		}

	case "enter", " ":
		if len(m.profiles) > 0 {
			name := m.profiles[m.cursor]
			m.setStatus(config.SetDefaultProfile(name), fmt.Sprintf("%q is now the default robot", name))
			m.refreshProfiles()
		}
	}

	return m, nil
}

func (m *SettingsModel) updateAddProfile(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyEsc:
		m.goTo(screenProfiles, 0)
		m.input = ""
		m.status = "Cancelled"
		return m, nil

	case tea.KeyBackspace:
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
		return m, nil

	case tea.KeyEnter:
		value := strings.TrimSpace(m.input)

		switch m.step {
		case stepName:
			if value == "" {
				m.err = fmt.Errorf("profile name cannot be empty")
				return m, nil
			}
			m.newName = value
			m.step = stepSSID
			m.input = ""
			m.err = nil

		case stepSSID:
			if value == "" {
				m.err = fmt.Errorf("SSID cannot be empty")
				return m, nil
			}
			m.newSSID = value
			m.step = stepPassword
			m.input = ""
			m.maskInput = true
			m.err = nil

		case stepPassword:

			m.setStatus(
				config.AddProfile(m.newName, m.newSSID, m.input),
				fmt.Sprintf("Added profile %q", m.newName),
			)
			m.input = ""
			m.maskInput = false
			m.refreshProfiles()
			m.goTo(screenProfiles, 0)
		}
		return m, nil

	case tea.KeySpace:
		m.input += " "
		return m, nil

	case tea.KeyRunes:
		m.input += string(key.Runes)
		return m, nil
	}

	return m, nil
}

func (m *SettingsModel) updateHomeNetwork(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc", "q", "left", "h":
		m.goTo(screenMain, 0)

	case "up", "k":
		m.moveCursor(-1, len(m.networks)+1)
	case "down", "j":
		m.moveCursor(1, len(m.networks)+1)

	case "enter", " ":

		if m.cursor == 0 {
			m.setStatus(config.SetHomeSSID(""), "Home network cleared")
		} else if m.cursor-1 < len(m.networks) {
			ssid := m.networks[m.cursor-1]
			m.setStatus(config.SetHomeSSID(ssid), fmt.Sprintf("Home network set to %q", ssid))
		}
		m.goTo(screenMain, 1)
	}

	return m, nil
}

func (m *SettingsModel) updateThreads(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyEsc:
		m.goTo(screenMain, 6)
		m.input = ""
		return m, nil

	case tea.KeyBackspace:
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
		return m, nil

	case tea.KeyEnter:
		count, err := strconv.Atoi(strings.TrimSpace(m.input))
		if err != nil || count < 1 {
			m.err = fmt.Errorf("enter a positive whole number")
			return m, nil
		}
		m.err = nil
		m.setStatus(config.SetThreads(count), fmt.Sprintf("Gradle threads set to %d", count))
		m.goTo(screenMain, 6)
		return m, nil

	case tea.KeyRunes:
		for _, r := range key.Runes {
			if r >= '0' && r <= '9' {
				m.input += string(r)
			}
		}
		return m, nil
	}

	return m, nil
}

func (m *SettingsModel) toggleAutoSlim() {
	enabling := !config.GetAutoSlim()

	if err := config.SetAutoSlim(enabling); err != nil {
		m.err = err
		return
	}

	m.err = nil
	switch {
	case !enabling:
		m.status = "Pushes will package every architecture again"
	case config.GetHubABI() == "":
		m.status = "On — but connect the robot and run 'pusher slim' once first"
	default:
		m.status = fmt.Sprintf("On — pushes will package %s only", config.GetHubABI())
	}
}

func (m *SettingsModel) moveCursor(delta, length int) {
	if length <= 0 {
		m.cursor = 0
		m.offset = 0
		return
	}

	m.cursor = (m.cursor + delta + length) % length
	m.offset = clampOffset(m.offset, m.cursor, m.visibleRows(), length)
}

func (m *SettingsModel) goTo(target screen, cursor int) {
	m.screen = target
	m.cursor = cursor
	m.offset = clampOffset(0, cursor, m.visibleRows(), m.listLength())
}

func (m *SettingsModel) listLength() int {
	switch m.screen {
	case screenMain:
		return len(m.rows())
	case screenProfiles:
		return len(m.profiles)
	case screenHomeNetwork:

		return len(m.networks) + 1
	case screenBlob:
		return len(m.blobMenuItems())
	case screenBlobRuns:
		return len(m.blob.traces)
	case screenBlobToken:
		return 0
	case screenUpdate:
		return 0
	case screenDeploy:
		return len(deployItems)
	case screenExtreme:
		return len(extremeItems)
	}
	return 0
}

func (m *SettingsModel) visibleRows() int {
	const chrome = 10

	rows := m.height - chrome
	if rows < minVisibleRows {
		return minVisibleRows
	}
	return rows
}

func clampOffset(offset, cursor, visible, total int) int {
	if visible <= 0 || total <= visible {
		return 0
	}

	if cursor < offset {
		offset = cursor
	}
	if cursor >= offset+visible {
		offset = cursor - visible + 1
	}

	if max := total - visible; offset > max {
		offset = max
	}
	if offset < 0 {
		offset = 0
	}

	return offset
}

func (m *SettingsModel) setStatus(err error, success string) {
	if err != nil {
		m.err = err
		m.status = ""
		return
	}
	m.err = nil
	m.status = success
	m.refreshProfiles()
}

func (m *SettingsModel) loadNetworks() {
	networks, err := wifi.NewManager().PreferredNetworks()
	if err != nil {
		m.err = err
		m.networks = nil
		return
	}
	m.networks = networks
}

// View satisfies tea.Model.
func (m *SettingsModel) View() string {
	if m.quit {
		return ""
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render("Pusher Settings"))
	b.WriteString("\n\n")

	switch m.screen {
	case screenMain:
		b.WriteString(m.viewMain())
	case screenProfiles:
		b.WriteString(m.viewProfiles())
	case screenAddProfile:
		b.WriteString(m.viewAddProfile())
	case screenHomeNetwork:
		b.WriteString(m.viewHomeNetwork())
	case screenThreads:
		b.WriteString(m.viewThreads())
	case screenBlob:
		b.WriteString(m.viewBlob())
	case screenBlobRuns:
		b.WriteString(m.viewBlobRuns())
	case screenBlobToken:
		b.WriteString(m.viewBlobToken())
	case screenUpdate:
		b.WriteString(m.viewUpdate())
	case screenDeploy:
		b.WriteString(m.viewDeploy())
	case screenExtreme:
		b.WriteString(m.viewExtreme())
	}

	if m.err != nil {
		b.WriteString("\n" + errStyle.Render("  ! "+m.err.Error()) + "\n")
	} else if m.status != "" {
		b.WriteString("\n" + okStyle.Render("  ✓ "+m.status) + "\n")
	}

	return b.String()
}

func (m *SettingsModel) viewMain() string {
	values := []string{
		m.defaultProfileLabel(),
		orUnset(config.GetHomeSSID(), "not set"),
		onOff(config.GetSwitchBack()),
		onOff(config.GetPreferUSB()),
		m.autoSlimLabel(),
		onOff(config.GetDeltaTransfer()),
		strconv.Itoa(config.GetThreads()),
		m.blobLabel(),
		m.deployLabel(),
		m.extremeLabel(),
		m.updateLabel(),
		"",
	}

	rows := m.rows()

	var b strings.Builder
	b.WriteString(m.renderList(len(rows), func(i int) string {
		return renderRow(i == m.cursor, mainItems[rows[i]], values[rows[i]], 29)
	}))

	b.WriteString("\n" + helpStyle.Render("  ↑/↓ move · enter select · q quit") + "\n")
	return b.String()
}

func (m *SettingsModel) autoSlimLabel() string {
	if !config.GetAutoSlim() {
		return "off"
	}
	if abi := config.GetHubABI(); abi != "" {
		return "on (" + abi + ")"
	}
	return "on (hub unknown)"
}

func (m *SettingsModel) defaultProfileLabel() string {
	if m.cfg == nil || m.cfg.DefaultProfile == "" {
		return "none"
	}
	if profile, ok := m.cfg.Profiles[m.cfg.DefaultProfile]; ok {
		return fmt.Sprintf("%s (%s)", m.cfg.DefaultProfile, profile.SSID)
	}
	return m.cfg.DefaultProfile
}

func (m *SettingsModel) viewProfiles() string {
	var b strings.Builder
	b.WriteString(helpStyle.Render("  Robot profiles") + "\n\n")

	if len(m.profiles) == 0 {
		b.WriteString(unsetStyle.Render("  No profiles yet — press 'a' to add one") + "\n")
	}

	b.WriteString(m.renderList(len(m.profiles), func(i int) string {
		name := m.profiles[i]

		if i == m.confirmDeleteIndex {
			return errStyle.Render(fmt.Sprintf("  > %s — delete? (y/n)", name)) + "\n"
		}

		marker := " "
		if m.cfg != nil && name == m.cfg.DefaultProfile {
			marker = "*"
		}

		ssid := ""
		if m.cfg != nil {
			if profile, ok := m.cfg.Profiles[name]; ok {
				ssid = profile.SSID
			}
		}

		return renderRow(i == m.cursor, fmt.Sprintf("%s %s", marker, name), ssid, 26)
	}))

	b.WriteString("\n" + helpStyle.Render("  enter set default · a add · d delete · esc back") + "\n")
	return b.String()
}

func (m *SettingsModel) viewAddProfile() string {
	labels := map[addStep]string{
		stepName:     "Profile name",
		stepSSID:     "Robot Wi-Fi SSID",
		stepPassword: "Robot Wi-Fi password",
	}

	shown := m.input
	if m.maskInput {
		shown = strings.Repeat("•", len(m.input))
	}

	var b strings.Builder
	b.WriteString(helpStyle.Render(fmt.Sprintf("  Add profile (step %d of 3)", int(m.step)+1)) + "\n\n")
	b.WriteString(fmt.Sprintf("  %s: %s\n", labels[m.step], valueStyle.Render(shown+"▌")))

	if m.step > stepName {
		b.WriteString("\n" + unsetStyle.Render("  name: "+m.newName) + "\n")
	}
	if m.step > stepSSID {
		b.WriteString(unsetStyle.Render("  ssid: "+m.newSSID) + "\n")
	}

	b.WriteString("\n" + helpStyle.Render("  enter next · esc cancel") + "\n")
	return b.String()
}

func (m *SettingsModel) viewHomeNetwork() string {
	var b strings.Builder
	b.WriteString(helpStyle.Render("  Network to return to after deploying") + "\n\n")

	if len(m.networks) == 0 {
		b.WriteString(unsetStyle.Render("  No saved Wi-Fi networks found") + "\n")
	}

	current := config.GetHomeSSID()

	b.WriteString(m.renderList(len(m.networks)+1, func(i int) string {
		if i == 0 {
			return renderRow(m.cursor == 0, "(none — stay on the robot)", "", 32)
		}

		ssid := m.networks[i-1]
		value := ""
		if ssid == current {
			value = "current"
		}
		return renderRow(m.cursor == i, ssid, value, 32)
	}))

	b.WriteString("\n" + helpStyle.Render("  ↑/↓ move · enter select · esc back") + "\n")
	return b.String()
}

func (m *SettingsModel) viewThreads() string {
	var b strings.Builder
	b.WriteString(helpStyle.Render("  Gradle worker threads") + "\n\n")
	b.WriteString(fmt.Sprintf("  Threads: %s\n", valueStyle.Render(m.input+"▌")))
	b.WriteString("\n" + helpStyle.Render("  enter save · esc cancel") + "\n")
	return b.String()
}

// helpBlock renders the note for the selected row at a fixed height.
//
// A screen whose height changes as the cursor moves leaves the taller frame's
// leftovers behind, which reads as the menu being broken while scrolling. Every
// note is padded to the same number of lines instead.
func helpBlock(notes []string, index, lines int) string {
	var b strings.Builder
	b.WriteString("\n")

	shown := 0
	if index >= 0 && index < len(notes) && notes[index] != "" {
		for _, line := range strings.Split(notes[index], "\n") {
			b.WriteString("  " + helpStyle.Render(line) + "\n")
			shown++
		}
	}

	for ; shown < lines; shown++ {
		b.WriteString("\n")
	}

	return b.String()
}

func (m *SettingsModel) renderList(total int, row func(int) string) string {
	visible := m.visibleRows()

	start := m.offset
	if start > total-visible {
		start = total - visible
	}
	if start < 0 {
		start = 0
	}

	end := start + visible
	if end > total {
		end = total
	}

	var b strings.Builder

	if start > 0 {
		b.WriteString(scrollStyle.Render(fmt.Sprintf("   ↑ %d more above", start)) + "\n")
	}

	for i := start; i < end; i++ {
		b.WriteString(row(i))
	}

	if end < total {
		b.WriteString(scrollStyle.Render(fmt.Sprintf("   ↓ %d more below", total-end)) + "\n")
	}

	return b.String()
}

func renderRow(selected bool, label, value string, width int) string {
	prefix := "   "
	if selected {
		prefix = cursorOn.Render(" > ")
		label = cursorOn.Render(label)

	}

	pad := width - lipgloss.Width(label)
	if pad < 1 {
		pad = 1
	}

	if value == "" {
		return prefix + label + "\n"
	}

	return prefix + label + strings.Repeat(" ", pad) + valueStyle.Render(value) + "\n"
}

func onOff(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

func orUnset(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
