package unlock

import "testing"

// run feeds keys through Advance and reports the step reached and whether the
// sequence ever completed.
func run(keys []string) (step int, done bool) {
	for _, key := range keys {
		var finished bool
		step, finished = Advance(step, key)
		if finished {
			done = true
			step = 0
		}
	}
	return step, done
}

func TestSequenceUnlocks(t *testing.T) {
	if _, done := run(Sequence); !done {
		t.Fatal("the full sequence did not unlock")
	}
}

func TestPartialSequenceDoesNotUnlock(t *testing.T) {
	for n := 1; n < len(Sequence); n++ {
		if _, done := run(Sequence[:n]); done {
			t.Errorf("unlocked after only %d of %d keys", n, len(Sequence))
		}
	}
}

func TestWrongKeyRestarts(t *testing.T) {
	// A stray key mid-sequence loses the progress made so far.
	spoiled := append([]string{"up", "up", "down", "j"}, Sequence...)
	if _, done := run(spoiled); !done {
		t.Fatal("a fresh sequence after a wrong key should still unlock")
	}

	if _, done := run([]string{"up", "up", "down", "down", "left", "right", "j", "left", "right", "b", "a", "enter"}); done {
		t.Error("a sequence interrupted partway should not unlock")
	}
}

// The first key doubles as the restart, so leaning on it must not desync the
// walk: three ups then the real sequence still has to work.
func TestRepeatedFirstKey(t *testing.T) {
	if _, done := run(append([]string{"up", "up", "up"}, Sequence...)); !done {
		t.Fatal("extra leading ups broke the sequence")
	}
}

func TestNavigationKeysStayUsable(t *testing.T) {
	// Nothing but the sequence's own first key may start it, or ordinary
	// browsing would silently arm the unlock.
	for _, key := range []string{"down", "j", "k", "enter", "q", "b", "a", "right"} {
		if next, _ := Advance(0, key); next != 0 {
			t.Errorf("%q started the sequence from rest", key)
		}
	}
}

func TestAdvanceHandlesOutOfRangeStep(t *testing.T) {
	for _, step := range []int{-1, len(Sequence), len(Sequence) + 5} {
		if next, done := Advance(step, "up"); next != 1 || done {
			t.Errorf("step %d: got (%d, %v), want (1, false)", step, next, done)
		}
	}
}
