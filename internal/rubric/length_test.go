package rubric_test

import (
	"strings"
	"testing"

	"github.com/StevenACoffman/skillet/speclint"
	"github.com/StevenACoffman/skillsaw/internal/rubric"
)

func TestDim7FlagsAnOversizedSkillWithoutDocking(t *testing.T) {
	t.Parallel()
	const flag = "exceeds the spec's 500-line recommendation"
	long := strings.Repeat("A plain line.\n", speclint.SkillMaxLines)
	cases := map[string]struct {
		body     string
		files    map[string]string
		wantFlag bool
	}{
		"short skill":              {body: "A plain line.\n"},
		"oversized with no bundle": {body: long, wantFlag: true},
		"oversized but split": {
			body:  long + "See [the guide](references/guide.md).\n",
			files: map[string]string{"references/guide.md": "detail"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := mkSkill(t, "demo-skill", "Use when testing length.", tc.body, tc.files)
			d := dim(rubric.Evaluate(s, rubric.DefaultConfig()), 7)
			if got := hasFlag(d.Flags, flag); got != tc.wantFlag {
				t.Errorf("flagged = %v, want %v (flags %v)", got, tc.wantFlag, d.Flags)
			}
		})
	}
}

func TestDim7LengthFlagCostsNoPoints(t *testing.T) {
	t.Parallel()
	// The flag must not move a score: results.tsv compares totals across runs.
	long := strings.Repeat("A plain line.\n", speclint.SkillMaxLines)
	short := mkSkill(t, "demo-skill", "Use when testing length.", "A plain line.\n", nil)
	oversized := mkSkill(t, "demo-skill", "Use when testing length.", long, nil)
	cfg := rubric.DefaultConfig()
	if a, b := dim(
		rubric.Evaluate(short, cfg),
		7,
	).Penalty, dim(
		rubric.Evaluate(oversized, cfg),
		7,
	).Penalty; a != b {
		t.Errorf("dim 7 penalty moved with length: %d vs %d", a, b)
	}
}

func TestDim8ReportsAnUnscoredEvalsFile(t *testing.T) {
	t.Parallel()
	const flag = "evals/evals.json present but not scored"
	cases := map[string]struct {
		files    map[string]string
		wantFlag bool
	}{
		"no evals file": {},
		"evals file present": {
			files:    map[string]string{"evals/evals.json": `{"evals":[]}`},
			wantFlag: true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := mkSkill(t, "demo-skill", "Use when testing evals.", "Body.\n", tc.files)
			ev := rubric.Evaluate(s, rubric.DefaultConfig())
			if got := hasFlag(dim(ev, 8).Flags, flag); got != tc.wantFlag {
				t.Errorf("flagged = %v, want %v (flags %v)", got, tc.wantFlag, dim(ev, 8).Flags)
			}
		})
	}
}
