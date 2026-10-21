package cmd

import (
	"reflect"
	"testing"

	"github.com/MunchemOG/ElectroPush/internal/bridge"
)

// A page finds its sockets from the address it was loaded from, so over USB
// every port it opens has to be forwarded on its own number, not only the page.
func TestEachPageOpensWhereItWorksFromHere(t *testing.T) {
	for _, tc := range []struct {
		name    string
		page    string
		serial  string
		bridged bool
		url     string
		forward []int
	}{
		{"Panels over Wi-Fi", "panels", "192.168.43.1:5555", false, "http://192.168.43.1:8001", nil},
		{"Panels on the hub's relay address", "panels", "192.168.1.183:5555", false, "http://192.168.1.183:8001", nil},
		{"Panels over USB", "panels", "ABC123", false, "http://localhost:8001", []int{8001, 8002}},
		{"AutoTune over USB", "autotune", "ABC123", false, "http://localhost:10158", []int{10158, 12649}},
		{"FtcDashboard through a bridge", "dash", "192.168.43.1:5555", true, "http://localhost:8091/dash", nil},
		{"AutoTune through a bridge", "autotune", "192.168.43.1:5555", true, "http://localhost:10158", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			url, forward := pageAddress(robotPages[tc.page], tc.serial, tc.bridged)
			if url != tc.url {
				t.Errorf("url = %q, want %q", url, tc.url)
			}
			if !reflect.DeepEqual(forward, tc.forward) {
				t.Errorf("forward = %v, want %v", forward, tc.forward)
			}
		})
	}
}

// Every page epsh can open through a bridge has to be one the tunnel carries,
// or it opens a localhost address with nothing behind it.
func TestTheTunnelCarriesEveryPage(t *testing.T) {
	for name, page := range robotPages {
		for _, port := range page.Ports {
			if _, ok := bridge.Local(port); !ok {
				t.Errorf("%s needs port %d, which the bridge does not forward", name, port)
			}
		}
	}
}

// Running setup again to change one thing keeps everything else it was set up
// with, password included.
func TestSettingUpAgainKeepsWhatWasNotChanged(t *testing.T) {
	previous := bridge.Server{User: "pi", Interface: "wlan1", SSID: "14270-RC", Password: "secret", Host: "100.64.0.7"}

	got := keepUnset(bridge.Server{Interface: "wlan2"}, previous)
	want := bridge.Server{User: "pi", Interface: "wlan2", SSID: "14270-RC", Password: "secret", Host: "100.64.0.7"}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}

	// A new network is a new password: the old one belongs to the old network.
	got = keepUnset(bridge.Server{SSID: "30686-RC"}, previous)
	if got.Password != "" {
		t.Errorf("kept %q for a different network", got.Password)
	}
}
