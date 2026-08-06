package robotcfg

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// RootTag is the element every configuration is wrapped in.
const RootTag = "Robot"

// RootType is what the Driver Station stamps on that element.
const RootType = "FirstInspires-FTC"

const disabledName = "NO$DEVICE$ATTACHED"

// Attr is one XML attribute, kept in the order it was written.
type Attr struct {
	Name  string
	Value string
}

// Device is one entry under a module: a motor, a servo, a sensor.
type Device struct {
	Tag  string
	Name string

	Port    int
	HasPort bool

	Bus    int
	HasBus bool
	Line   int
	Attrs  []Attr
}

// Enabled reports whether the device occupies its port.
func (d Device) Enabled() bool {
	return d.Name != "" && d.Name != disabledName && d.Tag != "Nothing"
}

// Module is one hub on the RS-485 chain.
type Module struct {
	Tag  string
	Name string

	Address    int
	HasAddress bool
	Devices    []Device
	Line       int
	Attrs      []Attr

	SelfClosing bool
}

// Portal is a top-level element: a hub chain, a webcam, an Ethernet device.
type Portal struct {
	Tag           string
	Name          string
	Serial        string
	ParentAddress int
	HasParent     bool
	Modules       []Module

	Devices []Device
	Line    int
	Attrs   []Attr

	SelfClosing bool
}

// Config is a parsed hardware configuration.
type Config struct {
	Portals []Portal

	Raw []byte

	Declaration string

	RootAttrs []Attr

	Indent string

	Trailer string
}

// Deliberately more forgiving than a schema check: real files contain oddities
// the SDK itself produces, and rejecting those would make this useless.
func Parse(data []byte) (*Config, error) {
	cfg := &Config{Raw: data}

	dec := xml.NewDecoder(bytes.NewReader(data))

	dec.CharsetReader = passthroughCharset

	var (
		portal    *Portal
		module    *Module
		seenRoot  bool
		rootFound bool
	)

	for {
		offset := dec.InputOffset()

		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineAt(data, offset), err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			line := lineAt(data, offset)
			name := t.Name.Local
			closed := selfClosing(data, dec.InputOffset())

			switch {
			case name == RootTag:
				rootFound = true
				if attr(t, "type") != RootType && attr(t, "type") != RootType+"-template" {
					return nil, fmt.Errorf(
						"line %d: <%s type=%q>, expected type=%q - this is not a hardware configuration",
						line, name, attr(t, "type"), RootType)
				}
				seenRoot = true
				cfg.RootAttrs = attrs(t)

			case !seenRoot:

			case module != nil:
				module.Devices = append(module.Devices, device(t, line))

			case isModuleTag(name) && portal != nil:
				address, hasAddress := intAttr(t, "port")
				module = &Module{
					Tag:         name,
					Name:        attr(t, "name"),
					Address:     address,
					HasAddress:  hasAddress,
					Line:        line,
					Attrs:       attrs(t),
					SelfClosing: closed,
				}

			case portal != nil:
				portal.Devices = append(portal.Devices, device(t, line))

			default:
				parent, hasParent := intAttr(t, "parentModuleAddress")
				portal = &Portal{
					Tag:           name,
					Name:          attr(t, "name"),
					Serial:        attr(t, "serialNumber"),
					ParentAddress: parent,
					HasParent:     hasParent,
					Line:          line,
					Attrs:         attrs(t),
					SelfClosing:   closed,
				}
			}

		case xml.EndElement:
			switch {
			case module != nil && t.Name.Local == module.Tag:
				portal.Modules = append(portal.Modules, *module)
				module = nil
			case module == nil && portal != nil && t.Name.Local == portal.Tag:
				cfg.Portals = append(cfg.Portals, *portal)
				portal = nil
			}
		}
	}

	if !rootFound {
		return nil, fmt.Errorf("no <%s> element - this is not a hardware configuration", RootTag)
	}

	cfg.Declaration = declarationOf(data)
	cfg.Indent = indentOf(data)
	cfg.Trailer = trailerOf(data)

	return cfg, nil
}

