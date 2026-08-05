package robotcfg

import (
	"strings"
	"testing"
)

// realConfig is a configuration the Driver Station actually wrote, kept
// verbatim. It is the shape that matters: single-quoted declaration, a Control
// Hub and an Expansion Hub on one portal, I2C devices carrying both port and
// bus, and an Ethernet device the SDK emits with name= twice.
const realConfig = `<?xml version='1.0' encoding='UTF-8' standalone='yes' ?>
<Robot type="FirstInspires-FTC">
    <LynxUsbDevice name="Control Hub Portal" serialNumber="(embedded)" parentModuleAddress="173">
        <LynxModule name="Expansion Hub 2" port="2">
            <goBILDA5202SeriesMotor name="transfer" port="0" />
            <goBILDA5202SeriesMotor name="intake" port="1" />
            <Servo name="capac" port="1" />
        </LynxModule>
        <LynxModule name="Control Hub" port="173">
            <goBILDA5202SeriesMotor name="fr" port="0" />
            <NeveRest40Gearmotor name="bl" port="2" />
            <Servo name="led" port="0" />
            <ServoFullRange name="turretL" port="2" />
            <AnalogInput name="turretEncoder" port="0" />
            <DigitalDevice name="beamBrakePos2" port="0" />
            <ControlHubImuBHI260AP name="imu" port="0" bus="0" />
            <goBILDAPinpoint name="pinpoint" port="0" bus="2" />
        </LynxModule>
    </LynxUsbDevice>
    <EthernetDevice name="limelight" serialNumber="EthernetOverUsb:eth0:172.29.0.30" name="limelight" port="-1" ipAddress="172.29.0.1" />
</Robot>
`

