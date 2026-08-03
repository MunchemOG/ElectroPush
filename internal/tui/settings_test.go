package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestMainMenuValueColumnClearsLongestLabel(t *testing.T) {
	const mainMenuWidth = 29

	longest := 0
	for _, item := range mainItems {
		if n := len(item); n > longest {
			longest = n
		}
	}

	if longest >= mainMenuWidth {
		t.Fatalf("longest menu label is %d chars but the value column starts at %d; widen it", longest, mainMenuWidth)
	}
}

func TestRenderRowAlignsValuesRegardlessOfSelection(t *testing.T) {
	plain := renderRow(false, "Gradle threads", "8", 29)
	selected := renderRow(true, "Slim APK before every push", "off", 29)

	valueColumn := func(row string) int {
		return lipgloss.Width(row[:strings.LastIndex(row, "\n")]) - visibleValueLen(row)
	}

	if got := valueColumn(plain); got != valueColumn(selected) {
		t.Errorf("value column moved between rows: %d vs %d", got, valueColumn(selected))
	}
}

func visibleValueLen(row string) int {
	fields := strings.Fields(stripANSI(row))
	if len(fields) == 0 {
		return 0
	}
	return len(fields[len(fields)-1])
}

func stripANSI(s string) string {
	var b strings.Builder
	inEscape := false

	for _, r := range s {
		switch {
		case r == 0x1b:
			inEscape = true
		case inEscape && (r == 'm' || r == 'K'):
			inEscape = false
		case !inEscape:
			b.WriteRune(r)
		}
	}

	return b.String()
}

func TestRenderRowOmitsPaddingWhenThereIsNoValue(t *testing.T) {
	row := stripANSI(renderRow(false, "Exit", "", 29))
	if strings.TrimRight(row, "\n") != "   Exit" {
		t.Errorf("a valueless row should not be padded, got %q", row)
	}
}

func TestClampOffsetScrollsOneRowAtATime(t *testing.T) {
	const (
		total   = 20
		visible = 8
	)

	tests := []struct {
		name       string
		offset     int
		cursor     int
		wantOffset int
	}{
		{"opens at the top", 0, 0, 0},
		{"moving inside the window does not scroll", 0, 5, 0},
		{"last visible row still does not scroll", 0, 7, 0},
		{"stepping past the bottom shifts by one", 0, 8, 1},
		{"and again by one", 1, 9, 2},
		{"scrolling back up above the window", 5, 4, 4},
		{"wrapping to the first row returns to the top", 12, 0, 0},
		{"wrapping to the last row shows the final screenful", 0, 19, 12},
		{"offset never runs past the end", 18, 19, 12},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampOffset(tt.offset, tt.cursor, visible, total)
			if got != tt.wantOffset {
				t.Errorf("clampOffset(%d, %d, %d, %d) = %d, want %d",
					tt.offset, tt.cursor, visible, total, got, tt.wantOffset)
			}
		})
	}
}

func TestClampOffsetNeverScrollsAListThatFits(t *testing.T) {
	for cursor := 0; cursor < 5; cursor++ {
		if got := clampOffset(0, cursor, 8, 5); got != 0 {
			t.Errorf("a 5-row list in an 8-row window must not scroll, got offset %d", got)
		}
	}
}

func TestClampOffsetAlwaysKeepsCursorVisible(t *testing.T) {
	const total, visible = 20, 6

	offset := 0
	for _, cursor := range []int{0, 3, 9, 19, 2, 15, 0, 19} {
		offset = clampOffset(offset, cursor, visible, total)
		if cursor < offset || cursor >= offset+visible {
			t.Fatalf("cursor %d fell outside window [%d,%d)", cursor, offset, offset+visible)
		}
	}
}

func TestRenderListShowsContinuationMarkers(t *testing.T) {
	m := &SettingsModel{height: defaultHeight, screen: screenHomeNetwork}
	m.networks = make([]string, 40)
	for i := range m.networks {
		m.networks[i] = "net"
	}

	row := func(i int) string { return "row\n" }

	m.offset = 0
	top := stripANSI(m.renderList(40, row))
	if strings.Contains(top, "more above") {
		t.Error("a list at the top should not claim rows above it")
	}
	if !strings.Contains(top, "more below") {
		t.Error("a truncated list must show that it continues below")
	}

	m.offset = 10
	middle := stripANSI(m.renderList(40, row))
	if !strings.Contains(middle, "more above") || !strings.Contains(middle, "more below") {
		t.Errorf("a mid-list window needs both markers, got:\n%s", middle)
	}

	short := stripANSI(m.renderList(3, row))
	if strings.Contains(short, "more above") || strings.Contains(short, "more below") {
		t.Errorf("a list that fits should have no markers, got:\n%s", short)
	}
}

func TestVisibleRowsStaysUsableInAShortTerminal(t *testing.T) {
	m := &SettingsModel{height: 5}
	if got := m.visibleRows(); got < minVisibleRows {
		t.Errorf("a short terminal should still show %d rows, got %d", minVisibleRows, got)
	}
}

func TestListLengthCountsTheHomeNetworkOptOutRow(t *testing.T) {
	m := &SettingsModel{screen: screenHomeNetwork, networks: []string{"a", "b", "c"}}
	if got := m.listLength(); got != 4 {
		t.Errorf("home network list should be networks+1, got %d", got)
	}
}

func TestOnOffAndOrUnset(t *testing.T) {
	if onOff(true) != "on" || onOff(false) != "off" {
		t.Error("onOff should render on/off")
	}
	if orUnset("", "not set") != "not set" {
		t.Error("orUnset should fall back when empty")
	}
	if orUnset("ASUS_5G", "not set") != "ASUS_5G" {
		t.Error("orUnset should pass through a real value")
	}
}
