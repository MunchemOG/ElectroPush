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

const releaseAPI = "https://api.github.com/repos/MunchemOG/ElectroPush/releases/latest"

// Method is how epsh was installed.
type Method int

// How epsh was installed.
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

// Install is where this copy of epsh lives and how it got there.
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

// Detect works out how this copy of epsh was installed.
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

// cellarFormula names the formula this binary was installed as, qualified by
// its tap.
//
// The tap matters. A bare name is ambiguous across taps and casks, and
// `brew upgrade epsh` resolves to the unrelated NWEpsh cask rather than
// this formula, so the upgrade fails saying a cask is not installed.
func cellarFormula(path string) (string, bool) {
	parts := strings.Split(filepath.ToSlash(path), "/")

	for i, part := range parts {
		if part != "Cellar" || i+2 >= len(parts) {
			continue
		}

		name := parts[i+1]

		// .../Cellar/<formula>/<version>/ holds the receipt, whatever is below.
		keg := strings.Join(parts[:i+3], "/")
		if tap := receiptTap(filepath.Join(filepath.FromSlash(keg), "INSTALL_RECEIPT.json")); tap != "" {
			name = tap + "/" + name
		}

		return name, true
	}

	return "", false
}

// receiptTap is the tap a keg was installed from, empty when it came from
// homebrew/core or the receipt cannot be read.
func receiptTap(path string) string {
	blob, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	var receipt struct {
		Source struct {
			Tap string `json:"tap"`
		} `json:"source"`
	}
	if err := json.Unmarshal(blob, &receipt); err != nil {
		return ""
	}

	if receipt.Source.Tap == "homebrew/core" {
		return ""
	}
	return receipt.Source.Tap
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

// AssetName is the release asset for the platform epsh is running on.
func AssetName() string {
	name := fmt.Sprintf("epsh-%s-%s", runtime.GOOS, runtime.GOARCH)
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

// UpgradeBrew hands the update to Homebrew and checks that it happened.
//
// want is the version being upgraded to, without its leading v. Empty skips the
// check.
//
// --formula stops brew resolving the name to a cask, which it will do for a
// bare name that some cask also claims.
func UpgradeBrew(formula, want string) (string, error) {
	if formula == "" {
		formula = "epsh"
	}

	// A tap is a cached git clone that brew only refreshes every so often, a
	// day by default. A release published minutes ago is invisible until it is
	// fetched, and brew then says the newest version is already installed and
	// exits 0 having done nothing.
	_ = exec.Command("brew", "update", "--quiet").Run()

	out, err := exec.Command("brew", "upgrade", "--formula", formula).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return text, fmt.Errorf("brew upgrade %s failed: %w", formula, err)
	}

	// Exiting 0 is not the same as having upgraded, so the outcome is read back
	// rather than assumed. Reporting an update that did not happen is worse
	// than reporting a failure.
	if want != "" && !BrewHas(formula, want) {
		have := strings.Join(BrewVersions(formula), ", ")
		if have == "" {
			have = "nothing"
		}
		return text, fmt.Errorf("brew still has %s, not %s\n"+
			"    The tap may not carry it yet. Try again shortly, or\n"+
			"    brew update && brew upgrade %s", have, want, formula)
	}

	return text, nil
}

// BrewVersions is what Homebrew currently has installed for a formula.
func BrewVersions(formula string) []string {
	out, err := exec.Command("brew", "list", "--versions", formula).Output()
	if err != nil {
		return nil
	}

	return parseBrewVersions(string(out))
}

// parseBrewVersions reads "epsh 1.2.1 1.2.2": the name first, then every keg.
func parseBrewVersions(listing string) []string {
	fields := strings.Fields(strings.TrimSpace(listing))
	if len(fields) < 2 {
		return nil
	}
	return fields[1:]
}

// BrewHas reports whether a version is among what Homebrew installed.
func BrewHas(formula, want string) bool {
	for _, version := range BrewVersions(formula) {
		if version == want {
			return true
		}
	}
	return false
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

	staged := filepath.Join(dir, ".epsh-update")
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
	probe := filepath.Join(dir, ".epsh-write-test")
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("%s is not writable, so epsh cannot replace itself there.\n"+
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
