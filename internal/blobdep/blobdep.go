// Package blobdep finds and edits the blob library dependency in an FTC
// project's TeamCode/build.gradle.
package blobdep

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	// ArtifactComp is the competition build: no path-recording code at all.
	ArtifactComp = "blob"
	// ArtifactDev is the practice build: records traces for `pusher visualiser`.
	ArtifactDev = "blob-dev"

	DefaultGroup = "com.github.PzmuV1517.blob"
	tagsAPI      = "https://api.github.com/repos/PzmuV1517/blob/tags"
)

// Dep is a blob dependency found in a gradle file.
type Dep struct {
	File     string
	Line     int
	Group    string
	Artifact string
	Version  string
	// Commented is true when the line is commented out, which is how the README
	// suggests keeping the variant you are not currently using.
	Commented bool
}

func (d Dep) IsDev() bool { return d.Artifact == ArtifactDev }

// VariantName is what to show a human.
func (d Dep) VariantName() string {
	if d.IsDev() {
		return "dev (records traces)"
	}
	return "competition (cannot log)"
}

// depRe matches an implementation/api line pulling in blob or blob-dev, whether
// quoted with ' or ", parenthesised or not, and commented out or not.
var depRe = regexp.MustCompile(
	`^([ \t]*)(//[ \t]*)?((?:implementation|api|compileOnly)[ \t(]+['"])([A-Za-z0-9_.\-]+):(blob|blob-dev):([^'"]+)(['"]\)?.*)$`)

// GradleFile is the file a project's dependencies live in.
func GradleFile(root string) string {
	return filepath.Join(root, "TeamCode", "build.gradle")
}

// Detect returns the blob dependency for a project, or nil if there is none.
// An active (uncommented) line always wins over a commented one.
func Detect(root string) (*Dep, error) {
	path := GradleFile(root)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}

	var fallback *Dep
	for i, line := range strings.Split(string(data), "\n") {
		m := depRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		dep := &Dep{
			File:      path,
			Line:      i + 1,
			Group:     m[4],
			Artifact:  m[5],
			Version:   m[6],
			Commented: m[2] != "",
		}
		if !dep.Commented {
			return dep, nil
		}
		if fallback == nil {
			fallback = dep
		}
	}

	return fallback, nil
}

// SetArtifact switches the active dependency between the competition and dev
// builds. If the target variant is sitting there commented out, the two lines
// swap rather than leaving a duplicate behind.
func SetArtifact(root, artifact string) error {
	if artifact != ArtifactComp && artifact != ArtifactDev {
		return fmt.Errorf("unknown blob variant %q", artifact)
	}

	path := GradleFile(root)
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", path, err)
	}

	lines := strings.Split(string(data), "\n")
	changed := false

	for i, line := range lines {
		m := depRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		indent, comment, prefix, group, current, version, tail := m[1], m[2], m[3], m[4], m[5], m[6], m[7]
		active := comment == ""

		switch {
		case active && current != artifact:
			// The line we are on is the wrong variant: retarget it.
			lines[i] = indent + prefix + group + ":" + artifact + ":" + version + tail
			changed = true
		case !active && current == artifact:
			// A commented line already names the variant we want: it becomes the
			// active one, and the previously active line gets commented above.
			lines[i] = indent + prefix + group + ":" + artifact + ":" + version + tail
			changed = true
		}
	}

	if !changed {
		return nil
	}

	// Comment out any now-duplicate line naming the other variant.
	seenActive := false
	for i, line := range lines {
		m := depRe.FindStringSubmatch(line)
		if m == nil || m[2] != "" {
			continue
		}
		if m[5] != artifact {
			lines[i] = m[1] + "// " + strings.TrimSpace(line[len(m[1]):])
			continue
		}
		if seenActive {
			lines[i] = m[1] + "// " + strings.TrimSpace(line[len(m[1]):])
			continue
		}
		seenActive = true
	}

	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}

// SetVersion rewrites the version on every blob line, active or commented, so
// the two variants never drift apart.
func SetVersion(root, version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		return fmt.Errorf("version cannot be empty")
	}

	path := GradleFile(root)
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", path, err)
	}

	lines := strings.Split(string(data), "\n")
	changed := false
	for i, line := range lines {
		m := depRe.FindStringSubmatch(line)
		if m == nil || m[6] == version {
			continue
		}
		lines[i] = m[1] + m[2] + m[3] + m[4] + ":" + m[5] + ":" + version + m[7]
		changed = true
	}

	if !changed {
		return nil
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}

var depBlockRe = regexp.MustCompile(`(?m)^dependencies\s*\{`)

// Add inserts a blob dependency into the project's dependencies block. It
// returns a warning when the JitPack repository is missing, because the build
// will not resolve without it and that is not something to fix silently in a
// file we were not asked to touch.
func Add(root, artifact, version string) (warning string, err error) {
	path := GradleFile(root)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", path, err)
	}

	content := string(data)
	loc := depBlockRe.FindStringIndex(content)
	if loc == nil {
		return "", fmt.Errorf("no top-level dependencies { } block in %s", path)
	}

	entry := fmt.Sprintf("\n    // blob path follower. `pusher settings` manages this line.\n"+
		"    implementation '%s:%s:%s'\n", DefaultGroup, artifact, version)

	depth := 0
	insertAt := -1
	for i := loc[1] - 1; i < len(content); i++ {
		switch content[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				insertAt = i
			}
		}
		if insertAt >= 0 {
			break
		}
	}
	if insertAt < 0 {
		return "", fmt.Errorf("unterminated dependencies { } block in %s", path)
	}

	updated := content[:insertAt] + entry + content[insertAt:]
	if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
		return "", fmt.Errorf("cannot write %s: %w", path, err)
	}

	if !hasJitPack(root) {
		warning = "JitPack repository not found. Add `maven { url = 'https://jitpack.io' }` " +
			"to your repositories before syncing."
	}
	return warning, nil
}

func hasJitPack(root string) bool {
	candidates := []string{
		filepath.Join(root, "settings.gradle"),
		filepath.Join(root, "build.gradle"),
		filepath.Join(root, "build.common.gradle"),
		GradleFile(root),
	}
	for _, path := range candidates {
		if data, err := os.ReadFile(path); err == nil {
			if strings.Contains(string(data), "jitpack.io") {
				return true
			}
		}
	}
	return false
}

// LatestVersion asks GitHub for the newest tag. Tags are what JitPack resolves,
// so the tag name is used verbatim rather than being normalised.
func LatestVersion() (string, error) {
	client := &http.Client{Timeout: 6 * time.Second}

	resp, err := client.Get(tagsAPI)
	if err != nil {
		return "", fmt.Errorf("cannot reach GitHub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub returned %s", resp.Status)
	}

	var tags []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return "", fmt.Errorf("cannot read GitHub response: %w", err)
	}
	if len(tags) == 0 {
		return "", fmt.Errorf("the blob repository has no tags")
	}

	return tags[0].Name, nil
}
