package robotcfg

import (
	// MD5 is used to tell whether two files are the same, never to protect
	// anything. It is what the hub's shell can compute without extra tools.
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/andreibanu/pusher/internal/adb"
)

// HubDir is where the robot controller keeps its configurations. Every
// configuration the Driver Station can see lives here as one XML file, and the
// filename without its extension is the name shown on the Driver Station.
const HubDir = "/sdcard/FIRST"

// Ext is the extension the robot controller looks for.
const Ext = ".xml"

// illegalNameChars are the characters the robot controller refuses in a
// configuration name, because the name becomes a filename.
const illegalNameChars = `?:"*|/\<>`

// CheckName reports whether a name can be used as a configuration name.
func CheckName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("a configuration name cannot be empty")
	}
	if name != strings.TrimSpace(name) {
		return fmt.Errorf("%q has leading or trailing whitespace", name)
	}
	if i := strings.IndexAny(name, illegalNameChars); i >= 0 {
		return fmt.Errorf("%q contains %q, which the robot controller does not allow in a "+
			"configuration name (none of %s)", name, name[i], illegalNameChars)
	}
	return nil
}

// RemotePath is where a named configuration lives on the robot.
func RemotePath(name string) string {
	return HubDir + "/" + name + Ext
}

// List returns the configuration names on the robot, sorted.
//
// Names routinely contain spaces, so the listing is one name per line and is
// never split on whitespace.
func List(serial string) ([]string, error) {
	out, err := adb.Shell(serial, "ls", "-1", HubDir, "2>/dev/null")
	if err != nil {
		return nil, fmt.Errorf("cannot list %s on the robot: %w", HubDir, err)
	}

	return parseListing(out), nil
}

func parseListing(out string) []string {
	var names []string

	for _, line := range strings.Split(out, "\n") {
		// adb shell hands back CRLF on some hubs.
		file := strings.TrimSpace(strings.TrimRight(line, "\r"))

		if !strings.HasSuffix(file, Ext) {
			continue
		}
		names = append(names, strings.TrimSuffix(file, Ext))
	}

	sort.Strings(names)
	return names
}

// Hashes returns an MD5 for every configuration on the robot, keyed by name.
//
// One shell call rather than a pull per configuration: over the robot's Wi-Fi
// a listing that transferred every file would take long enough to be worth
// avoiding. An empty result means the hub has no md5sum, which is not an error
// worth surfacing - the caller falls back to saying nothing about sameness.
func Hashes(serial string) map[string]string {
	out, err := adb.Shell(serial, "md5sum", HubDir+"/*"+Ext, "2>/dev/null")
	if err != nil {
		return nil
	}

	return parseHashes(out)
}

func parseHashes(out string) map[string]string {
	hashes := map[string]string{}

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))

		// "<32 hex chars>  <path>". The path is taken as the rest of the line
		// because configuration names contain spaces.
		digest, path, found := strings.Cut(line, "  ")
		if !found || len(digest) != 32 {
			continue
		}

		path = strings.TrimSpace(path)
		if !strings.HasPrefix(path, HubDir+"/") || !strings.HasSuffix(path, Ext) {
			continue
		}

		name := strings.TrimSuffix(strings.TrimPrefix(path, HubDir+"/"), Ext)
		hashes[name] = strings.ToLower(digest)
	}

	return hashes
}

// Hash is the digest form used to compare against Hashes.
func Hash(data []byte) string {
	return fmt.Sprintf("%x", md5.Sum(data))
}

// Fetch reads one configuration off the robot.
func Fetch(serial, name string) ([]byte, error) {
	local, err := os.CreateTemp("", "pusher-config-*.xml")
	if err != nil {
		return nil, err
	}
	local.Close()
	defer os.Remove(local.Name())

	if err := adb.Pull(serial, RemotePath(name), local.Name()); err != nil {
		return nil, fmt.Errorf("cannot read %q off the robot: %w", name, err)
	}

	data, err := os.ReadFile(local.Name())
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%q came back empty", name)
	}

	return data, nil
}

