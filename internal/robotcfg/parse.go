// Package robotcfg reads, checks and moves FTC hardware configuration files.
//
// A configuration is a single XML file in /sdcard/FIRST on the robot
// controller. The Driver Station writes it; nothing else on the robot depends
// on where it came from, so a file edited on a laptop and copied back is
// indistinguishable from one the Driver Station produced.
//
// Parsing here is for checking and describing only. Files move byte for byte:
// the Driver Station writes them with Android's XmlSerializer, and
// re-serialising through Go would rewrite the quoting and indentation of a file
// nobody asked to reformat.
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

// RootType is what the Driver Station stamps on that element. A file without it
// is not a hardware configuration.
const RootType = "FirstInspires-FTC"

// disabledName marks a port the Driver Station left empty. Several of them can
// appear in one file, so they are excluded from the duplicate-name check the
// same way the SDK excludes them.
const disabledName = "NO$DEVICE$ATTACHED"

// Attr is one XML attribute, kept in the order it was written.
//
// The modelled fields cover what gets edited; everything else - a webcam's
// serial number, an Ethernet device's IP, the duplicated name the SDK's own
// writer emits - is carried here so saving a file never drops what pusher does
// not understand.
type Attr struct {
	Name  string
	Value string
}

// Device is one entry under a module: a motor, a servo, a sensor.
type Device struct {
	Tag  string
	Name string
	// Port is -1 when the attribute is absent. Ethernet devices legitimately
	// carry port="-1", so absence cannot be encoded as a sentinel value.
	Port    int
	HasPort bool
	// Bus is the I2C bus. Only I2C devices carry it.
	Bus    int
	HasBus bool
	Line   int
	Attrs  []Attr
}

// Enabled reports whether the device occupies its port. The Driver Station
// writes a placeholder for ports you left empty.
func (d Device) Enabled() bool {
	return d.Name != "" && d.Name != disabledName && d.Tag != "Nothing"
}

// Module is one device on a hub's RS-485 chain: a Control Hub, an Expansion
// Hub, or a Servo Hub.
type Module struct {
	Tag  string
	Name string
	// Address is the RS-485 address, written as port= on the element. The
	// Control Hub's built-in module is always 173.
	Address    int
	HasAddress bool
	Devices    []Device
	Line       int
	Attrs      []Attr
	// SelfClosing records that the module was written as <LynxModule ... />
	// with no children, so an empty hub keeps the shape it arrived in.
	SelfClosing bool
}

// Portal is a top-level element under <Robot>: a USB-attached hub chain, a
// webcam, an Ethernet device.
type Portal struct {
	Tag           string
	Name          string
	Serial        string
	ParentAddress int
	HasParent     bool
	Modules       []Module
	// Devices holds children of a portal that is not a hub chain, so nothing
	// in the file is silently dropped.
	Devices []Device
	Line    int
	Attrs   []Attr
	// SelfClosing records that the portal was written as <Webcam ... /> rather
	// than with a closing tag.
	SelfClosing bool
}

// Config is a parsed hardware configuration.
type Config struct {
	Portals []Portal
	// Raw is the file exactly as it was read. Anything that moves a
	// configuration without editing it writes this back untouched.
	Raw []byte
	// Declaration is the <?xml ... ?> line as it was written. The Driver
	// Station emits single quotes and standalone='yes'; Go's encoder would
	// not, and reproducing it keeps an unedited save byte-identical.
	Declaration string
	// RootAttrs are the attributes on <Robot>.
	RootAttrs []Attr
	// Indent is the indentation of one level, taken from the file so a save
	// matches whatever wrote it last.
	Indent string
	// Trailer is whatever followed </Robot>, normally a single newline.
	Trailer string
}

// Parse reads a configuration file.
//
// It is deliberately more forgiving than a schema check. Real files contain
// oddities the SDK itself produces — the Limelight writer emits a duplicated
// name attribute — and refusing to read those would make this useless on the
// configurations people actually have.
func Parse(data []byte) (*Config, error) {
	cfg := &Config{Raw: data}

	dec := xml.NewDecoder(bytes.NewReader(data))
	// Configurations are ASCII in practice, but a team name in a comment can
	// carry anything. Without this a stray high byte fails the whole parse.
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
				// Anything before <Robot> is not part of the configuration.

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

// selfClosing reports whether the start tag ending at offset was written as
// "<tag ... />". Go reports a self-closing element as a start followed by an
// end, so the raw bytes are the only way to tell it from "<tag></tag>".
func selfClosing(data []byte, offset int64) bool {
	if offset < 2 || offset > int64(len(data)) {
		return false
	}
	return data[offset-2] == '/' && data[offset-1] == '>'
}

// declarationOf keeps the <?xml ... ?> line verbatim. The Driver Station writes
// single quotes and standalone='yes', which no encoder would reproduce.
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

// indentOf measures one level of indentation from the first indented line, so
// a save matches whatever wrote the file last.
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

// trailerOf keeps whatever followed the closing tag, normally one newline.
func trailerOf(data []byte) string {
	close := bytes.LastIndex(data, []byte("</"+RootTag+">"))
	if close < 0 {
		return "\n"
	}
	return string(data[close+len("</"+RootTag+">"):])
}

// isModuleTag reports whether an element is a hub on the RS-485 chain rather
// than a device plugged into one.
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

// attrs keeps every attribute in the order it was written, including any the
// model does not understand and any the SDK duplicated.
func attrs(t xml.StartElement) []Attr {
	out := make([]Attr, 0, len(t.Attr))
	for _, a := range t.Attr {
		out = append(out, Attr{Name: a.Name.Local, Value: a.Value})
	}
	return out
}

// attr returns the first value for a name.
//
// First, not last: the SDK's Ethernet writer emits name= twice, and the Driver
// Station reads the first one.
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

// lineAt turns a byte offset into a 1-based line number.
//
// The offset handed in is where the previous token ended, so it usually points
// at the whitespace before an element. Skipping forward to the next '<' lands
// on the element itself, which is the line worth reporting.
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

// passthroughCharset accepts any declared encoding and reads the bytes as they
// are. Configurations are written as UTF-8 whatever the declaration says.
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
//
// A webcam or an Ethernet device is a top-level element, but its name is looked
// up through hardwareMap exactly like a motor's, so it competes for the same
// namespace. A hub chain is not: nothing resolves "Control Hub Portal".
func (p Portal) AsDevice() Device {
	return Device{Tag: p.Tag, Name: p.Name, Port: -1, Line: p.Line}
}

// InHardwareMap reports whether a portal's own name is resolvable from an
// OpMode.
func (p Portal) InHardwareMap() bool {
	return len(p.Modules) == 0 && p.Name != ""
}

// Named lists everything that occupies a name in the hardware map, in document
// order.
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

// Names lists the names an OpMode can look up in the hardware map.
func (c *Config) Names() []string {
	var names []string
	for _, d := range c.Named() {
		if d.Enabled() {
			names = append(names, d.Name)
		}
	}
	return names
}
