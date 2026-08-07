package extreme

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// A reloaded class lives below the APK in the classloader chain, so the APK can
// see nothing of it. Team code calling into a library is fine. A library
// reaching back into team code is not, and it fails at runtime rather than at
// compile time.
//
// Most FTC libraries never do it: pedro, Panels, EasyOpenCV and blob all go
// through the SDK, which does see reloaded classes. FtcDashboard is the
// exception. It scans the base APK itself with getPackageCodePath, so a @Config
// class that is reloaded is invisible to it however correctly it loads.
//
// Leaving those classes in the APK would fix it, but that is not a default
// worth having. In a real project @Config turned out to be on 45 of 120 files
// including the OpModes themselves, so keeping them all would make most of the
// project unreloadable, which is worse than what it fixes.
//
// So they are bridged instead: see bridge.go. What is found here is handed to
// the dashboard by generated code that runs inside the reload, which gets the
// tuning back without keeping anything in the APK. Keeping a package in the APK
// stays available for a library nothing here knows how to bridge.

// reflectedBy are annotations a library reads by scanning rather than by being
// handed the class.
var reflectedBy = map[string]string{
	"@Config": "FtcDashboard scans the APK for these, so a reloaded one will not appear",
}

var annotationRe = regexp.MustCompile(`(?m)^\s*@(\w+)`)

// Reflected is a class something in the APK reads by scanning.
type Reflected struct {
	Package string
	File    string
	Why     string
}

// Reflection is what a project would lose by reloading.
type Reflection struct {
	Classes  []Reflected
	Packages []string
	Why      string
}

// Any reports whether there is anything to say.
func (r Reflection) Any() bool { return len(r.Classes) > 0 }

// Summary is the one line for a menu.
func (r Reflection) Summary() string {
	if !r.Any() {
		return ""
	}
	return fmt.Sprintf("%d classes use @Config: they are registered with "+
		"FtcDashboard from inside the reload, since it cannot find them itself", len(r.Classes))
}

// FindReflected looks for team code something in the APK reads by scanning.
//
// Source rather than bytecode: this runs before anything is compiled, and the
// answer decides what gets compiled.
func FindReflected(root string) Reflection {
	base := filepath.Join(root, SourceRoot)

	var out Reflection
	packages := map[string]bool{}

	filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".java") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		for _, match := range annotationRe.FindAllStringSubmatch(string(content), -1) {
			why, reflected := reflectedBy["@"+match[1]]
			if !reflected {
				continue
			}

			rel, err := filepath.Rel(base, path)
			if err != nil {
				continue
			}

			pkg := filepath.ToSlash(filepath.Dir(rel))
			packages[pkg] = true
			out.Classes = append(out.Classes, Reflected{
				Package: pkg,
				File:    filepath.Base(path),
				Why:     why,
			})
			break
		}
		return nil
	})

	for pkg := range packages {
		out.Packages = append(out.Packages, pkg)
	}
	sort.Strings(out.Packages)
	sort.Slice(out.Classes, func(a, b int) bool { return out.Classes[a].File < out.Classes[b].File })

	if out.Any() {
		out.Why = reflectedBy["@Config"]
	}

	return out
}
