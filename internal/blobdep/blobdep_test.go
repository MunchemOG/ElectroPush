package blobdep

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func project(t *testing.T, gradle string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "TeamCode"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(GradleFile(root), []byte(gradle), 0644); err != nil {
		t.Fatal(err)
	}
	return root
}

func read(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(GradleFile(root))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

const withComp = `dependencies {
    implementation 'org.firstinspires.ftc:Vision:11.1.0'
    implementation 'com.github.PzmuV1517.blob:blob:2.0.0'
}
`

func TestDetectFindsTheActiveDependency(t *testing.T) {
	dep, err := Detect(project(t, withComp))
	if err != nil {
		t.Fatal(err)
	}
	if dep == nil {
		t.Fatal("expected to find blob")
	}
	if dep.Artifact != ArtifactComp || dep.Version != "2.0.0" {
		t.Errorf("got %s:%s", dep.Artifact, dep.Version)
	}
	if dep.Commented {
		t.Error("dependency is not commented out")
	}
}

func TestDetectReturnsNilWhenAbsent(t *testing.T) {
	dep, err := Detect(project(t, "dependencies {\n    implementation 'x:y:1'\n}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if dep != nil {
		t.Errorf("expected no blob, got %+v", dep)
	}
}

// An active line must win, otherwise the menu would report the variant you are
// not building.
func TestDetectPrefersActiveOverCommented(t *testing.T) {
	gradle := `dependencies {
    // implementation 'com.github.PzmuV1517.blob:blob-dev:2.0.0'
    implementation 'com.github.PzmuV1517.blob:blob:2.0.0'
}
`
	dep, err := Detect(project(t, gradle))
	if err != nil {
		t.Fatal(err)
	}
	if dep.Artifact != ArtifactComp {
		t.Errorf("should report the active variant, got %s", dep.Artifact)
	}
}

func TestSetArtifactSwitchesToDev(t *testing.T) {
	root := project(t, withComp)
	if err := SetArtifact(root, ArtifactDev); err != nil {
		t.Fatal(err)
	}

	dep, _ := Detect(root)
	if dep.Artifact != ArtifactDev {
		t.Fatalf("expected dev, got %s", dep.Artifact)
	}
	if dep.Version != "2.0.0" {
		t.Errorf("version should survive the switch, got %s", dep.Version)
	}
}

func TestSetArtifactIsReversible(t *testing.T) {
	root := project(t, withComp)
	for _, want := range []string{ArtifactDev, ArtifactComp, ArtifactDev} {
		if err := SetArtifact(root, want); err != nil {
			t.Fatal(err)
		}
		dep, _ := Detect(root)
		if dep.Artifact != want {
			t.Fatalf("expected %s, got %s", want, dep.Artifact)
		}
	}
}

// Switching must never leave two live blob dependencies, which would be an
// unresolvable build at best and a logging competition build at worst.
func TestSetArtifactNeverLeavesTwoActiveLines(t *testing.T) {
	gradle := `dependencies {
    implementation 'com.github.PzmuV1517.blob:blob:2.0.0'
    // implementation 'com.github.PzmuV1517.blob:blob-dev:2.0.0'
}
`
	root := project(t, gradle)
	if err := SetArtifact(root, ArtifactDev); err != nil {
		t.Fatal(err)
	}

	active := 0
	for _, line := range strings.Split(read(t, root), "\n") {
		if m := depRe.FindStringSubmatch(line); m != nil && m[2] == "" {
			active++
		}
	}
	if active != 1 {
		t.Errorf("expected exactly 1 active blob line, got %d\n%s", active, read(t, root))
	}
}

func TestSetVersionUpdatesCommentedLinesToo(t *testing.T) {
	gradle := `dependencies {
    implementation 'com.github.PzmuV1517.blob:blob:2.0.0'
    // implementation 'com.github.PzmuV1517.blob:blob-dev:2.0.0'
}
`
	root := project(t, gradle)
	if err := SetVersion(root, "2.1.0"); err != nil {
		t.Fatal(err)
	}

	out := read(t, root)
	if strings.Contains(out, "2.0.0") {
		t.Errorf("old version survived:\n%s", out)
	}
	if strings.Count(out, "2.1.0") != 2 {
		t.Errorf("both lines should be bumped so they cannot drift:\n%s", out)
	}
}

func TestAddInsertsIntoDependenciesBlock(t *testing.T) {
	root := project(t, "dependencies {\n    implementation 'x:y:1'\n}\n")
	if _, err := Add(root, ArtifactComp, "2.0.0"); err != nil {
		t.Fatal(err)
	}

	dep, err := Detect(root)
	if err != nil || dep == nil {
		t.Fatalf("added dependency not detected: %v", err)
	}
	if dep.Artifact != ArtifactComp || dep.Version != "2.0.0" {
		t.Errorf("got %s:%s", dep.Artifact, dep.Version)
	}
}

func TestAddWarnsWhenJitPackIsMissing(t *testing.T) {
	root := project(t, "dependencies {\n}\n")
	warning, err := Add(root, ArtifactComp, "2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if warning == "" {
		t.Error("expected a warning about the missing JitPack repository")
	}
}

func TestAddStaysQuietWhenJitPackIsPresent(t *testing.T) {
	root := project(t, "dependencies {\n}\n")
	os.WriteFile(filepath.Join(root, "settings.gradle"),
		[]byte("maven { url = 'https://jitpack.io' }"), 0644)

	warning, err := Add(root, ArtifactComp, "2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if warning != "" {
		t.Errorf("unexpected warning: %s", warning)
	}
}

func TestDetectHandlesDoubleQuotesAndParens(t *testing.T) {
	root := project(t, "dependencies {\n    implementation(\"com.github.PzmuV1517.blob:blob-dev:v1.3.0\")\n}\n")
	dep, err := Detect(root)
	if err != nil || dep == nil {
		t.Fatalf("not detected: %v", err)
	}
	if dep.Artifact != ArtifactDev || dep.Version != "v1.3.0" {
		t.Errorf("got %s:%s", dep.Artifact, dep.Version)
	}
}