func parse(t *testing.T, xml string) *Config {
	t.Helper()

	cfg, err := Parse([]byte(xml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return cfg
}

func TestParsesAConfigurationTheDriverStationWrote(t *testing.T) {
	cfg := parse(t, realConfig)

	if len(cfg.Portals) != 2 {
		t.Fatalf("got %d portals, want the hub chain and the Ethernet device", len(cfg.Portals))
	}

	hub := cfg.Portals[0]
	if hub.Tag != "LynxUsbDevice" || hub.Name != "Control Hub Portal" {
		t.Errorf("got portal %s %q", hub.Tag, hub.Name)
	}
	if hub.Serial != "(embedded)" {
		t.Errorf("got serial %q", hub.Serial)
	}
	if !hub.HasParent || hub.ParentAddress != 173 {
		t.Errorf("got parent address %d (present: %v)", hub.ParentAddress, hub.HasParent)
	}

	if len(hub.Modules) != 2 {
		t.Fatalf("got %d modules", len(hub.Modules))
	}
	if hub.Modules[0].Address != 2 || hub.Modules[1].Address != ControlHubAddress {
		t.Errorf("got addresses %d and %d", hub.Modules[0].Address, hub.Modules[1].Address)
	}
}

// The SDK's Ethernet writer emits name= twice. A file the robot happily loads
// must not be rejected here, and the first value is the one that counts.
func TestADuplicatedAttributeDoesNotBreakParsing(t *testing.T) {
	cfg := parse(t, realConfig)

	ethernet := cfg.Portals[1]
	if ethernet.Tag != "EthernetDevice" {
		t.Fatalf("got %s", ethernet.Tag)
	}
	if ethernet.Name != "limelight" {
		t.Errorf("got name %q", ethernet.Name)
	}
}

func TestI2cDevicesKeepTheirBus(t *testing.T) {
	cfg := parse(t, realConfig)

	byName := map[string]Device{}
	for _, d := range cfg.Devices() {
		byName[d.Name] = d
	}

	pinpoint, ok := byName["pinpoint"]
	if !ok {
		t.Fatal("pinpoint is missing")
	}
	if !pinpoint.HasBus || pinpoint.Bus != 2 || pinpoint.Port != 0 {
		t.Errorf("got bus %d port %d (bus present: %v)", pinpoint.Bus, pinpoint.Port, pinpoint.HasBus)
	}

	// Both I2C devices sit on port 0 of different buses, which is legal. If
	// the bus were dropped this would read as a collision.
	if got := Validate(cfg); got.Errors() {
		t.Errorf("a real configuration reported errors: %v", got)
	}
}

// Line numbers are what makes an error message actionable, so they have to
// point at the element rather than the whitespace in front of it.
func TestIssuesPointAtTheRightLine(t *testing.T) {
	cfg := parse(t, realConfig)

	byName := map[string]Device{}
	for _, d := range cfg.Named() {
		byName[d.Name] = d
	}

	// "transfer" is the first device, on line 5 of the file above.
	if got := byName["transfer"].Line; got != 5 {
		t.Errorf("got line %d for the first motor, want 5", got)
	}
	if got := byName["limelight"].Line; got != 20 {
		t.Errorf("got line %d for the Ethernet device, want 20", got)
	}
}

// A webcam and an Ethernet device are top-level elements, but an OpMode
// resolves them through hardwareMap by name like everything else. A motor
// sharing a webcam's name is the same collision as two motors sharing one.
func TestTopLevelDevicesShareTheHardwareMapNamespace(t *testing.T) {
	cfg := parse(t, `<Robot type="FirstInspires-FTC">
    <LynxUsbDevice name="portal" serialNumber="(embedded)" parentModuleAddress="173">
        <LynxModule name="Control Hub" port="173">
            <Motor name="Webcam 1" port="0" />
        </LynxModule>
    </LynxUsbDevice>
    <Webcam name="Webcam 1" serialNumber="abc" />
</Robot>`)

	issues := Validate(cfg)
	if !issues.Errors() {
		t.Fatalf("a motor named after the webcam was accepted: %v", issues)
	}
	if !strings.Contains(issues[0].Msg, "Webcam 1") {
		t.Errorf("got %q", issues[0].Msg)
	}

	// The hub chain's own name is not resolvable, so it must not collide.
	clean := parse(t, `<Robot type="FirstInspires-FTC">
    <LynxUsbDevice name="portal" serialNumber="(embedded)" parentModuleAddress="173">
        <LynxModule name="portal" port="173">
            <Motor name="drive" port="0" />
        </LynxModule>
    </LynxUsbDevice>
</Robot>`)

	if got := Validate(clean); got.Errors() {
		t.Errorf("a portal and a hub sharing a name reported errors: %v", got)
	}
}

func TestRejectsSomethingThatIsNotAConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name string
		xml  string
		want string
	}{
		{
			name: "an unrelated document",
			xml:  `<?xml version="1.0"?><manifest package="com.example" />`,
			want: "not a hardware configuration",
		},
		{
			name: "the wrong root type",
			xml:  `<Robot type="something-else"></Robot>`,
			want: "not a hardware configuration",
		},
		{
			name: "truncated part way through",
			xml:  "<Robot type=\"FirstInspires-FTC\">\n  <LynxUsbDevice name=\"x\"",
			want: "line 2",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.xml))
			if err == nil {
				t.Fatal("parsed without complaint")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

// The template the SDK ships with an empty configuration has to load, or
// starting from scratch is impossible.
func TestAnEmptyConfigurationIsValid(t *testing.T) {
	cfg := parse(t, "<?xml version=\"1.0\" encoding=\"utf-8\" standalone=\"yes\"?>\n<Robot type=\"FirstInspires-FTC\">\n</Robot>\n")

	if len(cfg.Portals) != 0 {
		t.Errorf("got %d portals", len(cfg.Portals))
	}
	if got := Validate(cfg); len(got) != 0 {
		t.Errorf("got issues for an empty configuration: %v", got)
	}
}

// Ports the Driver Station left empty are written as placeholders, and several
// of them share the same name.
func TestUnconfiguredPortsAreIgnored(t *testing.T) {
	cfg := parse(t, `<Robot type="FirstInspires-FTC">
    <LynxUsbDevice name="portal" serialNumber="(embedded)" parentModuleAddress="173">
        <LynxModule name="Control Hub" port="173">
            <Motor name="NO$DEVICE$ATTACHED" port="0" />
            <Motor name="NO$DEVICE$ATTACHED" port="1" />
            <Nothing name="" port="2" />
        </LynxModule>
    </LynxUsbDevice>
</Robot>`)

	if got := cfg.Names(); len(got) != 0 {
		t.Errorf("got usable names %v, want none", got)
	}
	if got := Validate(cfg); got.Errors() {
		t.Errorf("placeholders reported as errors: %v", got)
	}
}

func TestRawIsKeptExactly(t *testing.T) {
	cfg := parse(t, realConfig)

	if string(cfg.Raw) != realConfig {
		t.Error("the original bytes were not preserved")
	}
}
