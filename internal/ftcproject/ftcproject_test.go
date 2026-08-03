package ftcproject

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const commonGradle = `apply plugin: 'com.android.application'

android {
    buildTypes {
        release {
            ndk {
                abiFilters "armeabi-v7a", "arm64-v8a"
            }
        }
        debug {
            debuggable true
            ndk {
                abiFilters "armeabi-v7a", "arm64-v8a"
            }
        }
    }
}
`

const teamCodeGradle = `apply from: '../build.common.gradle'

android {
    namespace = 'org.firstinspires.ftc.teamcode'

    packagingOptions {
        jniLibs.useLegacyPackaging true
    }
}

dependencies {
    implementation project(':FtcRobotController')
}
`

func newProject(t *testing.T) *Project {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "TeamCode"), 0755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "build.common.gradle"), commonGradle)
	write(t, filepath.Join(root, "TeamCode", "build.gradle"), teamCodeGradle)

	project, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	return project
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestDetectRejectsNonFTCDirectory(t *testing.T) {
	if _, err := Detect(t.TempDir()); err == nil {
		t.Fatal("expected an error for a directory with no build.common.gradle")
	}
}

func TestAnalyzeReadsBothABIs(t *testing.T) {
	analysis, err := newProject(t).Analyze()
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"arm64-v8a", "armeabi-v7a"}
	if strings.Join(analysis.ABIs, ",") != strings.Join(want, ",") {
		t.Errorf("got ABIs %v, want %v", analysis.ABIs, want)
	}
	if analysis.HasBackups {
		t.Error("a fresh project should have no backups")
	}
}

func TestSetABIRewritesEveryBuildTypeAndBacksUp(t *testing.T) {
	project := newProject(t)

	changed, err := project.SetABI("armeabi-v7a")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected SetABI to report a change")
	}

	patched := read(t, project.CommonGradle)
	if strings.Contains(patched, "arm64-v8a") {
		t.Error("arm64-v8a should be gone from every build type")
	}
	if count := strings.Count(patched, `abiFilters "armeabi-v7a"`); count != 2 {
		t.Errorf("expected both release and debug rewritten, got %d", count)
	}

	if !strings.Contains(patched, "                abiFilters \"armeabi-v7a\"") {
		t.Error("original indentation was not preserved")
	}

	if !project.HasBackups() {
		t.Error("SetABI should leave a backup behind")
	}
	if got := read(t, project.CommonGradle+backupSuffix); got != commonGradle {
		t.Error("backup does not match the original file")
	}
}

func TestSetABIIsIdempotent(t *testing.T) {
	project := newProject(t)

	if _, err := project.SetABI("armeabi-v7a"); err != nil {
		t.Fatal(err)
	}
	changed, err := project.SetABI("armeabi-v7a")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("re-applying the same ABI should report no change")
	}
}

func TestBackupSurvivesRepeatedPatching(t *testing.T) {
	project := newProject(t)

	if _, err := project.SetABI("armeabi-v7a"); err != nil {
		t.Fatal(err)
	}
	if _, err := project.SetABI("arm64-v8a"); err != nil {
		t.Fatal(err)
	}

	if got := read(t, project.CommonGradle+backupSuffix); got != commonGradle {
		t.Error("backup was clobbered by the second patch")
	}
}

func TestStripSourceMapsInsertsInsideAndroidBlock(t *testing.T) {
	project := newProject(t)

	changed, err := project.StripSourceMaps()
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected StripSourceMaps to report a change")
	}

	patched := read(t, project.TeamCodeGradle)

	if strings.Contains(patched, "ignoreAssetsPatterns +=") {
		t.Error("must use .add(), not += , which AGP rejects")
	}
	if !strings.Contains(patched, "ignoreAssetsPatterns.add('*.map')") {
		t.Error("source map exclusion missing")
	}

	androidAt := strings.Index(patched, "android {")
	insertAt := strings.Index(patched, "androidResources")
	depsAt := strings.Index(patched, "dependencies {")
	if !(androidAt < insertAt && insertAt < depsAt) {
		t.Errorf("insertion landed outside the android block (android=%d insert=%d deps=%d)", androidAt, insertAt, depsAt)
	}

	for _, line := range strings.Split(patched, "\n") {
		if line != strings.TrimRight(line, " \t") {
			t.Errorf("patched file has trailing whitespace on %q", line)
		}
	}

	changedAgain, err := project.StripSourceMaps()
	if err != nil {
		t.Fatal(err)
	}
	if changedAgain {
		t.Error("stripping source maps twice should be a no-op")
	}
}

func TestUndoRestoresEveryPatchedFile(t *testing.T) {
	project := newProject(t)

	if _, err := project.SetABI("armeabi-v7a"); err != nil {
		t.Fatal(err)
	}
	if _, err := project.StripSourceMaps(); err != nil {
		t.Fatal(err)
	}

	restored, err := project.Undo()
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) != 2 {
		t.Errorf("expected 2 files restored, got %v", restored)
	}

	if got := read(t, project.CommonGradle); got != commonGradle {
		t.Error("build.common.gradle was not restored exactly")
	}
	if got := read(t, project.TeamCodeGradle); got != teamCodeGradle {
		t.Error("TeamCode/build.gradle was not restored exactly")
	}
	if project.HasBackups() {
		t.Error("undo should remove the backups it consumed")
	}
}

func TestUndoWithNothingToRestoreErrors(t *testing.T) {
	if _, err := newProject(t).Undo(); err == nil {
		t.Fatal("expected an error when there is nothing to undo")
	}
}

func TestPickABI(t *testing.T) {
	tests := []struct {
		name    string
		device  []string
		project []string
		want    string
		wantErr bool
	}{
		{
			name:    "device preference wins when the project ships it",
			device:  []string{"arm64-v8a", "armeabi-v7a"},
			project: []string{"arm64-v8a", "armeabi-v7a"},
			want:    "arm64-v8a",
		},
		{
			name:    "falls to the next device ABI the project actually ships",
			device:  []string{"arm64-v8a", "armeabi-v7a"},
			project: []string{"armeabi-v7a"},
			want:    "armeabi-v7a",
		},
		{
			name:    "32-bit only hub picks the 32-bit library",
			device:  []string{"armeabi-v7a"},
			project: []string{"arm64-v8a", "armeabi-v7a"},
			want:    "armeabi-v7a",
		},
		{
			name:    "no project filters means take the device's favourite",
			device:  []string{"armeabi-v7a"},
			project: nil,
			want:    "armeabi-v7a",
		},
		{
			name:    "no overlap is an error rather than a bad guess",
			device:  []string{"x86_64"},
			project: []string{"armeabi-v7a"},
			wantErr: true,
		},
		{
			name:    "no device ABIs is an error",
			device:  nil,
			project: []string{"armeabi-v7a"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PickABI(tt.device, tt.project)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAppendToAndroidBlockRejectsMalformedFiles(t *testing.T) {
	if _, err := appendToAndroidBlock("dependencies {\n}\n", "x"); err == nil {
		t.Error("expected an error when there is no android block")
	}
	if _, err := appendToAndroidBlock("android {\n", "x"); err == nil {
		t.Error("expected an error for an unterminated android block")
	}
}
