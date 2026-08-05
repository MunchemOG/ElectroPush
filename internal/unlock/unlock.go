// Package unlock hides features behind a key sequence entered once on the
// settings screen. The result is remembered for that device.
//
// This is obscurity, not security. The sequence is in a public repository and
// is a famous one besides. It keeps a feature out of sight of someone who just
// installed pusher; it protects nothing.
package unlock

import "github.com/andreibanu/pusher/internal/config"

// Sequence is the Konami code, in the key names bubbletea reports.
var Sequence = []string{
	"up", "up", "down", "down",
	"left", "right", "left", "right",
	"b", "a", "enter",
}

// token is what a device that has entered the sequence stores. Opaque so the
// config file offers nothing to flip.
const token = "5943a6ad7bb3e150"

// failure is the KMP prefix function for Sequence. The sequence overlaps itself
// (it opens "up up"), so a wrong key cannot simply reset to zero: pressing up
// three times has to leave you two keys in, not one. Falling back through this
// table keeps step equal to the longest prefix of Sequence that is a suffix of
// everything typed so far, which is the only bookkeeping that survives stutter.
var failure = buildFailure(Sequence)

func buildFailure(seq []string) []int {
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

// Advance walks the sequence one key at a time, returning how far along the
// next keystroke leaves you.
func Advance(step int, key string) (next int, done bool) {
	if step < 0 || step >= len(Sequence) {
		step = 0
	}

	for {
		if key == Sequence[step] {
			step++
			break
		}
		if step == 0 {
			break
		}
		step = failure[step-1]
	}

	return step, step == len(Sequence)
}

func Unlocked() bool {
	return config.GetUnlockToken() == token
}

func Remember() error {
	return config.SetUnlockToken(token)
}
