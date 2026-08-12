package adb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Session commands get a deadline. Streaming an APK through the device's shell
// can wedge outright if the hub drops off mid-write, and without a deadline the
// deploy hangs with no output rather than falling back to the staged install.
const (
	sessionTimeout = 2 * time.Minute
	writeTimeout   = 10 * time.Minute
)

const installedMarker = remoteRoot + "/installed"

// InstallPlan is what an install did, for the caller to report.
type InstallPlan struct {
	Skipped   bool
	Streamed  bool
	Delta     bool
	Splits    int
	Reason    string
	BytesSent int64
}

// APKFingerprint identifies an APK by content.
func APKFingerprint(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(sum.Sum(nil)), nil
}

// The package stamp is checked too, or an install from Android Studio in
// between leaves the marker claiming epsh's build is present.
func alreadyInstalled(serial, fingerprint, pkg string) bool {
	out, err := Shell(serial, "cat", installedMarker, "2>/dev/null")
	if err != nil {
		return false
	}

	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) != 2 || fields[0] != fingerprint {
		return false
	}

	return fields[1] == packageStamp(serial, pkg)
}

func packageStamp(serial, pkg string) string {
	if pkg == "" {
		return ""
	}

	out, err := Shell(serial, "dumpsys", "package", pkg, "2>/dev/null",
		"|", "grep", "-m1", "lastUpdateTime")
	if err != nil {
		return ""
	}

	return strings.TrimSpace(strings.ReplaceAll(out, " ", ""))
}

func recordInstalled(serial, fingerprint, pkg string) {
	_, _ = Shell(serial, "mkdir", "-p", remoteRoot)
	_, _ = Shell(serial, "sh", "-c",
		fmt.Sprintf("'echo %s %s > %s'", fingerprint, packageStamp(serial, pkg), installedMarker))
}

func forgetInstalled(serial string) {
	_, _ = Shell(serial, "rm", "-f", installedMarker)
}

// InstalledFingerprint is the APK the robot was last given by epsh, empty
// when it cannot tell.
func InstalledFingerprint(serial string) string {
	out, err := Shell(serial, "cat", installedMarker, "2>/dev/null")
	if err != nil {
		return ""
	}

	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// ForgetInstalled makes the next deploy install unconditionally.
func ForgetInstalled(serial string) { forgetInstalled(serial) }

// PackageName reads the application id out of an APK, empty if it cannot.
func PackageName(apkPath string) string {
	for _, tool := range aaptCandidates() {
		out, err := exec.Command(tool, "dump", "badging", apkPath).Output()
		if err != nil {
			continue
		}

		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "package:") {
				if name := quoted(line, "name="); name != "" {
					return name
				}
			}
		}
	}

	return ""
}

func aaptCandidates() []string {
	tools := []string{"aapt", "aapt2"}

	roots := []string{os.Getenv("ANDROID_HOME"), os.Getenv("ANDROID_SDK_ROOT")}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots,
			home+"/Library/Android/sdk",
			home+"/Android/Sdk",
			home+"/AppData/Local/Android/Sdk")
	}

	for _, root := range roots {
		if root == "" {
			continue
		}

		entries, err := os.ReadDir(root + "/build-tools")
		if err != nil {
			continue
		}

		for i := len(entries) - 1; i >= 0; i-- {
			tools = append(tools, root+"/build-tools/"+entries[i].Name()+"/aapt")
		}
	}

	return tools
}

func quoted(line, key string) string {
	i := strings.Index(line, key)
	if i < 0 {
		return ""
	}
	rest := line[i+len(key):]
	if len(rest) == 0 || rest[0] != '\'' {
		return ""
	}
	if end := strings.IndexByte(rest[1:], '\''); end >= 0 {
		return rest[1 : 1+end]
	}
	return ""
}

// StreamInstall writes the APK straight into a package-manager session.
func StreamInstall(serial, apkPath string) error { return streamFrom(serial, apkPath, "") }

// streamFrom installs through a session. When remote is set the APK is already
// on the device, so the session reads it there instead of it being sent again.
func streamFrom(serial, apkPath, remote string) error {
	info, err := os.Stat(apkPath)
	if err != nil {
		return fmt.Errorf("cannot read APK: %w", err)
	}
	size := info.Size()

	out, err := runTimeout(serial, sessionTimeout, "shell", "pm", "install-create",
		"-r", "-d", "-g", "-t", "-S", strconv.FormatInt(size, 10))
	if err != nil {
		return fmt.Errorf("cannot open an install session: %w", err)
	}

	session := sessionID(out)
	if session == "" {
		return fmt.Errorf("the device did not return a session id: %s", strings.TrimSpace(out))
	}

	if err := writeSession(serial, session, "base", apkPath, remote, size); err != nil {
		_, _ = runTimeout(serial, sessionTimeout, "shell", "pm", "install-abandon", session)
		return err
	}

	commit, err := runTimeout(serial, sessionTimeout, "shell", "pm", "install-commit", session)
	if err != nil {
		return fmt.Errorf("install-commit failed: %w", err)
	}
	if !strings.Contains(strings.ToLower(commit), "success") {
		return fmt.Errorf("install-commit did not succeed: %s", strings.TrimSpace(commit))
	}

	return nil
}

