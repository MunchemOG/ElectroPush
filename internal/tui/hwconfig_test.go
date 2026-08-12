package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MunchemOG/ElectroPush/internal/robotcfg"
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

func openEditorOn(t *testing.T, m *hwModel, name string) {
	t.Helper()

	if m.entries[0].Name != name {
		t.Fatalf("expected %q first, got %q", name, m.entries[0].Name)
	}

	press(t, m, "enter")
	if m.screen != hwScreenActions {
		t.Fatalf("got screen %v", m.screen)
	}

	press(t, m, "enter")
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

func TestActionsWithoutARobotOfferNothingThatNeedsOne(t *testing.T) {
	m := hwModelIn(t)
	press(t, m, "enter")

	for _, item := range m.actionItems() {
		if strings.Contains(item, "robot") {
			t.Errorf("%q is offered with no robot connected", item)
		}
	}

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

	if !hasRow(m.rows, hwRowAddDevice) {
		t.Error("there is no way to add a device without a shortcut")
	}
}

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

func TestTypeAutocompletionFindsADeviceFromAFragment(t *testing.T) {
	m := hwModelIn(t)
	openEditorOn(t, m, "comp")

	press(t, m, "enter")
	if m.screen != hwScreenDevice {
		t.Fatalf("got screen %v", m.screen)
	}

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

func TestChoosingAnI2cTypeOffersABusAndAFreePort(t *testing.T) {
	m := hwModelIn(t)
	openEditorOn(t, m, "comp")

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

	if m.form.port != "1" {
		t.Errorf("got port %q, want the first free one", m.form.port)
	}
}

func TestADuplicateNameIsCaughtWhileTyping(t *testing.T) {
	m := hwModelIn(t)
	openEditorOn(t, m, "comp")

	press(t, m, "enter")
	press(t, m, "tab")

	for range m.form.name {
		press(t, m, "backspace")
	}
	typeIn(t, m, "fr")

	if m.form.problem == "" || !strings.Contains(m.form.problem, "already used") {
		t.Errorf("got %q, want a duplicate-name complaint", m.form.problem)
	}

	press(t, m, "enter")
	if m.screen != hwScreenDevice {
		t.Error("a duplicate name was accepted")
	}

	typeIn(t, m, "ont")
	if m.form.problem != "" {
		t.Errorf("got %q for a unique name", m.form.problem)
	}
}

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

	press(t, m, "enter")
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

	press(t, m, "enter")
	press(t, m, "tab", "tab")

	for range m.form.port {
		press(t, m, "backspace")
	}
	typeIn(t, m, "1")

	if !strings.Contains(m.form.problem, `"fr" is already on that port`) {
		t.Errorf("got %q", m.form.problem)
	}
}

func TestSavingWritesTheFileAndNothingElseChanges(t *testing.T) {
	m := hwModelIn(t)
	openEditorOn(t, m, "comp")

	press(t, m, "enter")
	press(t, m, "tab")
	for range m.form.name {
		press(t, m, "backspace")
	}
	typeIn(t, m, "leftFront")
	press(t, m, "enter")

	if !m.dirty {
		t.Error("the edit was not marked unsaved")
	}

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

	want := strings.Replace(testConfig, `name="fl"`, `name="leftFront"`, 1)
	if string(data) != want {
		t.Errorf("the file was reformatted.\n--- want ---\n%s\n--- got ---\n%s", want, data)
	}
}

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

	press(t, m, "n")
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

func TestANewConfigurationOpensInTheEditorWithAHub(t *testing.T) {
	m := hwModelIn(t)

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

	backup := filepath.Join(m.store.Dir, ".epsh-backup", "comp.xml")
	if _, err := os.Stat(backup); err != nil {
		t.Errorf("no copy was kept: %v", err)
	}
}

func TestTheEditorReportsProblemsItCreates(t *testing.T) {
	m := hwModelIn(t)
	openEditorOn(t, m, "comp")

	if m.issues.Errors() {
		t.Fatalf("the fixture already has errors: %v", m.issues)
	}

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
