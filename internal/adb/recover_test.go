package adb

import (
	"errors"
	"testing"
)

func TestRecoverableConnectionErrors(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{errors.New("adb shell failed: device offline"), true},
		{errors.New("failed to get feature set: device offline"), true},
		{errors.New("transport endpoint is not connected"), true},
		{errors.New("INSTALL_FAILED_VERSION_DOWNGRADE"), false},
		{errors.New("device unauthorized"), false},
	} {
		if got := isRecoverableConnectionError(tc.err); got != tc.want {
			t.Errorf("isRecoverableConnectionError(%q) = %t, want %t", tc.err, got, tc.want)
		}
	}
}

func TestWirelessSerial(t *testing.T) {
	if !isWirelessSerial("192.168.43.1:5555") {
		t.Error("TCP serial was not considered wireless")
	}
	if isWirelessSerial("USB123") {
		t.Error("USB serial was considered wireless")
	}
}

func TestRecoveryDefaults(t *testing.T) {
	r := newConnectionRecovery("192.168.43.1:5555", 0)
	if r.attempts != defaultRecoveryAttempts {
		t.Errorf("attempts = %d, want %d", r.attempts, defaultRecoveryAttempts)
	}
}
