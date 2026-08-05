package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andreibanu/pusher/internal/robotcfg"
	tea "github.com/charmbracelet/bubbletea"
)

const testConfig = `<?xml version='1.0' encoding='UTF-8' standalone='yes' ?>
<Robot type="FirstInspires-FTC">
    <LynxUsbDevice name="Control Hub Portal" serialNumber="(embedded)" parentModuleAddress="173">
        <LynxModule name="Control Hub" port="173">
            <goBILDA5202SeriesMotor name="fl" port="0" />
            <goBILDA5202SeriesMotor name="fr" port="1" />
            <Servo name="claw" port="0" />
            <ControlHubImuBHI260AP name="imu" port="0" bus="0" />
        </LynxModule>
    </LynxUsbDevice>
</Robot>
`

// hwModelIn builds a menu over a throwaway project holding one configuration.
// No robot: everything on the project side has to work without one, which is
// how this gets used on the bus to a competition.
func hwModelIn(t *testing.T) *hwModel {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "configs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "comp.xml"), []byte(testConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &hwModel{store: robotcfg.NewStore(dir), height: 40}
	m.refreshLocal()
	m.rebuildEntries()
	return m
}

func press(t *testing.T, m *hwModel, keys ...string) {
	t.Helper()

	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		m.Update(msg)
	}
}

