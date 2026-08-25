package noise

import (
	"github.com/StevenACoffman/skillet/ratchet"
)

// Evidenced reports which halves of a confusion matrix the sample can speak to.
//
// The two halves are independent and either can be unevidenced. Recall is evidenced by
// targets: without one, TPR is a ratio over nothing. Precision is evidenced by distractors
// that could have fired, and this is the half that goes wrong quietly -- a decoy set in
// which nothing ever fires cannot distinguish perfect precision from trivial decoys, and
// the matrix reports 0.00 either way.
//
// Measured over sixty real skills: 38 distractors, none of which fired, and 42 of the
// sixty carried no distractors at all -- each of those printing FPR 0.00 (0/0), a precision
// figure computed from nothing, in the same column as one computed from something.
//
// This is reported and never gated. --min already refuses what it cannot resolve, and a
// clean FP over a genuine decoy set is a real perfect score that must not be punished for
// being one.
//
// It is the same idea this codebase already spells eight ways -- VerdictUnknown, an
// unresolved bound, TransitionNotCompared, Aggregate.Resolved, scores.Entry.Baseline,
// finding.Unexamined, an empty rubric edition, and an activation report marked Unmeasured.
// A number nobody measured must not sit in a column beside one somebody did. EVIDENCE.md
// is where that principle is written down for readers rather than callers.
type Evidenced struct {
	// Recall is false when no target was scored.
	Recall bool `json:"recall"`

	// Precision is false when no distractor was scored, and separately Discriminating is
	// false when distractors existed but none fired. The two want different repairs:
	// write some decoys, versus write harder ones.
	Precision      bool `json:"precision"`
	Discriminating bool `json:"discriminating"`
}

// EvidenceIn reads which halves of a report are supported by its sample.
//
// Requires: a is a report from ratchet.Score over a **readable description**. A report
// scored against an empty one has TP and FP of zero for a reason this cannot see, and the
// caveat would then tell an author to write harder decoys when the decoys are fine and
// nothing could have fired. Measured over 60 book skills: 30 had no description to score,
// 22 because the frontmatter did not parse and 8 because it was empty. cmd/activation
// refuses those before scoring rather than handing them here.
// Ensures:  pure. Discriminating is false whenever Precision is, since a decoy set that
// does not exist cannot discriminate.
func EvidenceIn(a *ratchet.Report) Evidenced {
	return Evidenced{
		Recall:         a.Targets > 0,
		Precision:      a.Distractors > 0,
		Discriminating: a.Distractors > 0 && a.FP > 0,
	}
}

// Caveat renders what the sample does not support, or "" when it supports both halves.
//
// It names the repair rather than the statistic, because a reader looking at FPR 0.00 has
// no way to guess which of the two situations produced it.
func (e Evidenced) Caveat() string {
	switch {
	case !e.Recall && !e.Precision:
		return "no targets and no distractors: nothing here was measured"
	case !e.Recall:
		return "no should_trigger prompts: TPR is a ratio over nothing"
	case !e.Precision:
		return "no should_not_trigger decoys: FPR claims nothing, so precision is unevidenced"
	case !e.Discriminating:
		return "no decoy ever fired: this is either perfect precision or decoys too easy " +
			"to separate them, and the matrix cannot tell which. Write harder decoys."
	default:
		return ""
	}
}