// Send writes one configuration to the robot.
//
// It goes through a temporary file rather than a shell redirect: configuration
// names contain spaces and quotes survive adb's shell round trip badly, and a
// push either transfers the file or fails.
func Send(serial, name string, data []byte) error {
	if err := CheckName(name); err != nil {
		return err
	}

	local, err := os.CreateTemp("", "pusher-config-*.xml")
	if err != nil {
		return err
	}
	defer os.Remove(local.Name())

	if _, err := local.Write(data); err != nil {
		local.Close()
		return err
	}
	if err := local.Close(); err != nil {
		return err
	}

	if err := adb.Push(serial, local.Name(), RemotePath(name)); err != nil {
		return fmt.Errorf("cannot write %q to the robot: %w", name, err)
	}

	return nil
}

// Remove deletes a configuration from the robot.
func Remove(serial, name string) error {
	if _, err := adb.Shell(serial, "rm", "-f", shellQuote(RemotePath(name))); err != nil {
		return fmt.Errorf("cannot delete %q from the robot: %w", name, err)
	}
	return nil
}

// Exists reports whether the robot has a configuration by that name.
func Exists(serial, name string) bool {
	names, err := List(serial)
	if err != nil {
		return false
	}
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// shellQuote wraps a path for the shell adb runs on the far side. Only used for
// rm; transfers go through adb push and pull, which take paths verbatim.
func shellQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}

// rcPackages are the robot controller applications whose settings might hold
// the active configuration. The first is what every team builds from; the
// second is the Control Hub's factory-installed one.
var rcPackages = []string{
	"com.qualcomm.ftcrobotcontroller",
	"com.revrobotics.ftcrobotcontroller",
}

// activeConfigPref is the settings key the robot controller stores the active
// configuration under. The value is the JSON form of the SDK's RobotConfigFile.
const activeConfigPref = "pref_hardware_config_filename"

// ActiveConfig returns the configuration the robot controller currently has
// selected.
//
// This is best effort by design. It reads the application's own settings, which
// only works where adb runs privileged - true on a Control Hub, not on a phone
// used as a robot controller. Nothing depends on the answer: an empty string
// means "could not tell", never "none selected".
func ActiveConfig(serial string) string {
	for _, pkg := range rcPackages {
		path := fmt.Sprintf("/data/data/%s/shared_prefs/%s_preferences.xml", pkg, pkg)

		out, err := adb.Shell(serial, "cat", path, "2>/dev/null")
		if err != nil || strings.TrimSpace(out) == "" {
			continue
		}

		if name := activeFromPrefs(out); name != "" {
			return name
		}
	}

	return ""
}

// activeFromPrefs digs the configuration name out of the settings file.
//
// The stored value is a JSON object that has been XML-escaped into an Android
// preferences file, so it arrives as &quot; rather than ". Pulling the key out
// by hand avoids parsing an Android preferences file to read one string.
func activeFromPrefs(prefs string) string {
	marker := `name="` + activeConfigPref + `"`

	start := strings.Index(prefs, marker)
	if start < 0 {
		return ""
	}

	rest := prefs[start:]
	open := strings.Index(rest, ">")
	closing := strings.Index(rest, "</string>")
	if open < 0 || closing < 0 || closing < open {
		return ""
	}

	value := unescapeXML(rest[open+1 : closing])

	var stored struct {
		Name string `json:"name"`
	}
	if json.Unmarshal([]byte(value), &stored) != nil {
		return ""
	}

	return stored.Name
}

func unescapeXML(s string) string {
	return strings.NewReplacer(
		"&quot;", `"`,
		"&apos;", "'",
		"&lt;", "<",
		"&gt;", ">",
		"&amp;", "&",
	).Replace(s)
}

// LocalDir is where configurations are kept in an FTC project. It sits at the
// project root rather than under TeamCode so a configuration is versioned
// alongside the code that names its devices.
func LocalDir(projectRoot string) string {
	return filepath.Join(projectRoot, "configs")
}
