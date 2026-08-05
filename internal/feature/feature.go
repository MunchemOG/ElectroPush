// Package feature reports which optional menu entries and commands an install
// exposes. Everything here is presentation: nothing it gates is privileged, and
// nothing it hides is protected.
package feature

import (
	"encoding/hex"
	"strings"

	"github.com/andreibanu/pusher/internal/config"
	"github.com/andreibanu/pusher/internal/ghauth"
)

// token marks a device that has turned the optional surfaces on. Opaque so the
// config file offers nothing to flip.
const token = "5943a6ad7bb3e150"

// pattern is held encoded rather than spelled out, so reading the source does
// not hand it over at a glance. Reversible by anyone who cares to.
const (
	patternMask = 0x5b
	patternData = "2e2b772e2b773f342c35773f342c3577373e3d2f7729323c332f77373e3d2f77" +
		"29323c332f7739773a773e352f3e29"
)

var (
	pattern = decode(patternData)

	// prefix is the KMP prefix function for pattern. The pattern overlaps
	// itself, so a mismatch cannot reset to zero without discarding progress
	// that is still valid. Falling back through this table keeps step equal to
	// the longest prefix of pattern that is also a suffix of everything entered
	// so far, which is the only bookkeeping that survives a stutter.
	prefix = buildPrefix(pattern)
)

func decode(encoded string) []string {
	raw, err := hex.DecodeString(encoded)
	if err != nil {
		return nil
	}

	for i := range raw {
		raw[i] ^= patternMask
	}
	return strings.Split(string(raw), ",")
}

func buildPrefix(seq []string) []int {
	table := make([]int, len(seq))

	k := 0
	for i := 1; i < len(seq); i++ {
		for k > 0 && seq[i] != seq[k] {
			k = table[k-1]
		}
		if seq[i] == seq[k] {
			k++
		}
		table[i] = k
	}

	return table
}

// Steps is how many entries the pattern has.
func Steps() int { return len(pattern) }

// Holds reports whether the pattern's nth entry is value.
func Holds(n int, value string) bool {
	return n >= 0 && n < len(pattern) && pattern[n] == value
}

// Match advances one entry at a time, returning how far along the next input
// leaves you and whether it completed the pattern.
func Match(step int, value string) (next int, done bool) {
	if len(pattern) == 0 {
		return 0, false
	}
	if step < 0 || step >= len(pattern) {
		step = 0
	}

	for {
		if value == pattern[step] {
			step++
			break
		}
		if step == 0 {
			break
		}
		step = prefix[step-1]
	}

	return step, step == len(pattern)
}

// Revealed reports whether the optional surfaces are shown at all. This is a
// local flag and nothing more: it decides what appears in a menu, never what
// anyone is allowed to do. Cheap enough to consult on every invocation.
func Revealed() bool {
	return config.GetInstallKey() == token
}

// Authorized reports real access to the private blob repository, which is what
// actually gates the library. Kept separate from Revealed because a flag in a
// config file is something anyone can set, and this is not.
func Authorized() (ghauth.Status, ghauth.Credentials) {
	return ghauth.Resolve()
}

// Grant turns them on for this device.
func Grant() error {
	return config.SetInstallKey(token)
}
