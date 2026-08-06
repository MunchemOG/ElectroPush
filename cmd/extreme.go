package cmd

import (
	"fmt"
	"time"

	"github.com/andreibanu/pusher/internal/config"
	"github.com/andreibanu/pusher/internal/extreme"
	"github.com/andreibanu/pusher/internal/gradle"
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
		fmt.Printf("\n[*] Pusher Extreme is on, but installing this time: %s\n", state.Reason)
		return false, nil
	}

	fmt.Println("\n[>] Pusher Extreme: reloading team code, not installing")

	classpath, err := extreme.ResolveClasspath(project.Wrapper, extreme.Module)
	if err != nil {
		fmt.Printf("[!] Could not work out what to compile against: %v\n", err)
		fmt.Println("[*] Installing instead.")
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
		fmt.Printf("\n[!] Reload failed: %v\n", err)
		fmt.Println("[*] Falling back to a full install.")
		return false, nil
	}

	for _, warning := range result.Warnings {
		fmt.Printf("[!] %s\n", warning)
	}

	fmt.Printf("\n[OK] Reloaded %d classes in %.1fs, without installing\n",
		result.Classes, result.Total.Seconds())

	return true, nil
}

// extremeDeploy is the deploy path when Pusher Extreme is set up.
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

	fmt.Printf("[OK] Deployed in %.1fs\n", time.Since(start).Seconds())
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
