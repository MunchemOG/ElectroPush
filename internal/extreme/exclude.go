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

// block is appended to the module's gradle file.
//
// A second android { } is legal: it is a method call configuring the same
// extension, not a declaration, so this composes with whatever is above it
// instead of having to be spliced into it.
var block = beginMarker + `
//
// Remove this block, or run the menu entry that added it, to go back to
// packaging team code in the APK.
android {
    sourceSets {
        main {
            java {
                exclude '` + TeamPackage + `/**'
            }
        }
    }
}
` + endMarker

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

// Exclude stops team code being packaged into the APK.
func Exclude(root string) error {
	path := GradleFile(root)

	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", path, err)
	}
	if blockRe.Match(content) {
		return nil
	}

	updated := strings.TrimRight(string(content), "\n") + "\n\n" + block + "\n"

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
