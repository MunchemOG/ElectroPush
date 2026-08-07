package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const releaseAPI = "https://api.github.com/repos/PzmuV1517/Pusher/releases/latest"

// Method is how pusher was installed.
type Method int

// How pusher was installed.
const (
	Binary Method = iota

	Homebrew
)

// String names the install method for showing a person.
func (m Method) String() string {
	if m == Homebrew {
		return "Homebrew"
	}
	return "binary"
}

// Install is where this copy of pusher lives and how it got there.
type Install struct {
	Method Method

	Path string

	Formula string
}

var current = "dev"

// SetCurrent records the running version.
func SetCurrent(version string) { current = version }

// Current is the running version.
func Current() string { return current }

// Detect works out how this copy of pusher was installed.
func Detect() (Install, error) {
	exe, err := os.Executable()
	if err != nil {
		return Install{}, fmt.Errorf("cannot locate the running binary: %w", err)
	}

	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	if formula, ok := cellarFormula(exe); ok {
		return Install{Method: Homebrew, Path: exe, Formula: formula}, nil
	}
	return Install{Method: Binary, Path: exe}, nil
}

func cellarFormula(path string) (string, bool) {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i, part := range parts {
		if part == "Cellar" && i+1 < len(parts) {
			return parts[i+1], true
		}
	}
	return "", false
}

// Release is a published version and where to download it.
type Release struct {
	Tag      string
	AssetURL string
	SumsURL  string
}

// Version is the tag without its leading v.
func (r Release) Version() string { return strings.TrimPrefix(r.Tag, "v") }

// Newer reports whether the release differs from what is running.
func (r Release) Newer() bool {
	running := strings.TrimPrefix(current, "v")
	if running == "" || running == "dev" {
		return true
	}
	return r.Version() != running
}

// AssetName is the release asset for the platform pusher is running on.
func AssetName() string {
	name := fmt.Sprintf("pusher-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// Latest asks GitHub for the newest release.
func Latest() (Release, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Get(releaseAPI)
	if err != nil {
		return Release{}, fmt.Errorf("cannot reach GitHub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub returned %s", resp.Status)
	}

	var payload struct {
		Tag    string `json:"tag_name"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Release{}, fmt.Errorf("cannot read GitHub response: %w", err)
	}
	if payload.Tag == "" {
		return Release{}, fmt.Errorf("the newest release has no tag")
	}

	rel := Release{Tag: payload.Tag}
	want := AssetName()
	for _, asset := range payload.Assets {
		switch asset.Name {
		case want:
			rel.AssetURL = asset.URL
		case "SHA256SUMS":
			rel.SumsURL = asset.URL
		}
	}
	if rel.AssetURL == "" {
		return rel, fmt.Errorf("release %s has no %s build", payload.Tag, want)
	}

	return rel, nil
}

// UpgradeBrew hands the update to Homebrew.
func UpgradeBrew(formula string) (string, error) {
	if formula == "" {
		formula = "pusher"
	}

	out, err := exec.Command("brew", "upgrade", formula).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return text, fmt.Errorf("brew upgrade %s failed: %w", formula, err)
	}
	return text, nil
}

// LastLine is the final non-empty line of some output. Homebrew says plenty and
// only the end of it is the outcome.
func LastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}

// Apply replaces the running binary, verified against the release checksums.
func Apply(rel Release, path string) error {
	if rel.AssetURL == "" {
		return fmt.Errorf("no download for this platform")
	}

	dir := filepath.Dir(path)
	if err := writable(dir, path); err != nil {
		return err
	}

	blob, err := download(rel.AssetURL)
	if err != nil {
		return err
	}

	if rel.SumsURL != "" {
		if err := verify(blob, rel.SumsURL); err != nil {
			return err
		}
	}

	staged := filepath.Join(dir, ".pusher-update")
	if err := os.WriteFile(staged, blob, 0o755); err != nil {
		return fmt.Errorf("cannot write the new binary: %w", err)
	}

	if err := swap(staged, path); err != nil {
		os.Remove(staged)
		return err
	}
	return nil
}

func swap(staged, path string) error {
	if runtime.GOOS == "windows" {
		previous := path + ".old"
		os.Remove(previous)
		if err := os.Rename(path, previous); err != nil {
			return fmt.Errorf("cannot move the old binary aside: %w", err)
		}
		if err := os.Rename(staged, path); err != nil {
			os.Rename(previous, path)
			return fmt.Errorf("cannot install the new binary: %w", err)
		}
		return nil
	}

	if err := os.Rename(staged, path); err != nil {
		return fmt.Errorf("cannot install the new binary: %w", err)
	}
	return nil
}

func writable(dir, path string) error {
	probe := filepath.Join(dir, ".pusher-write-test")
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("%s is not writable, so pusher cannot replace itself there.\n"+
			"Re-run with sudo, or reinstall from the latest release", dir)
	}
	f.Close()
	os.Remove(probe)
	return nil
}

func download(url string) ([]byte, error) {
	client := &http.Client{Timeout: 5 * time.Minute}

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("cannot download the release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned %s", resp.Status)
	}

	blob, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("download was cut short: %w", err)
	}
	if len(blob) == 0 {
		return nil, fmt.Errorf("download was empty")
	}
	return blob, nil
}

func verify(blob []byte, sumsURL string) error {
	sums, err := download(sumsURL)
	if err != nil {
		return err
	}

	want, ok := SumFor(string(sums), AssetName())
	if !ok {

		return nil
	}

	got := sha256.Sum256(blob)
	if hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("the download did not match its published checksum")
	}
	return nil
}

// SumFor pulls one asset's checksum out of a SHA256SUMS file.
func SumFor(sums, asset string) (string, bool) {
	for _, line := range strings.Split(sums, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 2 {
			continue
		}
		if strings.TrimPrefix(fields[1], "*") == asset {
			return strings.ToLower(fields[0]), true
		}
	}
	return "", false
}
