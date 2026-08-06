package robotcfg

import (
	"fmt"
	"sort"
	"strings"
)

// Level separates what the robot will refuse from what merely looks wrong.
type Level int

// Warning is worth reading before a match; Error is what the robot rejects.
const (
	Warning Level = iota

	Error
)

// String names the level for showing a person.
func (l Level) String() string {
	if l == Error {
		return "error"
	}
	return "warning"
}

// Issue is one problem found in a configuration.
type Issue struct {
	Level Level
	Line  int
	Msg   string
}

// String renders the issue with its line number.
func (i Issue) String() string {
	if i.Line > 0 {
		return fmt.Sprintf("line %d: %s", i.Line, i.Msg)
	}
	return i.Msg
}

// Issues is what a check produced.
type Issues []Issue

// Errors reports whether anything here would stop the robot using the file.
func (is Issues) Errors() bool {
	for _, i := range is {
		if i.Level == Error {
			return true
		}
	}
	return false
}

// Count returns how many issues are at a level.
func (is Issues) Count(l Level) int {
	n := 0
	for _, i := range is {
		if i.Level == l {
			n++
		}
	}
	return n
}

// Validate checks a configuration against the rules the robot controller enforces.
func Validate(cfg *Config) Issues {
	var issues Issues

	issues = append(issues, duplicateNames(cfg)...)

	for _, portal := range cfg.Portals {
		issues = append(issues, checkPortal(portal)...)
	}

	sort.SliceStable(issues, func(a, b int) bool {
		return issues[a].Line < issues[b].Line
	})

	return issues
}

func duplicateNames(cfg *Config) Issues {
	first := map[string]Device{}
	var issues Issues

	for _, d := range cfg.Named() {
		if !d.Enabled() {
			continue
		}

		if earlier, seen := first[d.Name]; seen {
			issues = append(issues, Issue{
				Level: Error,
				Line:  d.Line,
				Msg: fmt.Sprintf("two devices are called %q (also on line %d) - "+
					"hardwareMap cannot tell them apart", d.Name, earlier.Line),
			})
			continue
		}

		first[d.Name] = d
	}

	return issues
}

func checkPortal(p Portal) Issues {
	var issues Issues

	if p.Name == "" {
		issues = append(issues, Issue{Warning, p.Line, fmt.Sprintf("<%s> has no name", p.Tag)})
	}

	addresses := map[int]Module{}

	for _, m := range p.Modules {
		if !m.HasAddress {
			issues = append(issues, Issue{Error, m.Line,
				fmt.Sprintf("%s %q has no address (port=)", m.Tag, m.Name)})
		} else {
			if earlier, seen := addresses[m.Address]; seen {
				issues = append(issues, Issue{Error, m.Line,
					fmt.Sprintf("two hubs are at address %d (%q on line %d, %q here)",
						m.Address, earlier.Name, earlier.Line, m.Name)})
			}
			addresses[m.Address] = m

			issues = append(issues, checkAddress(m, p)...)
		}

		if m.Name == "" {
			issues = append(issues, Issue{Warning, m.Line,
				fmt.Sprintf("%s at address %d has no name", m.Tag, m.Address)})
		}

		issues = append(issues, checkPorts(m)...)
	}

	for _, d := range p.Devices {
		issues = append(issues, checkName(d)...)
	}

	return issues
}

func checkAddress(m Module, p Portal) Issues {
	var issues Issues

	if m.Address == ControlHubAddress && p.HasParent && p.ParentAddress != ControlHubAddress {
		issues = append(issues, Issue{Error, m.Line,
			fmt.Sprintf("%q is at address %d, which is reserved for the Control Hub - "+
				"change the Expansion Hub's address and rebuild the configuration",
				m.Name, ControlHubAddress)})
	}

	if m.Address > MaxUnreservedAddress && m.Address != ControlHubAddress {
		issues = append(issues, Issue{Warning, m.Line,
			fmt.Sprintf("%q is at address %d; addresses above %d are reserved for system use",
				m.Name, m.Address, MaxUnreservedAddress)})
	}

	if m.Address < 1 {
		issues = append(issues, Issue{Error, m.Line,
			fmt.Sprintf("%q is at address %d; hub addresses start at 1", m.Name, m.Address)})
	}

	return issues
}

type slot struct {
	flavor Flavor
	bus    int
	port   int
}

func checkPorts(m Module) Issues {
	var issues Issues
	taken := map[slot]Device{}

	for _, d := range m.Devices {
		issues = append(issues, checkName(d)...)

		if !d.Enabled() {
			continue
		}

		flavor := FlavorOf(d.Tag)
		if flavor == Unclassified {

			continue
		}

		if !d.HasPort {
			issues = append(issues, Issue{Error, d.Line,
				fmt.Sprintf("%q has no port", d.Name)})
			continue
		}

		issues = append(issues, checkRange(d, flavor, m)...)

		key := slot{flavor: flavor, port: d.Port}
		if flavor == I2C {
			key.bus = d.Bus
		}

		if earlier, seen := taken[key]; seen {
			issues = append(issues, Issue{Error, d.Line,
				fmt.Sprintf("%q and %q (line %d) are both on %s of %q",
					d.Name, earlier.Name, earlier.Line, describe(flavor, d), m.Name)})
			continue
		}
		taken[key] = d
	}

	return issues
}

func describe(f Flavor, d Device) string {
	if f == I2C {
		return fmt.Sprintf("I2C bus %d port %d", d.Bus, d.Port)
	}
	return fmt.Sprintf("%s port %d", f, d.Port)
}

func checkRange(d Device, f Flavor, m Module) Issues {
	var issues Issues

	if f == I2C {
		if d.HasBus && (d.Bus < 0 || d.Bus >= Buses) {
			issues = append(issues, Issue{Error, d.Line,
				fmt.Sprintf("%q is on I2C bus %d; %q has buses 0-%d",
					d.Name, d.Bus, m.Name, Buses-1)})
		}
		return issues
	}

	if ports := f.Ports(); ports > 0 && (d.Port < 0 || d.Port >= ports) {
		issues = append(issues, Issue{Error, d.Line,
			fmt.Sprintf("%q is on %s port %d; %q has %s ports 0-%d",
				d.Name, f, d.Port, m.Name, f, ports-1)})
	}

	return issues
}

func checkName(d Device) Issues {
	if !d.Enabled() {
		return nil
	}

	var issues Issues

	if strings.TrimSpace(d.Name) != d.Name {
		issues = append(issues, Issue{Error, d.Line,
			fmt.Sprintf("%q has leading or trailing whitespace in its name; "+
				"hardwareMap.get would need the spaces too", d.Name)})
	}

	if strings.TrimSpace(d.Name) == "" {
		issues = append(issues, Issue{Error, d.Line,
			fmt.Sprintf("a <%s> has no name", d.Tag)})
	}

	return issues
}
