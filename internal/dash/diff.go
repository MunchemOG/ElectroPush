package dash

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Comparing the robot against the source answers one question: does what you
// tuned exist in your code yet. It cannot answer the other one on its own.
//
// The dashboard's values start as the code's values, because it reflects over
// the class and reads whatever the static fields hold. So "the robot agrees
// with the source" covers both never touched and tuned then written back, and
// telling those apart needs what the robot said last time.

// Change is one tunable that differs, or used to.
type Change struct {
	Key string
	// Code is the value the source declares.
	Code string
	// Live is the value the robot holds.
	Live string
	// Was is what the robot held at the last snapshot.
	Was string
	// File and Line locate the declaration, empty when the source has no such
	// field.
	File string
	Line int
}

// Diff is what a comparison found.
type Diff struct {
	// Unsaved are tuned on the robot and not in the source. These are what a
	// deploy throws away.
	Unsaved []Change
	// Saved were tuned since the last snapshot and now match the source.
	Saved []Change
	// Untouched is how many agree and always did.
	Untouched int
	// Computed is how many the source works out rather than states, which
	// cannot be compared.
	Computed int
	// Unknown are held by the robot with no field in the source to match, which
	// is what a stale build on the robot looks like.
	Unknown []string

	// Snapshot is when the previous reading was taken, zero when there was not
	// one.
	Snapshot time.Time
}

// Any reports whether there is anything worth showing.
func (d Diff) Any() bool { return len(d.Unsaved) > 0 || len(d.Saved) > 0 }

// Compare works out what the robot holds that the source does not.
//
// previous may be nil, in which case nothing can be reported as saved.
func Compare(live Values, code Source, previous Values) Diff {
	var out Diff

	for _, key := range live.Names() {
		value := live[key]

		field, declared := code[key]
		if !declared {
			out.Unknown = append(out.Unknown, key)
			continue
		}

		// The source states how this one is worked out, not what it comes to.
		// Reporting it against the robot's evaluated value would flag it as
		// changed every single run.
		if field.Computed {
			out.Computed++
			continue
		}

		if value != field.Value {
			out.Unsaved = append(out.Unsaved, Change{
				Key: key, Code: field.Value, Live: value,
				Was: previous[key], File: field.File, Line: field.Line,
			})
			continue
		}

		// Agrees now. It only counts as saved if the robot was holding
		// something else when it was last looked at.
		if was, seen := previous[key]; seen && was != value {
			out.Saved = append(out.Saved, Change{
				Key: key, Code: field.Value, Live: value,
				Was: was, File: field.File, Line: field.Line,
			})
			continue
		}

		out.Untouched++
	}

	return out
}

// snapshot is a reading of the robot, kept so the next one can tell what moved.
type snapshot struct {
	Taken  time.Time `json:"taken"`
	Values Values    `json:"values"`
}

// SnapshotPath is where a robot's last reading is kept.
//
// Per serial, because two robots do not hold the same tuning and reporting one
// against the other's history would be nonsense.
func SnapshotPath(dir, serial string) string {
	safe := strings.NewReplacer("/", "_", ":", "_", "\\", "_", ".", "_").Replace(serial)
	if safe == "" {
		safe = "robot"
	}
	return filepath.Join(dir, "dash", safe+".json")
}

// Save records a reading.
func Save(path string, values Values) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	blob, err := json.MarshalIndent(snapshot{Taken: time.Now(), Values: values}, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, blob, 0o644)
}

// Load reads back the last recording, returning nil when there is not one.
func Load(path string) (Values, time.Time) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}
	}

	var saved snapshot
	if json.Unmarshal(blob, &saved) != nil {
		return nil, time.Time{}
	}

	return saved.Values, saved.Taken
}

// Report renders a diff for a terminal.
func (d Diff) Report() string {
	var b strings.Builder

	if len(d.Unsaved) > 0 {
		fmt.Fprintf(&b, "\nChanged on the robot, not in your code (%d)\n", len(d.Unsaved))
		b.WriteString(column(d.Unsaved, true))
		b.WriteString("\n    These go back to the code values on the next deploy.\n")
	}

	if len(d.Saved) > 0 {
		fmt.Fprintf(&b, "\nChanged since the last check, and already in your code (%d)\n", len(d.Saved))
		b.WriteString(column(d.Saved, false))
	}

	if !d.Any() {
		if d.Untouched > 0 {
			fmt.Fprintf(&b, "\nThe robot matches your code. %d tunables checked.\n", d.Untouched)
		} else {
			b.WriteString("\nThe dashboard is holding nothing that your code declares.\n")
		}
	} else if d.Untouched > 0 {
		fmt.Fprintf(&b, "\n%d others match your code.\n", d.Untouched)
	}

	if d.Computed > 0 {
		fmt.Fprintf(&b, "\n%d are worked out in code rather than written down, so they are not compared.\n",
			d.Computed)
	}

	if len(d.Unknown) > 0 {
		fmt.Fprintf(&b, "\n%d on the robot have no field in your source: %s\n",
			len(d.Unknown), strings.Join(short(d.Unknown), ", "))
		b.WriteString("    The robot is probably running an older build.\n")
	}

	return b.String()
}

// column lays the changes out with the values lined up.
func column(changes []Change, arrow bool) string {
	sorted := append([]Change(nil), changes...)
	sort.Slice(sorted, func(a, b int) bool { return sorted[a].Key < sorted[b].Key })

	width := 0
	for _, c := range sorted {
		if len(c.Key) > width {
			width = len(c.Key)
		}
	}

	var b strings.Builder
	for _, c := range sorted {
		if arrow {
			fmt.Fprintf(&b, "  %-*s   %s  ->  %s\n", width, c.Key, c.Code, c.Live)
			continue
		}
		fmt.Fprintf(&b, "  %-*s   %s  (was %s)\n", width, c.Key, c.Live, c.Was)
	}
	return b.String()
}

// short keeps a list from taking over the screen.
func short(names []string) []string {
	if len(names) <= 6 {
		return names
	}
	return append(append([]string(nil), names[:6]...),
		fmt.Sprintf("and %d more", len(names)-6))
}
