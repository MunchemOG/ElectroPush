package adb

import (
	"fmt"
	"sort"
	"strings"
)

// TraceDir is where the blob library writes path traces on the hub.
const TraceDir = "/sdcard/FIRST/pusher-traces"

// RemoteTrace is one trace file sitting on the robot.
type RemoteTrace struct {
	Path   string
	Name   string
	OpMode string
}

// Shell runs an adb shell command and returns its combined output.
func Shell(serial string, args ...string) (string, error) {
	return run(serial, append([]string{"shell"}, args...)...)
}

// Pull copies a file off the device.
func Pull(serial, remote, local string) error {
	_, err := run(serial, "pull", remote, local)
	return err
}

// ListTraces returns the trace files on the device, newest first.
//
// ls -t orders by mtime, which is what "newest" has to mean here: the filename
// carries the wall clock of a hub whose clock is frequently wrong.
func ListTraces(serial string) ([]RemoteTrace, error) {
	out, err := Shell(serial, "ls", "-t", TraceDir, "2>/dev/null")
	if err != nil {
		return nil, fmt.Errorf("cannot list %s on the robot: %w", TraceDir, err)
	}

	var traces []RemoteTrace
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if !strings.HasSuffix(name, ".json") {
			continue
		}

		traces = append(traces, RemoteTrace{
			Path:   TraceDir + "/" + name,
			Name:   name,
			OpMode: opModeFromName(name),
		})
	}

	return traces, nil
}

// Files are named <OpMode>-<millis>.json, so the OpMode is everything before the
// final dash. Splitting on the last dash keeps names that contain dashes intact.
func opModeFromName(name string) string {
	base := strings.TrimSuffix(name, ".json")
	if i := strings.LastIndex(base, "-"); i > 0 {
		return base[:i]
	}
	return base
}

// MatchTraces returns the traces whose OpMode matches name, case-insensitively.
// An empty name matches everything.
func MatchTraces(traces []RemoteTrace, name string) []RemoteTrace {
	if name == "" {
		return traces
	}

	want := strings.ToLower(name)
	var hits []RemoteTrace
	for _, t := range traces {
		if strings.ToLower(t.OpMode) == want {
			hits = append(hits, t)
		}
	}
	if len(hits) > 0 {
		return hits
	}

	// Fall back to a substring match so a partial class name still finds something.
	for _, t := range traces {
		if strings.Contains(strings.ToLower(t.OpMode), want) {
			hits = append(hits, t)
		}
	}
	return hits
}

// OpModeNames lists the distinct OpModes present, for error messages.
func OpModeNames(traces []RemoteTrace) []string {
	seen := map[string]bool{}
	var names []string
	for _, t := range traces {
		if !seen[t.OpMode] {
			seen[t.OpMode] = true
			names = append(names, t.OpMode)
		}
	}
	sort.Strings(names)
	return names
}
