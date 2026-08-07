package hotreload

import (
	"archive/zip"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/andreibanu/pusher/internal/adb"
)

// Re-registration is not per class. An exception anywhere in it empties the
// whole OpMode list rather than skipping the file that caused it, so a robot
// left with no OpModes at all is the cost of one bad byte. Everything is
// checked here, on both sides, before the trigger is written.

// verifyLocal checks the files are what the robot will expect before they are
// sent.
func verifyLocal(files built) error {
	archive, err := zip.OpenReader(files.Jar)
	if err != nil {
		return fmt.Errorf("the jar is not readable as a zip: %w", err)
	}
	defer archive.Close()

	want := strings.ReplaceAll(Package, ".", "/") + "/" + ClassName + ".class"

	found := false
	for _, entry := range archive.File {
		if entry.Name == want {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("the jar has no %s, so the class would never be named", want)
	}

	dex, err := os.ReadFile(files.Dex)
	if err != nil {
		return fmt.Errorf("the dex is not readable: %w", err)
	}
	if len(dex) < 8 || string(dex[:4]) != "dex\n" {
		return fmt.Errorf("the dex does not start like a dex file")
	}

	return nil
}

// verifyOnHub confirms the files arrived whole.
//
// Comparing sizes rather than trusting the push: a transfer that stopped part
// way leaves a file with the right name and the wrong contents, and that is
// exactly what empties the OpMode list.
func verifyOnHub(serial, dir string, files built) error {
	for _, pair := range []struct {
		local, remote string
	}{
		{files.Jar, dir + "/" + ProofName + ".jar"},
		{files.Dex, dir + "/" + ProofName + ".dex"},
	} {
		info, err := os.Stat(pair.local)
		if err != nil {
			return err
		}

		size, err := remoteSize(serial, pair.remote)
		if err != nil {
			return err
		}

		if size != info.Size() {
			return fmt.Errorf("%s arrived as %d bytes, not %d", pair.remote, size, info.Size())
		}
	}

	return nil
}

// remoteSize is the size of a file on the hub.
func remoteSize(serial, path string) (int64, error) {
	// wc -c rather than stat, whose output format differs between the
	// toybox on a Control Hub and everything else.
	out, err := adb.Shell(serial, "wc", "-c", "<", shellQuote(path), "2>/dev/null")
	if err != nil {
		return 0, fmt.Errorf("%s is not readable on the hub", path)
	}

	field := strings.Fields(strings.TrimSpace(out))
	if len(field) == 0 {
		return 0, fmt.Errorf("%s is not on the hub", path)
	}

	size, err := strconv.ParseInt(field[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("cannot read the size of %s", path)
	}
	return size, nil
}

// noEmptyFiles reports anything zero length under the output root, which would
// stop the next reload whatever else is correct.
func noEmptyFiles(serial string) error {
	out, err := adb.Shell(serial, "find", OutputRoot, "-type", "f", "-size", "0", "2>/dev/null")
	if err != nil {
		return nil
	}

	var empty []string
	for _, line := range strings.Split(out, "\n") {
		if path := strings.TrimSpace(strings.TrimRight(line, "\r")); path != "" {
			empty = append(empty, path)
		}
	}

	if len(empty) > 0 {
		return fmt.Errorf("empty files under %s would stop the reload: %s",
			OutputRoot, strings.Join(empty, ", "))
	}
	return nil
}
