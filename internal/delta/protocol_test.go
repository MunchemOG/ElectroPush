package delta

import (
	"bytes"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

type fakeDevice struct {
	cacheDir string
	t        *testing.T
}

func newFakeDevice(t *testing.T) *fakeDevice {
	t.Helper()
	return &fakeDevice{cacheDir: t.TempDir(), t: t}
}

func (d *fakeDevice) cached() map[string]bool {
	entries, err := os.ReadDir(d.cacheDir)
	if err != nil {
		d.t.Fatal(err)
	}

	present := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if len(name) > len(".chunk") {
			present[name[:len(name)-len(".chunk")]] = true
		}
	}
	return present
}

func (d *fakeDevice) receive(data []byte, chunks []Chunk) {
	d.t.Helper()
	for _, c := range chunks {
		path := filepath.Join(d.cacheDir, c.Filename())
		if err := os.WriteFile(path, data[c.Offset:c.Offset+c.Size], 0644); err != nil {
			d.t.Fatal(err)
		}
	}
}

func (d *fakeDevice) reassemble(chunks []Chunk) []byte {
	d.t.Helper()

	var out bytes.Buffer
	for _, c := range chunks {
		piece, err := os.ReadFile(filepath.Join(d.cacheDir, c.Filename()))
		if err != nil {
			d.t.Fatalf("reassembly referenced a chunk the device does not have: %v", err)
		}
		out.Write(piece)
	}
	return out.Bytes()
}

func TestDeployCycleReproducesTheFileExactly(t *testing.T) {
	device := newFakeDevice(t)

	build1 := buildAPK(t, 12<<20)
	chunks1 := Split(build1)

	missing1 := Missing(chunks1, device.cached())
	if len(missing1) != len(chunks1) {
		t.Fatalf("first deploy should send every chunk, sent %d of %d", len(missing1), len(chunks1))
	}
	device.receive(build1, missing1)

	if got := device.reassemble(chunks1); !bytes.Equal(got, build1) {
		t.Fatal("first deploy did not reproduce the APK")
	}

	build2 := append([]byte(nil), build1...)
	scratch := make([]byte, 300<<10)
	rand.New(rand.NewSource(7)).Read(scratch)
	copy(build2[4<<20:], scratch)

	chunks2 := Split(build2)
	missing2 := Missing(chunks2, device.cached())

	if len(missing2) >= len(chunks2) {
		t.Fatalf("second deploy reused nothing: sent %d of %d chunks", len(missing2), len(chunks2))
	}
	device.receive(build2, missing2)

	got := device.reassemble(chunks2)
	if !bytes.Equal(got, build2) {
		t.Fatalf("second deploy did not reproduce the APK (%d bytes vs %d)", len(got), len(build2))
	}

	t.Logf("second deploy sent %.2f MB of %.2f MB",
		float64(TotalSize(missing2))/(1<<20), float64(len(build2))/(1<<20))
}

func TestRedeployingTheSameBuildSendsNothing(t *testing.T) {
	device := newFakeDevice(t)

	build := buildAPK(t, 6<<20)
	chunks := Split(build)
	device.receive(build, Missing(chunks, device.cached()))

	if missing := Missing(chunks, device.cached()); len(missing) != 0 {
		t.Errorf("redeploying an identical build should send nothing, got %d chunks", len(missing))
	}

	if got := device.reassemble(chunks); !bytes.Equal(got, build) {
		t.Error("redeploy did not reproduce the APK")
	}
}

func TestPruningKeepsTheCurrentBuildIntact(t *testing.T) {
	device := newFakeDevice(t)

	old := buildAPK(t, 8<<20)
	oldChunks := Split(old)
	device.receive(old, Missing(oldChunks, device.cached()))

	current := buildAPK(t, 8<<20)
	currentChunks := Split(current)
	device.receive(current, Missing(currentChunks, device.cached()))

	for _, hash := range Unreferenced(currentChunks, device.cached()) {
		if err := os.Remove(filepath.Join(device.cacheDir, hash+".chunk")); err != nil {
			t.Fatal(err)
		}
	}

	if got := device.reassemble(currentChunks); !bytes.Equal(got, current) {
		t.Fatal("pruning removed chunks the current build still needs")
	}

	if missing := Missing(currentChunks, device.cached()); len(missing) != 0 {
		t.Errorf("current build should be fully cached after pruning, %d missing", len(missing))
	}
}

func TestDuplicateContentRoundTrips(t *testing.T) {
	device := newFakeDevice(t)

	block := buildAPK(t, 1<<20)
	build := append(append([]byte(nil), block...), block...)

	chunks := Split(build)
	missing := Missing(chunks, device.cached())
	device.receive(build, missing)

	if got := device.reassemble(chunks); !bytes.Equal(got, build) {
		t.Fatalf("duplicate content did not round-trip (%d bytes vs %d)", len(got), len(build))
	}
}
