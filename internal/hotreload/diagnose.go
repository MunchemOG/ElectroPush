package hotreload

import (
	"fmt"
	"strings"

	"github.com/andreibanu/pusher/internal/adb"
)

// Diagnosis is what the robot says about its own ability to load classes this
// way. Pushing the files is the easy half; this is the half that explains a
// silent failure.
type Diagnosis struct {
	Package   string
	OutputDir string
	OnHub     []string
	Findings  []string
	Crash     string
	// Log is what the app said while it was reloading. Guessing at why a
	// re-registration produced nothing is a good way to be wrong twice; the
	// app writes the reason down.
	Log []string
}

// OK reports whether anything looked wrong.
func (d Diagnosis) OK() bool { return len(d.Findings) == 0 && d.Crash == "" }

// ClearLog drops what is already in the log, so what is captured afterwards is
// only the reload.
func ClearLog(serial string) {
	_, _ = adb.Shell(serial, "logcat", "-c", "2>/dev/null")
}

// interesting are the tags and messages that explain a reload finding nothing.
var interesting = []string{
	"pusherproof", ClassName,
	"OnBotJava", "ClassManager", "RegisteredOpModes", "OpModeMeta",
	"NoClassDefFound", "ClassNotFound", "UnsupportedClassVersion",
	"rejecting", "Rejecting", "dex", "Dex",
}

// CaptureLog returns the lines from the app that bear on the reload.
//
// The head, not the tail. A stack trace puts the exception type and message on
// its first line and the call chain under it, so keeping the last N lines keeps
// only the part that says where it was called from and throws away the part
// that says what went wrong. The log is cleared before the attempt, so the
// first matching lines are the right ones.
func CaptureLog(serial string) []string {
	out, err := adb.Shell(serial, "logcat", "-d", "-v", "brief", "-t", "600", "2>/dev/null")
	if err != nil {
		return nil
	}

	var kept []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" {
			continue
		}
		for _, want := range interesting {
			if strings.Contains(line, want) {
				kept = append(kept, line)
				break
			}
		}
	}

	return headOfError(kept)
}

// headOfError starts the capture at the first line that is not a stack frame,
// so the exception and its message lead rather than being trimmed away.
func headOfError(lines []string) []string {
	for i, line := range lines {
		if !strings.Contains(line, "E/") {
			continue
		}
		if trimmed := strings.TrimSpace(line); strings.Contains(trimmed, ") at ") ||
			strings.Contains(line, ":     at ") || strings.Contains(line, ": \tat ") {
			continue
		}
		lines = lines[i:]
		break
	}

	if len(lines) > 30 {
		lines = lines[:30]
	}
	return lines
}

func (d *Diagnosis) find(format string, args ...any) {
	d.Findings = append(d.Findings, fmt.Sprintf(format, args...))
}

// Diagnose looks for the reasons a pushed OpMode would not appear.
//
// The failure mode this exists for is silence: the files land, nothing reads
// them, and the Driver Station shows nothing with no error anywhere obvious.
func Diagnose(serial string) Diagnosis {
	var d Diagnosis

	d.Package = robotControllerPackage(serial)
	if d.Package == "" {
		d.find("no robot controller app found on this device")
		return d
	}

	dir := currentOutputDir(serial)
	if dir == "" {
		d.find("%s names no directory, so the SDK has nowhere to read classes from", PointerFile)
	}
	d.OutputDir = dir

	if out, err := adb.Shell(serial, "ls", "-l", shellQuote(dir), "2>/dev/null"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			if line = strings.TrimSpace(strings.TrimRight(line, "\r")); line != "" {
				d.OnHub = append(d.OnHub, line)
			}
		}
	}

	if !hasFile(serial, dir+"/"+ProofName+".jar") {
		d.find("the jar is not on the hub; the class name cannot be discovered without it")
	}
	if !hasFile(serial, dir+"/"+ProofName+".dex") {
		d.find("the dex is not on the hub; the class cannot be loaded without it")
	}
	if !hasFile(serial, TriggerFile) {
		d.find("%s does not exist, so nothing tells the app to rescan", TriggerFile)
	}

	// OnBotJava is what owns the classloader this relies on. If its service is
	// dying the helper may never be installed, and then nothing pushed here is
	// ever looked at.
	if crash := onBotJavaCrash(serial); crash != "" {
		d.Crash = crash
		d.find("the OnBotJava service is crashing, so the class scanning it owns may never run")
	}

	return d
}

func hasFile(serial, path string) bool {
	_, err := adb.Shell(serial, "ls", shellQuote(path), "2>/dev/null")
	return err == nil
}

func shellQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}

// robotControllerPackage finds the installed robot controller.
func robotControllerPackage(serial string) string {
	out, err := adb.Shell(serial, "pm", "list", "packages", "2>/dev/null")
	if err != nil {
		return ""
	}

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		name := strings.TrimPrefix(line, "package:")
		if strings.Contains(name, "ftcrobotcontroller") {
			return name
		}
	}
	return ""
}

// onBotJavaCrash pulls the reason OnBotJava died, if it did.
//
// The ActivityManager line only says something crashed. The exception above it
// is the part worth reading, so both are searched.
func onBotJavaCrash(serial string) string {
	out, err := adb.Shell(serial, "logcat", "-d", "-t", "600", "2>/dev/null")
	if err != nil {
		return ""
	}

	lines := strings.Split(out, "\n")

	crashing := false
	for _, line := range lines {
		if strings.Contains(line, "OnBotJavaService") && strings.Contains(line, "crashed") {
			crashing = true
		}
	}
	if !crashing {
		return ""
	}

	// Walk back from the end for the most recent exception, which is what the
	// service actually died of.
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if !strings.Contains(line, "AndroidRuntime") && !strings.Contains(line, "FATAL") {
			continue
		}

		end := i + 12
		if end > len(lines) {
			end = len(lines)
		}

		var trace []string
		for _, l := range lines[i:end] {
			if l = strings.TrimSpace(strings.TrimRight(l, "\r")); l != "" {
				trace = append(trace, l)
			}
		}
		return strings.Join(trace, "\n")
	}

	return "the service is restarting repeatedly, but no exception is in the recent log"
}
