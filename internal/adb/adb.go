package adb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

// The robot's fixed address on its own network.
const (
	RobotIP   = "192.168.43.1"
	RobotPort = "5555"

	remoteAPKPath = "/data/local/tmp/epsh_app.apk"
)

// ErrNoRobot means nothing is attached: no hub over USB, and no adb connection
// over Wi-Fi. Callers that can do something about it, like offering to join the
// robot's network, test for this rather than reading the message.
var ErrNoRobot = errors.New("no robot connected")

// ErrNoADB means the platform tools are missing, which is the one reason for
// having no robot that connecting cannot fix.
var ErrNoADB = errors.New("adb not found")

// Transport is how a device is attached.
type Transport string

// How a device is attached.
const (
	TransportUSB Transport = "usb"

	TransportTCP Transport = "tcp"
)

// Device is one attached device as adb reports it.
type Device struct {
	Serial    string
	State     string
	Model     string
	Transport Transport
}

// IsOnline reports whether the device is ready for commands.
func (d Device) IsOnline() bool {
	return d.State == "device"
}

// Label is the device's model and serial, for showing a person.
func (d Device) Label() string {
	if d.Model != "" {
		return fmt.Sprintf("%s (%s)", d.Model, d.Serial)
	}
	return d.Serial
}

// robotAddr is where the robot answers adb.
//
// The hub's own access point puts it at a fixed address, and for most of
// epsh's life that was the only place it could be. A robot that has joined a
// network you were already on is somewhere else entirely, so this is where
// epsh is talking to it this run rather than a constant.
var robotAddr = fmt.Sprintf("%s:%s", RobotIP, RobotPort)

// RobotAddr is the robot's adb address over Wi-Fi.
func RobotAddr() string { return robotAddr }

// UseAddress points epsh at a robot somewhere other than its own access
// point, and reports whether the address was usable.
//
// Set once, from what discovery found or from what was remembered, rather than
// threaded through every call: which robot epsh is talking to is one fact for
// the whole run.
func UseAddress(addr string) bool {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return false
	}
	if !strings.Contains(addr, ":") {
		addr += ":" + RobotPort
	}

	if _, _, err := net.SplitHostPort(addr); err != nil {
		return false
	}

	robotAddr = addr
	return true
}

// UseOwnAccessPoint puts the address back to the hub's own, which is where it
// is unless something has said otherwise.
func UseOwnAccessPoint() {
	robotAddr = fmt.Sprintf("%s:%s", RobotIP, RobotPort)
}

// IsInstalled reports whether adb is on the path.
func IsInstalled() bool {
	_, err := exec.LookPath("adb")
	return err == nil
}

// Devices lists what adb can currently see.
func Devices() ([]Device, error) {
	if !IsInstalled() {
		return nil, fmt.Errorf("adb not found - please install Android SDK Platform-Tools")
	}

	out, err := exec.Command("adb", "devices", "-l").Output()
	if err != nil {
		return nil, fmt.Errorf("adb devices failed: %w", err)
	}

	return parseDevices(string(out)), nil
}

func parseDevices(output string) []Device {
	var devices []Device

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)

		if line == "" || strings.HasPrefix(line, "List of devices") || strings.HasPrefix(line, "*") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		dev := Device{
			Serial:    fields[0],
			State:     fields[1],
			Transport: TransportUSB,
		}

		if strings.Contains(dev.Serial, ":") {
			dev.Transport = TransportTCP
		}

		for _, field := range fields[2:] {
			if key, value, found := strings.Cut(field, ":"); found && key == "model" {
				dev.Model = strings.ReplaceAll(value, "_", " ")
			}
		}

		devices = append(devices, dev)
	}

	return devices
}

// FindUSBDevice returns an attached hub, if one is plugged in.
func FindUSBDevice() (*Device, bool) {
	devices, err := Devices()
	if err != nil {
		return nil, false
	}

	for _, dev := range devices {
		if dev.Transport == TransportUSB && dev.IsOnline() {
			found := dev
			return &found, true
		}
	}

	return nil, false
}

