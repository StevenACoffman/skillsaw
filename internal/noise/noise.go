// Package noise decides whether a measured difference is larger than the sample that
// produced it can explain. It exists because a gate on a point estimate reports a coin
// flip as evidence: a net utility of +0.10 over eight prompts is consistent with a true
// value on either side of zero, and CI going green on that is worse than CI going red,
// since a green build is taken as a result.
//
// Everything here is pure.
package noise

import (
	"github.com/StevenACoffman/skillet/ratchet"
)

// How a measurement stands against a floor once sampling noise is accounted for. The zero
// value is the unevaluated one and never reads as a pass, so a result that skipped the
// check cannot be mistaken for one that cleared it.
const (
	VerdictUnknown    Verdict = ""
	VerdictClears     Verdict = "clears"
	VerdictBelow      Verdict = "below"
	VerdictUnresolved Verdict = "unresolved"
)

// Verdict is the outcome of gating one measurement against a floor.
type Verdict string

// Valid reports whether v is one of the four defined verdicts.
func (v Verdict) Valid() bool {
	switch v {
	case VerdictUnknown, VerdictClears, VerdictBelow, VerdictUnresolved:
		return true
	default:
		return false
	}
}

// UtilityBound returns a conservative interval on a report's net utility, derived from the
// Wilson intervals the report already carries.
//
// Net utility is (TP-FP)/total, which rises with TPR and falls with FPR, so its extremes
// over the rectangle formed by the two intervals are that rectangle's corners. Targets and
// distractors are disjoint samples, so the intervals are independent and the rectangle
// covers at least 90%. The result is therefore wider than a proper 95% interval on net
// utility would be -- wide is the safe direction for something that decides whether to
// pass, and calling it 95% would be the kind of borrowed precision this package exists to
// refuse.
//
// Requires: a comes from ratchet.Score, whose intervals follow Wilson's n=0 convention
// of [0,1].
// Ensures:  the result contains a.NetUtility, and is [-1,1] when there are no prompts at
// all -- no sample resolves nothing, rather than resolving zero.
func UtilityBound(a *ratchet.Report) [2]float64 {
	total := a.Targets + a.Distractors
	if total == 0 {
		return [2]float64{-1, 1}
	}
	targets, distractors, n := float64(a.Targets), float64(a.Distractors), float64(total)
	lo := (a.TPRInterval[0]*targets - a.FPRInterval[1]*distractors) / n
	hi := (a.TPRInterval[1]*targets - a.FPRInterval[0]*distractors) / n
	return [2]float64{lo, hi}
}

// Gate decides whether a bound clears floor by more than the sample can explain. An
// unresolved result is not a pass: the caller cannot tell yet, and "cannot tell" must cost
// what "no" costs, or the gate quietly becomes optional on exactly the small samples where
// it matters most.
//
// Requires: bound is ordered lo,hi.
// Ensures:  VerdictClears only when the whole bound is at or above floor; VerdictBelow
// only when the whole bound is beneath it; VerdictUnresolved whenever the bound spans
// floor, which includes the no-prompts bound of [-1,1].
func Gate(bound [2]float64, floor float64) Verdict {
	switch {
	case bound[0] >= floor:
		return VerdictClears
	case bound[1] < floor:
		return VerdictBelow
	default:
		return VerdictUnresolved
	}
}
