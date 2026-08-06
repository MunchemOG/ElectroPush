package adb

import (
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/andreibanu/pusher/internal/delta"
)

const (
	remoteRoot     = "/data/local/tmp/pusher"
	remoteCacheDir = remoteRoot + "/cache"
	remoteManifest = remoteRoot + "/manifest"
	remoteStaging  = remoteRoot + "/incoming"
	remoteDeltaAPK = remoteRoot + "/app.apk"

	spaceFactor = 3

	okMarker = "PUSHER_OK"
)

// ErrDeltaUnavailable means a delta transfer cannot be used this time.
type ErrDeltaUnavailable struct{ Reason string }

// Error explains why the delta transfer was unavailable.
func (e ErrDeltaUnavailable) Error() string { return e.Reason }

// DeltaResult is what a delta transfer sent and what it skipped.
type DeltaResult struct {
	TotalChunks  int
	SentChunks   int
	SentBytes    int64
	SkippedBytes int64

	chunks []delta.Chunk
}

func deltaInstall(serial, apkPath string) (*DeltaResult, error) {
	chunks, data, err := delta.SplitFile(apkPath)
	if err != nil {
		return nil, ErrDeltaUnavailable{err.Error()}
	}
	if len(chunks) == 0 {
		return nil, ErrDeltaUnavailable{"APK is empty"}
	}

	if err := ensureSpace(serial, int64(len(data))); err != nil {
		return nil, err
	}

	if _, err := run(serial, "shell", "mkdir -p "+remoteCacheDir); err != nil {
		return nil, ErrDeltaUnavailable{"cannot create cache directory: " + err.Error()}
	}

	present := listCachedChunks(serial)
	missing := delta.Missing(chunks, present)

	result := &DeltaResult{
		TotalChunks: len(chunks),
		SentChunks:  len(missing),
		SentBytes:   delta.TotalSize(missing),
		chunks:      chunks,
	}
	result.SkippedBytes = int64(len(data)) - result.SentBytes

	if len(present) == 0 {
		fmt.Printf("[*] No cache on the hub yet - sending all %d chunks this time.\n", len(chunks))
		fmt.Println("    Future pushes will only send what changed.")
	} else {
		fmt.Printf("[*] Hub already has %d of %d chunks (%.1f MB reused)\n",
			len(chunks)-len(missing), len(chunks), mb(result.SkippedBytes))
	}

	if len(missing) > 0 {
		fmt.Printf("[*] Sending %d chunks (%.1f MB)...\n", len(missing), mb(result.SentBytes))
		if err := pushChunks(serial, data, missing); err != nil {
			return nil, err
		}
	}

	if err := pushManifest(serial, chunks); err != nil {
		return nil, err
	}

	if err := reassemble(serial); err != nil {
		return nil, err
	}

	if err := verifyRemote(serial, data); err != nil {
		return nil, err
	}

	return result, nil
}

func listCachedChunks(serial string) map[string]bool {
	present := map[string]bool{}

	out, err := run(serial, "shell", "ls -1 "+remoteCacheDir+" 2>/dev/null")
	if err != nil {
		return present
	}

	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if hash, ok := strings.CutSuffix(name, ".chunk"); ok && hash != "" {
			present[hash] = true
		}
	}

	return present
}

func pushChunks(serial string, data []byte, missing []delta.Chunk) error {
	stagingDir, err := os.MkdirTemp("", "pusher-chunks-")
	if err != nil {
		return ErrDeltaUnavailable{"cannot create staging directory: " + err.Error()}
	}
	defer os.RemoveAll(stagingDir)

	for _, c := range missing {
		path := filepath.Join(stagingDir, c.Filename())
		if err := os.WriteFile(path, data[c.Offset:c.Offset+c.Size], 0644); err != nil {
			return ErrDeltaUnavailable{"cannot stage chunk: " + err.Error()}
		}
	}

	if _, err := run(serial, "shell", "rm -rf "+remoteStaging); err != nil {
		return ErrDeltaUnavailable{"cannot clear staging directory: " + err.Error()}
	}

	pushArgs := []string{"push", stagingDir, remoteStaging}
	if serial != "" {
		pushArgs = append([]string{"-s", serial}, pushArgs...)
	}

	cmd := exec.Command("adb", pushArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return ErrDeltaUnavailable{"chunk push failed: " + err.Error()}
	}

	move := fmt.Sprintf("mv %s/*.chunk %s/ 2>/dev/null; mv %s/*/*.chunk %s/ 2>/dev/null; rm -rf %s; echo %s",
		remoteStaging, remoteCacheDir, remoteStaging, remoteCacheDir, remoteStaging, okMarker)
	if _, err := run(serial, "shell", move); err != nil {
		return ErrDeltaUnavailable{"cannot move chunks into cache: " + err.Error()}
	}

	cached := listCachedChunks(serial)
	for _, c := range missing {
		if !cached[c.Hash] {
			return ErrDeltaUnavailable{"chunks did not reach the cache on the hub"}
		}
	}

	return nil
}

