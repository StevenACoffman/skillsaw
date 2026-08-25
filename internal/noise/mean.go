package noise

import (
	"math"
)

// MeanInterval returns a conservative 95% interval on the mean of bounded observations.
//
// It is the ordinary t interval, t(n-1)·s/√n, with one departure: the sample standard
// deviation is floored at 1/(n+1) before use. Judge cases very often score identically --
// the same checks pass on every one -- and a t interval over identical observations has
// zero width, so it reports an exactly pinned mean on the strength of three samples. That
// is the overconfidence this package exists to refuse.
//
// The floor is the rule of succession read as a dispersion: having seen n observations and
// no disagreement, one unseen dissenting case would arrive at a rate near 1/(n+1), so that
// is the least dispersion worth assuming. It is a deliberate assumption rather than a
// derived bound, chosen because it decays at the right speed. Measured over identical
// observations, it leaves three cases spanning bases 2-10, ten cases spanning 8±1, and
// fifteen cases resolving to a single base -- which is about where a reader should start
// believing one. Flooring the half-width instead (Hoeffding, or the
// rule of three) is defensible and was tried: both leave forty identical cases spanning
// three bases, and a caveat that never clears is not a signal.
//
// Requires: every x in xs is in [0,1].
// Ensures:  the result contains the sample mean, lies within [0,1], and is the whole of
// [0,1] for fewer than two observations -- one measurement resolves nothing about a mean.
func MeanInterval(xs []float64) [2]float64 {
	n := len(xs)
	if n < 2 {
		return [2]float64{0, 1}
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	mean := sum / float64(n)

	var ss float64
	for _, x := range xs {
		ss += (x - mean) * (x - mean)
	}
	stdDev := math.Max(math.Sqrt(ss/float64(n-1)), 1/float64(n+1))

	half := tCritical(n-1) * stdDev / math.Sqrt(float64(n))
	return [2]float64{math.Max(0, mean-half), math.Min(1, mean+half)}
}

// tCritical returns the two-sided 95% critical value of Student's t for df degrees of
// freedom, falling back to the normal 1.96 past the table. The table is small on purpose:
// judge runs score a handful of cases, so the small-df rows are the ones that get used and
// the tail converges to the normal value anyway.
func tCritical(df int) float64 {
	table := []float64{
		12.706, 4.303, 3.182, 2.776, 2.571, 2.447, 2.365, 2.306, 2.262, 2.228,
		2.201, 2.179, 2.160, 2.145, 2.131, 2.120, 2.110, 2.101, 2.093, 2.086,
		2.080, 2.074, 2.069, 2.064, 2.060, 2.056, 2.052, 2.048, 2.045, 2.042,
	}
	if df >= 1 && df <= len(table) {
		return table[df-1]
	}
	return 1.96
}
