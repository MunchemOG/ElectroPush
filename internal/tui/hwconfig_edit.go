package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/andreibanu/pusher/internal/robotcfg"
	tea "github.com/charmbracelet/bubbletea"
)

// openEditor loads the configuration and builds the device tree.
func (m *hwModel) openEditor() tea.Cmd {
	if err := m.loadConfig(); err != nil {
		m.err = err
		return nil
	}

	m.rebuildRows()
	m.goTo(hwScreenDevices, m.firstSelectable())
	return nil
}

func (m *hwModel) loadConfig() error {
	data, err := m.store.Read(m.sel)
	if err != nil {
		return err
	}

	cfg, err := robotcfg.Parse(data)
	if err != nil {
		return fmt.Errorf("%s does not parse: %w", m.sel, err)
	}

	// Edits happen on a copy, so backing out of the editor leaves the file
	// exactly as it was.
	m.cfg = robotcfg.Clone(cfg)
	m.saved = string(data)
	m.dirty = false
	m.revalidate()

	return nil
}

func (m *hwModel) revalidate() {
	if m.cfg == nil {
		m.issues = nil
		return
	}
	m.issues = robotcfg.Validate(m.cfg)
}

// rebuildRows flattens the configuration into the lines the editor shows.
//
// Every action has a row of its own - adding a device, adding a hub - so the
// editor can be used without knowing a single shortcut.
func (m *hwModel) rebuildRows() {
	m.rows = m.rows[:0]
	if m.cfg == nil {
		return
	}

	worst := map[int]robotcfg.Level{}
	present := map[int]bool{}
	for _, issue := range m.issues {
		if level, seen := worst[issue.Line]; !seen || issue.Level > level {
			worst[issue.Line] = issue.Level
		}
		present[issue.Line] = true
	}

	for pi, portal := range m.cfg.Portals {
		label := portal.Name
		if label == "" {
			label = "<" + portal.Tag + ">"
		}

		m.rows = append(m.rows, hwRow{
			Kind:   hwRowPortal,
			Label:  label,
			Detail: portal.Tag,
			Portal: pi,
			Module: -1,
		})

		for di, d := range portal.Devices {
			m.rows = append(m.rows, m.deviceRow(d, robotcfg.Slot{Portal: pi, Module: -1, Device: di}, pi, -1, worst, present))
		}

		for mi, module := range portal.Modules {
			m.rows = append(m.rows, hwRow{
				Kind:   hwRowModule,
				Label:  module.Name,
				Detail: fmt.Sprintf("address %d", module.Address),
				Portal: pi,
				Module: mi,
				Issue:  worst[module.Line],
				HasIss: present[module.Line],
			})

			for di, d := range module.Devices {
				m.rows = append(m.rows, m.deviceRow(d, robotcfg.Slot{Portal: pi, Module: mi, Device: di}, pi, mi, worst, present))
			}

			m.rows = append(m.rows, hwRow{
				Kind:   hwRowAddDevice,
				Label:  "+ add a device",
				Portal: pi,
				Module: mi,
			})
		}

		if len(portal.Modules) > 0 {
			m.rows = append(m.rows, hwRow{
				Kind:   hwRowAddModule,
				Label:  "+ add an Expansion Hub",
				Portal: pi,
				Module: -1,
			})
		}
	}
}

func (m *hwModel) deviceRow(d robotcfg.Device, slot robotcfg.Slot, portal, module int,
	worst map[int]robotcfg.Level, present map[int]bool) hwRow {

	return hwRow{
		Kind:   hwRowDevice,
		Label:  hwDeviceLabel(d),
		Detail: d.Tag,
		Slot:   slot,
		Portal: portal,
		Module: module,
		Issue:  worst[d.Line],
		HasIss: present[d.Line],
	}
}

func hwDeviceLabel(d robotcfg.Device) string {
	where := ""
	switch {
	case robotcfg.FlavorOf(d.Tag) == robotcfg.I2C && d.HasBus:
		where = fmt.Sprintf("I2C %d.%d", d.Bus, d.Port)
	case d.HasPort && d.Port >= 0:
		where = fmt.Sprintf("%s %d", robotcfg.FlavorOf(d.Tag), d.Port)
	}

	name := d.Name
	if !d.Enabled() {
		name = "(empty)"
	}

	return fmt.Sprintf("%-11s %s", where, name)
}