func pushManifest(serial string, chunks []delta.Chunk) error {
	file, err := os.CreateTemp("", "pusher-manifest-")
	if err != nil {
		return ErrDeltaUnavailable{"cannot create manifest: " + err.Error()}
	}
	defer os.Remove(file.Name())

	if _, err := file.WriteString(delta.Manifest(chunks)); err != nil {
		file.Close()
		return ErrDeltaUnavailable{"cannot write manifest: " + err.Error()}
	}
	file.Close()

	if _, err := run(serial, "push", file.Name(), remoteManifest); err != nil {
		return ErrDeltaUnavailable{"cannot push manifest: " + err.Error()}
	}

	return nil
}

func reassemble(serial string) error {
	script := fmt.Sprintf("cd %s && cat $(cat %s) > %s && echo %s",
		remoteCacheDir, remoteManifest, remoteDeltaAPK, okMarker)

	out, err := run(serial, "shell", script)
	if err != nil {
		return ErrDeltaUnavailable{"reassembly failed: " + err.Error()}
	}

	if !strings.Contains(out, okMarker) {
		return ErrDeltaUnavailable{"reassembly did not complete: " + strings.TrimSpace(out)}
	}

	return nil
}

func verifyRemote(serial string, data []byte) error {
	sha := sha1.Sum(data)
	md5sum := md5.Sum(data)

	candidates := []struct {
		command string
		want    string
	}{
		{"sha1sum", hex.EncodeToString(sha[:])},
		{"md5sum", hex.EncodeToString(md5sum[:])},
	}

	for _, candidate := range candidates {
		out, err := run(serial, "shell", candidate.command+" "+remoteDeltaAPK)
		if err != nil {
			continue
		}

		fields := strings.Fields(strings.TrimSpace(out))
		if len(fields) == 0 {
			continue
		}

		got := strings.ToLower(fields[0])
		if len(got) != len(candidate.want) {
			continue
		}

		if got != candidate.want {
			return ErrDeltaUnavailable{fmt.Sprintf(
				"rebuilt APK does not match (%s %s != %s)", candidate.command, got, candidate.want)}
		}

		return nil
	}

	return ErrDeltaUnavailable{"could not checksum the rebuilt APK on the device"}
}

func pruneCache(serial string, chunks []delta.Chunk) {
	stale := delta.Unreferenced(chunks, listCachedChunks(serial))
	if len(stale) == 0 {
		return
	}

	const batch = 200
	for start := 0; start < len(stale); start += batch {
		end := start + batch
		if end > len(stale) {
			end = len(stale)
		}

		var b strings.Builder
		b.WriteString("cd " + remoteCacheDir + " && rm -f")
		for _, hash := range stale[start:end] {
			b.WriteString(" " + hash + ".chunk")
		}

		if _, err := run(serial, "shell", b.String()); err != nil {
			return
		}
	}
}

func ensureSpace(serial string, apkSize int64) error {
	free, ok := freeBytes(serial, "/data")
	if !ok {

		return nil
	}

	if need := apkSize * spaceFactor; free < need {
		return ErrDeltaUnavailable{fmt.Sprintf(
			"not enough free space on the hub (%.0f MB free, needs about %.0f MB)",
			mb(free), mb(need))}
	}

	return nil
}

func freeBytes(serial, path string) (int64, bool) {
	out, err := run(serial, "shell", "df -k "+path)
	if err != nil {
		return 0, false
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		return 0, false
	}

	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return 0, false
	}

	blocks, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return 0, false
	}

	return blocks * 1024, true
}

func mb(bytes int64) float64 {
	return float64(bytes) / (1024 * 1024)
}

func installDelta(serial, apkPath string) error {
	start := time.Now()

	result, err := deltaInstall(serial, apkPath)
	if err != nil {
		return err
	}

	elapsed := time.Since(start).Seconds()
	if elapsed > 0 && result.SentBytes > 0 {
		fmt.Printf("[OK] Transferred %.1f MB in %.1fs (%.1f MB/s), reused %.1f MB\n",
			mb(result.SentBytes), elapsed, mb(result.SentBytes)/elapsed, mb(result.SkippedBytes))
	} else {
		fmt.Printf("[OK] Nothing to transfer - the hub already had every chunk (%.1fs)\n", elapsed)
	}

	fmt.Println("[*] Installing...")
	if err := runInstall(serial, remoteDeltaAPK); err != nil {
		return err
	}

	pruneCache(serial, result.chunks)

	return nil
}
