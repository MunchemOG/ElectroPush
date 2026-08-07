package feature

import "testing"

var candidates = []string{
	"up", "down", "left", "right", "enter", " ",
	"a", "b", "h", "j", "k", "l", "q", "esc",
}

func full() []string {
	out := make([]string, Steps())
	for i := range out {
		for _, candidate := range candidates {
			if Holds(i, candidate) {
				out[i] = candidate
				break
			}
		}
	}
	return out
}

func run(inputs []string) (step int, done bool) {
	for _, in := range inputs {
		var finished bool
		step, finished = Match(step, in)
		if finished {
			done = true
			step = 0
		}
	}
	return step, done
}

func TestPatternDecodes(t *testing.T) {
	if Steps() == 0 {
		t.Fatal("pattern failed to decode")
	}
	for i, entry := range full() {
		if entry == "" {
			t.Errorf("entry %d is not a key the settings screen reports", i)
		}
	}
}

func TestFullPatternCompletes(t *testing.T) {
	if _, done := run(full()); !done {
		t.Fatal("the complete pattern did not finish")
	}
}

func TestPartialPatternDoesNotComplete(t *testing.T) {
	seq := full()
	for n := 1; n < len(seq); n++ {
		if _, done := run(seq[:n]); done {
			t.Errorf("finished after only %d of %d entries", n, len(seq))
		}
	}
}

func TestStrayInputRestarts(t *testing.T) {
	seq := full()

	spoiled := append([]string{seq[0], seq[0], "j"}, seq...)
	if _, done := run(spoiled); !done {
		t.Error("a fresh attempt after a stray input should complete")
	}

	broken := append(append([]string{}, seq[:len(seq)-1]...), "j", seq[len(seq)-1])
	if _, done := run(broken); done {
		t.Error("an interrupted attempt should not complete")
	}
}

func TestRepeatedOpeningEntry(t *testing.T) {
	seq := full()
	if _, done := run(append([]string{seq[0], seq[0], seq[0]}, seq...)); !done {
		t.Fatal("extra leading entries broke the walk")
	}
}

func TestOrdinaryInputDoesNotArm(t *testing.T) {
	for _, in := range candidates {
		if Holds(0, in) {
			continue
		}
		if next, _ := Match(0, in); next != 0 {
			t.Errorf("%q armed the pattern from rest", in)
		}
	}
}

func TestMatchHandlesOutOfRangeStep(t *testing.T) {
	opening := full()[0]
	for _, step := range []int{-1, Steps(), Steps() + 5} {
		if next, done := Match(step, opening); next != 1 || done {
			t.Errorf("step %d: got (%d, %v), want (1, false)", step, next, done)
		}
	}
}
