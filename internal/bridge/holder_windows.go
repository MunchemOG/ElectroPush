//go:build windows

package bridge

import (
	"os/exec"
	"strconv"
	"strings"
)

// portHolder is the process listening on a local port: its pid and image name
// without ".exe", or zero when nothing is.
func portHolder(port int) (int, string) {
	out, err := exec.Command("netstat", "-ano", "-p", "tcp").Output()
	if err != nil {
		return 0, ""
	}
	pid := parseNetstat(string(out), port)
	if pid == 0 {
		return 0, ""
	}

	list, err := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/FO", "CSV", "/NH").Output()
	if err != nil {
		return pid, ""
	}
	return pid, parseTasklist(string(list))
}

// parseNetstat finds the pid listening on a port in `netstat -ano` output.
func parseNetstat(out string, port int) int {
	suffix := ":" + strconv.Itoa(port)
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || !strings.EqualFold(fields[3], "LISTENING") {
			continue
		}
		if strings.HasSuffix(fields[1], suffix) {
			pid, _ := strconv.Atoi(fields[4])
			return pid
		}
	}
	return 0
}

// parseTasklist reads the image name out of `tasklist /FO CSV /NH`.
func parseTasklist(out string) string {
	line := strings.TrimSpace(out)
	if !strings.HasPrefix(line, `"`) {
		return ""
	}
	name := strings.SplitN(line[1:], `"`, 2)[0]
	return strings.TrimSuffix(strings.ToLower(name), ".exe")
}
