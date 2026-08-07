package blobdep

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// The library artifacts and the version used when none is pinned.
const (
	ArtifactComp = "blob-competition"

	ArtifactDev = "blob-dev"

	FallbackVersion = "v1.4.0"

	ignoreRule = "TeamCode/libs/*.aar"
)

// Dep is the library dependency as the project's gradle file declares it.
type Dep struct {
	File     string
	Line     int
	Artifact string
	Version  string

	Commented bool

	Present bool
}

// IsDev reports whether the dev build, which can record traces, is selected.
func (d Dep) IsDev() bool { return d.Artifact == ArtifactDev }

// VariantName names the selected build for showing a person.
func (d Dep) VariantName() string {
	if d.IsDev() {
		return "dev (records traces)"
	}
	return "competition (cannot log)"
}

var depRe = regexp.MustCompile(
	`^([ \t]*)(//[ \t]*)?(implementation|api|compileOnly)[ \t]+files\((['"])libs/(blob-competition|blob-dev)-(.+?)\.aar['"]\)(.*)$`)

// GradleFile is the project gradle file the dependency lives in.
func GradleFile(root string) string {
	return filepath.Join(root, "TeamCode", "build.gradle")
}

// LibsDir is where the library AAR is kept in the project.
func LibsDir(root string) string {
	return filepath.Join(root, "TeamCode", "libs")
}

// AARName is the file name for an artifact at a version.
func AARName(artifact, version string) string {
	return fmt.Sprintf("%s-%s.aar", artifact, version)
}

// AARPath is where an artifact at a version belongs in the project.
func AARPath(root, artifact, version string) string {
	return filepath.Join(LibsDir(root), AARName(artifact, version))
}

func rebuild(indent, keyword, quote, artifact, version, tail string, commented bool) string {
	line := fmt.Sprintf("%s files(%slibs/%s%s)%s",
		keyword, quote, AARName(artifact, version), quote, tail)
	if commented {
		return indent + "// " + line
	}
	return indent + line
}

// Detect reads the current library dependency out of the project.
func Detect(root string) (*Dep, error) {
	path := GradleFile(root)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}

	var parked *Dep
	for i, line := range strings.Split(string(data), "\n") {
		m := depRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		dep := &Dep{
			File:      path,
			Line:      i + 1,
			Artifact:  m[5],
			Version:   m[6],
			Commented: m[2] != "",
		}
		_, statErr := os.Stat(AARPath(root, dep.Artifact, dep.Version))
		dep.Present = statErr == nil

		if !dep.Commented {
			return dep, nil
		}
		if parked == nil {
			parked = dep
		}
	}

	return parked, nil
}

func blobLines(lines []string) []int {
	var idx []int
	for i, line := range lines {
		if depRe.MatchString(line) {
			idx = append(idx, i)
		}
	}
	return idx
}

func readLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	return strings.Split(string(data), "\n"), nil
}

func writeLines(path string, lines []string) error {
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}

func currentVersion(lines []string, idx []int) string {
	version := ""
	for _, i := range idx {
		m := depRe.FindStringSubmatch(lines[i])
		if version == "" {
			version = m[6]
		}
		if m[2] == "" {
			return m[6]
		}
	}
	return version
}

// SetArtifact switches build, keeping exactly one dependency line active.
func SetArtifact(root, artifact string) error {
	if artifact != ArtifactComp && artifact != ArtifactDev {
		return fmt.Errorf("unknown blob variant %q", artifact)
	}

	path := GradleFile(root)
	lines, err := readLines(path)
	if err != nil {
		return err
	}

	idx := blobLines(lines)
	if len(idx) == 0 {
		return fmt.Errorf("no blob dependency in %s", path)
	}

	version := currentVersion(lines, idx)

	target := idx[0]
	for _, i := range idx {
		if depRe.FindStringSubmatch(lines[i])[5] == artifact {
			target = i
			break
		}
	}

	for _, i := range idx {
		m := depRe.FindStringSubmatch(lines[i])
		if i == target {
			lines[i] = rebuild(m[1], m[3], m[4], artifact, version, m[7], false)
			continue
		}
		lines[i] = rebuild(m[1], m[3], m[4], other(artifact), version, m[7], true)
	}

	return writeLines(path, lines)
}

