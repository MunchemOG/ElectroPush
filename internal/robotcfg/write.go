package robotcfg

import (
	"fmt"
	"strconv"
	"strings"
)

// Write renders a configuration back to XML.
//
// The output has to match what the Driver Station writes, not merely be valid
// XML: a file that comes back reformatted turns every edit into an unreadable
// diff and makes it impossible to tell a real change from a round trip. So the
// declaration, the indentation and the attribute order are all reproduced from
// the file that was read, and attributes the model does not understand are
// carried through untouched.
//
// Parsing a file and writing it straight back returns the original bytes. That
// is what TestWritingBackWhatWasReadIsByteIdentical pins down.
func Write(cfg *Config) []byte {
	var b strings.Builder

	indent := cfg.Indent
	if indent == "" {
		indent = "    "
	}

	if cfg.Declaration != "" {
		b.WriteString(cfg.Declaration)
		b.WriteString("\n")
	}

	b.WriteString("<" + RootTag)
	writeAttrs(&b, rootAttrs(cfg))
	b.WriteString(">\n")

	for _, portal := range cfg.Portals {
		writePortal(&b, portal, indent)
	}

	b.WriteString("</" + RootTag + ">")

	trailer := cfg.Trailer
	if trailer == "" {
		trailer = "\n"
	}
	b.WriteString(trailer)

	return []byte(b.String())
}

func rootAttrs(cfg *Config) []Attr {
	if len(cfg.RootAttrs) > 0 {
		return cfg.RootAttrs
	}
	return []Attr{{Name: "type", Value: RootType}}
}

func writePortal(b *strings.Builder, p Portal, indent string) {
	b.WriteString(indent + "<" + p.Tag)
	writeAttrs(b, portalAttrs(p))

	// A portal with nothing under it keeps the shape it arrived in: the SDK
	// writes an unpopulated webcam self-closed and a hub chain with children.
	if len(p.Modules) == 0 && len(p.Devices) == 0 && p.SelfClosing {
		b.WriteString(" />\n")
		return
	}

	b.WriteString(">\n")

	for _, d := range p.Devices {
		writeDevice(b, d, strings.Repeat(indent, 2))
	}
	for _, m := range p.Modules {
		writeModule(b, m, indent)
	}

	b.WriteString(indent + "</" + p.Tag + ">\n")
}

func writeModule(b *strings.Builder, m Module, indent string) {
	prefix := strings.Repeat(indent, 2)

	b.WriteString(prefix + "<" + m.Tag)
	writeAttrs(b, moduleAttrs(m))

	if len(m.Devices) == 0 && m.SelfClosing {
		b.WriteString(" />\n")
		return
	}

	b.WriteString(">\n")

	for _, d := range m.Devices {
		writeDevice(b, d, strings.Repeat(indent, 3))
	}

	b.WriteString(prefix + "</" + m.Tag + ">\n")
}

// writeDevice always self-closes. Every device element the Driver Station
// writes is a leaf.
func writeDevice(b *strings.Builder, d Device, prefix string) {
	b.WriteString(prefix + "<" + d.Tag)
	writeAttrs(b, deviceAttrs(d))
	b.WriteString(" />\n")
}

func writeAttrs(b *strings.Builder, list []Attr) {
	for _, a := range list {
		fmt.Fprintf(b, " %s=%q", a.Name, escapeAttr(a.Value))
	}
}

// escapeAttr covers what can legally appear in an attribute written with double
// quotes. Device names come from people, so an ampersand in one is not
// far-fetched.
func escapeAttr(value string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		`"`, "&quot;",
		"<", "&lt;",
		">", "&gt;",
	).Replace(value)
}

// deviceAttrs rebuilds the attribute list from the edited fields, keeping every
// attribute that was there in its original position.
//
// Editing a name must change the name attribute in place rather than appending
// a second one, and an attribute the model knows nothing about - a webcam's
// serial number, an Ethernet device's IP - has to survive untouched.
func deviceAttrs(d Device) []Attr {
	known := map[string]string{}
	if d.Name != "" || has(d.Attrs, "name") {
		known["name"] = d.Name
	}
	if d.HasPort {
		known["port"] = strconv.Itoa(d.Port)
	}
	if d.HasBus {
		known["bus"] = strconv.Itoa(d.Bus)
	}

	return merge(d.Attrs, known, []string{"name", "port", "bus"})
}

func moduleAttrs(m Module) []Attr {
	known := map[string]string{"name": m.Name}
	if m.HasAddress {
		known["port"] = strconv.Itoa(m.Address)
	}

	return merge(m.Attrs, known, []string{"name", "port"})
}

func portalAttrs(p Portal) []Attr {
	known := map[string]string{"name": p.Name}
	if p.Serial != "" || has(p.Attrs, "serialNumber") {
		known["serialNumber"] = p.Serial
	}
	if p.HasParent {
		known["parentModuleAddress"] = strconv.Itoa(p.ParentAddress)
	}

	return merge(p.Attrs, known, []string{"name", "serialNumber", "parentModuleAddress"})
}

// merge overwrites the modelled attributes where they already appear and
// appends the ones that do not, in the order given.
//
// Only the first occurrence of a name is updated: the SDK's Ethernet writer
// emits name= twice, and the Driver Station reads the first, so rewriting both
// would change which value wins if they ever differed.
func merge(original []Attr, values map[string]string, order []string) []Attr {
	out := make([]Attr, 0, len(original)+len(order))
	seen := map[string]bool{}

	for _, a := range original {
		if value, modelled := values[a.Name]; modelled && !seen[a.Name] {
			a.Value = value
		}
		seen[a.Name] = true
		out = append(out, a)
	}

	for _, name := range order {
		if value, modelled := values[name]; modelled && !seen[name] {
			out = append(out, Attr{Name: name, Value: value})
			seen[name] = true
		}
	}

	return out
}

func has(list []Attr, name string) bool {
	for _, a := range list {
		if a.Name == name {
			return true
		}
	}
	return false
}
