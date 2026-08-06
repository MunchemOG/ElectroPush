package robotcfg

import (
	"os"
	"strings"
	"testing"
)

func load(t *testing.T) *Config {
	t.Helper()

	data, err := os.ReadFile("testdata/real.xml")
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func original(t *testing.T) string {
	t.Helper()

	data, err := os.ReadFile("testdata/real.xml")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCloneDoesNotShareAnything(t *testing.T) {
	wantOriginal := original(t)
	original := load(t)
	copied := Clone(original)

	copied.Portals[0].Modules[0].Devices[0].Name = "changed"
	copied.Portals[0].Modules[0].Devices[0].Attrs[0].Value = "changed"
	copied.Portals[0].Name = "changed"

	if got := original.Portals[0].Modules[0].Devices[0].Name; got == "changed" {
		t.Error("editing the copy changed the original's device name")
	}
	if got := original.Portals[0].Modules[0].Devices[0].Attrs[0].Value; got == "changed" {
		t.Error("editing the copy changed the original's attributes")
	}
	if got := original.Portals[0].Name; got == "changed" {
		t.Error("editing the copy changed the original's portal")
	}

	if got := string(Write(original)); got != wantOriginal {
		t.Error("the original no longer writes back unchanged")
	}
}

func TestAddedDevicesLandInPortOrder(t *testing.T) {
	cfg := load(t)

	if err := cfg.AddDevice(0, 1, Device{Tag: "Servo", Name: "newClaw", Port: 4, HasPort: true}); err != nil {
		t.Fatal(err)
	}

	var servos []string
	for _, d := range cfg.Portals[0].Modules[1].Devices {
		if FlavorOf(d.Tag) == Servo {
			servos = append(servos, d.Name)
		}
	}

	want := []string{"led", "powerArm", "turretL", "turretR", "newClaw", "tilt"}
	if strings.Join(servos, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", servos, want)
	}
}

func TestFreePortSkipsWhatIsTaken(t *testing.T) {
	cfg := load(t)

	if _, ok := cfg.FreePort(0, 1, Motor, 0); ok {
		t.Error("found a free motor port on a full hub")
	}

	port, ok := cfg.FreePort(0, 1, Servo, 0)
	if !ok || port != 4 {
		t.Errorf("got port %d (found: %v), want 4", port, ok)
	}

	port, ok = cfg.FreePort(0, 1, Analog, 0)
	if !ok || port != 1 {
		t.Errorf("got analog port %d, want 1", port)
	}

	if port, ok := cfg.FreePort(0, 1, I2C, 0); !ok || port != 1 {
		t.Errorf("got I2C bus 0 port %d, want 1", port)
	}
	if port, ok := cfg.FreePort(0, 1, I2C, 1); !ok || port != 0 {
		t.Errorf("got I2C bus 1 port %d, want 0", port)
	}
}

func TestNameTakenIgnoresTheSlotBeingEdited(t *testing.T) {
	cfg := load(t)

	here := Slot{Portal: 0, Module: 1, Device: 0}

	if !cfg.NameTaken("fr", Slot{Portal: 9}) {
		t.Error("fr should be taken when nothing is excluded")
	}
	if cfg.NameTaken("fr", here) {
		t.Error("a device collided with itself")
	}
	if !cfg.NameTaken("bl", here) {
		t.Error("a real collision was missed")
	}

	if !cfg.NameTaken("limelight", here) {
		t.Error("the Ethernet device's name was not counted")
	}
}

func TestSetDeviceKeepsAttributesItDoesNotModel(t *testing.T) {
	cfg := load(t)

	slot := Slot{Portal: 0, Module: 1, Device: 14}

	before, ok := cfg.DeviceAt(slot)
	if !ok {
		t.Fatal("no device at the slot")
	}
	if before.Name != "pinpoint" {
		t.Fatalf("got %q, expected the pinpoint", before.Name)
	}

	changed := before
	changed.Tag = "SparkFunOTOS"
	changed.Name = "otos"
	if err := cfg.SetDevice(slot, changed); err != nil {
		t.Fatal(err)
	}

	out := string(Write(cfg))
	if !strings.Contains(out, `<SparkFunOTOS name="otos" port="0" bus="2" />`) {
		t.Errorf("the device did not come back as expected:\n%s", out)
	}
}

func TestRemoveDevice(t *testing.T) {
	cfg := load(t)

	before := len(cfg.Portals[0].Modules[1].Devices)
	if err := cfg.RemoveDevice(Slot{Portal: 0, Module: 1, Device: 0}); err != nil {
		t.Fatal(err)
	}

	if got := len(cfg.Portals[0].Modules[1].Devices); got != before-1 {
		t.Errorf("got %d devices, want %d", got, before-1)
	}
	if strings.Contains(string(Write(cfg)), `name="fr"`) {
		t.Error("the removed device is still in the file")
	}
}

func TestAddModuleTakesTheLowestFreeAddress(t *testing.T) {
	cfg := load(t)

	index, err := cfg.AddModule(0)
	if err != nil {
		t.Fatal(err)
	}

	if got := cfg.Portals[0].Modules[index].Address; got != 1 {
		t.Errorf("got address %d, want 1", got)
	}

	index, err = cfg.AddModule(0)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Portals[0].Modules[index].Address; got != 3 {
		t.Errorf("got address %d, want 3", got)
	}

	if issues := Validate(cfg); issues.Errors() {
		t.Errorf("adding hubs produced errors: %v", issues)
	}
}

func TestSuggestTagsRanksPrefixMatchesFirst(t *testing.T) {
	got := SuggestTags("gobilda")
	if len(got) < 3 {
		t.Fatalf("got %v", got)
	}
	for _, tag := range got {
		if !strings.HasPrefix(strings.ToLower(tag), "gobilda") {
			t.Errorf("%q is not a goBILDA part", tag)
		}
	}

	got = SuggestTags("servo")
	if len(got) == 0 {
		t.Fatal("no servo types")
	}
	if got[0] != "Servo" {
		t.Errorf("got %q first, want the plain Servo", got[0])
	}
	if !contains(got, "ContinuousRotationServo") {
		t.Errorf("a mid-string match was dropped: %v", got)
	}

	if len(SuggestTags("")) != len(KnownTags()) {
		t.Error("an empty query should offer every type")
	}

	if got := SuggestTags("zzzznotathing"); len(got) != 0 {
		t.Errorf("got %v for nonsense", got)
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