// firstSelectable opens the editor on the first device rather than the hub
// heading above it, so the cursor starts on something worth pressing enter on.
func (m *hwModel) firstSelectable() int {
	for i, row := range m.rows {
		if row.Kind == hwRowDevice {
			return i
		}
	}

	for i, row := range m.rows {
		if row.selectable() {
			return i
		}
	}
	return 0
}

// moveRows skips the headings, so holding a direction never parks the cursor
// somewhere enter does nothing.
func (m *hwModel) moveRows(delta int) {
	if len(m.rows) == 0 {
		return
	}

	for i := 0; i < len(m.rows); i++ {
		m.cursor = (m.cursor + delta + len(m.rows)) % len(m.rows)
		if m.rows[m.cursor].selectable() {
			break
		}
	}

	m.offset = clampOffset(m.offset, m.cursor, m.visibleRows(), len(m.rows))
}

func (m *hwModel) updateHWDevices(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if len(m.rows) == 0 {
		if key.String() == "esc" {
			m.goTo(hwScreenActions, 0)
		}
		return m, nil
	}

	row := m.rows[m.cursor]

	switch key.String() {
	case "esc", "left", "h":
		if m.dirty {
			m.confirm = hwConfirm{
				title:  "Discard the changes to " + m.sel + "?",
				detail: "They have not been saved.",
				action: "discard",
			}
			m.goTo(hwScreenConfirm, 0)
			return m, nil
		}
		m.goTo(hwScreenActions, 0)

	case "up", "k":
		m.moveRows(-1)
	case "down", "j":
		m.moveRows(1)

	case "enter", " ", "right", "l":
		m.clear()

		switch row.Kind {
		case hwRowDevice:
			m.openDeviceForm(row)
		case hwRowAddDevice:
			m.openAddForm(row.Portal, row.Module)
		case hwRowAddModule:
			m.addModule(row.Portal)
		case hwRowModule:
			m.openAddForm(row.Portal, row.Module)
		}

	case "a":
		m.clear()
		if row.Module >= 0 {
			m.openAddForm(row.Portal, row.Module)
		}

	case "d", "delete", "backspace":
		m.clear()

		switch row.Kind {
		case hwRowDevice:
			device, _ := m.cfg.DeviceAt(row.Slot)
			m.confirm = hwConfirm{
				title:  fmt.Sprintf("Remove %q?", device.Name),
				detail: device.Tag,
				action: "remove-device",
			}
			m.goTo(hwScreenConfirm, 0)

		case hwRowModule:
			module := m.cfg.Portals[row.Portal].Modules[row.Module]
			m.confirm = hwConfirm{
				title: fmt.Sprintf("Remove %q?", module.Name),
				detail: fmt.Sprintf("address %d, and the %d device(s) on it",
					module.Address, len(module.Devices)),
				action: "remove-module",
			}
			m.goTo(hwScreenConfirm, 0)
		}

	case "s":
		m.clear()
		m.save()

	case "p":
		m.clear()
		if m.dirty {
			m.save()
		}
		if m.err == nil && m.serial != "" {
			return m, m.push(m.sel)
		}
	}

	return m, nil
}

func (m *hwModel) addModule(portal int) {
	index, err := m.cfg.AddModule(portal)
	if err != nil {
		m.err = err
		return
	}

	m.dirty = true
	m.revalidate()
	m.rebuildRows()
	m.status = fmt.Sprintf("Added %s", m.cfg.Portals[portal].Modules[index].Name)
}

// save writes the edited configuration back to the project.
func (m *hwModel) save() {
	if m.cfg == nil {
		return
	}

	data := robotcfg.Write(m.cfg)
	if err := m.store.Write(m.sel, data); err != nil {
		m.err = err
		return
	}

	m.saved = string(data)
	m.dirty = false
	m.status = "Saved " + m.store.Path(m.sel)
	m.refreshLocal()
	m.rebuildEntries()
}

// openDeviceForm fills the form from an existing device.
func (m *hwModel) openDeviceForm(row hwRow) {
	device, ok := m.cfg.DeviceAt(row.Slot)
	if !ok {
		return
	}

	m.form = hwForm{
		slot:   row.Slot,
		portal: row.Portal,
		module: row.Module,
		typed:  device.Tag,
		name:   device.Name,
		port:   strconv.Itoa(device.Port),
		bus:    strconv.Itoa(device.Bus),
	}
	if !device.HasPort {
		m.form.port = ""
	}
	if !device.HasBus {
		m.form.bus = ""
	}

	m.refreshSuggestions()
	m.checkForm()
	m.goTo(hwScreenDevice, 0)
}

