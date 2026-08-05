package robotcfg

import (
	"os"
	"path/filepath"
	"testing"
)

// Configuration names contain spaces, so nothing in the listing may split on
// whitespace. This is the shape adb hands back, CRLF and all.
func TestListingKeepsNamesWithSpaces(t *testing.T) {
	// The parsing under test is what List does with adb's output.
	out := "Tuttifrutii ca la mondiale.xml\r\ncomp.xml\r\npractice.xml\r\nsomething.json\r\n"

	names := parseListing(out)

	want := []string{"Tuttifrutii ca la mondiale", "comp", "practice"}
	if len(names) != len(want) {
		t.Fatalf("got %v", names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("got %q at %d, want %q", names[i], i, want[i])
		}
	}
}

func TestHashParsingKeepsNamesWithSpaces(t *testing.T) {
	out := "d41d8cd98f00b204e9800998ecf8427e  /sdcard/FIRST/Tuttifrutii ca la mondiale.xml\r\n" +
		"098f6bcd4621d373cade4e832627b4f6  /sdcard/FIRST/comp.xml\r\n" +
		"md5sum: /sdcard/FIRST/*.xml: No such file or directory\r\n" +
		"deadbeef  /sdcard/FIRST/short-hash.xml\r\n" +
		"098f6bcd4621d373cade4e832627b4f6  /sdcard/other/elsewhere.xml\r\n"

	hashes := parseHashes(out)

	if got := hashes["Tuttifrutii ca la mondiale"]; got != "d41d8cd98f00b204e9800998ecf8427e" {
		t.Errorf("got %q for the name with spaces", got)
	}
	if got := hashes["comp"]; got != "098f6bcd4621d373cade4e832627b4f6" {
		t.Errorf("got %q for comp", got)
	}
	if _, ok := hashes["short-hash"]; ok {
		t.Error("a line that is not a digest was accepted")
	}
	if _, ok := hashes["elsewhere"]; ok {
		t.Error("a file outside the configuration directory was accepted")
	}
	if len(hashes) != 2 {
		t.Errorf("got %d entries: %v", len(hashes), hashes)
	}
}

// The active configuration is stored as JSON that has been XML-escaped into an
// Android preferences file, so it arrives with &quot; in place of every quote.
func TestReadingTheActiveConfigurationOutOfSettings(t *testing.T) {
	prefs := `<?xml version='1.0' encoding='utf-8' standalone='yes' ?>
<map>
    <boolean name="pref_sound_on_off" value="true" />
    <string name="pref_hardware_config_filename">{&quot;name&quot;:&quot;Tuttifrutii ca la mondiale&quot;,&quot;resourceId&quot;:0,&quot;isDirty&quot;:false,&quot;location&quot;:&quot;LOCAL_STORAGE&quot;}</string>
    <string name="pref_device_name">ICHB-Robotics</string>
</map>`

	if got := activeFromPrefs(prefs); got != "Tuttifrutii ca la mondiale" {
		t.Errorf("got %q", got)
	}
}

// A robot controller that has never had a configuration selected has no such
// setting. That has to read as "cannot tell", not as a crash.
func TestNoActiveConfigurationReadsAsUnknown(t *testing.T) {
	for _, prefs := range []string{
		"",
		"<map></map>",
		`<map><string name="pref_hardware_config_filename">not json</string></map>`,
		`<map><string name="pref_hardware_config_filename"></map>`,
	} {
		if got := activeFromPrefs(prefs); got != "" {
			t.Errorf("got %q from %q", got, prefs)
		}
	}
}

func TestRemotePathsAreUnderTheDirectoryTheRobotReads(t *testing.T) {
	if got := RemotePath("comp"); got != "/sdcard/FIRST/comp.xml" {
		t.Errorf("got %q", got)
	}
	if got := RemotePath("Tuttifrutii ca la mondiale"); got != "/sdcard/FIRST/Tuttifrutii ca la mondiale.xml" {
		t.Errorf("got %q", got)
	}
}

func TestShellQuotingSurvivesAnApostrophe(t *testing.T) {
	if got := shellQuote("/sdcard/FIRST/Ben's robot.xml"); got != `'/sdcard/FIRST/Ben'\''s robot.xml'` {
		t.Errorf("got %s", got)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "configs")
	s := NewStore(dir)

	// Listing a project that never pulled anything must not create the
	// directory, or every project grows an empty configs/.
	if names, err := s.Names(); err != nil || len(names) != 0 {
		t.Fatalf("got %v, %v", names, err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("listing created the directory")
	}

	if err := s.Write("comp", []byte(realConfig)); err != nil {
		t.Fatal(err)
	}
	if !s.Has("comp") {
		t.Error("comp is not there after writing it")
	}

	back, err := s.Read("comp")
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != realConfig {
		t.Error("the file changed on the way through")
	}

	names, err := s.Names()
	if err != nil || len(names) != 1 || names[0] != "comp" {
		t.Fatalf("got %v, %v", names, err)
	}
}

// The backup directory holds the robot's copy of things about to be
// overwritten, and must not show up as a configuration of its own.
func TestBackupsStayOutOfTheListing(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "configs"))

	if err := s.Write("comp", []byte(realConfig)); err != nil {
		t.Fatal(err)
	}

	path, err := s.Backup("comp", []byte("<Robot type=\"FirstInspires-FTC\"></Robot>"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the backup is not on disk: %v", err)
	}

	names, err := s.Names()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "comp" {
		t.Errorf("got %v", names)
	}
}

func TestStoreRefusesANameTheRobotWouldNotAccept(t *testing.T) {
	s := NewStore(t.TempDir())

	if err := s.Write("has/slash", []byte(realConfig)); err == nil {
		t.Error("a name with a slash was written")
	}
}

func TestHashMatchesWhatMd5sumWouldSay(t *testing.T) {
	// echo -n "" | md5sum
	if got := Hash(nil); got != "d41d8cd98f00b204e9800998ecf8427e" {
		t.Errorf("got %q", got)
	}
	// echo -n "test" | md5sum
	if got := Hash([]byte("test")); got != "098f6bcd4621d373cade4e832627b4f6" {
		t.Errorf("got %q", got)
	}
}
