package cmd

import (
	"fmt"
	"time"

	"github.com/MunchemOG/ElectroPush/internal/config"
	"github.com/MunchemOG/ElectroPush/internal/extreme"
	"github.com/MunchemOG/ElectroPush/internal/gradle"
)

// tryExtreme replaces the install with a reload when that is genuinely
// equivalent, and says why when it is not.
//
// The dangerous outcome here is not failing. It is reloading when the robot
// needed an install: everything reports success and the robot runs stale code,
// which at a competition is discovered by the robot doing last week's
// autonomous. So every doubt falls back to installing.
func tryExtreme(gradlePath, serial, apkPath string) (bool, error) {
	if !config.GetExtreme() {
		return false, nil
	}

	project, err := extreme.FindProject()
	if err != nil {
		return false, nil
	}

	state := extreme.Status(project.Root, serial, apkPath)
	if !state.Usable() {
		uiStatus("wait", "Epsh Extreme will install this time · "+state.Reason)
		return false, nil
	}

	uiStatus("run", "Epsh Extreme · reloading team code")

	classpath, err := extreme.ResolveClasspath(project.Wrapper, extreme.Module)
	if err != nil {
		uiStatus("warn", fmt.Sprintf("Could not resolve compile classpath · %v", err))
		uiStatus("wait", "Installing instead")
		return false, nil
	}

	result, err := extreme.Reload(project, serial, classpath, extreme.Kept(project.Root))
	for _, step := range result.Steps {
		fmt.Printf("    %s\n", step)
	}
	if err != nil {
		// A failed reload leaves the robot with whatever it had, which may now
		// be a directory the SDK cannot read. Installing puts it back to a
		// state that certainly works.
		uiStatus("warn", fmt.Sprintf("Reload failed · %v", err))
		uiStatus("wait", "Falling back to a full install")
		return false, nil
	}

	for _, warning := range result.Warnings {
		fmt.Printf("[!] %s\n", warning)
	}

	uiStatus("ok", fmt.Sprintf("Reloaded %d classes in %.1fs · no install needed",
		result.Classes, result.Total.Seconds()))

	return true, nil
}

// recordExtremeState notes what the robot now holds, after an install that
// went through the ordinary path. Without it the next deploy cannot tell
// whether anything outside team code changed and installs again.
func recordExtremeState(serial string) {
	project, err := extreme.FindProject()
	if err != nil {
		return
	}

	// Off and on again regenerates an identical block, so an install that
	// packaged team code has to take the signature away rather than let the
	// robot keep agreeing with it. It would otherwise reload classes the APK
	// already has, and the SDK then registers no OpModes at all.
	if !config.GetExtreme() || !extreme.Excluded(project.Root) {
		extreme.ForgetSignature(serial)
		return
	}

	if signature, err := extreme.Signature(project.Root); err == nil {
		extreme.RecordSignature(serial, signature)
	}
}

// reloadAfterInstall puts team code onto the robot once an APK that no longer
// carries it has been installed.
//
// An install on its own leaves the robot with no OpModes at all: they are
// excluded from the APK, and the reload that supplies them has not happened.
// Reporting a successful deploy in that state is how somebody gets to a match
// with an empty OpMode list.
func reloadAfterInstall(serial string) error {
	if !config.GetExtreme() {
		return nil
	}

	project, err := extreme.FindProject()
	if err != nil || !extreme.Excluded(project.Root) {
		return nil
	}

	stranded := func(err error) error {
		return fmt.Errorf("the APK is installed but carries no team code, so the robot "+
			"has no OpModes: %w\n"+
			"    Run `epsh` again, or undo the setup in `epsh settings`", err)
	}

	uiStatus("run", "Epsh Extreme · reloading team code into the new APK")

	classpath, err := extreme.ResolveClasspath(project.Wrapper, extreme.Module)
	if err != nil {
		return stranded(err)
	}

	result, err := extreme.Reload(project, serial, classpath, extreme.Kept(project.Root))
	for _, step := range result.Steps {
		fmt.Printf("    %s\n", step)
	}
	if err != nil {
		return stranded(err)
	}

	for _, warning := range result.Warnings {
		fmt.Printf("[!] %s\n", warning)
	}

	uiStatus("ok", fmt.Sprintf("Reloaded %d classes · OpModes are ready", result.Classes))
	return nil
}

// extremeDeploy is the deploy path when Epsh Extreme is set up.
//
// The APK is still built, because it is the only way to know whether anything
// outside team code changed, and with team code excluded that build has almost
// nothing to do.
func extremeDeploy(gradlePath, serial string) (bool, error) {
	apkPath, err := gradle.FindApk(gradle.ProjectDir(gradlePath))
	if err != nil {
		return false, nil
	}

	start := time.Now()

	done, err := tryExtreme(gradlePath, serial, apkPath)
	if err != nil || !done {
		return done, err
	}

	uiStatus("ok", fmt.Sprintf("Deployed in %.1fs", time.Since(start).Seconds()))
	return true, nil
}

// extremeReady reports whether the robot is set up for reloading, for the
// status line.
func extremeReady(serial string) (extreme.State, bool) {
	project, err := extreme.FindProject()
	if err != nil {
		return extreme.State{}, false
	}

	apkPath, _ := gradle.FindApk(project.Root)

	return extreme.Status(project.Root, serial, apkPath), true
}
