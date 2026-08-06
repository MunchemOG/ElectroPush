package extreme

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/andreibanu/pusher/internal/adb"
	"github.com/andreibanu/pusher/internal/hotreload"
)

// Name is what the team's files are called on the hub.
const Name = "pusher-extreme-teamcode"

// State is whether a reload can stand in for an install.
type State struct {
	// Excluded is whether the project keeps team code out of the APK. Without
	// it the APK's copy wins and a reload changes nothing.
	Excluded bool
	// APKMatches is whether the robot is running the build this project
	// produces. If it is not, something other than team code changed and the
	// APK has to be installed before a reload means anything.
	APKMatches bool
	// Reason explains a no, for showing rather than guessing.
	Reason string
}

// Usable reports whether the reload sequence can replace the install.
func (s State) Usable() bool { return s.Excluded && s.APKMatches }

// Status works out whether a reload would actually take effect.
//
// Deliberately conservative. Reloading when the robot needed an install leaves
// it running stale code with everything appearing to have worked, which is the
// worst thing this can do and the hardest to notice at a competition.
func Status(root, serial, apkPath string) State {
	var s State

	s.Excluded = Excluded(root)
	if !s.Excluded {
		s.Reason = "team code is still packaged in the APK, so a reload would be ignored"
		return s
	}

	if serial == "" {
		s.Reason = "no robot connected"
		return s
	}

	if apkPath == "" {
		s.Reason = "no APK has been built yet"
		return s
	}

	fingerprint, err := adb.APKFingerprint(apkPath)
	if err != nil {
		s.Reason = "cannot read the APK"
		return s
	}

	installed := adb.InstalledFingerprint(serial)
	switch {
	case installed == "":
		s.Reason = "the robot has not been deployed to by pusher yet"
	case installed != fingerprint:
		s.Reason = "the APK changed, so it has to be installed before a reload counts"
	default:
		s.APKMatches = true
	}

	return s
}

// Result is what a reload did.
type Result struct {
	Sources  int
	Classes  int
	Bytes    int64
	Compile  time.Duration
	Push     time.Duration
	Total    time.Duration
	Steps    []string
	Warnings []string
}

// Reload compiles the team's code and puts it on the robot, without installing
// anything.
func Reload(p *Project, serial string, cp Classpath, keep []string) (*Result, error) {
	out := &Result{}
	started := time.Now()

	work, err := os.MkdirTemp("", "pusher-extreme-*")
	if err != nil {
		return out, err
	}
	defer os.RemoveAll(work)

	start := time.Now()
	build, err := Compile(p, cp, work, keep)
	if err != nil {
		return out, err
	}
	out.Compile = time.Since(start)
	out.Sources, out.Classes = build.Sources, build.Classes
	kept := ""
	if build.Kept > 0 {
		kept = fmt.Sprintf(", %d kept in the APK", build.Kept)
	}
	if build.Bridged > 0 {
		kept += fmt.Sprintf(", %d @Config classes bridged to FtcDashboard", build.Bridged)
	}
	out.Steps = append(out.Steps,
		fmt.Sprintf("compiled %d sources into %d reloadable classes%s in %s",
			build.Sources, build.Classes, kept, out.Compile.Round(time.Millisecond)))

	if err := checkOpModes(build.Jar); err != nil {
		out.Warnings = append(out.Warnings, err.Error())
	}

	marker := time.Now().Format("150405")

	delivery, err := hotreload.Deliver(serial, Name, build.Jar, build.Dex, marker)
	out.Steps = append(out.Steps, delivery.Steps...)
	if err != nil {
		return out, err
	}

	out.Push, out.Bytes = delivery.Push, delivery.Bytes
	out.Total = time.Since(started)

	if delivery.ColdStart {
		out.Warnings = append(out.Warnings,
			"first reload on this robot: restart it once so it starts watching, then reloads are live")
	}

	return out, nil
}

// checkOpModes warns when nothing that got compiled looks like an OpMode.
//
// A jar with no OpMode in it reloads perfectly and changes nothing visible,
// which reads as the reload having failed.
func checkOpModes(jarPath string) error {
	names, err := classNames(jarPath)
	if err != nil {
		return nil
	}

	for _, name := range names {
		if strings.Contains(name, "OpMode") || strings.Contains(name, "Auto") ||
			strings.Contains(name, "TeleOp") {
			return nil
		}
	}

	return fmt.Errorf("none of the %d classes look like an OpMode, so nothing may appear on the Driver Station", len(names))
}
