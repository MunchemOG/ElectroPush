package tui

import (
	"fmt"
	"strings"

	"github.com/andreibanu/pusher/internal/robotcfg"
	tea "github.com/charmbracelet/bubbletea"
)

// hwListExtras sit under the configurations, so everything is reachable by
// moving the cursor rather than by knowing a key.
var hwListExtras = []string{
	"New configuration",
	"Pull everything from the robot",
	"Refresh",
	"Exit",
}

func (m *hwModel) updateHWList(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	total := len(m.entries) + len(hwListExtras)

	switch key.String() {
	case "q", "esc":
		m.quit = true
		return m, tea.Quit

	case "up", "k":
		m.move(-1, total)
	case "down", "j":
		m.move(1, total)

	case "enter", " ", "right", "l":
		m.clear()

		if m.cursor < len(m.entries) {
			m.sel = m.entries[m.cursor].Name
			m.goTo(hwScreenActions, 0)
			return m, nil
		}

		switch hwListExtras[m.cursor-len(m.entries)] {
		case "New configuration":
			m.prompt = hwPrompt{title: "Name for the new configuration", action: "new"}
			m.goTo(hwScreenPrompt, 0)
		case "Pull everything from the robot":
			return m, m.pullAll()
		case "Refresh":
			m.loading = true
			return m, hwLoad
		case "Exit":
			m.quit = true
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m *hwModel) entry(name string) hwEntry {
	for _, e := range m.entries {
		if e.Name == name {
			return e
		}
	}
	return hwEntry{Name: name}
}

// actionItems is built per configuration, so nothing offers an action that
// cannot work: pushing something the project does not have, or pulling
// something the robot does not have.
func (m *hwModel) actionItems() []string {
	e := m.entry(m.sel)

	var items []string
	if e.InLocal {
		items = append(items, "Edit devices", "View summary")
	}
	if e.InLocal && m.serial != "" {
		items = append(items, "Push to the robot")
	}
	if e.OnRobot {
		items = append(items, "Pull from the robot")
	}
	if e.InLocal && e.OnRobot {
		items = append(items, "Compare with the robot")
	}
	if e.InLocal {
		items = append(items, "Duplicate", "Rename", "Delete from the project")
	}
	if e.OnRobot {
		items = append(items, "Delete from the robot")
	}

	return append(items, "Back")
}

func (m *hwModel) updateHWActions(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.actionItems()

	switch key.String() {
	case "q":
		m.quit = true
		return m, tea.Quit

	case "esc", "left", "h":
		m.goTo(hwScreenList, m.cursor)
		return m, nil

	case "up", "k":
		m.move(-1, len(items))
	case "down", "j":
		m.move(1, len(items))

	case "enter", " ", "right", "l":
		m.clear()

		switch items[m.cursor] {
		case "Edit devices":
			return m, m.openEditor()

		case "View summary":
			if err := m.loadConfig(); err != nil {
				m.err = err
				return m, nil
			}
			m.goTo(hwScreenSummary, 0)

		case "Push to the robot":
			return m, m.push(m.sel)

		case "Pull from the robot":
			return m, m.pull(m.sel)

		case "Compare with the robot":
			return m, m.compare(m.sel)

		case "Duplicate":
			m.prompt = hwPrompt{title: "Name for the copy", value: m.sel + " copy", action: "duplicate"}
			m.goTo(hwScreenPrompt, 0)

		case "Rename":
			m.prompt = hwPrompt{title: "New name", value: m.sel, action: "rename"}
			m.goTo(hwScreenPrompt, 0)

		case "Delete from the project":
			m.confirm = hwConfirm{
				title:  fmt.Sprintf("Delete %q from the project?", m.sel),
				detail: m.store.Path(m.sel),
				action: "delete-local",
				name:   m.sel,
			}
			m.goTo(hwScreenConfirm, 0)

		case "Delete from the robot":
			detail := robotcfg.RemotePath(m.sel)
			if m.entry(m.sel).Active {
				detail += "\n  This is the configuration the robot is running."
			}
			m.confirm = hwConfirm{
				title:  fmt.Sprintf("Delete %q from the robot?", m.sel),
				detail: detail,
				action: "delete-robot",
				name:   m.sel,
			}
			m.goTo(hwScreenConfirm, 0)

		case "Back":
			m.goTo(hwScreenList, 0)
		}
	}

	return m, nil
}

func (m *hwModel) viewHWList() string {
	var b strings.Builder

	fmt.Fprintf(&b, "  %s\n", helpStyle.Render(m.store.Dir))
	if m.loading {
		fmt.Fprintf(&b, "  %s\n", helpStyle.Render("asking the robot..."))
	} else if m.serial != "" {
		robot := "robot: " + m.serial
		if m.active != "" {
			robot += "   active: " + m.active
		}
		fmt.Fprintf(&b, "  %s\n", helpStyle.Render(robot))
	}
	b.WriteString("\n")

	if len(m.entries) == 0 {
		b.WriteString("  " + unsetStyle.Render("No configurations yet.") + "\n\n")
	} else {
		// Two spaces, matching the width of the cursor on the rows below.
		fmt.Fprintf(&b, "  %-28s %-18s %s\n",
			helpStyle.Render("CONFIGURATION"), helpStyle.Render("WHERE"), helpStyle.Render("STATUS"))
	}

	visible := m.visibleRows()
	total := len(m.entries) + len(hwListExtras)

	for i := 0; i < total; i++ {
		if i < m.offset || i >= m.offset+visible {
			continue
		}

		// A blank line between the configurations and the actions below them.
		// Printed rather than counted, so it cannot shift the cursor.
		if i == len(m.entries) && len(m.entries) > 0 {
			b.WriteString("\n")
		}

		cursor := "  "
		if i == m.cursor {
			cursor = cursorOn.Render("> ")
		}

		if i < len(m.entries) {
			e := m.entries[i]

			name := e.Name
			if e.Active {
				name += " *"
			}

			status := e.status()
			styled := valueStyle.Render(status)
			if status == "differs" {
				styled = scrollStyle.Render(status)
			} else if !e.OnRobot || !e.InLocal {
				styled = unsetStyle.Render(status)
			}

			fmt.Fprintf(&b, "%s%-28s %-18s %s\n", cursor, name, e.where(), styled)
			continue
		}

		fmt.Fprintf(&b, "%s%s\n", cursor, hwListExtras[i-len(m.entries)])
	}

	if total > visible {
		fmt.Fprintf(&b, "\n  %s\n", scrollStyle.Render(fmt.Sprintf("%d-%d of %d",
			m.offset+1, min(m.offset+visible, total), total)))
	}

	if m.active != "" {
		b.WriteString("\n  " + helpStyle.Render("* is the configuration the robot is running") + "\n")
	}

	b.WriteString("\n" + helpStyle.Render("  enter open   up/down move   q quit") + "\n")
	return b.String()
}

func (m *hwModel) viewHWActions() string {
	var b strings.Builder

	e := m.entry(m.sel)

	fmt.Fprintf(&b, "  %s\n", titleStyle.Render(m.sel))
	fmt.Fprintf(&b, "  %s\n\n", helpStyle.Render(e.where()+"   "+e.status()))

	for i, item := range m.actionItems() {
		cursor := "  "
		if i == m.cursor {
			cursor = cursorOn.Render("> ")
		}
		fmt.Fprintf(&b, "%s%s\n", cursor, item)
	}

	b.WriteString("\n" + helpStyle.Render("  enter choose   esc back") + "\n")
	return b.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
