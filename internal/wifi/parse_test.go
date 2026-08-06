package wifi

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestParseNetworksetupSSID(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{"normal association", "Current Wi-Fi Network: ASUS_5G\n", "ASUS_5G"},
		{"SSID containing a colon keeps everything after the first one",
			"Current Wi-Fi Network: Andrei: Robot\n", "Andrei: Robot"},

		{"the not-associated line yields nothing",
			"You are not associated with an AirPort network.\n", ""},
		{"empty output", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseNetworksetupSSID(tt.output); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsRedacted(t *testing.T) {
	for _, ssid := range []string{"<redacted>", "<unknown>", "You are not associated with an AirPort network."} {
		if !isRedacted(ssid) {
			t.Errorf("%q should count as redacted", ssid)
		}
	}
	for _, ssid := range []string{"ASUS_5G", "14270-RC", "ICHB-Robotics-5G", "FTC-9RbP"} {
		if isRedacted(ssid) {
			t.Errorf("%q is a real network name, not a placeholder", ssid)
		}
	}
}

func TestParseDarwinPreferred(t *testing.T) {
	output := "Preferred networks on en0:\n\tICHB-Robotics-5G\n\t14270-RC\n\tASUS_5G\n"

	got := parseDarwinPreferred(output)
	want := []string{"ICHB-Robotics-5G", "14270-RC", "ASUS_5G"}

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSplitTerseHandlesEscapes(t *testing.T) {
	tests := []struct {
		line string
		want []string
	}{
		{"wlp3s0:wifi", []string{"wlp3s0", "wifi"}},
		{`yes:Andrei\:Robot`, []string{"yes", "Andrei:Robot"}},
		{`yes:back\\slash`, []string{"yes", `back\slash`}},
		{"a::c", []string{"a", "", "c"}},
		{"", []string{""}},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			got := splitTerse(tt.line)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseNmcliWiFiDevice(t *testing.T) {
	output := "enp0s31f6:ethernet\nwlp3s0:wifi\nlo:loopback\n"
	if got := parseNmcliWiFiDevice(output); got != "wlp3s0" {
		t.Errorf("got %q, want wlp3s0", got)
	}

	if got := parseNmcliWiFiDevice("enp0s31f6:ethernet\nlo:loopback\n"); got != "" {
		t.Errorf("a machine with no wireless device should yield \"\", got %q", got)
	}
}

func TestParseNmcliActiveSSID(t *testing.T) {
	output := "no:ICHB-GIM\nyes:ICHB-Robotics-5G\nno:ASUS_5G\n"
	if got := parseNmcliActiveSSID(output); got != "ICHB-Robotics-5G" {
		t.Errorf("got %q, want ICHB-Robotics-5G", got)
	}

	if got := parseNmcliActiveSSID("no:ICHB-GIM\nno:ASUS_5G\n"); got != "" {
		t.Errorf("nothing active should yield \"\", got %q", got)
	}

	if got := parseNmcliActiveSSID(`yes:Andrei\:Robot`); got != "Andrei:Robot" {
		t.Errorf("escaped colon mishandled: got %q", got)
	}
}

func TestParseNmcliSavedNetworksOrdersByLastUsed(t *testing.T) {
	output := strings.Join([]string{
		"ASUS_5G:802-11-wireless:1754100000",
		"Wired connection 1:802-3-ethernet:1754999999",
		"ICHB-Robotics-5G:802-11-wireless:1754251200",
		"14270-RC:802-11-wireless:1754200000",
		"NeverConnected:802-11-wireless:0",
	}, "\n")

	got := parseNmcliSavedNetworks(output)
	want := []string{"ICHB-Robotics-5G", "14270-RC", "ASUS_5G", "NeverConnected"}

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}

	for _, name := range got {
		if name == "Wired connection 1" {
			t.Error("a wired profile must not appear in the Wi-Fi list")
		}
	}
}

func TestParseNmcliSavedNetworksToleratesBadTimestamps(t *testing.T) {
	output := "Good:802-11-wireless:1754251200\nBroken:802-11-wireless:notanumber\n"

	got := parseNmcliSavedNetworks(output)
	if len(got) != 2 || got[0] != "Good" || got[1] != "Broken" {
		t.Errorf("an unparseable timestamp should sort last, not drop the profile: %v", got)
	}
}

func TestParseNmcliRadio(t *testing.T) {
	if !parseNmcliRadio("enabled\n") {
		t.Error("enabled should read as powered on")
	}
	if parseNmcliRadio("disabled\n") {
		t.Error("disabled should read as powered off")
	}
}

func TestNetshFieldMatchesExactKey(t *testing.T) {
	output := `
There is 1 interface on the system:

    Name                   : Wi-Fi
    Description            : Intel(R) Wi-Fi 6 AX201 160MHz
    Physical address       : aa:bb:cc:dd:ee:ff
    State                  : connected
    SSID                   : ICHB-Robotics-5G
    BSSID                  : 11:22:33:44:55:66
    Signal                 : 87%
`

	if got := netshField(output, "SSID"); got != "ICHB-Robotics-5G" {
		t.Errorf("SSID: got %q", got)
	}
	if got := netshField(output, "BSSID"); got != "11:22:33:44:55:66" {
		t.Errorf("BSSID should keep its colons: got %q", got)
	}
	if got := netshField(output, "Name"); got != "Wi-Fi" {
		t.Errorf("Name: got %q", got)
	}
	if got := netshField(output, "Missing"); got != "" {
		t.Errorf("absent key should yield \"\", got %q", got)
	}
}

func TestParseNetshProfiles(t *testing.T) {
	output := `
Profiles on interface Wi-Fi:

Group policy profiles (read only)
---------------------------------
    <None>

User profiles
-------------
    All User Profile     : ICHB-Robotics-5G
    All User Profile     : 14270-RC
    All User Profile     : ASUS_5G
`

	got := parseNetshProfiles(output)
	want := []string{"ICHB-Robotics-5G", "14270-RC", "ASUS_5G"}

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseNetshProfilesEmpty(t *testing.T) {
	output := "Profiles on interface Wi-Fi:\n\nUser profiles\n-------------\n    <None>\n"
	if got := parseNetshProfiles(output); len(got) != 0 {
		t.Errorf("expected no profiles, got %v", got)
	}
}

func TestWlanProfileXMLEscapesAndStaysValid(t *testing.T) {
	profile, err := wlanProfileXML("Robot & <Friends>", `p@ss"w<o>rd&`)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(profile, "Robot & <Friends>") {
		t.Error("the SSID was inserted without escaping")
	}
	if strings.Contains(profile, `w<o>rd&`) {
		t.Error("the password was inserted without escaping")
	}

	if err := xml.Unmarshal([]byte(profile), new(struct {
		XMLName xml.Name
	})); err != nil {
		t.Fatalf("generated profile is not valid XML: %v", err)
	}

	for _, required := range []string{"WPA2PSK", "AES", "passPhrase"} {
		if !strings.Contains(profile, required) {
			t.Errorf("profile is missing %q, which the robot hotspot needs", required)
		}
	}
}

func TestEscapeSingleQuotes(t *testing.T) {
	if got := escapeSingleQuotes("Andrei's Wi-Fi"); got != "Andrei''s Wi-Fi" {
		t.Errorf("got %q", got)
	}
}

func TestFirstNotIn(t *testing.T) {
	atTheLab := []string{"ICHB-Robotics-5G", "14270-RC", "ICHB-GIM", "ASUS_5G"}
	onTheRobot := []string{"14270-RC", "ICHB-Robotics-5G", "ICHB-GIM", "ASUS_5G"}

	tests := []struct {
		name     string
		networks []string
		exclude  []string
		want     string
	}{
		{"before switching, the top entry is where we are", atTheLab,
			[]string{"14270-RC", "FTC-9RbP"}, "ICHB-Robotics-5G"},
		{"after switching, skipping the robot finds where we came from", onTheRobot,
			[]string{"14270-RC", "FTC-9RbP"}, "ICHB-Robotics-5G"},
		{"an empty exclusion just takes the top entry", atTheLab, nil, "ICHB-Robotics-5G"},
		{"blank exclusions are ignored rather than matching", atTheLab,
			[]string{""}, "ICHB-Robotics-5G"},
		{"every network excluded yields nothing", []string{"14270-RC"},
			[]string{"14270-RC"}, ""},
		{"no saved networks yields nothing", nil, []string{"14270-RC"}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstNotIn(tt.networks, tt.exclude); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
