package extreme

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Parent-first delegation means a class present in the APK always wins. So team
// code that is going to be reloaded cannot also be packaged, or the reload
// loads fine and the robot keeps running the old copy with nothing to show for
// it.
//
// This is the one change made to a team's repository, and it is marked rather
// than backed up. `pusher slim` already keeps a .pusher-bak of the same file,
// and two features sharing one backup means undoing either undoes both. A
// marked block can be added and removed exactly, whatever else edited the file
// in between.

const (
	beginMarker = "// pusher extreme: begin - team code is reloaded, not packaged"
	endMarker   = "// pusher extreme: end"
)

// TeamPackage is what gets excluded from the APK.
const TeamPackage = "org/firstinspires/ftc/teamcode"

var blockRe = regexp.MustCompile(`(?s)\n*` + regexp.QuoteMeta(beginMarker) + `.*?` + regexp.QuoteMeta(endMarker) + `\n*`)

var keptRe = regexp.MustCompile(`// Kept in the APK anyway: (.*)`)

// blockFor builds what gets appended to the module's gradle file.
//
// A second android { } is legal: it is a method call configuring the same
// extension, not a declaration, so this composes with whatever is above it
// instead of having to be spliced into it.
//
// The exclusion is a closure rather than a pattern because of the keep list.
// Ant patterns cannot say "everything under here except that", and generating
// one pattern per subpackage would stop covering a package added later.
func blockFor(keep []string) string {
	var conditions strings.Builder
	for _, pkg := range keep {
		fmt.Fprintf(&conditions, " &&\n                        !path.startsWith('%s/')",
			strings.TrimSuffix(pkg, "/"))
	}

	kept := "nothing"
	if len(keep) > 0 {
		kept = strings.Join(keep, ", ")
	}

	return beginMarker + `
//
// Remove this block, or run the menu entry that added it, to go back to
// packaging team code in the APK.
//
// Kept in the APK anyway: ` + kept + `
android {
    sourceSets {
        main {
            java {
                exclude { details ->
                    def path = details.path
                    path.startsWith('` + TeamPackage + `/')` + conditions.String() + `
                }
            }
        }
    }
}
` + endMarker
}

// GradleFile is the module file the exclusion lives in.
func GradleFile(root string) string {
	return filepath.Join(root, Module, "build.gradle")
}

// Excluded reports whether team code is being kept out of the APK.
func Excluded(root string) bool {
	content, err := os.ReadFile(GradleFile(root))
	if err != nil {
		return false
	}
	return blockRe.Match(content)
}

// Kept reports the packages the block leaves in the APK.
//
// Read back from the file rather than from settings, because the file is what
// the build actually obeys.
func Kept(root string) []string {
	content, err := os.ReadFile(GradleFile(root))
	if err != nil {
		return nil
	}

	match := keptRe.FindSubmatch(content)
	if match == nil {
		return nil
	}

	line := strings.TrimSpace(string(match[1]))
	if line == "" || line == "nothing" {
		return nil
	}

	var out []string
	for _, pkg := range strings.Split(line, ",") {
		if pkg = strings.TrimSpace(pkg); pkg != "" {
			out = append(out, pkg)
		}
	}
	return out
}

// Exclude stops team code being packaged into the APK, except for the packages
// named in keep.
//
// Something a library reflects over has to stay in the APK. FtcDashboard scans
// the base APK itself with getPackageCodePath, so a @Config class that is
// reloaded is invisible to it however correctly it loads.
func Exclude(root string, keep ...string) error {
	path := GradleFile(root)

	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", path, err)
	}

	// Replace rather than skip: the keep list may have changed, and a block
	// that no longer matches the settings is worse than no block.
	stripped := blockRe.ReplaceAllString(string(content), "\n")

	updated := strings.TrimRight(stripped, "\n") + "\n\n" + blockFor(keep) + "\n"

	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	return nil
}

// Include puts team code back in the APK.
func Include(root string) error {
	path := GradleFile(root)

	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", path, err)
	}
	if !blockRe.Match(content) {
		return nil
	}

	updated := blockRe.ReplaceAllString(string(content), "\n")

	if err := os.WriteFile(path, []byte(strings.TrimRight(updated, "\n")+"\n"), 0o644); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	return nil
}
