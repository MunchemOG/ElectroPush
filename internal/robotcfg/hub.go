package robotcfg

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/andreibanu/pusher/internal/adb"
)

// HubDir is where the robot controller keeps its configurations.
const HubDir = "/sdcard/FIRST"

// Ext is the extension the robot controller looks for.
const Ext = ".xml"

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

func shellQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}

var rcPackages = []string{
	"com.qualcomm.ftcrobotcontroller",
	"com.revrobotics.ftcrobotcontroller",
}

const activeConfigPref = "pref_hardware_config_filename"

// ActiveConfig returns the configuration the robot has selected, empty if it cannot tell.
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

// LocalDir is where configurations are kept in an FTC project.
func LocalDir(projectRoot string) string {
	return filepath.Join(projectRoot, "configs")
}
