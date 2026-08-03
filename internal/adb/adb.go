package adb

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	RobotIP   = "192.168.43.1"
	RobotPort = "5555"

	remoteAPKPath = "/data/local/tmp/pusher_app.apk"
)

type Transport string

const (
	TransportUSB Transport = "usb"

	TransportTCP Transport = "tcp"
)

type Device struct {
	Serial    string
	State     string
	Model     string
	Transport Transport
}

func (d Device) IsOnline() bool {
	return d.State == "device"
}

func (d Device) Label() string {
	if d.Model != "" {
		return fmt.Sprintf("%s (%s)", d.Model, d.Serial)
	}
	return d.Serial
}

func RobotAddr() string {
	return fmt.Sprintf("%s:%s", RobotIP, RobotPort)
}

func IsInstalled() bool {
	_, err := exec.LookPath("adb")
	return err == nil
}

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

func Connect() error {
	if !IsInstalled() {
		return fmt.Errorf("adb not found - please install Android SDK Platform-Tools")
	}

	addr := RobotAddr()
	fmt.Printf("[*] Attempting ADB connection to %s...\n", addr)

	maxRetries := 5
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			fmt.Printf("[*] ADB retry %d/%d...\n", i+1, maxRetries)
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

func Install(serial, apkPath string, useDelta bool) error {
	if !IsInstalled() {
		return fmt.Errorf("adb not found")
	}

	if useDelta {
		err := installDelta(serial, apkPath)
		if err == nil {
			return nil
		}

		var unavailable ErrDeltaUnavailable
		if errors.As(err, &unavailable) {
			fmt.Printf("\n[!] Delta transfer unavailable: %s\n", unavailable.Reason)
			fmt.Println("[*] Falling back to a full transfer.")
		} else {

			fmt.Printf("\n[!] Delta install failed: %v\n", err)
			fmt.Println("[*] Falling back to a full transfer.")
		}
	}

	err := tryInstall(serial, apkPath)
	if err == nil {
		return nil
	}

	isWireless := serial == "" || strings.Contains(serial, ":")
	if !isWireless {
		return err
	}

	errLower := strings.ToLower(err.Error())
	if strings.Contains(errLower, "device offline") ||
		strings.Contains(errLower, "failed to install") ||
		strings.Contains(errLower, "closed") ||
		strings.Contains(errLower, "error:") {

		fmt.Printf("\n[!] Install failed: %v\n", err)
		fmt.Println("[*] Attempting recovery: disconnect and reconnect...")

		if disconnectErr := Disconnect(); disconnectErr != nil {
			fmt.Printf("[!] Warning: disconnect failed: %v\n", disconnectErr)
		}

		time.Sleep(2 * time.Second)

		if connectErr := Connect(); connectErr != nil {
			return fmt.Errorf("reconnect failed: %w", connectErr)
		}

		time.Sleep(1 * time.Second)

		fmt.Println("[*] Retrying install...")
		if retryErr := tryInstall(serial, apkPath); retryErr != nil {
			return fmt.Errorf("install failed after reconnect: %w", retryErr)
		}

		fmt.Println("[OK] Install succeeded after reconnect")
		return nil
	}

	return err
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
