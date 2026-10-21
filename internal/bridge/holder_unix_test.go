//go:build !windows

package bridge

import "testing"

func TestReadingWhoHoldsAPort(t *testing.T) {
	for _, tc := range []struct {
		name   string
		parse  func(string) (int, string)
		out    string
		pid    int
		holder string
	}{
		{"lsof, the bridge's tunnel", parseLsof, "p4242\ncssh\nf5\n", 4242, "ssh"},
		{"lsof, this laptop's adb", parseLsof, "p77\ncadb\nf9\n", 77, "adb"},
		{"lsof, nothing", parseLsof, "", 0, ""},
		{"ss, the tunnel", parseSS, `LISTEN 0 128 127.0.0.1:5037 0.0.0.0:* users:(("ssh",pid=4242,fd=5))`, 4242, "ssh"},
		{"ss, nothing", parseSS, "", 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pid, holder := tc.parse(tc.out)
			if pid != tc.pid || holder != tc.holder {
				t.Errorf("got %d %q, want %d %q", pid, holder, tc.pid, tc.holder)
			}
		})
	}
}