// ABIList is the CPU architectures a device supports, most preferred first.
func ABIList(serial string) ([]string, error) {
	out, err := run(serial, "shell", "getprop", "ro.product.cpu.abilist")
	if err != nil {
		return nil, err
	}

	raw := strings.TrimSpace(out)
	if raw == "" {

		out, err = run(serial, "shell", "getprop", "ro.product.cpu.abi")
		if err != nil {
			return nil, err
		}
		raw = strings.TrimSpace(out)
	}

	var abis []string
	for _, abi := range strings.Split(raw, ",") {
		if abi = strings.TrimSpace(abi); abi != "" {
			abis = append(abis, abi)
		}
	}

	if len(abis) == 0 {
		return nil, fmt.Errorf("device reported no CPU ABI")
	}

	return abis, nil
}

func run(serial string, args ...string) (string, error) {
	full := args
	if serial != "" {
		full = append([]string{"-s", serial}, args...)
	}

	out, err := exec.Command("adb", full...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("adb %s failed: %w (output: %s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}

	return string(out), nil
}

// Connect establishes an adb connection to the robot over Wi-Fi, retrying.
func Connect() error { return ConnectTo(os.Stdout) }

// ConnectTo is Connect, saying what it is doing to a caller's writer.
//
// A menu cannot let this print to stdout: bubbletea owns the screen, and six
// lines of retry chatter painted over a menu stay there for the rest of the
// session.
// The retries are what make this work after a Wi-Fi join: the robot's adb is
// not listening the instant the laptop has an address on its network.
func ConnectTo(out io.Writer) error { return connectWithRetries(out) }

// ProbeTimeout is how long one address gets to prove it has adb behind it.
const ProbeTimeout = 2 * time.Second

// ConnectAt attaches to a robot at a named address, with one attempt rather
// than the retries ConnectTo makes. A sweep tries many addresses and cannot
// spend fifteen seconds on each one that turns out to be a printer.
func ConnectAt(out io.Writer, addr string) error {
	if !IsInstalled() {
		return fmt.Errorf("%w - please install Android SDK Platform-Tools", ErrNoADB)
	}

	// Bounded, because adb spends about ten seconds on a host that accepts a
	// socket and then does not speak its protocol. That is any device with the
	// port open and no adb behind it, and a sweep that meets two of them has
	// somebody watching a blank terminal for half a minute.
	ctx, cancel := context.WithTimeout(context.Background(), ProbeTimeout)
	defer cancel()

	output, err := exec.CommandContext(ctx, "adb", "connect", addr).CombinedOutput()
	if ctx.Err() != nil {
		_, _ = exec.Command("adb", "disconnect", addr).CombinedOutput()
		return fmt.Errorf("%s did not answer adb in %s", addr, ProbeTimeout)
	}
	if err != nil {
		return fmt.Errorf("adb connect %s failed: %w", addr, err)
	}

	text := strings.ToLower(strings.TrimSpace(string(output)))
	if strings.Contains(text, "connected") {
		return nil
	}
	return fmt.Errorf("adb would not connect to %s: %s", addr, strings.TrimSpace(string(output)))
}

// DisconnectFrom drops one address, leaving any others attached.
func DisconnectFrom(addr string) error {
	_, err := exec.Command("adb", "disconnect", addr).CombinedOutput()
	return err
}

func connectWithRetries(out io.Writer) error {
	if !IsInstalled() {
		return fmt.Errorf("%w - please install Android SDK Platform-Tools", ErrNoADB)
	}

	addr := RobotAddr()
	fmt.Fprintf(out, "[*] Attempting ADB connection to %s...\n", addr)

	maxRetries := 5
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			fmt.Fprintf(out, "[*] ADB retry %d/%d...\n", i+1, maxRetries)
			time.Sleep(3 * time.Second)
		}

		cmd := exec.Command("adb", "connect", addr)
		output, err := cmd.CombinedOutput()
		outputStr := strings.TrimSpace(string(output))

		if err != nil {
			lastErr = fmt.Errorf("adb command failed: %w", err)
			continue
		}

		lowerOutput := strings.ToLower(outputStr)
		if strings.Contains(lowerOutput, "connected") || strings.Contains(lowerOutput, "already connected") {
			return nil
		}

		lastErr = fmt.Errorf("unexpected response: %s", outputStr)
	}

	return fmt.Errorf("ADB connection failed after %d attempts: %w\n\n[!] Troubleshooting:\n  1. Ensure you're connected to the robot's Wi-Fi\n  2. Enable ADB debugging on Robot Controller\n  3. Try 'adb connect %s' manually\n  4. Check robot app is running", maxRetries, lastErr, addr)
}

