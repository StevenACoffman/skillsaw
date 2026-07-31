package stats_test

import (
	"math"
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/stats"
)

func TestWilsonProducesValidIntervals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		k, n int
	}{
		{name: "all successes", k: 10, n: 10},
		{name: "all failures", k: 0, n: 10},
		{name: "half", k: 5, n: 10},
		{name: "sparse", k: 1, n: 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			lo, hi := stats.Wilson(tc.k, tc.n)
			if lo < 0 || hi > 1 || lo > hi {
				t.Errorf("Wilson(%d,%d) = [%v,%v]; want a valid [0,1] interval", tc.k, tc.n, lo, hi)
			}
		})
	}
}

func TestWilsonEmptyIsMaximallyUncertain(t *testing.T) {
	t.Parallel()
	if lo, hi := stats.Wilson(0, 0); lo != 0 || hi != 1 {
		t.Errorf("Wilson(0,0) = [%v,%v], want [0,1]", lo, hi)
	}
}

func TestWilsonKnownValue(t *testing.T) {
	t.Parallel()
	// 8/10 at 95%: Wilson interval ~ [0.490, 0.943] (standard reference value).
	lo, hi := stats.Wilson(8, 10)
	if math.Abs(lo-0.4901) > 0.01 || math.Abs(hi-0.9430) > 0.01 {
		t.Errorf("Wilson(8,10) = [%.4f,%.4f], want ~[0.490,0.943]", lo, hi)
	}
}