func selfClosing(data []byte, offset int64) bool {
	if offset < 2 || offset > int64(len(data)) {
		return false
	}
	return data[offset-2] == '/' && data[offset-1] == '>'
}

func declarationOf(data []byte) string {
	open := bytes.Index(data, []byte("<?"))
	if open != 0 {
		return ""
	}

	close := bytes.Index(data, []byte("?>"))
	if close < 0 {
		return ""
	}

	return string(data[:close+2])
}

func indentOf(data []byte) string {
	for _, line := range bytes.Split(data, []byte("\n")) {
		trimmed := bytes.TrimLeft(line, " \t")
		if len(trimmed) == 0 || trimmed[0] != '<' {
			continue
		}
		if indent := line[:len(line)-len(trimmed)]; len(indent) > 0 {
			return string(indent)
		}
	}
	return "    "
}

func trailerOf(data []byte) string {
	close := bytes.LastIndex(data, []byte("</"+RootTag+">"))
	if close < 0 {
		return "\n"
	}
	return string(data[close+len("</"+RootTag+">"):])
}

func isModuleTag(name string) bool {
	switch name {
	case "LynxModule", "RhspModule", "ServoHub":
		return true
	}
	return false
}

func device(t xml.StartElement, line int) Device {
	port, hasPort := intAttr(t, "port")
	bus, hasBus := intAttr(t, "bus")

	return Device{
		Tag:     t.Name.Local,
		Name:    attr(t, "name"),
		Port:    port,
		HasPort: hasPort,
		Bus:     bus,
		HasBus:  hasBus,
		Line:    line,
		Attrs:   attrs(t),
	}
}

func attrs(t xml.StartElement) []Attr {
	out := make([]Attr, 0, len(t.Attr))
	for _, a := range t.Attr {
		out = append(out, Attr{Name: a.Name.Local, Value: a.Value})
	}
	return out
}

// First match, not last: the SDK's Ethernet writer emits name= twice and the
// Driver Station reads the first.
func attr(t xml.StartElement, name string) string {
	for _, a := range t.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func intAttr(t xml.StartElement, name string) (int, bool) {
	raw := attr(t, name)
	if raw == "" {
		return 0, false
	}

	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, false
	}
	return value, true
}

func lineAt(data []byte, offset int64) int {
	if offset < 0 {
		offset = 0
	}
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}

	start := int(offset)
	if next := bytes.IndexByte(data[start:], '<'); next >= 0 {
		start += next
	}

	return bytes.Count(data[:start], []byte("\n")) + 1
}

func passthroughCharset(_ string, input io.Reader) (io.Reader, error) {
	return input, nil
}

// Devices lists every device in the file, in document order.
func (c *Config) Devices() []Device {
	var devices []Device
	for _, p := range c.Portals {
		devices = append(devices, p.Devices...)
		for _, m := range p.Modules {
			devices = append(devices, m.Devices...)
		}
	}
	return devices
}

// AsDevice presents a portal as something with a name and a line.
func (p Portal) AsDevice() Device {
	return Device{Tag: p.Tag, Name: p.Name, Port: -1, Line: p.Line}
}

// InHardwareMap reports whether a portal's own name is resolvable from an OpMode.
func (p Portal) InHardwareMap() bool {
	return len(p.Modules) == 0 && p.Name != ""
}

// Named lists everything that occupies a name in the hardware map.
func (c *Config) Named() []Device {
	var named []Device
	for _, p := range c.Portals {
		if p.InHardwareMap() {
			named = append(named, p.AsDevice())
		}
		named = append(named, p.Devices...)
		for _, m := range p.Modules {
			named = append(named, m.Devices...)
		}
	}
	return named
}

// Names lists the names an OpMode can look up.
func (c *Config) Names() []string {
	var names []string
	for _, d := range c.Named() {
		if d.Enabled() {
			names = append(names, d.Name)
		}
	}
	return names
}
