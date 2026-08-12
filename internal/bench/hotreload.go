package bench

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MunchemOG/ElectroPush/internal/adb"
)

// Reload is the measured cost of the pieces a hot reload would need.
type Reload struct {
	DexBytes int64

	Push time.Duration

	Compile    time.Duration
	CompileOK  bool
	CompileWhy string

	// StubBytes and Overhead come from compiling a near-empty dex, which
	// measures what dex2oat costs before it compiles anything. Without it a
	// small sample reads as "compiling is free" when it is mostly startup.
	StubBytes int64
	Overhead  time.Duration

	// Measured distinguishes a run that happened from one that was never
	// asked for. Without it a skipped measurement renders as a row of zeros.
	Measured bool

	Err error
}

// Floor is the least a reload could cost, given these measurements.
func (r Reload) Floor() time.Duration { return r.Push + r.Compile }

// Marginal is the compile cost with dex2oat's fixed startup taken out.
func (r Reload) Marginal() time.Duration {
	if r.Compile <= r.Overhead {
		return 0
	}
	return r.Compile - r.Overhead
}

// MeasureReload times pushing a team-code-sized dex to the hub and compiling it there.
func MeasureReload(serial, apkPath string) Reload {
	out := Reload{Measured: true}

	dex, size, err := extractSampleDex(apkPath)
	if err != nil {
		out.Err = err
		return out
	}
	defer os.Remove(dex)

	out.DexBytes = size

	const remote = "/data/local/tmp/epsh-bench.dex"

	start := time.Now()
	if err := adb.Push(serial, dex, remote); err != nil {
		out.Err = fmt.Errorf("cannot push the dex: %w", err)
		return out
	}
	out.Push = time.Since(start)

	defer func() { _, _ = adb.Shell(serial, "rm", "-f", remote, remote+".oat") }()

	abi := "arm64"
	if abis, err := adb.ABIList(serial); err == nil && len(abis) > 0 {
		if strings.HasPrefix(abis[0], "armeabi") {
			abi = "arm"
		}
	}

	start = time.Now()
	compileOut, err := adb.Shell(serial, "dex2oat",
		"--dex-file="+remote,
		"--oat-file="+remote+".oat",
		"--instruction-set="+abi,
		"2>&1")
	elapsed := time.Since(start)

	lower := strings.ToLower(compileOut)
	switch {
	case err != nil:
		out.CompileWhy = "dex2oat could not be run: " + err.Error()
	case strings.Contains(lower, "not found") || strings.Contains(lower, "permission denied"):
		out.CompileWhy = "dex2oat is not available to the shell on this hub"
	case strings.Contains(lower, "error") || strings.Contains(lower, "failed"):
		out.CompileWhy = "dex2oat reported: " + firstLine(compileOut)
		if strings.TrimSpace(compileOut) == "" {
			out.CompileWhy = "dex2oat failed without saying why"
		}
	default:
		out.Compile = elapsed
		out.CompileOK = true
	}

	if out.CompileOK {
		out.StubBytes, out.Overhead = measureOverhead(serial, apkPath, abi)
	}

	return out
}

// measureOverhead compiles a near-empty dex, so the sample's number can be read
// as startup plus real work rather than as one opaque figure.
func measureOverhead(serial, apkPath, abi string) (int64, time.Duration) {
	dex, size, err := extractStubDex(apkPath)
	if err != nil {
		return 0, 0
	}
	defer os.Remove(dex)

	const remote = "/data/local/tmp/epsh-bench-stub.dex"

	if err := adb.Push(serial, dex, remote); err != nil {
		return 0, 0
	}
	defer func() { _, _ = adb.Shell(serial, "rm", "-f", remote, remote+".oat") }()

	start := time.Now()
	out, err := adb.Shell(serial, "dex2oat",
		"--dex-file="+remote, "--oat-file="+remote+".oat",
		"--instruction-set="+abi, "2>&1")
	if err != nil || strings.Contains(strings.ToLower(out), "error") {
		return 0, 0
	}

	return size, time.Since(start)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// minSampleDex is the smallest dex worth timing. A multidex build contains
// stubs of a kilobyte or two, and compiling one of those measures dex2oat
// starting up rather than dex2oat working.
const minSampleDex = 64 << 10

// extractSampleDex writes out a dex the size a team's own code would be: the
// smallest one that is not a stub.
func extractSampleDex(apkPath string) (string, int64, error) {
	return extractDex(apkPath, func(best, entry *zip.File) bool {
		if entry.UncompressedSize64 < minSampleDex {
			return false
		}
		return best == nil || entry.UncompressedSize64 < best.UncompressedSize64
	})
}

// extractStubDex writes out the smallest dex, for measuring what dex2oat costs
// before it compiles anything.
func extractStubDex(apkPath string) (string, int64, error) {
	return extractDex(apkPath, func(best, entry *zip.File) bool {
		return best == nil || entry.UncompressedSize64 < best.UncompressedSize64
	})
}

// extractDex writes out whichever dex the chooser prefers.
func extractDex(apkPath string, better func(best, entry *zip.File) bool) (string, int64, error) {
	archive, err := zip.OpenReader(apkPath)
	if err != nil {
		return "", 0, fmt.Errorf("cannot read the APK: %w", err)
	}
	defer archive.Close()

	var chosen, smallest *zip.File
	for _, entry := range archive.File {
		if !strings.HasSuffix(entry.Name, ".dex") {
			continue
		}
		if smallest == nil || entry.UncompressedSize64 < smallest.UncompressedSize64 {
			smallest = entry
		}
		if better(chosen, entry) {
			chosen = entry
		}
	}

	if chosen == nil {
		chosen = smallest
	}
	if chosen == nil {
		return "", 0, fmt.Errorf("the APK has no dex files")
	}

	reader, err := chosen.Open()
	if err != nil {
		return "", 0, err
	}
	defer reader.Close()

	out, err := os.CreateTemp("", "epsh-bench-*.dex")
	if err != nil {
		return "", 0, err
	}
	defer out.Close()

	written, err := io.Copy(out, reader)
	if err != nil {
		os.Remove(out.Name())
		return "", 0, err
	}

	return out.Name(), written, nil
}

// SaveReport writes a report into the project so it can be reread or compared.
func SaveReport(projectRoot, body string) (string, error) {
	dir := filepath.Join(projectRoot, "epsh-reports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	path := filepath.Join(dir, time.Now().Format("2006-01-02-150405")+".md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", err
	}

	return path, nil
}
