package rubric_test

import (
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/rubric"
)

// TestScoresAreStable is what makes scoringRevision more than a comment. A threshold
// written in Go cannot be hashed into the edition, so the edition can only cover it if
// something catches the change and tells the author to turn the constant. This is that
// something: any move in a fixture's score fails here first.
//
// If a failure here is intentional, update the expected score AND bump scoringRevision in
// edition.go. Updating only the score leaves every cached base from the previous rules
// looking current.
func TestScoresAreStable(t *testing.T) {
	t.Parallel()
	cfg := rubric.DefaultConfig()
	cases := map[string]struct {
		desc, body string
		want       float64
	}{
		"clean skill":         {cleanDesc, cleanBody, 98.8},
		"no counter-examples": {cleanDesc, "## Workflow\n\nStep 1: do it\n", 95.2},
		"hedged and slopped": {
			cleanDesc,
			cleanBody + "\n建议这样 可以考虑那样 视情况而定\n说白了 换句话说 综上\n",
			90.1,
		},
		"broken frontmatter": {`"Site Reliability Engineering" by Betsy Beyer`, cleanBody, 94.6},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ev := rubric.Evaluate(mkSkill(t, "demo-skill", tc.desc, tc.body, nil), cfg)
			if ev.DeterministicScore != tc.want {
				t.Errorf("score %.2f, want %.2f. If this change is intentional, update the "+
					"expected value AND bump scoringRevision in edition.go -- otherwise "+
					"every base cached under the old rules still reads as current",
					ev.DeterministicScore, tc.want)
			}
		})
	}
}

// TestEditionIsPresent guards the one failure a caller cannot see: an empty edition would
// be recorded, compared, and match every other empty one, so every stale base would read
// as current. What the edition *tracks* is covered by TestEditionMovesWithTheDocument.
func TestEditionIsPresent(t *testing.T) {
	t.Parallel()
	if rubric.Edition() == "" {
		t.Error("the edition is empty; an absent key would match every recorded one")
	}
}
