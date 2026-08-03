package wifi

import (
	"encoding/xml"
	"sort"
	"strconv"
	"strings"
)

// Parsers live here, untagged, so their tests run on every platform. The
// platform files only issue commands and hand the output to these.

func firstNotIn(networks, exclude []string) string {
	skip := make(map[string]bool, len(exclude))
	for _, ssid := range exclude {
		if ssid != "" {
			skip[ssid] = true
		}
	}

	for _, ssid := range networks {
		if !skip[ssid] {
			return ssid
		}
	}

	return ""
}

// Success looks like "Current Wi-Fi Network: <ssid>". The failure line,
// "You are not associated with an AirPort network.", has no colon -- and macOS
// prints it even when associated, if it is withholding the name.
func parseNetworksetupSSID(output string) string {
	_, ssid, found := strings.Cut(strings.TrimSpace(output), ":")
	if !found {
		return ""
	}
	return strings.TrimSpace(ssid)
}

func isRedacted(ssid string) bool {
	lower := strings.ToLower(ssid)
	return strings.Contains(lower, "redacted") ||
		strings.Contains(lower, "not associated") ||
		lower == "<unknown>"
}

func parseDarwinPreferred(output string) []string {
	var networks []string
	for _, line := range strings.Split(output, "\n") {
		// Entries are tab-indented under a header line.
		if !strings.HasPrefix(line, "\t") {
			continue
		}
		if name := strings.TrimSpace(line); name != "" {
			networks = append(networks, name)
		}
	}
	return networks
}

// splitTerse splits one line of `nmcli -t` output. nmcli escapes both the
// field separator and the escape character itself, so a naive Split on ":"
// would corrupt any SSID containing a colon.
func splitTerse(line string) []string {
	var (
		fields  []string
		current strings.Builder
		escaped bool
	)

	for _, r := range line {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case r == ':':
			fields = append(fields, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}

	return append(fields, current.String())
}

// parseNmcliWiFiDevice picks the wireless device out of
// `nmcli -t -f DEVICE,TYPE device`.
func parseNmcliWiFiDevice(output string) string {
	for _, line := range strings.Split(output, "\n") {
		fields := splitTerse(strings.TrimSpace(line))
		if len(fields) >= 2 && fields[1] == "wifi" && fields[0] != "" {
			return fields[0]
		}
	}
	return ""
}

// parseNmcliActiveSSID reads the connected network from
// `nmcli -t -f ACTIVE,SSID device wifi`.
func parseNmcliActiveSSID(output string) string {
	for _, line := range strings.Split(output, "\n") {
		fields := splitTerse(strings.TrimSpace(line))
		if len(fields) >= 2 && fields[0] == "yes" {
			return fields[1]
		}
	}
	return ""
}

// parseNmcliSavedNetworks reads `nmcli -t -f NAME,TYPE,TIMESTAMP connection
// show`, keeps the wireless profiles and orders them most-recently-used first.
//
// Unlike macOS, this is a real last-connected timestamp that NetworkManager
// maintains, not an inference from list ordering.
func parseNmcliSavedNetworks(output string) []string {
	type profile struct {
		name string
		used int64
	}

	var profiles []profile
	for _, line := range strings.Split(output, "\n") {
		fields := splitTerse(strings.TrimSpace(line))
		if len(fields) < 3 || fields[0] == "" {
			continue
		}
		if !strings.Contains(fields[1], "wireless") {
			continue
		}

		used, err := strconv.ParseInt(strings.TrimSpace(fields[2]), 10, 64)
		if err != nil {
			// A profile that has never connected still belongs in the list,
			// just last.
			used = 0
		}
		profiles = append(profiles, profile{name: fields[0], used: used})
	}

	sort.SliceStable(profiles, func(i, j int) bool {
		return profiles[i].used > profiles[j].used
	})

	names := make([]string, 0, len(profiles))
	for _, p := range profiles {
		names = append(names, p.name)
	}
	return names
}

func parseNmcliRadio(output string) bool {
	return strings.EqualFold(strings.TrimSpace(output), "enabled")
}

// netshField pulls a value from `netsh ... show` output by exact key, so that
// asking for "SSID" cannot accidentally match the "BSSID" line.
//
// netsh localises its labels, so this only works on an English-language
// Windows. Callers should prefer a PowerShell source and treat this as a
// fallback.
func netshField(output, key string) string {
	for _, line := range strings.Split(output, "\n") {
		name, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		if strings.TrimSpace(name) == key {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// parseNetshProfiles reads the saved network names out of
// `netsh wlan show profiles`.
//
// Values are taken from the right of the colon regardless of the label, which
// survives localisation; the section headers have nothing after their colon
// and drop out on their own.
func parseNetshProfiles(output string) []string {
	var networks []string

	for _, line := range strings.Split(output, "\n") {
		// Profile entries are indented; headers are not.
		if strings.TrimSpace(line) == "" || !strings.HasPrefix(line, " ") {
			continue
		}

		_, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}

		name := strings.TrimSpace(value)
		if name == "" || name == "<None>" {
			continue
		}
		networks = append(networks, name)
	}

	return networks
}

// wlanProfileXML builds the WPA2-PSK/AES profile Windows needs before it will
// connect, since netsh cannot take a password inline. The FTC Robot Controller
// hotspot uses exactly this security.
//
// Both values are XML-escaped: an unescaped & or < in a password would produce
// a profile Windows silently rejects.
func wlanProfileXML(ssid, password string) (string, error) {
	name, err := xmlEscape(ssid)
	if err != nil {
		return "", err
	}
	key, err := xmlEscape(password)
	if err != nil {
		return "", err
	}

	return `<?xml version="1.0"?>
<WLANProfile xmlns="http://www.microsoft.com/networking/WLAN/profile/v1">
  <name>` + name + `</name>
  <SSIDConfig>
    <SSID>
      <name>` + name + `</name>
    </SSID>
  </SSIDConfig>
  <connectionType>ESS</connectionType>
  <connectionMode>manual</connectionMode>
  <MSM>
    <security>
      <authEncryption>
        <authentication>WPA2PSK</authentication>
        <encryption>AES</encryption>
        <useOneX>false</useOneX>
      </authEncryption>
      <sharedKey>
        <keyType>passPhrase</keyType>
        <protected>false</protected>
        <keyMaterial>` + key + `</keyMaterial>
      </sharedKey>
    </security>
  </MSM>
</WLANProfile>`, nil
}

func xmlEscape(value string) (string, error) {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(value)); err != nil {
		return "", err
	}
	return b.String(), nil
}

func escapeSingleQuotes(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}