// Disconnect drops every adb network connection.
func Disconnect() error {
	if !IsInstalled() {
		return fmt.Errorf("adb not found")
	}

	cmd := exec.Command("adb", "disconnect")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb disconnect failed: %w (output: %s)", err, string(output))
	}

	return nil
}

// IsConnected reports whether adb currently holds the robot.
func IsConnected() bool {
	if !IsInstalled() {
		return false
	}

	cmd := exec.Command("adb", "devices")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	return strings.Contains(string(output), RobotAddr())
}

// Options is how a deploy is allowed to install.
type Options struct {
	Delta bool

	SkipUnchanged bool

	Stream bool

	Splits []string

	// RecoveryAttempts limits reconnects across the whole deploy. Zero uses
	// the default, so API callers get recovery without extra setup.
	RecoveryAttempts int

	// CanaryTimeout bounds the quick connection check before transfer begins.
	CanaryTimeout time.Duration
}

// InstallWith installs an APK and reports what it actually did.
func InstallWith(serial, apkPath string, opt Options) (InstallPlan, error) {
	plan := InstallPlan{}

	if !IsInstalled() {
		return plan, fmt.Errorf("adb not found")
	}

	recovery := newConnectionRecovery(serial, opt.RecoveryAttempts)
	canaryTimeout := opt.CanaryTimeout
	if canaryTimeout <= 0 {
		canaryTimeout = defaultCanaryTimeout
	}
	if err := recovery.run("checking the robot connection", func() error {
		_, err := runTimeout(serial, canaryTimeout, "shell", "echo", "ok")
		return err
	}); err != nil {
		return plan, err
	}

	// What was installed is recorded whatever the settings say. Skipping an
	// unchanged install is only one reader of that record: Epsh Extreme uses
	// it to decide whether a reload is equivalent to an install, and without it
	// it can never tell and always installs.
	pkg := PackageName(apkPath)

	fingerprint := ""
	if pkg != "" {
		if sum, err := APKFingerprint(apkPath); err == nil {
			fingerprint = sum

			if opt.SkipUnchanged && alreadyInstalled(serial, sum, pkg) {
				plan.Skipped = true
				plan.Reason = "the robot already has this exact build"
				return plan, nil
			}
		}
	}

	if len(opt.Splits) > 1 && pkg != "" {
		count, err := SplitInstall(serial, pkg, opt.Splits)
		switch {
		case err != nil:
			fmt.Printf("\n[!] Split install failed: %v\n", err)
			fmt.Println("[*] Falling back to installing the whole APK.")
		case count == 0:
			plan.Skipped = true
			plan.Reason = "no split changed"
			return plan, nil
		default:
			recordSplits(serial, opt.Splits)
			plan.Splits = count
			return plan, nil
		}
	}

	// Transfer first, install second. They are separate choices: delta decides
	// how the bytes get to the robot, streaming decides how they are installed.
	// Trying streaming first sent the whole APK and left delta as dead code.
	remote := ""
	if opt.Delta {
		var result *DeltaResult
		err := recovery.run("preparing the delta transfer", func() error {
			var err error
			result, err = deltaInstall(serial, apkPath)
			return err
		})
		if err == nil {
			remote = remoteDeltaAPK
			defer pruneCache(serial, result.chunks)
		} else {
			if isConnectionRecoveryError(err) {
				return plan, err
			}
			var unavailable ErrDeltaUnavailable
			if errors.As(err, &unavailable) {
				fmt.Printf("\n[!] Delta transfer unavailable: %s\n", unavailable.Reason)
			} else {
				fmt.Printf("\n[!] Delta transfer failed: %v\n", err)
			}
			fmt.Println("[*] Sending the whole APK instead.")
		}
	}

	forgetInstalled(serial)

	if opt.Stream {
		// Binary on a device shell's stdin is not uniformly reliable on older
		// Android, so a failure here is expected and must not end the deploy.
		err := recovery.run("opening the streaming install", func() error {
			return streamFrom(serial, apkPath, remote)
		})
		if err == nil {
			plan.Streamed, plan.Delta = true, remote != ""
			if fingerprint != "" {
				recordInstalled(serial, fingerprint, pkg)
			}
			return plan, nil
		}

		if isRecoverableConnectionError(err) || isConnectionRecoveryError(err) {
			return plan, err
		}
		fmt.Printf("\n[!] Streaming install unavailable: %v\n", err)
		fmt.Println("[*] Falling back to a staged install.")
	}

	var err error
	if remote != "" {
		fmt.Println("[*] Installing...")
		err = recovery.run("installing the delta APK", func() error { return runInstall(serial, remote) })
	} else {
		err = recovery.run("transferring the APK", func() error { return tryInstall(serial, apkPath) })
	}
	if err != nil {
		return plan, err
	}

	plan.Delta = remote != ""
	if fingerprint != "" {
		recordInstalled(serial, fingerprint, pkg)
	}

	return plan, nil
}

