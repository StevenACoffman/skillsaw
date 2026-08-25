package noise_test

import (
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/noise"
)

// rep returns n copies of x, the shape a judge run produces when every case scores alike.
func rep(x float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = x
	}
	return out
}

// TestMeanIntervalContainsTheMean is the property that makes the interval usable: one that
// excluded the number it qualifies would contradict the report it appears next to.
func TestMeanIntervalContainsTheMean(t *testing.T) {
	t.Parallel()
	cases := map[string][]float64{
		"identical":     {0.8, 0.8, 0.8},
		"spread":        {0.1, 0.5, 0.9},
		"at the top":    {1, 1, 1, 1},
		"at the bottom": {0, 0, 0, 0},
		"two":           {0.4, 0.6},
		"many":          {0.7, 0.8, 0.9, 0.6, 0.75, 0.85, 0.7, 0.8, 0.65, 0.9},
	}
	for name, xs := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var sum float64
			for _, x := range xs {
				sum += x
			}
			mean := sum / float64(len(xs))
			iv := noise.MeanInterval(xs)
			if mean < iv[0] || mean > iv[1] {
				t.Errorf("mean %.3f outside interval [%.3f,%.3f]", mean, iv[0], iv[1])
			}
			if iv[0] < 0 || iv[1] > 1 {
				t.Errorf("interval [%.3f,%.3f] leaves [0,1]; soft scores are bounded", iv[0], iv[1])
			}
		})
	}
}

// TestOneObservationResolvesNothing pins the degenerate end. A single case has no
// dispersion to measure and no second point to disagree with, so anything narrower than
// the whole scale would be invented rather than measured.
func TestOneObservationResolvesNothing(t *testing.T) {
	t.Parallel()
	for name, xs := range map[string][]float64{"none": nil, "one": {0.9}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if iv := noise.MeanInterval(xs); iv != [2]float64{0, 1} {
				t.Errorf("interval = %v, want the whole scale", iv)
			}
		})
	}
}

// TestIdenticalScoresDoNotPinTheMean is the case the rule-of-three floor exists for and the
// common one in practice: checks tend to all pass or all fail, so the observed dispersion is
// zero and the t term alone would report an exactly pinned mean from three samples.
func TestIdenticalScoresDoNotPinTheMean(t *testing.T) {
	t.Parallel()
	iv := noise.MeanInterval(rep(0.8, 3))
	if width := iv[1] - iv[0]; width < 0.5 {
		t.Errorf("three identical scores gave interval %v (width %.2f); zero observed "+
			"dispersion is not evidence of zero true dispersion", iv, width)
	}
}

// TestTheFloorRelaxesAsCasesAccumulate is the other half. A floor that never stopped binding
// would make every base unresolved forever, and a signal that never fires is not a signal.
func TestTheFloorRelaxesAsCasesAccumulate(t *testing.T) {
	t.Parallel()
	wide := noise.MeanInterval(rep(0.8, 3))
	narrow := noise.MeanInterval(rep(0.8, 40))
	if narrow[1]-narrow[0] >= wide[1]-wide[0] {
		t.Errorf("40 cases gave %v, 3 cases gave %v; more evidence must narrow the interval",
			narrow, wide)
	}
	if width := narrow[1] - narrow[0]; width > 0.2 {
		t.Errorf("40 identical scores still span %.2f; the floor never stops binding", width)
	}
}
