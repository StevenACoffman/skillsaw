package rubric_test

import (
	"strings"
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/rubric"
)

// evalOf builds an evaluation carrying one dimension's final score under a named edition.
func evalOf(edition string, num, final int) *rubric.Evaluation {
	return &rubric.Evaluation{
		Skill:  "alpha",
		Rubric: edition,
		Dims:   []rubric.DimScore{{Num: num, Name: "d", Final: final}},
	}
}

// TestTransitionSeparatesRegressedFromNeverClean is the distinction the whole item exists
// for. Both cases below end at the same score, and only one has a known-good version to
// diff against; a loop that cannot tell them apart reimplements when it should have read a
// diff.
func TestTransitionSeparatesRegressedFromNeverClean(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		prev *rubric.Evaluation
		want rubric.Transition
	}{
		"scored higher before":       {evalOf("ed1", 3, 10), rubric.TransitionRegressed},
		"scored the same before":     {evalOf("ed1", 3, 7), rubric.TransitionPersistent},
		"scored lower before":        {evalOf("ed1", 3, 4), rubric.TransitionPersistent},
		"the dimension is new":       {evalOf("ed1", 5, 9), rubric.TransitionNew},
		"nothing to compare against": {nil, rubric.TransitionUnknown},
		"a different rubric": {
			evalOf("ed2", 3, 10), rubric.TransitionNotCompared,
		},
		"the earlier one predates editions": {
			evalOf("", 3, 10), rubric.TransitionNotCompared,
		},
	}
	cur := evalOf("ed1", 3, 7)
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := rubric.TransitionOf(tc.prev, cur, 3); got != tc.want {
				t.Errorf("TransitionOf() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestADerivedBaseRegressionIsSeen guards the choice of Final over Penalty. Dims 4, 6 and 9
// derive bases below 10 by design — three checkpoint markers score 9 and that is the top of
// the band — so a regression there is a lower derived base with no penalty at all, and a
// check on Penalty would look straight past it.
func TestADerivedBaseRegressionIsSeen(t *testing.T) {
	t.Parallel()
	prev := &rubric.Evaluation{Skill: "alpha", Rubric: "ed1", Dims: []rubric.DimScore{
		{Num: 9, Name: "blacklist", Base: 9, Final: 9, Penalty: 0},
	}}
	cur := &rubric.Evaluation{Skill: "alpha", Rubric: "ed1", Dims: []rubric.DimScore{
		{Num: 9, Name: "blacklist", Base: 2, Final: 2, Penalty: 0},
	}}
	if got := rubric.TransitionOf(prev, cur, 9); got != rubric.TransitionRegressed {
		t.Errorf("TransitionOf() = %q, want %q; the base fell 9 to 2 with no penalty either "+
			"side, which a check on Penalty would not see", got, rubric.TransitionRegressed)
	}
}

// TestTransitionValidIsTotal keeps Valid honest about the set it guards.
func TestTransitionValidIsTotal(t *testing.T) {
	t.Parallel()
	for _, tr := range []rubric.Transition{
		rubric.TransitionUnknown, rubric.TransitionNotCompared, rubric.TransitionNew,
		rubric.TransitionRegressed, rubric.TransitionPersistent,
	} {
		if !tr.Valid() {
			t.Errorf("Valid() = false for the defined transition %q", tr)
		}
	}
	if rubric.Transition("improved").Valid() {
		t.Error("Valid() = true for an undefined transition")
	}
}

// TestDiagnoseAgainstRoutesARegressionDifferently checks the half a consuming loop reads.
// Noticing the transition is only worth anything if it changes the recommendation.
func TestDiagnoseAgainstRoutesARegressionDifferently(t *testing.T) {
	t.Parallel()
	cfg := rubric.DefaultConfig()
	weak := rubric.Evaluate(
		mkSkill(t, "ok-skill", cleanDesc, "## Workflow\n\nStep 1: do it\n", nil), cfg)

	// Same skill, but the earlier evaluation had the dimension healthy.
	prev := &rubric.Evaluation{Skill: weak.Skill, Rubric: weak.Rubric, Dims: []rubric.DimScore{
		{Num: 9, Name: "Counter-examples / blacklist", Final: 9},
	}}
	d := rubric.DiagnoseAgainst(weak, prev)
	if d.TargetNum != 9 {
		t.Fatalf("target is dim %d, not the blacklist dim this fixture weakens", d.TargetNum)
	}
	if d.Transition != rubric.TransitionRegressed {
		t.Fatalf("Transition = %q, want %q", d.Transition, rubric.TransitionRegressed)
	}
	if d.PreviousFinal != 9 {
		t.Errorf("PreviousFinal = %d, want 9", d.PreviousFinal)
	}
	if !strings.Contains(d.Rationale, "read the diff") {
		t.Errorf("the rationale does not say to read the diff, so noticing the regression "+
			"changed nothing about the next attempt: %q", d.Rationale)
	}

	// Without a baseline the diagnosis must say nothing rather than imply stability.
	bare := rubric.Diagnose(weak)
	if bare.Transition != rubric.TransitionUnknown {
		t.Errorf("Transition = %q with no baseline, want the zero value", bare.Transition)
	}
	if strings.Contains(bare.Rationale, "read the diff") {
		t.Errorf("an uncompared diagnosis recommends reading a diff: %q", bare.Rationale)
	}
}
