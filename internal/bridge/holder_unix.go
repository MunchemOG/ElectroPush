//go:build !windows

package bridge

import (
	"os/exec"
	"strconv"
	"strings"
)

// portHolder is the process listening on a local port: its pid and command
// name, or zero when nothing is.
//
// lsof is on every Mac and most Linux desktops; ss is on every Linux that
// lacks it.
func portHolder(port int) (int, string) {
	if out, err := exec.Command("lsof", "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fpc").Output(); err == nil {
		if pid, name := parseLsof(string(out)); pid != 0 {
			return pid, name
		}
	}
	if out, err := exec.Command("ss", "-ltnpH", "sport", "= :"+strconv.Itoa(port)).Output(); err == nil {
		return parseSS(string(out))
	}
	return 0, ""
}

// parseLsof reads lsof's field output: a "p<pid>" line, then "c<command>".
func parseLsof(out string) (int, string) {
	pid, name := 0, ""
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "p") && pid == 0:
			pid, _ = strconv.Atoi(strings.TrimPrefix(line, "p"))
		case strings.HasPrefix(line, "c") && name == "":
			name = strings.TrimPrefix(line, "c")
		}
	}
	return pid, name
}

// parseSS reads users:(("ssh",pid=1234,fd=5)) out of ss's process column.
func parseSS(out string) (int, string) {
	start := strings.Index(out, `(("`)
	if start < 0 {
		return 0, ""
	}
	rest := out[start+3:]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return 0, ""
	}
	name := rest[:end]

	pidAt := strings.Index(rest, "pid=")
	if pidAt < 0 {
		return 0, name
	}
	digits := rest[pidAt+4:]
	if comma := strings.IndexAny(digits, ",)"); comma >= 0 {
		digits = digits[:comma]
	}
	pid, _ := strconv.Atoi(digits)
	return pid, name
}
