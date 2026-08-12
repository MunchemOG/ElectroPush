package tui

import (
	"fmt"
	"strings"

	"github.com/MunchemOG/ElectroPush/internal/robotcfg"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *hwModel) pull(name string) tea.Cmd {
	m.busy = "Pulling " + name

	serial, store := m.serial, m.store

	return func() tea.Msg {
		data, err := robotcfg.Fetch(serial, name)
		if err != nil {
			return hwOpMsg{err: err}
		}
		if err := store.Write(name, data); err != nil {
			return hwOpMsg{err: err}
		}
		return hwOpMsg{status: "Pulled " + name, reload: true}
	}
}

func (m *hwModel) pullAll() tea.Cmd {
	if m.serial == "" {
		m.err = fmt.Errorf("no robot connected")
		return nil
	}

	m.busy = "Pulling everything"

	serial, store, names := m.serial, m.store, append([]string(nil), m.robot...)

	return func() tea.Msg {
		if len(names) == 0 {
			return hwOpMsg{err: fmt.Errorf("the robot has no configurations in %s", robotcfg.HubDir)}
		}

		for _, name := range names {
			data, err := robotcfg.Fetch(serial, name)
			if err != nil {
				return hwOpMsg{err: err}
			}
			if err := store.Write(name, data); err != nil {
				return hwOpMsg{err: err}
			}
		}

		return hwOpMsg{status: fmt.Sprintf("Pulled %d configuration(s)", len(names)), reload: true}
	}
}

func (m *hwModel) push(name string) tea.Cmd {
	if m.serial == "" {
		m.err = fmt.Errorf("no robot connected")
		return nil
	}

	data, err := m.store.Read(name)
	if err != nil {
		m.err = err
		return nil
	}

	cfg, err := robotcfg.Parse(data)
	if err != nil {
		m.err = fmt.Errorf("%s does not parse: %w", name, err)
		return nil
	}

	if issues := robotcfg.Validate(cfg); issues.Errors() {
		m.err = fmt.Errorf("%s has %d error(s) the robot would reject - fix them first",
			name, issues.Count(robotcfg.Error))
		return nil
	}

	m.busy = "Pushing " + name

	serial, store, active := m.serial, m.store, m.active

	return func() tea.Msg {
		if current, err := robotcfg.Fetch(serial, name); err == nil && !robotcfg.Same(current, data) {
			if _, err := store.Backup(name, current); err != nil {
				return hwOpMsg{err: err}
			}
		}

		if err := robotcfg.Send(serial, name, data); err != nil {
			return hwOpMsg{err: err}
		}

		status := "Pushed " + name
		if name == active {

			status += " - re-select it on the Driver Station to apply it"
		}

		return hwOpMsg{status: status, reload: true}
	}
}

func (m *hwModel) compare(name string) tea.Cmd {
	if m.serial == "" {
		m.err = fmt.Errorf("no robot connected")
		return nil
	}

	m.busy = "Comparing " + name

	serial, store := m.serial, m.store

	return func() tea.Msg {
		mine, err := store.Read(name)
		if err != nil {
			return hwOpMsg{err: err}
		}

		theirs, err := robotcfg.Fetch(serial, name)
		if err != nil {
			return hwOpMsg{err: err}
		}

		if robotcfg.Same(mine, theirs) {
			return hwOpMsg{status: name + " is identical to the robot's"}
		}

		robotCfg, err := robotcfg.Parse(theirs)
		if err != nil {
			return hwOpMsg{err: fmt.Errorf("the robot's %s does not parse: %w", name, err)}
		}
		myCfg, err := robotcfg.Parse(mine)
		if err != nil {
			return hwOpMsg{err: fmt.Errorf("%s does not parse: %w", name, err)}
		}

		changes := robotcfg.Diff(robotCfg, myCfg)
		if len(changes) == 0 {
			return hwOpMsg{status: name + " wires the same things; only the file differs"}
		}

		return hwOpMsg{status: "vs robot: " + strings.Join(changes, ";  ")}
	}
}

func (m *hwModel) deleteFromRobot(name string) tea.Cmd {
	m.busy = "Deleting " + name

	serial, store := m.serial, m.store

	return func() tea.Msg {

		if data, err := robotcfg.Fetch(serial, name); err == nil {
			if _, err := store.Backup(name, data); err != nil {
				return hwOpMsg{err: err}
			}
		}

		if err := robotcfg.Remove(serial, name); err != nil {
			return hwOpMsg{err: err}
		}

		return hwOpMsg{status: "Deleted " + name + " from the robot", reload: true}
	}
}

