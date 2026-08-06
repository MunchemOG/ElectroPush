package hotreload

import (
	"fmt"
	"os"
	"strings"

	"github.com/andreibanu/pusher/internal/adb"
)

// Diagnosis is what the robot says about its own ability to load classes this
// way. Pushing the files is the easy half; this is the half that explains a
// silent failure.
type Diagnosis struct {
	Package   string
	OutputDir string
	// Pointer is what the pointer file actually contains, read back.
	Pointer  string
	OnHub    []string
	Findings []string
	Crash    string
	// Exception is the one line worth reading: the type and message. The
	// frames under it only say the event loop called it, which is already
	// known.
	Exception string
	// LogPath is the whole log, unfiltered, written where it can be read
	// without a terminal scrolling it away.
	LogPath string
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

// CaptureLog saves the whole log and picks out the one line worth reading.
//
// The whole trace goes to a file rather than the screen. A menu that does not
// scroll turns thirty lines of stack frames into "the bottom of a stack trace",
// which is the half that says the event loop called it.
func CaptureLog(serial string) (exception, path string) {
	out, err := adb.Shell(serial, "logcat", "-d", "-v", "brief", "-t", "800", "2>/dev/null")
	if err != nil {
		return "", ""
	}

	path = saveLog(out)

	return firstException(strings.Split(out, "\n")), path
}

// isFrame reports whether a log line is a stack frame rather than the message
// above it.
func isFrame(line string) bool {
	rest := line
	if i := strings.Index(line, "): "); i >= 0 {
		rest = line[i+3:]
	}
	return strings.HasPrefix(strings.TrimSpace(rest), "at ") ||
		strings.HasPrefix(strings.TrimSpace(rest), "... ")
}

// firstException finds the exception a trace leads with.
func firstException(lines []string) string {
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" || isFrame(line) {
			continue
		}
		if !strings.Contains(line, "E/") && !strings.Contains(line, "W/") {
			continue
		}

		// A message with an exception in it is what is being looked for; a
		// plain error line without one is not the trace header.
		if strings.Contains(line, "Exception") || strings.Contains(line, "Error") ||
			strings.Contains(line, "Caused by") {
			if i := strings.Index(line, "): "); i >= 0 {
				return strings.TrimSpace(line[i+3:])
			}
			return line
		}
	}
	return ""
}

func saveLog(out string) string {
	file, err := os.CreateTemp("", "pusher-reload-log-*.txt")
	if err != nil {
		return ""
	}
	defer file.Close()

	if _, err := file.WriteString(out); err != nil {
		return ""
	}
	return file.Name()
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

	// Read the pointer back rather than trusting the write. The SDK logs
	// "getCurrentOutputDir() unavailable" and carries on when this is wrong,
	// so a bad write looks exactly like everything working.
	d.Pointer = readPointer(serial)

	dir := currentOutputDir(serial)
	if dir == "" {
		if d.Pointer == "" {
			d.find("%s is empty or missing, so the SDK has nowhere to read classes from", PointerFile)
		} else {
			d.find("%s says %q, which is not a directory on the hub", PointerFile, d.Pointer)
		}
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

// readPointer returns the pointer file's contents exactly as they are on disk.
func readPointer(serial string) string {
	out, err := adb.Shell(serial, "cat", PointerFile, "2>/dev/null")
	if err != nil {
		return ""
	}
	return strings.TrimRight(out, "\r\n")
}