// openAddForm starts a new device, already pointed at a free port.
func (m *hwModel) openAddForm(portal, module int) {
	if module < 0 {
		return
	}

	m.form = hwForm{
		adding: true,
		portal: portal,
		module: module,
		slot:   robotcfg.Slot{Portal: portal, Module: module, Device: -1},
	}

	m.refreshSuggestions()
	m.checkForm()
	m.goTo(hwScreenDevice, 0)
}

func (m *hwModel) refreshSuggestions() {
	m.form.suggest = robotcfg.SuggestTags(m.form.typed)
	if m.form.pick >= len(m.form.suggest) {
		m.form.pick = 0
	}
}

// applyType fills in what follows from a device type: a free port of the right
// flavour, and a bus for an I2C device. Choosing a type is most of the work,
// and everything after it has a sensible answer.
func (m *hwModel) applyType(tag string) {
	m.form.typed = tag

	flavor := robotcfg.FlavorOf(tag)
	if flavor == robotcfg.Unclassified {
		if m.form.port == "" {
			m.form.port = "0"
		}
		return
	}

	if flavor == robotcfg.I2C {
		if m.form.bus == "" {
			m.form.bus = "0"
		}
	} else {
		m.form.bus = ""
	}

	if m.form.adding || m.form.port == "" {
		bus, _ := strconv.Atoi(m.form.bus)
		if port, ok := m.cfg.FreePort(m.form.portal, m.form.module, flavor, bus); ok {
			m.form.port = strconv.Itoa(port)
		} else if m.form.port == "" {
			m.form.port = "0"
		}
	}
}

func (m *hwModel) formFields() []hwField {
	fields := []hwField{hwFieldType, hwFieldName, hwFieldPort}
	if robotcfg.FlavorOf(m.form.typed) == robotcfg.I2C {
		fields = append(fields, hwFieldBus)
	}
	return fields
}

func (m *hwModel) updateHWDevice(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	fields := m.formFields()

	index := 0
	for i, f := range fields {
		if f == m.form.field {
			index = i
		}
	}

	switch key.String() {
	case "esc":
		m.goTo(hwScreenDevices, m.cursor)
		return m, nil

	case "tab", "down":
		// On the type field the list is what down moves through, since that is
		// the whole point of it.
		if m.form.field == hwFieldType && key.String() == "down" {
			if len(m.form.suggest) > 0 {
				m.form.pick = (m.form.pick + 1) % len(m.form.suggest)
			}
			return m, nil
		}
		m.form.field = fields[(index+1)%len(fields)]
		return m, nil

	case "shift+tab", "up":
		if m.form.field == hwFieldType && key.String() == "up" {
			if len(m.form.suggest) > 0 {
				m.form.pick = (m.form.pick - 1 + len(m.form.suggest)) % len(m.form.suggest)
			}
			return m, nil
		}
		m.form.field = fields[(index-1+len(fields))%len(fields)]
		return m, nil

	case "enter":
		// On the type field, enter takes the highlighted suggestion rather than
		// saving, so a half-typed name never becomes a device type.
		if m.form.field == hwFieldType && len(m.form.suggest) > 0 {
			m.applyType(m.form.suggest[m.form.pick])
			m.refreshSuggestions()
			m.checkForm()
			m.form.field = hwFieldName
			return m, nil
		}
		return m, m.commitForm()

	case "backspace":
		m.editField(func(s string) string {
			if s == "" {
				return s
			}
			return s[:len(s)-1]
		})
		return m, nil
	}

	if r := key.Runes; len(r) == 1 && key.Type == tea.KeyRunes {
		m.editField(func(s string) string { return s + string(r) })
	}

	return m, nil
}

func (m *hwModel) editField(edit func(string) string) {
	switch m.form.field {
	case hwFieldType:
		m.form.typed = edit(m.form.typed)
		m.form.pick = 0
		m.refreshSuggestions()
	case hwFieldName:
		m.form.name = edit(m.form.name)
	case hwFieldPort:
		m.form.port = digitsOnly(edit(m.form.port))
	case hwFieldBus:
		m.form.bus = digitsOnly(edit(m.form.bus))
	}

	m.checkForm()
}

