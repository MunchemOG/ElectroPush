package adb

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	defaultRecoveryAttempts = 2
	defaultCanaryTimeout    = 3 * time.Second
)

// connectionRecovery keeps one deploy from repeatedly retrying separate
// stages after the robot's Wi-Fi connection has become unreliable.
type connectionRecovery struct {
	serial   string
	attempts int
	used     int
}

func newConnectionRecovery(serial string, attempts int) *connectionRecovery {
	if attempts <= 0 {
		attempts = defaultRecoveryAttempts
	}
	return &connectionRecovery{serial: serial, attempts: attempts}
}

func (r *connectionRecovery) run(step string, operation func() error) error {
	err := operation()
	if err == nil || !isRecoverableConnectionError(err) || !isWirelessSerial(r.serial) {
		return err
	}

	for err != nil && isRecoverableConnectionError(err) {
		if r.used >= r.attempts {
			return fmt.Errorf("robot connection kept dropping while %s after %d recovery attempts; try moving closer to the hub or power-cycling it: %w", step, r.attempts, err)
		}

		r.used++
		fmt.Printf("\n[!] Robot connection dropped while %s. Recovering (%d/%d)...\n", step, r.used, r.attempts)
		if recoverErr := recoverConnection(r.serial); recoverErr != nil {
			return fmt.Errorf("robot connection kept dropping while %s; recovery %d/%d failed: %w", step, r.used, r.attempts, recoverErr)
		}

		fmt.Printf("[*] Retrying %s...\n", step)
		err = operation()
	}

	return err
}

// recoverConnection resets only the failing wireless transport, then connects
// it again. USB devices have no comparable reconnect command.
func recoverConnection(serial string) error {
	if !isWirelessSerial(serial) {
		return fmt.Errorf("cannot reconnect non-wireless device %q", serial)
	}

	output, err := exec.Command("adb", "disconnect", serial).CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb disconnect failed: %w (output: %s)", err, strings.TrimSpace(string(output)))
	}

	time.Sleep(2 * time.Second)
	output, err = exec.Command("adb", "connect", serial).CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb connect failed: %w (output: %s)", err, strings.TrimSpace(string(output)))
	}

	lower := strings.ToLower(string(output))
	if !strings.Contains(lower, "connected") && !strings.Contains(lower, "already connected") {
		return fmt.Errorf("adb connect returned: %s", strings.TrimSpace(string(output)))
	}

	time.Sleep(time.Second)
	return nil
}

func isWirelessSerial(serial string) bool { return strings.Contains(serial, ":") }

func isRecoverableConnectionError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"device offline",
		"failed to get feature set",
		"transport endpoint is not connected",
		"connection reset",
		"broken pipe",
		"connection closed",
		"device not found",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}

	return false
}

func isConnectionRecoveryError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "robot connection kept dropping")
}