func other(artifact string) string {
	if artifact == ArtifactDev {
		return ArtifactComp
	}
	return ArtifactDev
}

// SetVersion moves every dependency line to a new version.
func SetVersion(root, version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		return fmt.Errorf("version cannot be empty")
	}

	path := GradleFile(root)
	lines, err := readLines(path)
	if err != nil {
		return err
	}

	changed := false
	for _, i := range blobLines(lines) {
		m := depRe.FindStringSubmatch(lines[i])
		if m[6] == version {
			continue
		}
		lines[i] = rebuild(m[1], m[3], m[4], m[5], version, m[7], m[2] != "")
		changed = true
	}

	if !changed {
		return nil
	}
	return writeLines(path, lines)
}

var depBlockRe = regexp.MustCompile(`(?m)^dependencies\s*\{`)

// Add writes a dependency line into a project that has none.
func Add(root, artifact, version string) error {
	path := GradleFile(root)
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", path, err)
	}

	content := string(data)

	if len(blobLines(strings.Split(content, "\n"))) > 0 {
		return fmt.Errorf("blob is already in %s", path)
	}

	loc := depBlockRe.FindStringIndex(content)
	if loc == nil {
		return fmt.Errorf("no top-level dependencies { } block in %s", path)
	}

	insertAt := closingBrace(content, loc[1]-1)
	if insertAt < 0 {
		return fmt.Errorf("unterminated dependencies { } block in %s", path)
	}

	entry := fmt.Sprintf("\n    // blob path follower. `pusher settings` manages this line.\n"+
		"    implementation files('libs/%s')\n", AARName(artifact, version))

	updated := content[:insertAt] + entry + content[insertAt:]
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	return nil
}

func closingBrace(content string, from int) int {
	depth := 0
	for i := from; i < len(content); i++ {
		switch content[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// Place writes the AAR into the project and gitignores it.
func Place(root, artifact, version string, data []byte) error {
	if err := os.MkdirAll(LibsDir(root), 0o755); err != nil {
		return fmt.Errorf("cannot create %s: %w", LibsDir(root), err)
	}
	if err := os.WriteFile(AARPath(root, artifact, version), data, 0o644); err != nil {
		return fmt.Errorf("cannot write the library: %w", err)
	}
	return nil
}

// Prune deletes AARs other than the one in use.
func Prune(root, keepArtifact, keepVersion string) error {
	entries, err := os.ReadDir(LibsDir(root))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	keep := AARName(keepArtifact, keepVersion)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == keep || !isBlobAAR(name) {
			continue
		}
		os.Remove(filepath.Join(LibsDir(root), name))
	}
	return nil
}

func isBlobAAR(name string) bool {
	return strings.HasSuffix(name, ".aar") &&
		(strings.HasPrefix(name, ArtifactComp+"-") || strings.HasPrefix(name, ArtifactDev+"-"))
}

// EnsureIgnored adds the AAR pattern to the project's gitignore.
func EnsureIgnored(root string) (added bool, err error) {
	path := filepath.Join(root, ".gitignore")

	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("cannot read .gitignore: %w", err)
	}

	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == ignoreRule {
			return false, nil
		}
	}

	entry := ignoreRule + "\n"
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		entry = "\n" + entry
	}
	entry = "\n# blob library, kept out of the repository on purpose.\n" + entry

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return false, fmt.Errorf("cannot write .gitignore: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(entry); err != nil {
		return false, fmt.Errorf("cannot write .gitignore: %w", err)
	}
	return true, nil
}

// TrackedAARs lists AARs git already tracks, which publishing the library would leak.
func TrackedAARs(root string) []string {
	cmd := exec.Command("git", "-C", root, "ls-files", "--", "TeamCode/libs/*.aar")
	out, err := cmd.Output()
	if err != nil {

		return nil
	}

	var tracked []string
	for _, line := range strings.Split(string(out), "\n") {
		if name := strings.TrimSpace(line); name != "" {
			tracked = append(tracked, name)
		}
	}
	return tracked
}
