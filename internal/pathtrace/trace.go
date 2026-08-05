// Package pathtrace reads the JSON path traces written by the blob library and
// turns them into something you can look at: a speed profile over each curve and
// a self-contained HTML visualiser.
package pathtrace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	H float64 `json:"h"`
}

type Segment struct {
	Index            int         `json:"index"`
	Type             string      `json:"type"`
	StartMs          int64       `json:"startMs"`
	EndMs            int64       `json:"endMs"`
	MaxPower         float64     `json:"maxPower"`
	Start            Point       `json:"start"`
	Target           Point       `json:"target"`
	Intercept        *Point      `json:"intercept"`
	HeadingThreshold *float64    `json:"headingThreshold"`
	Waypoints        [][]float64 `json:"waypoints"`
	Curve            [][]float64 `json:"curve"`
	CallSite         []string    `json:"callSite"`

	// Filled in by Annotate and Profile.
	Label      string    `json:"-"`
	Source     string    `json:"-"`
	Length     float64   `json:"-"`
	EstSeconds float64   `json:"-"`
	PeakSpeed  float64   `json:"-"`
	Speeds     []float64 `json:"-"`
}

type Sample struct {
	T        int64   `json:"t"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	H        float64 `json:"h"`
	V        float64 `json:"v"`
	Progress float64 `json:"p"`
	Segment  int     `json:"seg"`
}

type Trace struct {
	Version      int       `json:"version"`
	OpMode       string    `json:"opMode"`
	RecordedAtMs int64     `json:"recordedAtMs"`
	DurationMs   int64     `json:"durationMs"`
	Segments     []Segment `json:"segments"`
	Samples      []Sample  `json:"samples"`
}

// Load reads a trace file.
func Load(path string) (*Trace, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read trace %s: %w", path, err)
	}

	var t Trace
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("trace %s is not valid JSON: %w", filepath.Base(path), err)
	}
	if len(t.Segments) == 0 {
		return nil, fmt.Errorf("trace %s contains no path segments", filepath.Base(path))
	}

	return &t, nil
}

// ActualSeconds is the measured wall-clock duration of a segment. The last
// segment has no end time, so it is charged the remainder of the run.
func (s Segment) ActualSeconds(totalMs int64) float64 {
	end := s.EndMs
	if end < 0 {
		end = totalMs
	}
	if end < s.StartMs {
		return 0
	}
	return float64(end-s.StartMs) / 1000.0
}

var frameRe = regexp.MustCompile(`^(.+)\.([A-Za-z0-9_$]+):(-?\d+)$`)

// Annotate maps each segment back to the source line that committed it, and to
// the enclosing `case LABEL:` when there is one. That is what turns a flat list
// of paths into the flow of the auto, without the OpMode having to cooperate.
func (t *Trace) Annotate(projectRoot string) {
	cache := map[string][]string{}

	for i := range t.Segments {
		seg := &t.Segments[i]

		// Innermost project frame is the line that actually made the call.
		for j := len(seg.CallSite) - 1; j >= 0; j-- {
			m := frameRe.FindStringSubmatch(seg.CallSite[j])
			if m == nil {
				continue
			}
			class, line := m[1], m[3]
			lineNo, err := strconv.Atoi(line)
			if err != nil || lineNo <= 0 {
				continue
			}

			file := findSource(projectRoot, class, cache)
			if file == "" {
				continue
			}

			seg.Source = fmt.Sprintf("%s:%d", filepath.Base(file), lineNo)
			seg.Label = enclosingCase(file, lineNo, cache)
			break
		}

		if seg.Label == "" {
			seg.Label = fmt.Sprintf("segment %d", seg.Index+1)
		}
	}
}

// findSource locates the .java file for a fully qualified class name. Nested
// classes (Outer$Inner) and alliance subclasses both land on the outer file.
func findSource(root, class string, cache map[string][]string) string {
	if root == "" {
		return ""
	}
	if hit, ok := cache["file:"+class]; ok {
		return hit[0]
	}

	simple := class
	if i := strings.LastIndex(simple, "."); i >= 0 {
		simple = simple[i+1:]
	}
	if i := strings.Index(simple, "$"); i >= 0 {
		simple = simple[:i]
	}

	relative := strings.ReplaceAll(class, ".", string(filepath.Separator)) + ".java"
	var found string

	// Build output and version control dwarf the source tree, and walking them
	// costs more than the lookup itself.
	skip := map[string]bool{"build": true, ".gradle": true, ".git": true, ".idea": true}

	filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry == nil {
			return nil
		}
		if entry.IsDir() {
			if skip[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, relative) || entry.Name() == simple+".java" {
			found = path
			return filepath.SkipAll
		}
		return nil
	})

	cache["file:"+class] = []string{found}
	return found
}

var caseRe = regexp.MustCompile(`^\s*case\s+([A-Za-z0-9_.]+)\s*:`)

// enclosingCase scans upward from a line for the `case X:` it sits under. A
// `break` or the switch itself ends the search, so a line after a case block
// does not inherit that block's label.
func enclosingCase(file string, line int, cache map[string][]string) string {
	lines, ok := cache["lines:"+file]
	if !ok {
		data, err := os.ReadFile(file)
		if err != nil {
			cache["lines:"+file] = nil
			return ""
		}
		lines = strings.Split(string(data), "\n")
		cache["lines:"+file] = lines
	}
	if line > len(lines) {
		return ""
	}

	for i := line - 1; i >= 0 && i > line-400; i-- {
		text := lines[i]
		if m := caseRe.FindStringSubmatch(text); m != nil {
			label := m[1]
			if i := strings.LastIndex(label, "."); i >= 0 {
				label = label[i+1:]
			}
			return label
		}
		if strings.Contains(text, "switch") && strings.Contains(text, "(") {
			return ""
		}
	}
	return ""
}
