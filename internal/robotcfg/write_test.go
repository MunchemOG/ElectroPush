package robotcfg

import (
	"os"
	"strings"
	"testing"
)

// This is the property the whole editor rests on. If reading and writing a file
// unchanged does not return the original bytes, then every save reformats the
// configuration, every diff against the robot becomes unreadable, and there is
// no way to tell a real edit from a round trip.
func TestWritingBackWhatWasReadIsByteIdentical(t *testing.T) {
	for _, tc := range []struct{ name, xml string }{
		{"a configuration the Driver Station wrote", realConfig},
		{"an empty configuration", "<?xml version=\"1.0\" encoding=\"utf-8\" standalone=\"yes\"?>\n<Robot type=\"FirstInspires-FTC\">\n</Robot>\n"},
		{"a self-closed hub", `<?xml version='1.0' encoding='UTF-8' standalone='yes' ?>
<Robot type="FirstInspires-FTC">
    <LynxUsbDevice name="portal" serialNumber="(embedded)" parentModuleAddress="173">
        <LynxModule name="Control Hub" port="173" />
    </LynxUsbDevice>
</Robot>
`},
		{"a webcam", `<?xml version='1.0' encoding='UTF-8' standalone='yes' ?>
<Robot type="FirstInspires-FTC">
    <Webcam name="Webcam 1" serialNumber="A1B2C3" autoOpen="true" />
</Robot>
`},
		{"two-space indentation", `<?xml version='1.0' encoding='UTF-8' standalone='yes' ?>
<Robot type="FirstInspires-FTC">
  <LynxUsbDevice name="portal" serialNumber="(embedded)" parentModuleAddress="173">
    <LynxModule name="Control Hub" port="173">
      <Motor name="drive" port="0" />
    </LynxModule>
  </LynxUsbDevice>
</Robot>
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Parse([]byte(tc.xml))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			if got := string(Write(cfg)); got != tc.xml {
				t.Errorf("round trip changed the file.\n--- want ---\n%s\n--- got ---\n%s", tc.xml, got)
			}
		})
	}
}

// The same property against the file on disk, not a copy pasted into a test.
func TestRoundTripOfTheConfigurationOnDisk(t *testing.T) {
	const path = "testdata/real.xml"

	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no %s", path)
	}

	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if got := Write(cfg); string(got) != string(data) {
		t.Errorf("round trip changed %s", path)
	}
}

// Attributes pusher does not model must survive a save, or editing a motor
// would quietly strip a webcam's serial number or a Limelight's address.
func TestUnmodelledAttributesSurviveAnEdit(t *testing.T) {
	cfg, err := Parse([]byte(realConfig))
	if err != nil {
		t.Fatal(err)
	}

	// Rename a motor, which is the most ordinary edit there is.
	cfg.Portals[0].Modules[0].Devices[0].Name = "renamed"

	out := string(Write(cfg))

	for _, want := range []string{
		`ipAddress="172.29.0.1"`,
		`serialNumber="EthernetOverUsb:eth0:172.29.0.30"`,
		`parentModuleAddress="173"`,
		`name="renamed"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%s is missing from the saved file:\n%s", want, out)
		}
	}

	if strings.Contains(out, `name="transfer"`) {
		t.Error("the old name is still there")
	}
}

// The SDK's own Ethernet writer emits name= twice. Renaming has to change the
// one the Driver Station reads, and must not append a third.
func TestRenamingSomethingWithADuplicatedAttribute(t *testing.T) {
	cfg, err := Parse([]byte(realConfig))
	if err != nil {
		t.Fatal(err)
	}

	cfg.Portals[1].Name = "ll3a"
	out := string(Write(cfg))

	if got := strings.Count(out, "name="); got != strings.Count(realConfig, "name=") {
		t.Errorf("the number of name attributes changed from %d to %d",
			strings.Count(realConfig, "name="), got)
	}
	if !strings.Contains(out, `<EthernetDevice name="ll3a" serialNumber=`) {
		t.Errorf("the first name was not the one updated:\n%s", out)
	}
}

// Device names come from people, so a character that means something in XML has
// to come back out as itself.
func TestNamesAreEscapedAndParseBackUnchanged(t *testing.T) {
	cfg, err := Parse([]byte(realConfig))
	if err != nil {
		t.Fatal(err)
	}

	const awkward = `arm & "claw" <left>`
	cfg.Portals[0].Modules[0].Devices[0].Name = awkward

	again, err := Parse(Write(cfg))
	if err != nil {
		t.Fatalf("the file no longer parses: %v", err)
	}

	if got := again.Portals[0].Modules[0].Devices[0].Name; got != awkward {
		t.Errorf("got %q, want %q", got, awkward)
	}
}

func TestANewConfigurationIsValidAndParses(t *testing.T) {
	cfg := New()

	data := Write(cfg)

	again, err := Parse(data)
	if err != nil {
		t.Fatalf("a new configuration does not parse: %v\n%s", err, data)
	}
	if issues := Validate(again); issues.Errors() {
		t.Errorf("a new configuration has errors: %v", issues)
	}

	// It has to have a Control Hub to hang devices off, or the editor opens on
	// nothing and there is nowhere to add anything.
	if len(again.Portals) != 1 || len(again.Portals[0].Modules) != 1 {
		t.Fatalf("got %d portals", len(again.Portals))
	}
	if got := again.Portals[0].Modules[0].Address; got != ControlHubAddress {
		t.Errorf("got address %d, want the Control Hub's", got)
	}
}
