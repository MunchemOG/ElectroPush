package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andreibanu/pusher/internal/blobdep"
	"github.com/andreibanu/pusher/internal/ghauth"
	tea "github.com/charmbracelet/bubbletea"
)

// modelIn builds a settings model rooted at a throwaway project. authed decides
// whether the model behaves as though GitHub already said yes, since every
// screen below the menu depends on that.
func modelIn(t *testing.T, gradle string, authed bool) *SettingsModel {
	t.Helper()

	root := t.TempDir()
	if gradle != "" {
		if err := os.MkdirAll(filepath.Join(root, "TeamCode"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(blobdep.GradleFile(root), []byte(gradle), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	m := &SettingsModel{height: defaultHeight, confirmDeleteIndex: -1, root: root}
	if authed {
		m.blob.auth = ghauth.Cached
		m.blob.creds = ghauth.Credentials{Token: "x", Login: "someone"}
	}
	m.refreshBlob()
	return m
}

const gradleWithBlob = `dependencies {
    implementation files('libs/blob-competition-v1.4.0.aar')
}
`

// Without access the menu must not be a dead end: it says why and offers the
// one thing that fixes it.
func TestBlobMenuWithoutAccessOffersTheToken(t *testing.T) {
	m := modelIn(t, gradleWithBlob, false)

	items := m.blobMenuItems()
	if len(items) != len(blobLockedItems) {
		t.Fatalf("expected the locked menu, got %v", items)
	}

	view := m.viewBlob()
	if !strings.Contains(view, "GitHub token") {
		t.Error("the locked menu must offer to set a token")
	}
	if !strings.Contains(view, "private") {
		t.Error("the locked menu should say why it is locked")
	}
}

// Nothing that touches the library may be reachable without access.
func TestBlobMenuWithoutAccessHidesTheLibraryActions(t *testing.T) {
	m := modelIn(t, gradleWithBlob, false)

	for _, item := range m.blobMenuItems() {
		switch item {
		case "Build variant", "Version", "Recorded runs", "Add blob to the project":
			t.Errorf("%q is reachable without access", item)
		}
	}
}

func TestBlobMenuOffersToAddWhenAbsent(t *testing.T) {
	m := modelIn(t, "dependencies {\n}\n", true)

	if got := m.blobMenuItems(); len(got) != len(blobMissingItems) {
		t.Fatalf("expected the missing-blob menu, got %v", got)
	}
	if !strings.Contains(m.viewBlob(), "Add blob to the project") {
		t.Error("view should offer to add blob")
	}
	if m.blobLabel() != "not installed" {
		t.Errorf("main menu label should say not installed, got %q", m.blobLabel())
	}
}

func TestBlobMenuShowsVersionAndVariant(t *testing.T) {
	m := modelIn(t, gradleWithBlob, true)

	if got := m.blobLabel(); got != "v1.4.0 (comp)" {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(m.viewBlob(), "competition") {
		t.Error("view should name the current variant")
	}
}

func TestTokenLabelReportsState(t *testing.T) {
	m := modelIn(t, gradleWithBlob, true)
	if got := m.tokenLabel(); !strings.Contains(got, "someone") {
		t.Errorf("an authorised token should name the account, got %q", got)
	}

	m = modelIn(t, gradleWithBlob, false)
	if got := m.tokenLabel(); got != "not set" {
		t.Errorf("got %q, want %q", got, "not set")
	}
}

// The token must never be readable off the screen, and must not survive in
// model state once it has been submitted.
func TestTokenEntryIsMaskedAndNotRetained(t *testing.T) {
	m := modelIn(t, gradleWithBlob, false)
	m.goTo(screenBlobToken, 0)
	m.maskInput = true
	m.input = "ghp_secretsecret"

	view := m.viewBlobToken()
	if strings.Contains(view, "ghp_secretsecret") {
		t.Error("the token appears on screen in clear text")
	}
	if !strings.Contains(view, strings.Repeat("*", len("ghp_secretsecret"))) {
		t.Error("the token should render as mask characters")
	}

	m.updateBlobToken(tea.KeyMsg{Type: tea.KeyEnter})
	if m.input != "" {
		t.Errorf("the token was left in model state as %q", m.input)
	}
	if m.maskInput {
		t.Error("mask flag should be cleared once entry finishes")
	}
}

// A missing TeamCode/build.gradle is the normal case when pusher runs outside a
// project, and must not blow up the settings screen.
func TestBlobScreensSurviveANonProjectDirectory(t *testing.T) {
	m := modelIn(t, "", true)

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
	m := modelIn(t, gradleWithBlob, true)
	m.blob.tracErr = errNoRobot{}

	if !strings.Contains(m.viewBlobRuns(), "no robot") {
		t.Error("the runs view should surface the reason, not just show nothing")
	}
}

// A dependency line pointing at an AAR that is not on disk is a real state,
// reached by cloning a project whose libs are gitignored. Say so.
func TestBlobViewFlagsAMissingAAR(t *testing.T) {
	m := modelIn(t, gradleWithBlob, true)

	if m.blob.dep.Present {
		t.Fatal("no AAR was written, so it cannot be present")
	}
	if !strings.Contains(m.viewBlob(), "missing from TeamCode/libs") {
		t.Error("view should point out the referenced AAR is not there")
	}
}

type errNoRobot struct{}

func (errNoRobot) Error() string { return "no robot connected" }
