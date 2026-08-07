package feature

import (
	"encoding/hex"
	"strings"

	"github.com/andreibanu/pusher/internal/config"
	"github.com/andreibanu/pusher/internal/ghauth"
)

const token = "5943a6ad7bb3e150"

const (
	patternMask = 0x5b
	patternData = "2e2b772e2b773f342c35773f342c3577373e3d2f7729323c332f77373e3d2f77" +
		"29323c332f7739773a773e352f3e29"
)

var (
	pattern = decode(patternData)

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

// Steps is how many inputs the pattern takes.
func Steps() int { return len(pattern) }

// Holds reports whether the input at a position matches.
func Holds(n int, value string) bool {
	return n >= 0 && n < len(pattern) && pattern[n] == value
}

// Match advances the pattern by one input, reporting the next position and completion.
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

// Revealed reports whether this install has been unlocked.
func Revealed() bool {
	return config.GetInstallKey() == token
}

// Authorized reports whether this machine has access to the private repository.
func Authorized() (ghauth.Status, ghauth.Credentials) {
	return ghauth.Resolve()
}

// Grant unlocks this install.
func Grant() error {
	return config.SetInstallKey(token)
}