func tryInstall(serial, apkPath string) error {
	fileInfo, err := os.Stat(apkPath)
	if err != nil {
		return fmt.Errorf("cannot read APK: %w", err)
	}
	sizeMB := float64(fileInfo.Size()) / (1024 * 1024)

	fmt.Printf("[*] Transferring APK (%.1f MB)...\n", sizeMB)

	pushArgs := []string{"push", apkPath, remoteAPKPath}
	if serial != "" {
		pushArgs = append([]string{"-s", serial}, pushArgs...)
	}

	pushStart := time.Now()
	pushCmd := exec.Command("adb", pushArgs...)
	pushCmd.Stdout = os.Stdout
	pushCmd.Stderr = os.Stderr
	if pushErr := pushCmd.Run(); pushErr != nil {
		return fmt.Errorf("adb push failed: %w", pushErr)
	}
	pushSecs := time.Since(pushStart).Seconds()

	if pushSecs > 0 {
		fmt.Printf("[OK] Transferred in %.1fs (%.1f MB/s)\n", pushSecs, sizeMB/pushSecs)
	}

	fmt.Println("[*] Installing...")

	defer func() {
		_, _ = run(serial, "shell", "rm", "-f", remoteAPKPath)
	}()

	return runInstall(serial, remoteAPKPath)
}

func runInstall(serial, remotePath string) error {
	out, err := run(serial, "shell", "pm", "install", "-r", "-d", "-g", "-t", remotePath)
	result := strings.TrimSpace(out)

	if err != nil {
		return fmt.Errorf("pm install failed: %w", err)
	}

	lower := strings.ToLower(result)
	if strings.Contains(lower, "success") {
		return nil
	}

	if strings.Contains(lower, "failure") ||
		strings.Contains(lower, "failed") ||
		strings.Contains(lower, "error") {
		return fmt.Errorf("pm install failed: %s", result)
	}

	return nil
}
