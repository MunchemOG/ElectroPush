package robotcfg

import (
	"fmt"
	"sort"
	"strings"
)

// Summary renders a configuration the way you would read it off the Driver Station.
func Summary(cfg *Config) string {
	var b strings.Builder

	for _, p := range cfg.Portals {
		fmt.Fprintf(&b, "%s", label(p.Tag, p.Name))
		if p.Serial != "" {
			fmt.Fprintf(&b, "  [%s]", p.Serial)
		}
		b.WriteString("\n")

		for _, d := range p.Devices {
			fmt.Fprintf(&b, "    %s\n", describeDevice(d))
		}

		for _, m := range p.Modules {
			fmt.Fprintf(&b, "  %s  (address %d)\n", label(m.Tag, m.Name), m.Address)
			summariseModule(&b, m)
		}
	}

	if b.Len() == 0 {
		return "  (empty configuration - nothing is configured)\n"
	}

	return b.String()
}

func summariseModule(b *strings.Builder, m Module) {
	groups := map[Flavor][]Device{}
	var order []Flavor

	for _, d := range m.Devices {
		if !d.Enabled() {
			continue
		}
		f := FlavorOf(d.Tag)
		if _, seen := groups[f]; !seen {
			order = append(order, f)
		}
		groups[f] = append(groups[f], d)
	}

	if len(order) == 0 {
		b.WriteString("      (nothing configured)\n")
		return
	}

	sort.Slice(order, func(a, c int) bool { return order[a] < order[c] })

	for _, f := range order {
		devices := groups[f]
		sort.SliceStable(devices, func(a, c int) bool {
			if devices[a].Bus != devices[c].Bus {
				return devices[a].Bus < devices[c].Bus
			}
			return devices[a].Port < devices[c].Port
		})

		for _, d := range devices {
			fmt.Fprintf(b, "      %s\n", describeDevice(d))
		}
	}
}

func describeDevice(d Device) string {
	where := ""
	switch {
	case FlavorOf(d.Tag) == I2C && d.HasBus:
		where = fmt.Sprintf("I2C %d.%d", d.Bus, d.Port)
	case d.HasPort && d.Port >= 0:
		where = fmt.Sprintf("%s %d", FlavorOf(d.Tag), d.Port)
	}

	if where == "" {
		return fmt.Sprintf("%-22s %s", d.Name, d.Tag)
	}
	return fmt.Sprintf("%-10s %-22s %s", where, d.Name, d.Tag)
}

func label(tag, name string) string {
	if name == "" {
		return "<" + tag + ">"
	}
	return name
}

// Diff describes what changed between two configurations, in devices rather than lines.
func Diff(before, after *Config) []string {
	oldDevices := index(before)
	newDevices := index(after)

	var changes []string

	for _, name := range sortedKeys(newDevices) {
		now := newDevices[name]
		was, existed := oldDevices[name]

		if !existed {
			changes = append(changes, fmt.Sprintf("+ %s (%s, %s)", name, now.Tag, position(now)))
			continue
		}

		if was.Tag != now.Tag {
			changes = append(changes, fmt.Sprintf("~ %s is now a %s (was %s)", name, now.Tag, was.Tag))
		}
		if position(was) != position(now) {
			changes = append(changes, fmt.Sprintf("~ %s moved to %s (was %s)", name, position(now), position(was)))
		}
	}

	for _, name := range sortedKeys(oldDevices) {
		if _, still := newDevices[name]; !still {
			was := oldDevices[name]
			changes = append(changes, fmt.Sprintf("- %s (%s, %s)", name, was.Tag, position(was)))
		}
	}

	return changes
}

func position(d Device) string {
	if FlavorOf(d.Tag) == I2C && d.HasBus {
		return fmt.Sprintf("I2C bus %d port %d", d.Bus, d.Port)
	}
	if !d.HasPort {
		return "no port"
	}
	return fmt.Sprintf("%s port %d", FlavorOf(d.Tag), d.Port)
}

func index(cfg *Config) map[string]Device {
	devices := map[string]Device{}
	if cfg == nil {
		return devices
	}
	for _, d := range cfg.Named() {
		if d.Enabled() {
			devices[d.Name] = d
		}
	}
	return devices
}

func sortedKeys(m map[string]Device) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
