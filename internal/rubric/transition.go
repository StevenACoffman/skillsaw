package rubric

// How a dimension's score moved since the evaluation it is being compared against.
//
// The distinction is the point: a dimension that scored well and now does not is a
// different event from one that has never scored well, and the two want opposite next
// moves. A regression has a known-good previous version, so the cheap next step is to read
// what changed between them; a persistent weakness has no diff to read and wants the
// strategy for that dimension. A loop that cannot tell them apart reimplements when it
// should have been diffing.
//
// TransitionUnknown is the zero value and is what every diagnosis carries when nothing was
// compared, which is the common case. It must never read as "nothing regressed".
const (
	TransitionUnknown     Transition = ""
	TransitionNotCompared Transition = "not-compared"
	TransitionNew         Transition = "new"
	TransitionRegressed   Transition = "regressed"
	TransitionPersistent  Transition = "persistent"
)

// Transition is how one dimension's score moved between two evaluations.
type Transition string

// Valid reports whether t is one of the defined transitions.
func (t Transition) Valid() bool {
	switch t {
	case TransitionUnknown, TransitionNotCompared, TransitionNew,
		TransitionRegressed, TransitionPersistent:
		return true
	default:
		return false
	}
}

// TransitionOf reports how dimension num moved from prev to cur.
//
// It compares Final rather than Penalty. Three dimensions derive a base below 10 by design
// -- three checkpoint markers score 9 and that is the top of the band -- so a regression
// there shows up as a lower derived base with no penalty at all, and a check on Penalty
// would not see it.
//
// Requires: prev and cur are evaluations of the same skill; either may be nil.
// Ensures:  pure. TransitionNotCompared whenever the two were produced under different
// rubric editions or either edition is unknown, because scores from different rules are
// not a before and after. TransitionRegressed only when the dimension scored strictly
// higher in prev.
func TransitionOf(prev, cur *Evaluation, num int) Transition {
	if prev == nil || cur == nil {
		return TransitionUnknown
	}
	if prev.Rubric == "" || cur.Rubric == "" || prev.Rubric != cur.Rubric {
		return TransitionNotCompared
	}
	before, had := findDim(prev, num)
	now, has := findDim(cur, num)
	switch {
	case !has:
		return TransitionUnknown
	case !had:
		return TransitionNew
	case now < before:
		return TransitionRegressed
	default:
		return TransitionPersistent
	}
}

// findDim returns one dimension's final score and whether the evaluation scored it.
func findDim(ev *Evaluation, num int) (final int, ok bool) {
	for i := range ev.Dims {
		if ev.Dims[i].Num == num {
			return ev.Dims[i].Final, true
		}
	}
	return 0, false
}