func typeIn(t *testing.T, m *hwModel, text string) {
	t.Helper()

	for _, r := range text {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// openEditorOn walks in from the list the way somebody would, rather than
// setting the screen directly, so the path itself is covered.
func openEditorOn(t *testing.T, m *hwModel, name string) {
	t.Helper()

	if m.entries[0].Name != name {
		t.Fatalf("expected %q first, got %q", name, m.entries[0].Name)
	}

	press(t, m, "enter") // into the actions for the first configuration
	if m.screen != hwScreenActions {
		t.Fatalf("got screen %v", m.screen)
	}

	press(t, m, "enter") // "Edit devices" is the first action
	if m.screen != hwScreenDevices {
		t.Fatalf("got screen %v, want the editor", m.screen)
	}
}

func TestTheMenuOpensOnWhatTheProjectHas(t *testing.T) {
	m := hwModelIn(t)

	if len(m.entries) != 1 || m.entries[0].Name != "comp" {
		t.Fatalf("got %v", m.entries)
	}
	if !m.entries[0].InLocal || m.entries[0].OnRobot {
		t.Errorf("got %+v", m.entries[0])
	}
	if got := m.entries[0].status(); got != "not on the robot" {
		t.Errorf("got %q", got)
	}
}

// Without a robot, nothing that needs one may be offered: an action that
// cannot work is worse than an action that is not there.
func TestActionsWithoutARobotOfferNothingThatNeedsOne(t *testing.T) {
	m := hwModelIn(t)
	press(t, m, "enter")

	for _, item := range m.actionItems() {
		if strings.Contains(item, "robot") {
			t.Errorf("%q is offered with no robot connected", item)
		}
	}

	// The project-side actions still have to be there.
	for _, want := range []string{"Edit devices", "Rename", "Delete from the project"} {
		if !hasItem(m.actionItems(), want) {
			t.Errorf("%q is missing", want)
		}
	}
}

func TestTheEditorListsEveryPortInOrder(t *testing.T) {
	m := hwModelIn(t)
	openEditorOn(t, m, "comp")

	var devices []string
	for _, row := range m.rows {
		if row.Kind == hwRowDevice {
			devices = append(devices, strings.Fields(row.Label)[len(strings.Fields(row.Label))-1])
		}
	}

	want := []string{"fl", "fr", "claw", "imu"}
	if strings.Join(devices, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", devices, want)
	}

	// Adding is a row rather than a shortcut, so the editor can be used
	// without knowing any keys.
	if !hasRow(m.rows, hwRowAddDevice) {
		t.Error("there is no way to add a device without a shortcut")
	}
}

// The cursor must never sit on a heading, or enter does nothing and the menu
// looks broken.
func TestTheCursorSkipsHeadings(t *testing.T) {
	m := hwModelIn(t)
	openEditorOn(t, m, "comp")

	for i := 0; i < len(m.rows)*2; i++ {
		if !m.rows[m.cursor].selectable() {
			t.Fatalf("the cursor landed on a heading at row %d", m.cursor)
		}
		press(t, m, "down")
	}
	for i := 0; i < len(m.rows)*2; i++ {
		if !m.rows[m.cursor].selectable() {
			t.Fatalf("the cursor landed on a heading going up, row %d", m.cursor)
		}
		press(t, m, "up")
	}
}

// Typing a fragment of a device type has to find it. Nobody remembers that a
// goBILDA Yellow Jacket is spelled goBILDA5202SeriesMotor.
func TestTypeAutocompletionFindsADeviceFromAFragment(t *testing.T) {
	m := hwModelIn(t)
	openEditorOn(t, m, "comp")

	// The first device row.
	press(t, m, "enter")
	if m.screen != hwScreenDevice {
		t.Fatalf("got screen %v", m.screen)
	}

	// Clear the type and search for the pinpoint by a fragment.
	for range m.form.typed {
		press(t, m, "backspace")
	}
	typeIn(t, m, "pinpoint")

	if len(m.form.suggest) == 0 {
		t.Fatal("nothing matched \"pinpoint\"")
	}
	if m.form.suggest[0] != "goBILDAPinpoint" {
		t.Errorf("got %v", m.form.suggest)
	}

	// Enter takes the highlighted suggestion rather than saving, so a
	// half-typed name never becomes a device type.
	press(t, m, "enter")
	if m.form.typed != "goBILDAPinpoint" {
		t.Errorf("got %q", m.form.typed)
	}
	if m.screen != hwScreenDevice {
		t.Error("enter on the type field left the form")
	}
	if m.form.field != hwFieldName {
		t.Error("choosing a type did not move on to the name")
	}
}

// Picking a type should fill in what follows from it, because choosing the type
// is most of the work.
func TestChoosingAnI2cTypeOffersABusAndAFreePort(t *testing.T) {
	m := hwModelIn(t)
	openEditorOn(t, m, "comp")

	// Walk to the add row and open it.
	for m.rows[m.cursor].Kind != hwRowAddDevice {
		press(t, m, "down")
	}
	press(t, m, "enter")

	typeIn(t, m, "RevColorSensorV3")
	press(t, m, "enter")

	if m.form.bus == "" {
		t.Error("an I2C device was offered no bus")
	}
	if !hasField(m.formFields(), hwFieldBus) {
		t.Error("the bus field is not shown for an I2C device")
	}

	// The IMU already holds bus 0 port 0, so the offer has to be somewhere else.
	if m.form.bus == "0" && m.form.port == "0" {
		t.Error("the new sensor was put on top of the IMU")
	}
	if m.form.problem != "" && strings.Contains(m.form.problem, "already on that port") {
		t.Errorf("the suggested port is taken: %s", m.form.problem)
	}
}

func TestANonI2cTypeHasNoBusField(t *testing.T) {
	m := hwModelIn(t)
	openEditorOn(t, m, "comp")

	for m.rows[m.cursor].Kind != hwRowAddDevice {
		press(t, m, "down")
	}
	press(t, m, "enter")

	typeIn(t, m, "Servo")
	press(t, m, "enter")

	if hasField(m.formFields(), hwFieldBus) {
		t.Error("a servo was given a bus field")
	}
	// Servo 0 is taken by the claw, so the offer must be 1.
	if m.form.port != "1" {
		t.Errorf("got port %q, want the first free one", m.form.port)
	}
}

// Catching a duplicate name as it is typed is most of what makes this easier
// than editing the XML by hand.
func TestADuplicateNameIsCaughtWhileTyping(t *testing.T) {
	m := hwModelIn(t)
	openEditorOn(t, m, "comp")

	press(t, m, "enter") // edit "fl"
	press(t, m, "tab")   // onto the name

	for range m.form.name {
		press(t, m, "backspace")
	}
	typeIn(t, m, "fr")

	if m.form.problem == "" || !strings.Contains(m.form.problem, "already used") {
		t.Errorf("got %q, want a duplicate-name complaint", m.form.problem)
	}

	// Saving has to be refused while it is wrong.
	press(t, m, "enter")
	if m.screen != hwScreenDevice {
		t.Error("a duplicate name was accepted")
	}

	// And accepted again once it is unique.
	typeIn(t, m, "ont")
	if m.form.problem != "" {
		t.Errorf("got %q for a unique name", m.form.problem)
	}
}

// Renaming a device must not collide with itself.
func TestADeviceKeepingItsOwnNameIsNotADuplicate(t *testing.T) {
	m := hwModelIn(t)
	openEditorOn(t, m, "comp")

	press(t, m, "enter")
	press(t, m, "tab")

	if m.form.name != "fl" {
		t.Fatalf("got %q", m.form.name)
	}
	if m.form.problem != "" {
		t.Errorf("a device collided with itself: %s", m.form.problem)
	}
}

func TestAPortOutsideTheHubIsCaughtWhileTyping(t *testing.T) {
	m := hwModelIn(t)
	openEditorOn(t, m, "comp")

	press(t, m, "enter") // "fl", a motor
	press(t, m, "tab", "tab")

	for range m.form.port {
		press(t, m, "backspace")
	}
	typeIn(t, m, "7")

	if !strings.Contains(m.form.problem, "motor ports 0-3") {
		t.Errorf("got %q", m.form.problem)
	}
}

func TestAnOccupiedPortIsCaughtWhileTyping(t *testing.T) {
	m := hwModelIn(t)
	openEditorOn(t, m, "comp")

	press(t, m, "enter") // "fl" on motor 0
	press(t, m, "tab", "tab")

	for range m.form.port {
		press(t, m, "backspace")
	}
	typeIn(t, m, "1") // where "fr" is

	if !strings.Contains(m.form.problem, `"fr" is already on that port`) {
		t.Errorf("got %q", m.form.problem)
	}
}

// An edit only reaches the file when it is saved, and then in the format the
// Driver Station uses.
func TestSavingWritesTheFileAndNothingElseChanges(t *testing.T) {
	m := hwModelIn(t)
	openEditorOn(t, m, "comp")

	press(t, m, "enter") // edit "fl"
	press(t, m, "tab")
	for range m.form.name {
		press(t, m, "backspace")
	}
	typeIn(t, m, "leftFront")
	press(t, m, "enter")

	if !m.dirty {
		t.Error("the edit was not marked unsaved")
	}

	// Nothing on disk yet.
	if data, _ := m.store.Read("comp"); strings.Contains(string(data), "leftFront") {
		t.Error("the file changed before it was saved")
	}

	press(t, m, "s")

	data, err := m.store.Read("comp")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `name="leftFront"`) {
		t.Errorf("the rename is not in the file:\n%s", data)
	}
	if m.dirty {
		t.Error("still marked unsaved after saving")
	}

	// Everything else has to be untouched, formatting included.
	want := strings.Replace(testConfig, `name="fl"`, `name="leftFront"`, 1)
	if string(data) != want {
		t.Errorf("the file was reformatted.\n--- want ---\n%s\n--- got ---\n%s", want, data)
	}
}

// Backing out of an unsaved edit must leave the file alone.
func TestLeavingWithUnsavedChangesAsksFirst(t *testing.T) {
	m := hwModelIn(t)
	openEditorOn(t, m, "comp")

	press(t, m, "enter")
	press(t, m, "tab")
	typeIn(t, m, "x")
	press(t, m, "enter")

	press(t, m, "esc")
	if m.screen != hwScreenConfirm {
		t.Fatal("leaving an unsaved edit did not ask")
	}

	press(t, m, "n") // anything but y cancels
	if m.screen != hwScreenDevices {
		t.Error("cancelling the question did not go back to the editor")
	}

	press(t, m, "esc", "y")
	if m.screen != hwScreenActions {
		t.Errorf("got screen %v", m.screen)
	}

	data, _ := m.store.Read("comp")
	if string(data) != testConfig {
		t.Error("discarding the edit still changed the file")
	}
}

// Removing a device is destructive enough to ask about.
func TestRemovingADeviceAsksAndThenRemovesIt(t *testing.T) {
	m := hwModelIn(t)
	openEditorOn(t, m, "comp")

	press(t, m, "d")
	if m.screen != hwScreenConfirm {
		t.Fatal("removing a device did not ask")
	}

	press(t, m, "y", "s")

	data, _ := m.store.Read("comp")
	if strings.Contains(string(data), `name="fl"`) {
		t.Errorf("the device is still there:\n%s", data)
	}
	if !strings.Contains(string(data), `name="fr"`) {
		t.Error("removing one device removed another")
	}
}

// A new configuration has to be usable straight away rather than an empty file
// with nowhere to put anything.
func TestANewConfigurationOpensInTheEditorWithAHub(t *testing.T) {
	m := hwModelIn(t)

	// Walk to "New configuration" and open it.
	for m.cursor < len(m.entries) {
		press(t, m, "down")
	}
	press(t, m, "enter")

	if m.screen != hwScreenPrompt {
		t.Fatalf("got screen %v", m.screen)
	}

	typeIn(t, m, "practice")
	press(t, m, "enter")

	if m.screen != hwScreenDevices {
		t.Fatalf("a new configuration did not open in the editor, got %v", m.screen)
	}
	if !m.store.Has("practice") {
		t.Fatal("the file was not created")
	}
	if !hasRow(m.rows, hwRowAddDevice) {
		t.Error("a new configuration has nowhere to add a device")
	}

	data, _ := m.store.Read("practice")
	cfg, err := robotcfg.Parse(data)
	if err != nil {
		t.Fatalf("a new configuration does not parse: %v", err)
	}
	if issues := robotcfg.Validate(cfg); issues.Errors() {
		t.Errorf("a new configuration has errors: %v", issues)
	}
}

// A name the robot controller would refuse has to be caught here, not after a
// push fails.
func TestANameTheRobotWouldRefuseIsRejected(t *testing.T) {
	m := hwModelIn(t)

	for m.cursor < len(m.entries) {
		press(t, m, "down")
	}
	press(t, m, "enter")

	typeIn(t, m, "bad/name")
	press(t, m, "enter")

	if m.err == nil {
		t.Fatal("a name with a slash was accepted")
	}
	if m.screen != hwScreenPrompt {
		t.Error("the prompt was left despite the error")
	}
}

// Deleting from the project keeps a copy, because an edit that was never
// pushed exists nowhere else.
func TestDeletingFromTheProjectKeepsACopy(t *testing.T) {
	m := hwModelIn(t)
	press(t, m, "enter")

	for m.actionItems()[m.cursor] != "Delete from the project" {
		press(t, m, "down")
	}
	press(t, m, "enter", "y")

	if m.store.Has("comp") {
		t.Error("the file is still there")
	}

	backup := filepath.Join(m.store.Dir, ".pusher-backup", "comp.xml")
	if _, err := os.Stat(backup); err != nil {
		t.Errorf("no copy was kept: %v", err)
	}
}

// The editor's problem count is what tells somebody the configuration is
// broken before they push it.
func TestTheEditorReportsProblemsItCreates(t *testing.T) {
	m := hwModelIn(t)
	openEditorOn(t, m, "comp")

	if m.issues.Errors() {
		t.Fatalf("the fixture already has errors: %v", m.issues)
	}

	// Put a second motor on port 1, where "fr" is, going around the form so
	// the editor's own validation is what gets tested.
	m.cfg.Portals[0].Modules[0].Devices = append(m.cfg.Portals[0].Modules[0].Devices,
		robotcfg.Device{Tag: "Motor", Name: "extra", Port: 1, HasPort: true})
	m.revalidate()
	m.rebuildRows()

	if !m.issues.Errors() {
		t.Fatal("a port collision was not reported")
	}
	if !strings.Contains(m.problemSummary(), "reject") {
		t.Errorf("got %q", m.problemSummary())
	}

	// The row itself has to be marked, or the message has no home.
	marked := false
	for _, row := range m.rows {
		if row.HasIss && row.Issue == robotcfg.Error {
			marked = true
		}
	}
	if !marked {
		t.Error("no row was marked with the error")
	}
}

func hasItem(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func hasRow(rows []hwRow, kind hwRowKind) bool {
	for _, row := range rows {
		if row.Kind == kind {
			return true
		}
	}
	return false
}

func hasField(fields []hwField, want hwField) bool {
	for _, f := range fields {
		if f == want {
			return true
		}
	}
	return false
}
