package delta

import (
	"bytes"
	"math/rand"
	"testing"
)

func buildAPK(t *testing.T, size int) []byte {
	t.Helper()

	data := make([]byte, size)
	rng := rand.New(rand.NewSource(1))
	if _, err := rng.Read(data); err != nil {
		t.Fatal(err)
	}
	return data
}

func hashSet(chunks []Chunk) map[string]bool {
	set := make(map[string]bool, len(chunks))
	for _, c := range chunks {
		set[c.Hash] = true
	}
	return set
}

func TestSplitCoversTheWholeFileInOrder(t *testing.T) {
	data := buildAPK(t, 8<<20)
	chunks := Split(data)

	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}

	var offset int64
	for i, c := range chunks {
		if c.Offset != offset {
			t.Fatalf("chunk %d starts at %d, expected %d (gap or overlap)", i, c.Offset, offset)
		}
		if c.Size <= 0 {
			t.Fatalf("chunk %d has size %d", i, c.Size)
		}

		if i < len(chunks)-1 && c.Size < MinChunk {
			t.Fatalf("chunk %d is %d bytes, below the %d minimum", i, c.Size, MinChunk)
		}
		if c.Size > MaxChunk {
			t.Fatalf("chunk %d is %d bytes, above the %d maximum", i, c.Size, MaxChunk)
		}
		offset += c.Size
	}

	if offset != int64(len(data)) {
		t.Errorf("chunks cover %d bytes, file is %d", offset, len(data))
	}
}

func TestSplitIsDeterministic(t *testing.T) {
	data := buildAPK(t, 4<<20)

	first := Manifest(Split(data))
	second := Manifest(Split(data))

	if first != second {
		t.Error("splitting the same data twice produced different chunks")
	}
}

func TestBoundariesResyncAfterAnInsertion(t *testing.T) {
	original := buildAPK(t, 16<<20)

	insertAt := 1 << 20
	modified := make([]byte, 0, len(original)+500)
	modified = append(modified, original[:insertAt]...)
	modified = append(modified, bytes.Repeat([]byte{0xAB}, 500)...)
	modified = append(modified, original[insertAt:]...)

	before := Split(original)
	after := Split(modified)

	shared := 0
	oldHashes := hashSet(before)
	for _, c := range after {
		if oldHashes[c.Hash] {
			shared++
		}
	}

	reuse := float64(shared) / float64(len(after))
	if reuse < 0.9 {
		t.Errorf("only %.0f%% of chunks were reusable after a 500-byte insertion; "+
			"boundaries are not resynchronising (fixed-size chunking would score ~0%%)",
			reuse*100)
	}

	t.Logf("%d of %d chunks reused (%.1f%%)", shared, len(after), reuse*100)
}

func TestRebuildOnlyDirtiesChunksNearTheChange(t *testing.T) {
	original := buildAPK(t, 32<<20)

	modified := append([]byte(nil), original...)
	rng := rand.New(rand.NewSource(99))

	scratch := make([]byte, 2<<20)
	rng.Read(scratch)
	copy(modified[10<<20:], scratch)

	before := Split(original)
	after := Split(modified)

	missing := Missing(after, hashSet(before))
	sent := TotalSize(missing)

	if sent > int64(6<<20) {
		t.Errorf("a 2 MB change forced %.1f MB to be resent; expected only the "+
			"chunks overlapping the edit", float64(sent)/(1<<20))
	}

	t.Logf("2 MB edit in a %d MB file resent %.1f MB across %d chunks",
		len(original)>>20, float64(sent)/(1<<20), len(missing))
}

func TestMissingSkipsCachedAndDeduplicates(t *testing.T) {
	chunks := []Chunk{
		{Hash: "a", Size: 10},
		{Hash: "b", Size: 20},
		{Hash: "a", Size: 10},
		{Hash: "c", Size: 30},
	}

	missing := Missing(chunks, map[string]bool{"b": true})

	if len(missing) != 2 {
		t.Fatalf("expected 2 chunks to send, got %d: %+v", len(missing), missing)
	}
	if missing[0].Hash != "a" || missing[1].Hash != "c" {
		t.Errorf("unexpected chunks to send: %+v", missing)
	}
	if got := TotalSize(missing); got != 40 {
		t.Errorf("a repeated chunk must only be counted once, got %d bytes", got)
	}
}

func TestManifestListsEveryOccurrenceInOrder(t *testing.T) {

	got := Manifest([]Chunk{{Hash: "aa"}, {Hash: "bb"}, {Hash: "aa"}})
	want := "aa.chunk\nbb.chunk\naa.chunk\n"

	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUnreferencedFindsStaleCacheEntries(t *testing.T) {
	chunks := []Chunk{{Hash: "keep1"}, {Hash: "keep2"}}
	present := map[string]bool{"keep1": true, "keep2": true, "old1": true, "old2": true}

	stale := Unreferenced(chunks, present)
	if len(stale) != 2 {
		t.Fatalf("expected 2 stale chunks, got %v", stale)
	}
	for _, hash := range stale {
		if hash == "keep1" || hash == "keep2" {
			t.Errorf("%q is still in use and must not be pruned", hash)
		}
	}
}

func TestSplitHandlesEmptyAndTinyFiles(t *testing.T) {
	if got := Split(nil); got != nil {
		t.Errorf("empty data should produce no chunks, got %v", got)
	}

	tiny := Split([]byte("hello"))
	if len(tiny) != 1 || tiny[0].Size != 5 {
		t.Errorf("a short file should be one chunk, got %+v", tiny)
	}
}

func TestGearTableIsStable(t *testing.T) {
	const (
		wantFirst = uint64(0x6E789E6AA1B965F4)
		wantLast  = uint64(0xCBDC6D34B7C7534D)
	)

	if gear[0] != wantFirst {
		t.Errorf("gear[0] = %#016x, want %#016x; chunk boundaries would move", gear[0], wantFirst)
	}
	if gear[255] != wantLast {
		t.Errorf("gear[255] = %#016x, want %#016x; chunk boundaries would move", gear[255], wantLast)
	}
}
