package selfupdate

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCellarFormula(t *testing.T) {
	cases := []struct {
		path    string
		formula string
		ok      bool
	}{
		{"/opt/homebrew/Cellar/pusher/1.0.34/bin/pusher", "pusher", true},
		{"/usr/local/Cellar/pusher/1.0.34/bin/pusher", "pusher", true},
		{"/home/linuxbrew/.linuxbrew/Cellar/pusher/1.0.34/bin/pusher", "pusher", true},
		{"/usr/local/bin/pusher", "", false},
		{"/Users/someone/go/bin/pusher", "", false},

		{"/opt/homebrew/Cellar", "", false},
	}

	for _, c := range cases {
		formula, ok := cellarFormula(c.path)
		if ok != c.ok || formula != c.formula {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", c.path, formula, ok, c.formula, c.ok)
		}
	}
}

func TestAssetNameMatchesPublishedNames(t *testing.T) {
	published := map[string]bool{
		"pusher-darwin-amd64":      true,
		"pusher-darwin-arm64":      true,
		"pusher-linux-amd64":       true,
		"pusher-windows-amd64.exe": true,
	}

	name := AssetName()
	if !published[name] {
		t.Errorf("AssetName() = %q, which the release workflow does not build", name)
	}
	if runtime.GOOS == "windows" && filepath.Ext(name) != ".exe" {
		t.Errorf("%q needs a .exe suffix on windows", name)
	}
}

func TestSumFor(t *testing.T) {
	sums := "abc123  pusher-darwin-arm64\n" +
		"DEF456  pusher-linux-amd64\n" +
		"789aaa *pusher-windows-amd64.exe\n" +
		"\n" +
		"malformed-line\n"

	cases := []struct {
		asset string
		want  string
		ok    bool
	}{
		{"pusher-darwin-arm64", "abc123", true},
		{"pusher-linux-amd64", "def456", true},
		{"pusher-windows-amd64.exe", "789aaa", true},
		{"pusher-darwin-universal", "", false},
	}

	for _, c := range cases {
		got, ok := SumFor(sums, c.asset)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", c.asset, got, ok, c.want, c.ok)
		}
	}
}

func TestReleaseVersionAndNewer(t *testing.T) {
	restore := current
	t.Cleanup(func() { current = restore })

	rel := Release{Tag: "v1.0.35"}
	if rel.Version() != "1.0.35" {
		t.Errorf("Version() = %q", rel.Version())
	}

	cases := []struct {
		running string
		newer   bool
	}{
		{"1.0.34", true},
		{"1.0.35", false},
		{"v1.0.35", false},
		{"dev", true},
		{"", true},
	}

	for _, c := range cases {
		current = c.running
		if got := rel.Newer(); got != c.newer {
			t.Errorf("running %q: Newer() = %v, want %v", c.running, got, c.newer)
		}
	}
}

func TestWritableRejectsReadOnlyDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	if err := writable(dir, filepath.Join(dir, "pusher")); err == nil {
		t.Error("a read-only directory should be reported before anything downloads")
	}
}

func TestWritableAcceptsNormalDirAndCleansUp(t *testing.T) {
	dir := t.TempDir()

	if err := writable(dir, filepath.Join(dir, "pusher")); err != nil {
		t.Fatalf("writable(%s): %v", dir, err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("probe left %d file(s) behind", len(entries))
	}
}

func TestSwapReplacesBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pusher")
	staged := filepath.Join(dir, ".pusher-update")

	if err := os.WriteFile(path, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := swap(staged, path); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("binary holds %q, want %q", got, "new")
	}

	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Error("the staged file should be gone once it is in place")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		t.Errorf("replacement is not executable: %v", info.Mode())
	}
}

func TestMethodString(t *testing.T) {
	for method, want := range map[Method]string{Homebrew: "Homebrew", Binary: "binary"} {
		if got := fmt.Sprint(method); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}
