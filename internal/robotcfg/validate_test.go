package robotcfg

import (
	"strings"
	"testing"
)

func wrap(devices string) string {
	return `<Robot type="FirstInspires-FTC">
    <LynxUsbDevice name="Control Hub Portal" serialNumber="(embedded)" parentModuleAddress="173">
        <LynxModule name="Control Hub" port="173">
` + devices + `
        </LynxModule>
    </LynxUsbDevice>
</Robot>`
}

func check(t *testing.T, xml string) Issues {
	t.Helper()

	cfg, err := Parse([]byte(xml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return Validate(cfg)
}

func mustFind(t *testing.T, issues Issues, level Level, substring string) {
	t.Helper()

	for _, i := range issues {
		if i.Level == level && strings.Contains(i.Msg, substring) {
			return
		}
	}
	t.Fatalf("no %s mentioning %q; got %v", level, substring, issues)
}

func mustBeClean(t *testing.T, issues Issues) {
	t.Helper()

	if issues.Errors() {
		t.Fatalf("reported errors: %v", issues)
	}
}

func TestTwoDevicesOnOnePort(t *testing.T) {
	issues := check(t, wrap(`
            <Motor name="left" port="0" />
            <Motor name="right" port="0" />`))

	mustFind(t, issues, Error, "motor port 0")
}

func TestSamePortNumberOnDifferentFlavoursIsFine(t *testing.T) {
	mustBeClean(t, check(t, wrap(`
            <Motor name="drive" port="0" />
            <Servo name="claw" port="0" />
            <AnalogInput name="pot" port="0" />
            <DigitalDevice name="touch" port="0" />`)))
}

func TestSameI2cPortOnDifferentBusesIsFine(t *testing.T) {
	mustBeClean(t, check(t, wrap(`
            <ControlHubImuBHI260AP name="imu" port="0" bus="0" />
            <goBILDAPinpoint name="odo" port="0" bus="2" />`)))
}

func TestSameI2cPortOnOneBusCollides(t *testing.T) {
	issues := check(t, wrap(`
            <RevColorSensorV3 name="colour" port="0" bus="1" />
            <SparkFunOTOS name="otos" port="0" bus="1" />`))

	mustFind(t, issues, Error, "I2C bus 1 port 0")
}

func TestPortsBeyondWhatTheHubHas(t *testing.T) {
	for _, tc := range []struct{ name, xml, want string }{
		{"a fifth motor", `<Motor name="m" port="4" />`, "motor ports 0-3"},
		{"a seventh servo", `<Servo name="s" port="6" />`, "servo ports 0-5"},
		{"a ninth digital", `<DigitalDevice name="d" port="8" />`, "digital ports 0-7"},
		{"a fifth analog", `<AnalogInput name="a" port="4" />`, "analog ports 0-3"},
		{"a fifth I2C bus", `<RevColorSensorV3 name="c" port="0" bus="4" />`, "buses 0-3"},
		{"a negative port", `<Motor name="m" port="-1" />`, "motor ports 0-3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mustFind(t, check(t, wrap("            "+tc.xml)), Error, tc.want)
		})
	}
}

func TestOneNameOnTwoHubs(t *testing.T) {
	issues := check(t, `<Robot type="FirstInspires-FTC">
    <LynxUsbDevice name="Control Hub Portal" serialNumber="(embedded)" parentModuleAddress="173">
        <LynxModule name="Control Hub" port="173">
            <Motor name="arm" port="0" />
        </LynxModule>
        <LynxModule name="Expansion Hub 2" port="2">
            <Motor name="arm" port="0" />
        </LynxModule>
    </LynxUsbDevice>
</Robot>`)

	mustFind(t, issues, Error, `two devices are called "arm"`)
}

func TestAnExpansionHubOnTheControlHubAddress(t *testing.T) {
	issues := check(t, `<Robot type="FirstInspires-FTC">
    <LynxUsbDevice name="portal" serialNumber="1234-5678" parentModuleAddress="1">
        <LynxModule name="Expansion Hub 1" port="173">
            <Motor name="drive" port="0" />
        </LynxModule>
    </LynxUsbDevice>
</Robot>`)

	mustFind(t, issues, Error, "reserved for the Control Hub")
}

func TestTheControlHubsOwnAddressIsFine(t *testing.T) {
	mustBeClean(t, check(t, wrap(`            <Motor name="drive" port="0" />`)))
}

func TestTwoHubsOnOneAddress(t *testing.T) {
	issues := check(t, `<Robot type="FirstInspires-FTC">
    <LynxUsbDevice name="portal" serialNumber="(embedded)" parentModuleAddress="173">
        <LynxModule name="Control Hub" port="173" />
        <LynxModule name="Expansion Hub A" port="2" />
        <LynxModule name="Expansion Hub B" port="2" />
    </LynxUsbDevice>
</Robot>`)

	mustFind(t, issues, Error, "two hubs are at address 2")
}

func TestAnAddressAboveTheUnreservedRangeWarns(t *testing.T) {
	issues := check(t, `<Robot type="FirstInspires-FTC">
    <LynxUsbDevice name="portal" serialNumber="(embedded)" parentModuleAddress="173">
        <LynxModule name="Control Hub" port="173" />
        <LynxModule name="Expansion Hub" port="42" />
    </LynxUsbDevice>
</Robot>`)

	mustFind(t, issues, Warning, "reserved for system use")
	if issues.Errors() {
		t.Errorf("an unusual address should not block a push: %v", issues)
	}
}

func TestWhitespaceAroundAName(t *testing.T) {
	mustFind(t, check(t, wrap(`            <Motor name="drive " port="0" />`)),
		Error, "whitespace")
}

func TestAnUnknownDeviceTypeIsNotAnError(t *testing.T) {
	issues := check(t, wrap(`
            <SomeTeamsCustomSensor name="custom" port="0" bus="0" />
            <Motor name="drive" port="0" />`))

	mustBeClean(t, issues)
}

func TestAnUnknownDeviceTypeStillNeedsAUniqueName(t *testing.T) {
	mustFind(t, check(t, wrap(`
            <SomeTeamsCustomSensor name="thing" port="0" bus="0" />
            <Motor name="thing" port="0" />`)),
		Error, `two devices are called "thing"`)
}

func TestIssuesAreOrderedByLine(t *testing.T) {
	issues := check(t, wrap(`
            <Motor name="a" port="0" />
            <Servo name="b" port="9" />
            <Motor name="a" port="1" />`))

	if len(issues) < 2 {
		t.Fatalf("got %v", issues)
	}
	for i := 1; i < len(issues); i++ {
		if issues[i].Line < issues[i-1].Line {
			t.Fatalf("out of order: %v", issues)
		}
	}
}

func TestCheckNameRejectsWhatTheRobotControllerWould(t *testing.T) {
	for _, name := range []string{"", "  ", "with/slash", `with"quote`, "with:colon", "with*star", " padded"} {
		if err := CheckName(name); err == nil {
			t.Errorf("%q was accepted as a configuration name", name)
		}
	}

	for _, name := range []string{"Tuttifrutii ca la mondiale", "comp", "Robot-2026_v2"} {
		if err := CheckName(name); err != nil {
			t.Errorf("%q was rejected: %v", name, err)
		}
	}
}