func writeSession(serial, session, name, path, remote string, size int64) error {
	source := "-"
	if remote != "" {
		source = remote
	}

	args := []string{"shell", "pm", "install-write", "-S",
		strconv.FormatInt(size, 10), session, name, source}
	if serial != "" {
		args = append([]string{"-s", serial}, args...)
	}

	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "adb", args...)

	// Only send the file when the device does not already have it.
	if remote == "" {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		cmd.Stdin = file
	}

	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("the device stopped accepting the APK after %s", writeTimeout)
	}
	if err != nil {
		return fmt.Errorf("install-write failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	if !strings.Contains(strings.ToLower(string(out)), "success") {
		return fmt.Errorf("install-write did not confirm: %s", strings.TrimSpace(string(out)))
	}

	return nil
}

func sessionID(out string) string {
	open := strings.LastIndex(out, "[")
	closing := strings.LastIndex(out, "]")
	if open < 0 || closing < open {
		return ""
	}

	id := strings.TrimSpace(out[open+1 : closing])
	if _, err := strconv.Atoi(id); err != nil {
		return ""
	}
	return id
}

// SplitInstall installs only the splits whose contents changed.
func SplitInstall(serial, pkg string, apks []string) (int, error) {
	if pkg == "" {
		return 0, fmt.Errorf("cannot install splits without knowing the package")
	}
	if len(apks) == 0 {
		return 0, fmt.Errorf("no APKs to install")
	}

	changed, err := changedSplits(serial, apks)
	if err != nil {
		return 0, err
	}
	if len(changed) == 0 {
		return 0, nil
	}

	out, err := runTimeout(serial, sessionTimeout, "shell", "pm", "install-create",
		"-r", "-d", "-g", "-t", "-p", pkg)
	if err != nil {
		return 0, fmt.Errorf("cannot open a split session: %w", err)
	}

	session := sessionID(out)
	if session == "" {
		return 0, fmt.Errorf("the device did not return a session id: %s", strings.TrimSpace(out))
	}

	for _, i := range changed {
		apk := apks[i]

		info, err := os.Stat(apk)
		if err != nil {
			_, _ = run(serial, "shell", "pm", "install-abandon", session)
			return 0, err
		}

		if err := writeSession(serial, session, splitName(apks, i), apk, "", info.Size()); err != nil {
			_, _ = runTimeout(serial, sessionTimeout, "shell", "pm", "install-abandon", session)
			return 0, err
		}
	}

	commit, err := runTimeout(serial, sessionTimeout, "shell", "pm", "install-commit", session)
	if err != nil {
		return 0, fmt.Errorf("install-commit failed: %w", err)
	}
	if !strings.Contains(strings.ToLower(commit), "success") {
		return 0, fmt.Errorf("install-commit did not succeed: %s", strings.TrimSpace(commit))
	}

	return len(changed), nil
}

func changedSplits(serial string, apks []string) ([]int, error) {
	recorded := splitFingerprints(serial)

	var changed []int
	for i, apk := range apks {
		sum, err := APKFingerprint(apk)
		if err != nil {
			return nil, err
		}
		if recorded[splitName(apks, i)] != sum {
			changed = append(changed, i)
		}
	}

	return changed, nil
}

func splitFingerprints(serial string) map[string]string {
	out, err := Shell(serial, "cat", remoteRoot+"/splits", "2>/dev/null")
	if err != nil {
		return nil
	}

	sums := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 2 {
			sums[fields[0]] = fields[1]
		}
	}
	return sums
}

func recordSplits(serial string, apks []string) {
	_, _ = Shell(serial, "mkdir", "-p", remoteRoot)
	_, _ = Shell(serial, "rm", "-f", remoteRoot+"/splits")

	for i, apk := range apks {
		sum, err := APKFingerprint(apk)
		if err != nil {
			return
		}
		_, _ = Shell(serial, "sh", "-c", fmt.Sprintf("'echo %s %s >> %s'",
			splitName(apks, i), sum, remoteRoot+"/splits"))
	}
}

// Position decides, not the filename: "TeamCode-debug.apk" is the base.
func splitName(apks []string, i int) string {
	if i == 0 {
		return "base"
	}
	return strings.TrimSuffix(fileBase(apks[i]), ".apk")
}

func fileBase(path string) string {
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		return path[i+1:]
	}
	return path
}

// runTimeout is run with a deadline, so a wedged device fails instead of
// hanging the deploy.
func runTimeout(serial string, limit time.Duration, args ...string) (string, error) {
	full := args
	if serial != "" {
		full = append([]string{"-s", serial}, args...)
	}

	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()

	out, err := exec.CommandContext(ctx, "adb", full...).CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("adb %s did not answer within %s", strings.Join(args, " "), limit)
	}
	if err != nil {
		return "", fmt.Errorf("adb %s failed: %w (output: %s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}

	return string(out), nil
}
