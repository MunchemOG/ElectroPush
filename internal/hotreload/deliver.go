package hotreload

import (
	"fmt"
	"os"
	"time"
)

// Delivery is one set of files put onto a robot and made live.
type Delivery struct {
	Dir       string
	Bytes     int64
	Push      time.Duration
	ColdStart bool
	Steps     []string
}

func (d *Delivery) step(format string, args ...any) {
	d.Steps = append(d.Steps, fmt.Sprintf(format, args...))
}

// Deliver puts a jar and a dex on the robot and tells it to reload.
//
// This is the whole mechanism, and everything it does is load bearing. Each
// step exists because leaving it out produced an empty OpMode list on real
// hardware rather than an error:
//
//   - both files, because the jar is where class names are read and the dex is
//     where classes are loaded
//   - a fresh directory, because the running app holds the previous dex mapped
//     and writing over it leaves the mapping inconsistent
//   - a rename into place, because adb push creates the destination at zero
//     length and the SDK opens everything in the directory as a zip
//   - sizes checked on the far side before anything is triggered, because
//     re-registration abandons everything on the first failure rather than
//     skipping one file
//   - the pointer written only once both files are whole, because the SDK
//     reads whichever directory it names
//
// name is what the files are called on the hub, without an extension.
func Deliver(serial, name, jar, dex, marker string) (*Delivery, error) {
	out := &Delivery{ColdStart: !statusDirExists(serial)}

	clearEmpty(serial)

	dir, err := newOutputDir(serial, marker)
	if err != nil {
		return out, err
	}
	out.Dir = dir
	out.step("fresh directory: %s", dir)

	remoteJar := dir + "/" + name + ".jar"
	remoteDex := dir + "/" + name + ".dex"

	start := time.Now()
	if err := pushAtomic(serial, jar, remoteJar); err != nil {
		return out, fmt.Errorf("cannot push the jar: %w", err)
	}
	if err := pushAtomic(serial, dex, remoteDex); err != nil {
		return out, fmt.Errorf("cannot push the dex: %w", err)
	}
	out.Push = time.Since(start)

	for _, path := range []string{jar, dex} {
		if info, err := os.Stat(path); err == nil {
			out.Bytes += info.Size()
		}
	}
	out.step("pushed %s in %s", bytes(out.Bytes), out.Push.Round(time.Millisecond))

	if err := verifyPair(serial, jar, remoteJar, dex, remoteDex); err != nil {
		return out, err
	}
	out.step("both files verified on the hub")

	if err := writePointer(serial, dir); err != nil {
		return out, err
	}
	out.step("pointed %s at it", PointerFile)

	clearOldDirs(serial, dir)

	if err := noEmptyFiles(serial); err != nil {
		return out, err
	}

	if err := trigger(serial); err != nil {
		return out, err
	}
	out.step("wrote %s", TriggerFile)

	return out, nil
}

// verifyPair confirms both files arrived at their full length.
func verifyPair(serial, jar, remoteJar, dex, remoteDex string) error {
	for _, pair := range []struct{ local, remote string }{
		{jar, remoteJar},
		{dex, remoteDex},
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

func bytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d bytes", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.0f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}
