package robotcfg

import (
	"fmt"
	"sort"
	"strings"
)

// Slot identifies where a device sits, without holding a pointer into the config.
type Slot struct {
	Portal int

	Module int
	Device int
}

// New builds an empty configuration with a Control Hub in it.
func New() *Config {
	return &Config{
		Declaration: `<?xml version='1.0' encoding='UTF-8' standalone='yes' ?>`,
		RootAttrs:   []Attr{{Name: "type", Value: RootType}},
		Indent:      "    ",
		Trailer:     "\n",
		Portals: []Portal{{
			Tag:           "LynxUsbDevice",
			Name:          "Control Hub Portal",
			Serial:        "(embedded)",
			ParentAddress: ControlHubAddress,
			HasParent:     true,
			Modules: []Module{{
				Tag:        "LynxModule",
				Name:       "Control Hub",
				Address:    ControlHubAddress,
				HasAddress: true,
			}},
		}},
	}
}

// Clone returns a copy that can be edited without touching the original.
func Clone(cfg *Config) *Config {
	out := *cfg
	out.RootAttrs = append([]Attr(nil), cfg.RootAttrs...)
	out.Portals = make([]Portal, len(cfg.Portals))

	for i, p := range cfg.Portals {
		p.Attrs = append([]Attr(nil), p.Attrs...)
		p.Devices = cloneDevices(p.Devices)

		modules := make([]Module, len(p.Modules))
		for j, m := range p.Modules {
			m.Attrs = append([]Attr(nil), m.Attrs...)
			m.Devices = cloneDevices(m.Devices)
			modules[j] = m
		}
		p.Modules = modules

		out.Portals[i] = p
	}

	return &out
}

func cloneDevices(devices []Device) []Device {
	out := make([]Device, len(devices))
	for i, d := range devices {
		d.Attrs = append([]Attr(nil), d.Attrs...)
		out[i] = d
	}
	return out
}

// DeviceAt returns the device a slot points at.
func (c *Config) DeviceAt(s Slot) (Device, bool) {
	list, ok := c.devicesAt(s)
	if !ok || s.Device < 0 || s.Device >= len(*list) {
		return Device{}, false
	}
	return (*list)[s.Device], true
}

// SetDevice replaces the device a slot points at.
func (c *Config) SetDevice(s Slot, d Device) error {
	list, ok := c.devicesAt(s)
	if !ok || s.Device < 0 || s.Device >= len(*list) {
		return fmt.Errorf("no device there")
	}

	d.Attrs = (*list)[s.Device].Attrs
	(*list)[s.Device] = d
	return nil
}

// AddDevice puts a device on a module, in port order.
func (c *Config) AddDevice(portal, module int, d Device) error {
	if portal < 0 || portal >= len(c.Portals) {
		return fmt.Errorf("no such portal")
	}
	if module < 0 || module >= len(c.Portals[portal].Modules) {
		return fmt.Errorf("no such hub")
	}

	list := &c.Portals[portal].Modules[module].Devices
	*list = append(*list, d)
	sortDevices(*list)

	return nil
}

// RemoveDevice deletes the device a slot points at.
func (c *Config) RemoveDevice(s Slot) error {
	list, ok := c.devicesAt(s)
	if !ok || s.Device < 0 || s.Device >= len(*list) {
		return fmt.Errorf("no device there")
	}

	*list = append((*list)[:s.Device], (*list)[s.Device+1:]...)
	return nil
}

func (c *Config) devicesAt(s Slot) (*[]Device, bool) {
	if s.Portal < 0 || s.Portal >= len(c.Portals) {
		return nil, false
	}

	p := &c.Portals[s.Portal]
	if s.Module < 0 {
		return &p.Devices, true
	}
	if s.Module >= len(p.Modules) {
		return nil, false
	}

	return &p.Modules[s.Module].Devices, true
}

func sortDevices(devices []Device) {
	sort.SliceStable(devices, func(a, b int) bool {
		fa, fb := FlavorOf(devices[a].Tag), FlavorOf(devices[b].Tag)
		if fa != fb {
			return fa < fb
		}
		if devices[a].Bus != devices[b].Bus {
			return devices[a].Bus < devices[b].Bus
		}
		return devices[a].Port < devices[b].Port
	})
}

// AddModule puts another hub on a portal, at the lowest free address.
func (c *Config) AddModule(portal int) (int, error) {
	if portal < 0 || portal >= len(c.Portals) {
		return 0, fmt.Errorf("no such portal")
	}

	taken := map[int]bool{}
	for _, m := range c.Portals[portal].Modules {
		taken[m.Address] = true
	}

	address := 0
	for candidate := 1; candidate <= MaxUnreservedAddress; candidate++ {
		if !taken[candidate] {
			address = candidate
			break
		}
	}
	if address == 0 {
		return 0, fmt.Errorf("every address from 1 to %d is taken", MaxUnreservedAddress)
	}

	c.Portals[portal].Modules = append(c.Portals[portal].Modules, Module{
		Tag:        "LynxModule",
		Name:       fmt.Sprintf("Expansion Hub %d", address),
		Address:    address,
		HasAddress: true,
	})

	return len(c.Portals[portal].Modules) - 1, nil
}

// RemoveModule deletes a hub and everything on it.
func (c *Config) RemoveModule(portal, module int) error {
	if portal < 0 || portal >= len(c.Portals) {
		return fmt.Errorf("no such portal")
	}
	list := &c.Portals[portal].Modules
	if module < 0 || module >= len(*list) {
		return fmt.Errorf("no such hub")
	}

	*list = append((*list)[:module], (*list)[module+1:]...)
	return nil
}

// FreePort is the lowest port of a flavour nothing is using on a module.
func (c *Config) FreePort(portal, module int, f Flavor, bus int) (int, bool) {
	if portal < 0 || portal >= len(c.Portals) {
		return 0, false
	}
	if module < 0 || module >= len(c.Portals[portal].Modules) {
		return 0, false
	}

	taken := map[int]bool{}
	for _, d := range c.Portals[portal].Modules[module].Devices {
		if !d.Enabled() || FlavorOf(d.Tag) != f {
			continue
		}
		if f == I2C && d.Bus != bus {
			continue
		}
		taken[d.Port] = true
	}

	limit := f.Ports()
	if f == I2C {
		limit = 8
	}

	for port := 0; port < limit; port++ {
		if !taken[port] {
			return port, true
		}
	}

	return 0, false
}

// NameTaken reports whether a name is used, ignoring one slot so a rename can keep its own.
func (c *Config) NameTaken(name string, except Slot) bool {
	for pi, p := range c.Portals {
		if p.InHardwareMap() && p.Name == name {
			return true
		}
		for di, d := range p.Devices {
			if d.Enabled() && d.Name == name && (Slot{pi, -1, di}) != except {
				return true
			}
		}
		for mi, m := range p.Modules {
			for di, d := range m.Devices {
				if d.Enabled() && d.Name == name && (Slot{pi, mi, di}) != except {
					return true
				}
			}
		}
	}
	return false
}

// SuggestTags returns the device types matching what has been typed, best match first.
func SuggestTags(typed string) []string {
	all := KnownTags()
	sort.Strings(all)

	query := strings.ToLower(strings.TrimSpace(typed))
	if query == "" {
		return all
	}

	var prefix, contains []string
	for _, tag := range all {
		lower := strings.ToLower(tag)
		switch {
		case strings.HasPrefix(lower, query):
			prefix = append(prefix, tag)
		case strings.Contains(lower, query):
			contains = append(contains, tag)
		}
	}

	return append(prefix, contains...)
}
