package gradle

import (
	"os"
	"path/filepath"
	"testing"
)

func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(prev) })
}

func writeWrapper(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, wrapperName())
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

// A bare "gradlew" sends exec looking through $PATH, so the result has to be
// absolute no matter which directory the wrapper was found in.
func TestDetectWrapperIsAbsolute(t *testing.T) {
	for _, depth := range []int{0, 1, 3} {
		root := t.TempDir()
		writeWrapper(t, root)

		start := root
		for i := 0; i < depth; i++ {
			start = filepath.Join(start, "sub")
			if err := os.Mkdir(start, 0755); err != nil {
				t.Fatal(err)
			}
		}
		chdir(t, start)

		got, err := DetectWrapper()
		if err != nil {
			t.Fatalf("depth %d: %v", depth, err)
		}
		if !filepath.IsAbs(got) {
			t.Errorf("depth %d: got %q, want an absolute path", depth, got)
		}
		if _, err := os.Stat(got); err != nil {
			t.Errorf("depth %d: %q does not resolve: %v", depth, got, err)
		}
	}
}

func TestDetectWrapperMissing(t *testing.T) {
	chdir(t, t.TempDir())

	if _, err := DetectWrapper(); err == nil {
		t.Fatal("expected an error when no wrapper exists")
	}
}
