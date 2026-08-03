package adb

import "testing"

func TestParseDevicesSeparatesUSBFromWireless(t *testing.T) {
	output := `List of devices attached
* daemon not running; starting now at tcp:5037
192.168.43.1:5555      device product:msm8916_32 model:Control_Hub_v1_0 device:msm8916_32
84B7N16919000123       device product:sdm660 model:moto_g6 device:ali
`

	devices := parseDevices(output)
	if len(devices) != 2 {
		t.Fatalf("expected 2 devices, got %d: %+v", len(devices), devices)
	}

	wireless := devices[0]
	if wireless.Transport != TransportTCP {
		t.Errorf("a host:port serial must be wireless, got %q", wireless.Transport)
	}
	if wireless.Model != "Control Hub v1 0" {
		t.Errorf("model underscores should become spaces, got %q", wireless.Model)
	}
	if !wireless.IsOnline() {
		t.Error("state 'device' should count as online")
	}

	usb := devices[1]
	if usb.Transport != TransportUSB {
		t.Errorf("a bare serial must be USB, got %q", usb.Transport)
	}
	if usb.Label() != "moto g6 (84B7N16919000123)" {
		t.Errorf("unexpected label %q", usb.Label())
	}
}

func TestParseDevicesSkipsNoiseAndOfflineIsNotOnline(t *testing.T) {
	devices := parseDevices("List of devices attached\n\n192.168.43.1:5555\toffline\n")
	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	if devices[0].IsOnline() {
		t.Error("an offline device must not be reported as online")
	}

	if devices[0].Label() != "192.168.43.1:5555" {
		t.Errorf("unexpected label %q", devices[0].Label())
	}
}

func TestParseDevicesEmpty(t *testing.T) {
	if got := parseDevices("List of devices attached\n\n"); len(got) != 0 {
		t.Errorf("expected no devices, got %+v", got)
	}
}
