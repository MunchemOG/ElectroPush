package robotcfg

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// backupDir holds the robot's copy of anything about to be overwritten. It is
// inside the configuration directory so it travels with a checkout, and dotted
// so it stays out of the listing.
const backupDir = ".pusher-backup"

// Store is a directory of configuration files in a project.
type Store struct {
	Dir string
}

// NewStore points at a directory. The directory is created when something is
// first written to it, not here: listing a project that has never pulled a
// configuration should not leave an empty directory behind.
func NewStore(dir string) *Store {
	return &Store{Dir: dir}
}

// Path is where a named configuration is kept.
func (s *Store) Path(name string) string {
	return filepath.Join(s.Dir, name+Ext)
}

// Names lists the configurations in the directory, sorted.
func (s *Store) Names() ([]string, error) {
	entries, err := os.ReadDir(s.Dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", s.Dir, err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), Ext) {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), Ext))
	}

	sort.Strings(names)
	return names, nil
}

// Read returns one configuration from the directory.
func (s *Store) Read(name string) ([]byte, error) {
	data, err := os.ReadFile(s.Path(name))
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("no configuration called %q in %s", name, s.Dir)
	}
	if err != nil {
		return nil, err
	}
	return data, nil
}

// Write saves a configuration, creating the directory if it is not there yet.
func (s *Store) Write(name string, data []byte) error {
	if err := CheckName(name); err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return fmt.Errorf("cannot create %s: %w", s.Dir, err)
	}
	if err := os.WriteFile(s.Path(name), data, 0o644); err != nil {
		return fmt.Errorf("cannot write %s: %w", s.Path(name), err)
	}
	return nil
}

// Remove deletes a configuration from the project, keeping a copy first: this
// is the only way to lose an edit that was never pushed.
func (s *Store) Remove(name string) error {
	data, err := s.Read(name)
	if err != nil {
		return err
	}

	if _, err := s.Backup(name, data); err != nil {
		return err
	}

	if err := os.Remove(s.Path(name)); err != nil {
		return fmt.Errorf("cannot delete %s: %w", s.Path(name), err)
	}

	return nil
}

// Has reports whether the directory holds that configuration.
func (s *Store) Has(name string) bool {
	_, err := os.Stat(s.Path(name))
	return err == nil
}

// Backup keeps a copy of what is about to be overwritten.
//
// Overwriting the robot's configuration from a laptop is the one thing here
// that destroys work nobody can get back: the change may have been made on the
// Driver Station between the pull and the push. The copy costs one file.
func (s *Store) Backup(name string, data []byte) (string, error) {
	dir := filepath.Join(s.Dir, backupDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create %s: %w", dir, err)
	}

	path := filepath.Join(dir, name+Ext)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("cannot write %s: %w", path, err)
	}

	return path, nil
}

// Same reports whether two configurations are byte for byte identical.
//
// Byte comparison rather than a parse: a file that differs only in formatting
// still differs, and reporting it as unchanged would hide a real edit.
func Same(a, b []byte) bool {
	return bytes.Equal(a, b)
}