// digitsOnly keeps a port field numeric, with a leading minus allowed because
// an Ethernet device carries port="-1".
func digitsOnly(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= '0' && r <= '9' || (i == 0 && r == '-') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// checkForm says what is wrong while it is being typed, rather than after
// saving. Catching a duplicate name at the moment it is typed is most of what
// makes this easier than editing the XML.
func (m *hwModel) checkForm() {
	m.form.problem = ""

	if strings.TrimSpace(m.form.typed) == "" {
		m.form.problem = "pick a device type"
		return
	}

	name := m.form.name
	if strings.TrimSpace(name) == "" {
		m.form.problem = "give it a name"
		return
	}
	if strings.TrimSpace(name) != name {
		m.form.problem = "the name has a space at the start or end"
		return
	}

	except := m.form.slot
	if m.form.adding {
		except = robotcfg.Slot{Portal: -1, Module: -1, Device: -1}
	}
	if m.cfg.NameTaken(name, except) {
		m.form.problem = fmt.Sprintf("%q is already used by something else", name)
		return
	}

	flavor := robotcfg.FlavorOf(m.form.typed)
	if flavor == robotcfg.Unclassified {
		// Nothing is known about this type's ports, so there is nothing left
		// to check. It is still allowed: teams register their own.
		return
	}

	port, err := strconv.Atoi(m.form.port)
	if err != nil {
		m.form.problem = "the port has to be a number"
		return
	}

	if flavor == robotcfg.I2C {
		bus, err := strconv.Atoi(m.form.bus)
		if err != nil {
			m.form.problem = "the bus has to be a number"
			return
		}
		if bus < 0 || bus >= robotcfg.Buses {
			m.form.problem = fmt.Sprintf("a hub has I2C buses 0-%d", robotcfg.Buses-1)
			return
		}
	} else if limit := flavor.Ports(); limit > 0 && (port < 0 || port >= limit) {
		m.form.problem = fmt.Sprintf("a hub has %s ports 0-%d", flavor, limit-1)
		return
	}

	if occupant, taken := m.portTaken(flavor, port); taken {
		m.form.problem = fmt.Sprintf("%q is already on that port", occupant)
	}
}

// portTaken reports what else is on the port the form is pointing at.
func (m *hwModel) portTaken(flavor robotcfg.Flavor, port int) (string, bool) {
	if m.form.module < 0 || m.form.portal < 0 {
		return "", false
	}
	if m.form.portal >= len(m.cfg.Portals) {
		return "", false
	}
	portal := m.cfg.Portals[m.form.portal]
	if m.form.module >= len(portal.Modules) {
		return "", false
	}

	bus, _ := strconv.Atoi(m.form.bus)

	for i, d := range portal.Modules[m.form.module].Devices {
		if !m.form.adding && i == m.form.slot.Device {
			continue
		}
		if !d.Enabled() || robotcfg.FlavorOf(d.Tag) != flavor || d.Port != port {
			continue
		}
		if flavor == robotcfg.I2C && d.Bus != bus {
			continue
		}
		return d.Name, true
	}

	return "", false
}

func (m *hwModel) commitForm() tea.Cmd {
	if m.form.problem != "" {
		return nil
	}

	flavor := robotcfg.FlavorOf(m.form.typed)
	port, _ := strconv.Atoi(m.form.port)
	bus, _ := strconv.Atoi(m.form.bus)

	device := robotcfg.Device{
		Tag:     m.form.typed,
		Name:    m.form.name,
		Port:    port,
		HasPort: m.form.port != "",
		Bus:     bus,
		HasBus:  m.form.bus != "" && flavor == robotcfg.I2C,
	}

	var err error
	if m.form.adding {
		err = m.cfg.AddDevice(m.form.portal, m.form.module, device)
	} else {
		err = m.cfg.SetDevice(m.form.slot, device)
	}

	if err != nil {
		m.err = err
		return nil
	}

	m.dirty = true
	m.revalidate()
	m.rebuildRows()
	m.goTo(hwScreenDevices, m.cursor)
	m.status = "Changed " + device.Name + " (s to save)"

	return nil
}
