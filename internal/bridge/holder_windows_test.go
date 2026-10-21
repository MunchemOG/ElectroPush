//go:build windows

package bridge

import "testing"

func TestReadingWhoHoldsAPortOnWindows(t *testing.T) {
	netstat := `
Active Connections

  Proto  Local Address          Foreign Address        State           PID
  TCP    0.0.0.0:135            0.0.0.0:0              LISTENING       1012
  TCP    127.0.0.1:5037         0.0.0.0:0              LISTENING       4242
  TCP    127.0.0.1:50370        127.0.0.1:5037         ESTABLISHED     9000
`
	if pid := parseNetstat(netstat, 5037); pid != 4242 {
		t.Errorf("pid = %d, want 4242", pid)
	}
	if pid := parseNetstat(netstat, 8000); pid != 0 {
		t.Errorf("nothing listens on 8000, got %d", pid)
	}
	if name := parseTasklist(`"ssh.exe","4242","Console","1","8,120 K"`); name != "ssh" {
		t.Errorf("name = %q", name)
	}
}
