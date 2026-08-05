package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andreibanu/pusher/internal/blobdep"
)

func modelIn(t *testing.T, gradle string) *SettingsModel {
	t.Helper()

	root := t.TempDir()
	if gradle != "" {
		if err := os.MkdirAll(filepath.Join(root, "TeamCode"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(blobdep.GradleFile(root), []byte(gradle), 0644); err != nil {
			t.Fatal(err)
		}
	}

	m := &SettingsModel{height: defaultHeight, confirmDeleteIndex: -1, root: root}
	m.refreshBlob()
	return m
}

const gradleWithBlob = `dependencies {
    implementation 'com.github.PzmuV1517.blob:blob:2.0.0'
}
`

// The whole point of the menu: when blob is missing you get an offer to add it,
// not a dead end.
func TestBlobMenuOffersToAddWhenAbsent(t *testing.T) {
	m := modelIn(t, "dependencies {\n}\n")

	if got := m.blobMenuItems(); len(got) != len(blobMissingItems) {
		t.Fatalf("expected the missing-blob menu, got %v", got)
	}
	if !strings.Contains(m.viewBlob(), "Add blob to build.gradle") {
		t.Error("view should offer to add blob")
	}
	if m.blobLabel() != "not installed" {
		t.Errorf("main menu label should say not installed, got %q", m.blobLabel())
	}
}

func TestBlobMenuShowsVersionAndVariant(t *testing.T) {
	m := modelIn(t, gradleWithBlob)

	if got := m.blobLabel(); got != "2.0.0 (comp)" {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(m.viewBlob(), "competition") {
		t.Error("view should name the current variant")
	}
}

// Switching variants from the menu has to actually rewrite the gradle file and
// be reflected back, or the label lies about what will ship.
func TestToggleVariantRewritesGradleAndRefreshes(t *testing.T) {
	m := modelIn(t, gradleWithBlob)

	m.toggleBlobVariant()
	if m.err != nil {
		t.Fatal(m.err)
	}
	if !m.blob.dep.IsDev() {
		t.Fatal("expected dev after toggle")
	}
	if got := m.blobLabel(); got != "2.0.0 (dev)" {
		t.Errorf("label not refreshed, got %q", got)
	}
	if !strings.Contains(m.status, "Do not take this to a match") {
		t.Errorf("switching to dev must warn about competition use, got %q", m.status)
	}

	m.toggleBlobVariant()
	if m.blob.dep.IsDev() {
		t.Error("expected competition after toggling back")
	}
}

// A missing TeamCode/build.gradle is the normal case when pusher is run outside
// a project, and must not blow up the settings screen.
func TestBlobScreensSurviveANonProjectDirectory(t *testing.T) {
	m := modelIn(t, "")

	if m.blob.dep != nil {
		t.Error("no gradle file means no dependency")
	}
	if m.viewBlob() == "" {
		t.Error("view should still render")
	}
	if m.viewBlobRuns() == "" {
		t.Error("runs view should still render")
	}
}

func TestBlobRunsViewReportsWhyItIsEmpty(t *testing.T) {
	m := modelIn(t, gradleWithBlob)
	m.blob.tracErr = errNoRobot{}

	if !strings.Contains(m.viewBlobRuns(), "no robot") {
		t.Error("the runs view should surface the reason, not just show nothing")
	}
}

type errNoRobot struct{}

func (errNoRobot) Error() string { return "no robot connected" }
