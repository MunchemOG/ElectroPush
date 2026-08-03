package ftcproject

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const backupSuffix = ".pusher-bak"

var abiFiltersRe = regexp.MustCompile(`(?m)^([ \t]*)abiFilters\s+(.+)$`)

var quotedRe = regexp.MustCompile(`["']([^"']+)["']`)

var sourceMapPatternRe = regexp.MustCompile(`ignoreAssetsPatterns`)

type Project struct {
	Root string

	CommonGradle string

	TeamCodeGradle string
}

func Detect(root string) (*Project, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve %s: %w", root, err)
	}

	proj := &Project{
		Root:           abs,
		CommonGradle:   filepath.Join(abs, "build.common.gradle"),
		TeamCodeGradle: filepath.Join(abs, "TeamCode", "build.gradle"),
	}

	if _, err := os.Stat(proj.CommonGradle); err != nil {
		return nil, fmt.Errorf("this does not look like an FTC project: no build.common.gradle in %s", abs)
	}

	return proj, nil
}

type Analysis struct {
	ABIs []string

	StripsSourceMaps bool

	HasBackups bool
}

func (p *Project) Analyze() (*Analysis, error) {
	common, err := os.ReadFile(p.CommonGradle)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", p.CommonGradle, err)
	}

	analysis := &Analysis{HasBackups: p.HasBackups()}

	seen := map[string]bool{}
	for _, match := range abiFiltersRe.FindAllStringSubmatch(string(common), -1) {
		for _, abi := range quotedRe.FindAllStringSubmatch(match[2], -1) {
			if !seen[abi[1]] {
				seen[abi[1]] = true
				analysis.ABIs = append(analysis.ABIs, abi[1])
			}
		}
	}
	sort.Strings(analysis.ABIs)

	if teamCode, err := os.ReadFile(p.TeamCodeGradle); err == nil {
		analysis.StripsSourceMaps = sourceMapPatternRe.Match(teamCode)
	}

	return analysis, nil
}

// Must edit build.common.gradle, not TeamCode/build.gradle: AGP unions
// ndk.abiFilters from defaultConfig with the build type's, so a narrower list
// added elsewhere merges straight back to the original set.
func (p *Project) SetABI(abi string) (bool, error) {
	original, err := os.ReadFile(p.CommonGradle)
	if err != nil {
		return false, fmt.Errorf("cannot read %s: %w", p.CommonGradle, err)
	}

	replacement := fmt.Sprintf(`abiFilters %q`, abi)
	patched := abiFiltersRe.ReplaceAllStringFunc(string(original), func(line string) string {
		match := abiFiltersRe.FindStringSubmatch(line)
		return match[1] + replacement
	})

	if patched == string(original) {
		return false, nil
	}

	if err := p.backup(p.CommonGradle, original); err != nil {
		return false, err
	}

	if err := os.WriteFile(p.CommonGradle, []byte(patched), 0644); err != nil {
		return false, fmt.Errorf("cannot write %s: %w", p.CommonGradle, err)
	}

	return true, nil
}

func (p *Project) StripSourceMaps() (bool, error) {
	original, err := os.ReadFile(p.TeamCodeGradle)
	if err != nil {
		return false, fmt.Errorf("cannot read %s: %w", p.TeamCodeGradle, err)
	}

	if sourceMapPatternRe.Match(original) {
		return false, nil
	}

	// .add(), never +=: AGP exposes ignoreAssetsPatterns as a read-only List,
	// so reassigning it fails at configuration time.
	block := `// pusher: source maps are debugger-only and never read on the robot.
androidResources {
    ignoreAssetsPatterns.add('*.map')
}`

	patched, err := appendToAndroidBlock(string(original), block)
	if err != nil {
		return false, err
	}

	if err := p.backup(p.TeamCodeGradle, original); err != nil {
		return false, err
	}

	if err := os.WriteFile(p.TeamCodeGradle, []byte(patched), 0644); err != nil {
		return false, fmt.Errorf("cannot write %s: %w", p.TeamCodeGradle, err)
	}

	return true, nil
}

func appendToAndroidBlock(content, block string) (string, error) {
	start := regexp.MustCompile(`(?m)^android\s*\{`).FindStringIndex(content)
	if start == nil {
		return "", fmt.Errorf("no top-level android { } block found")
	}

	depth := 0
	for i := start[1] - 1; i < len(content); i++ {
		switch content[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return content[:i] + indentBlock(block) + content[i:], nil
			}
		}
	}

	return "", fmt.Errorf("unterminated android { } block")
}

func indentBlock(block string) string {
	var b strings.Builder
	b.WriteString("\n")

	for _, line := range strings.Split(strings.Trim(block, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			b.WriteString("\n")
			continue
		}
		b.WriteString("    " + line + "\n")
	}

	return b.String()
}

// Never overwrites an existing backup: the first one is the only pristine copy,
// so a second slim run must not replace it with already-patched content.
func (p *Project) backup(path string, content []byte) error {
	backupPath := path + backupSuffix
	if _, err := os.Stat(backupPath); err == nil {
		return nil
	}

	if err := os.WriteFile(backupPath, content, 0644); err != nil {
		return fmt.Errorf("cannot write backup %s: %w", backupPath, err)
	}

	return nil
}

func (p *Project) backupTargets() []string {
	return []string{p.CommonGradle, p.TeamCodeGradle}
}

func (p *Project) HasBackups() bool {
	for _, path := range p.backupTargets() {
		if _, err := os.Stat(path + backupSuffix); err == nil {
			return true
		}
	}
	return false
}

func (p *Project) Undo() ([]string, error) {
	var restored []string

	for _, path := range p.backupTargets() {
		backupPath := path + backupSuffix

		content, err := os.ReadFile(backupPath)
		if err != nil {
			continue
		}

		if err := os.WriteFile(path, content, 0644); err != nil {
			return restored, fmt.Errorf("cannot restore %s: %w", path, err)
		}
		if err := os.Remove(backupPath); err != nil {
			return restored, fmt.Errorf("cannot remove backup %s: %w", backupPath, err)
		}

		restored = append(restored, filepath.Base(path))
	}

	if len(restored) == 0 {
		return nil, fmt.Errorf("nothing to undo: no pusher backups found in %s", p.Root)
	}

	return restored, nil
}

func PickABI(deviceABIs, projectABIs []string) (string, error) {
	if len(deviceABIs) == 0 {
		return "", fmt.Errorf("device reported no ABIs")
	}

	if len(projectABIs) == 0 {
		return deviceABIs[0], nil
	}

	available := map[string]bool{}
	for _, abi := range projectABIs {
		available[abi] = true
	}

	for _, abi := range deviceABIs {
		if available[abi] {
			return abi, nil
		}
	}

	return "", fmt.Errorf("device supports %s but the project only packages %s",
		strings.Join(deviceABIs, ", "), strings.Join(projectABIs, ", "))
}