func (m *hwModel) updateHWPrompt(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.goTo(hwScreenList, m.cursor)
		return m, nil

	case "enter":
		name := strings.TrimSpace(m.prompt.value)
		if err := robotcfg.CheckName(name); err != nil {
			m.err = err
			return m, nil
		}
		return m, m.runPrompt(name)

	case "backspace":
		if m.prompt.value != "" {
			m.prompt.value = m.prompt.value[:len(m.prompt.value)-1]
		}
		return m, nil
	}

	if r := key.Runes; len(r) == 1 && key.Type == tea.KeyRunes {
		m.prompt.value += string(r)
	}

	return m, nil
}

func (m *hwModel) runPrompt(name string) tea.Cmd {
	switch m.prompt.action {
	case "new":
		if m.store.Has(name) {
			m.err = fmt.Errorf("%q already exists", name)
			return nil
		}
		if err := m.store.Write(name, robotcfg.Write(robotcfg.New())); err != nil {
			m.err = err
			return nil
		}

		m.refreshLocal()
		m.rebuildEntries()
		m.sel = name
		m.status = "Created " + name
		return m.openEditor()

	case "duplicate":
		if m.store.Has(name) {
			m.err = fmt.Errorf("%q already exists", name)
			return nil
		}
		data, err := m.store.Read(m.sel)
		if err != nil {
			m.err = err
			return nil
		}
		if err := m.store.Write(name, data); err != nil {
			m.err = err
			return nil
		}

		m.refreshLocal()
		m.rebuildEntries()
		m.status = "Copied to " + name
		m.goTo(hwScreenList, 0)

	case "rename":
		if name == m.sel {
			m.goTo(hwScreenActions, 0)
			return nil
		}
		if m.store.Has(name) {
			m.err = fmt.Errorf("%q already exists", name)
			return nil
		}

		data, err := m.store.Read(m.sel)
		if err != nil {
			m.err = err
			return nil
		}
		if err := m.store.Write(name, data); err != nil {
			m.err = err
			return nil
		}
		if err := m.store.Remove(m.sel); err != nil {
			m.err = err
			return nil
		}

		m.status = fmt.Sprintf("Renamed to %s (the robot still has %s)", name, m.sel)
		if !m.entry(m.sel).OnRobot {
			m.status = "Renamed to " + name
		}

		m.sel = name
		m.refreshLocal()
		m.rebuildEntries()
		m.goTo(hwScreenList, 0)
	}

	return nil
}

func (m *hwModel) updateHWConfirm(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "y", "Y", "enter":
		return m, m.runConfirm()
	default:
		m.status = ""
		switch m.confirm.action {
		case "remove-device", "remove-module", "discard":
			m.goTo(hwScreenDevices, m.cursor)
		default:
			m.goTo(hwScreenActions, 0)
		}
		return m, nil
	}
}

func (m *hwModel) runConfirm() tea.Cmd {
	switch m.confirm.action {
	case "delete-local":
		if err := m.store.Remove(m.confirm.name); err != nil {
			m.err = err
			m.goTo(hwScreenList, 0)
			return nil
		}
		m.refreshLocal()
		m.rebuildEntries()
		m.status = "Deleted " + m.confirm.name + " from the project"
		m.goTo(hwScreenList, 0)

	case "delete-robot":
		m.goTo(hwScreenList, 0)
		return m.deleteFromRobot(m.confirm.name)

	case "remove-device", "remove-module":
		row := m.rows[m.cursor]

		var err error
		if m.confirm.action == "remove-device" {
			err = m.cfg.RemoveDevice(row.Slot)
		} else {
			err = m.cfg.RemoveModule(row.Portal, row.Module)
		}

		if err != nil {
			m.err = err
		} else {
			m.dirty = true
			m.revalidate()
			m.rebuildRows()
			m.status = "Removed (s to save)"
		}

		if m.cursor >= len(m.rows) {
			m.cursor = len(m.rows) - 1
		}
		if m.cursor >= 0 && m.cursor < len(m.rows) && !m.rows[m.cursor].selectable() {
			m.moveRows(1)
		}
		m.goTo(hwScreenDevices, m.cursor)

	case "discard":
		m.cfg = nil
		m.dirty = false
		m.goTo(hwScreenActions, 0)
	}

	return nil
}

func (m *hwModel) updateHWSummary(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc", "q", "left", "h", "enter":
		m.goTo(hwScreenActions, 0)
	}
	return m, nil
}
