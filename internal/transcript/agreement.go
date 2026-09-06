package transcript

import "sort"

// VerdictCount is one outcome and how many of a phrasing's runs produced it.
type VerdictCount struct {
	Order Order `json:"order"`
	Count int   `json:"count"`
}

// Agreement is how a phrasing's repeated runs compare with each other.
//
// The unit is agreement rather than the verdict, because that is what the question asks.
// When guidance lands, repetitions converge on the same shape; several different outcomes
// across several runs means the wording is not binding, whatever those outcomes were. A
// phrasing that scores first three times out of five is not "mostly fine" -- it is a
// phrasing the skill does not reliably survive, and reporting the majority would hide
// exactly that.
type Agreement struct {
	Runs      int            `json:"runs"`
	Unanimous bool           `json:"unanimous"`
	Tally     []VerdictCount `json:"tally"`
}

// Agree summarises the verdicts from repeated runs of one phrasing.
//
// Requires: orders are the outcomes of running the same phrasing, in any order.
// Ensures:  pure. Tally is sorted by descending count, ties broken by verdict name, so a
// report over the same runs reads the same way twice. Unanimous is false for no runs at
// all -- nothing measured is not agreement -- and true for one, which the caller should
// qualify rather than present as convergence.
func Agree(orders []Order) Agreement {
	a := Agreement{Runs: len(orders)}
	if len(orders) == 0 {
		return a
	}
	counts := make(map[Order]int, len(orders))
	for _, o := range orders {
		counts[o]++
	}
	a.Tally = make([]VerdictCount, 0, len(counts))
	for o, n := range counts {
		a.Tally = append(a.Tally, VerdictCount{Order: o, Count: n})
	}
	sort.Slice(a.Tally, func(i, j int) bool {
		if a.Tally[i].Count != a.Tally[j].Count {
			return a.Tally[i].Count > a.Tally[j].Count
		}
		return a.Tally[i].Order < a.Tally[j].Order
	})
	a.Unanimous = len(a.Tally) == 1
	return a
}
