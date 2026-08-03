// Package delta splits a file at content-defined boundaries. Fixed-size blocks
// would be useless here: inserting bytes near the start of an APK shifts every
// later block, so all of them would look changed and the whole file would be
// resent. Content-defined cut points resynchronise after an edit.
package delta

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

const (
	MinChunk = 128 << 10

	MaxChunk = 1 << 20

	cutMask = (1 << 18) - 1
)

var gear [256]uint64

// Generated deterministically on purpose. This table decides where chunk
// boundaries fall, so changing it moves every boundary and instantly
// invalidates the cache on every robot.
func init() {
	state := uint64(0x9E3779B97F4A7C15)
	for i := range gear {
		state += 0x9E3779B97F4A7C15
		z := state
		z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
		z = (z ^ (z >> 27)) * 0x94D049BB133111EB
		gear[i] = z ^ (z >> 31)
	}
}

type Chunk struct {
	Hash string

	Offset int64

	Size int64
}

func (c Chunk) Filename() string {
	return c.Hash + ".chunk"
}

func Split(data []byte) []Chunk {
	total := int64(len(data))
	if total == 0 {
		return nil
	}

	chunks := make([]Chunk, 0, total/(MaxChunk/4)+1)

	var start int64
	for start < total {
		end := cutPoint(data, start, total)
		chunks = append(chunks, Chunk{
			Hash:   hashOf(data[start:end]),
			Offset: start,
			Size:   end - start,
		})
		start = end
	}

	return chunks
}

func cutPoint(data []byte, start, total int64) int64 {
	end := start + MinChunk
	if end >= total {
		return total
	}

	limit := start + MaxChunk
	if limit > total {
		limit = total
	}

	var h uint64
	for end < limit {
		h = (h << 1) + gear[data[end]]
		end++
		if h&cutMask == 0 {
			break
		}
	}

	return end
}

func hashOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}

func SplitFile(path string) ([]Chunk, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read %s: %w", path, err)
	}

	return Split(data), data, nil
}

// Lists every occurrence, including duplicates: the device rebuilds the file by
// concatenating these in order, so dropping a repeat would truncate the APK.
func Manifest(chunks []Chunk) string {
	var b strings.Builder
	for _, c := range chunks {
		b.WriteString(c.Filename())
		b.WriteByte('\n')
	}
	return b.String()
}

func Missing(chunks []Chunk, present map[string]bool) []Chunk {
	var missing []Chunk
	seen := make(map[string]bool, len(chunks))

	for _, c := range chunks {
		if present[c.Hash] || seen[c.Hash] {
			continue
		}
		seen[c.Hash] = true
		missing = append(missing, c)
	}

	return missing
}

func TotalSize(chunks []Chunk) int64 {
	var total int64
	for _, c := range chunks {
		total += c.Size
	}
	return total
}

func Unreferenced(chunks []Chunk, present map[string]bool) []string {
	needed := make(map[string]bool, len(chunks))
	for _, c := range chunks {
		needed[c.Hash] = true
	}

	var stale []string
	for hash := range present {
		if !needed[hash] {
			stale = append(stale, hash)
		}
	}

	return stale
}
