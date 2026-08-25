package noise_test

import (
	"testing"

	"github.com/StevenACoffman/skillet/ratchet"
	"github.com/StevenACoffman/skillsaw/internal/noise"
)

// prompts builds n prompts, of which the first hit share vocabulary with term.
func prompts(term string, n, hit int) []string {
	out := make([]string, 0, n)
	for i := range n {
		if i < hit {
			out = append(out, "please "+term+" this thing")
			continue
		}
		out = append(out, "unrelated matters entirely different subject")
	}
	return out
}

// TestBoundContainsThePointEstimate is the property that makes the bound usable at all: a
// gate on an interval that did not contain the number it qualifies would reject results
// the sample actually supports.
func TestBoundContainsThePointEstimate(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ targets, distractors, tp, fp int }{
		{5, 5, 5, 0},
		{5, 5, 3, 2},
		{5, 5, 0, 5},
		{1, 1, 1, 0},
		{40, 40, 30, 4},
		{3, 0, 2, 0},
		{0, 3, 0, 1},
	} {
		got := ratchet.Score(
			"alpha beta gamma delta",
			prompts("alpha beta gamma", c.targets, c.tp),
			prompts("alpha beta gamma", c.distractors, c.fp),
		)
		b := noise.UtilityBound(&got)
		if got.NetUtility < b[0] || got.NetUtility > b[1] {
			t.Errorf("net_utility %+.3f outside bound [%+.3f,%+.3f] for %+v",
				got.NetUtility, b[0], b[1], c)
		}
	}
}

// TestNoPromptsResolvesNothing pins the case a division would otherwise turn into a
// confident zero. Scoring a skill with no test-prompts measures nothing, and nothing must
// not clear a floor of zero.
func TestNoPromptsResolvesNothing(t *testing.T) {
	t.Parallel()
	empty := ratchet.Score("alpha beta", nil, nil)
	b := noise.UtilityBound(&empty)
	if b != [2]float64{-1, 1} {
		t.Errorf("bound = %v, want the full range; no sample resolves nothing", b)
	}
	if v := noise.Gate(b, 0); v != noise.VerdictUnresolved {
		t.Errorf("Gate = %q, want %q for a skill with no prompts", v, noise.VerdictUnresolved)
	}
}

// TestGateRefusesWhatTheSampleCannotSeparate is the reason this package exists. The third
// case is the motivating one: a positive point estimate whose bound straddles the floor.
// Gating on the estimate alone passes it, which reports a coin flip as evidence.
func TestGateRefusesWhatTheSampleCannotSeparate(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		bound [2]float64
		floor float64
		want  noise.Verdict
	}{
		{"wholly above", [2]float64{0.10, 0.60}, 0, noise.VerdictClears},
		{"wholly below", [2]float64{-0.60, -0.10}, 0, noise.VerdictBelow},
		{"positive estimate, bound straddles", [2]float64{-0.05, 0.24}, 0, noise.VerdictUnresolved},
		{"touching the floor from above", [2]float64{0, 0.5}, 0, noise.VerdictClears},
		{"hi exactly at the floor", [2]float64{-0.5, 0}, 0, noise.VerdictUnresolved},
		{"clears a raised floor", [2]float64{0.55, 0.90}, 0.5, noise.VerdictClears},
		{"straddles a raised floor", [2]float64{0.45, 0.90}, 0.5, noise.VerdictUnresolved},
		{"estimate above a raised floor, bound below", [2]float64{0.10, 0.49}, 0.5, noise.VerdictBelow},
	} {
		if got := noise.Gate(c.bound, c.floor); got != c.want {
			t.Errorf("%s: Gate(%v, %.2f) = %q, want %q", c.name, c.bound, c.floor, got, c.want)
		}
	}
}

// TestASmallPerfectSampleStillStraddlesZero grounds the table in a real report rather than
// in numbers chosen to make the point. Two prompts that both classify correctly still leave
// a bound wide enough to include zero, which is why the estimate alone is not a gate.
func TestASmallPerfectSampleStillStraddlesZero(t *testing.T) {
	t.Parallel()
	small := ratchet.Score(
		"alpha beta gamma",
		prompts("alpha beta gamma", 1, 1),
		prompts("zeta", 1, 0),
	)
	if small.NetUtility <= 0 {
		t.Fatalf("net_utility = %+.2f, want a positive estimate for the case to mean anything",
			small.NetUtility)
	}
	if v := noise.Gate(noise.UtilityBound(&small), 0); v != noise.VerdictUnresolved {
		t.Errorf("Gate = %q for net_utility %+.2f over %d prompts, want %q",
			v, small.NetUtility, small.Targets+small.Distractors, noise.VerdictUnresolved)
	}
}

// TestALargeCleanSampleClears is the other half of the pair. Without it the gate could be
// stuck at "unresolved" for every input and every other test here would still pass.
func TestALargeCleanSampleClears(t *testing.T) {
	t.Parallel()
	big := ratchet.Score(
		"alpha beta gamma",
		prompts("alpha beta gamma", 40, 38),
		prompts("zeta", 40, 0),
	)
	if v := noise.Gate(noise.UtilityBound(&big), 0); v != noise.VerdictClears {
		t.Errorf("Gate = %q over %d prompts at net_utility %+.2f, want %q; a gate that never "+
			"clears is not a gate", v, big.Targets+big.Distractors, big.NetUtility, noise.VerdictClears)
	}
}

// TestVerdictValidIsTotal keeps Valid honest about the set it guards.
func TestVerdictValidIsTotal(t *testing.T) {
	t.Parallel()
	for _, v := range []noise.Verdict{
		noise.VerdictUnknown, noise.VerdictClears, noise.VerdictBelow, noise.VerdictUnresolved,
	} {
		if !v.Valid() {
			t.Errorf("Valid() = false for the defined verdict %q", v)
		}
	}
	if noise.Verdict("passed").Valid() {
		t.Error("Valid() = true for an undefined verdict")
	}
}
