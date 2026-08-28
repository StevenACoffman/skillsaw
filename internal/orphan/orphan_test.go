package orphan_test

import (
	"testing"

	"github.com/StevenACoffman/skillet/related"
	"github.com/StevenACoffman/skillsaw/internal/orphan"
)

// node builds a skill and the edges leaving it: "kind:target", repeated.
func node(slug string, edges ...string) related.Node {
	n := related.Node{Slug: slug}
	for _, e := range edges {
		for i := range e {
			if e[i] == ':' {
				n.Edges = append(n.Edges,
					related.Edge{Kind: related.Kind(e[:i]), Target: e[i+1:]})
				break
			}
		}
	}
	return n
}

// TestRankOrdersTheKindsByHowStronglyTheyClaimUse pins the ladder, including the two ends
// that are not in doubt and the middle pair that is a judgement. A reversed comparison
// elsewhere would still pass every other test in this file, so the order is asserted
// directly rather than only through its consequences.
func TestRankOrdersTheKindsByHowStronglyTheyClaimUse(t *testing.T) {
	t.Parallel()
	descending := []related.Kind{
		related.DependsOn, related.ComposesWith, related.Informs, related.ContrastsWith,
	}
	for i := 1; i < len(descending); i++ {
		if orphan.Rank(descending[i-1]) <= orphan.Rank(descending[i]) {
			t.Errorf("%q does not outrank %q", descending[i-1], descending[i])
		}
	}
	// Both spellings of "this says nothing about use".
	for _, k := range []related.Kind{"", related.SupersededBy} {
		if got := orphan.Rank(k); got != 0 {
			t.Errorf("Rank(%q) = %d, want 0", k, got)
		}
	}
}

// TestInboundIsTheStrongestThingPointingAtASkill covers the three rules a real corpus
// exercises. The self-edge row is the one that matters most: without it a document vouches
// for itself and can never be reported, which is the single thing an orphan check must not
// permit.
func TestInboundIsTheStrongestThingPointingAtASkill(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		nodes []related.Node
		want  map[string]related.Kind
	}{
		"the strongest of several inbound edges wins": {
			[]related.Node{
				node("a", "contrasts-with:c"),
				node("b", "depends-on:c"),
				node("c"),
			},
			map[string]related.Kind{"a": "", "b": "", "c": related.DependsOn},
		},
		"order of appearance does not decide it": {
			[]related.Node{
				node("a", "depends-on:c"),
				node("b", "contrasts-with:c"),
				node("c"),
			},
			map[string]related.Kind{"a": "", "b": "", "c": related.DependsOn},
		},
		"a self-edge does not cover its own skill": {
			[]related.Node{node("a", "depends-on:a")},
			map[string]related.Kind{"a": ""},
		},
		"an edge to a slug not in the tree creates no entry": {
			[]related.Node{node("a", "depends-on:nowhere")},
			map[string]related.Kind{"a": ""},
		},
		"superseded-by covers nothing": {
			[]related.Node{node("a", "superseded-by:b"), node("b")},
			map[string]related.Kind{"a": "", "b": ""},
		},
		"an uncovered skill is present with the zero kind, not absent": {
			[]related.Node{node("a")},
			map[string]related.Kind{"a": ""},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := orphan.Inbound(tc.nodes)
			if len(got) != len(tc.want) {
				t.Fatalf("Inbound() = %v, want %v", got, tc.want)
			}
			for slug, want := range tc.want {
				if got[slug] != want {
					t.Errorf("Inbound()[%q] = %q, want %q", slug, got[slug], want)
				}
			}
		})
	}
}

// assertPredicate checks that exactly the named direction holds for a reported change, and
// that the change was expected at all. Both halves matter: a check that only verified the
// named predicate would pass while the report also named half the corpus.
func assertPredicate(t *testing.T, c orphan.Change, want map[string]string) {
	t.Helper()
	got := map[string]bool{
		"orphaned":     c.Orphaned(),
		"weakened":     c.Weakened(),
		"covered":      c.Covered(),
		"strengthened": c.Strengthened(),
	}
	name, expected := want[c.Slug]
	if !expected {
		t.Errorf("%q was reported and should not have been: %+v", c.Slug, c)
		return
	}
	if !got[name] {
		t.Errorf("%q: want %s, got %+v (was %q now %q)", c.Slug, name, got, c.Was, c.Now)
	}
}

// TestCompareReportsRegressionsAndNotThePreexisting is where the two rules carried over
// from edit.Uncoupled earn their place. The "new skill" and "orphaned in both" rows are the
// ones that decide whether this check is usable at all: 135 of 285 skills in the real corpus
// have no inbound edge, so without them the first run reports 135 regressions and gets
// switched off before it catches the one that matters.
func TestCompareReportsRegressionsAndNotThePreexisting(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		base, cur []related.Node
		want      map[string]string // slug -> the predicate that must hold
	}{
		"lost its last inbound edge": {
			[]related.Node{node("a", "depends-on:b"), node("b")},
			[]related.Node{node("a"), node("b")},
			map[string]string{"b": "orphaned"},
		},
		"demoted from composes-with to contrasts-with": {
			// The reason coverage is a tier. A boolean gate sees "covered" on both sides
			// and reports nothing, while a real use relationship was dropped.
			[]related.Node{node("a", "composes-with:b"), node("b")},
			[]related.Node{node("a", "contrasts-with:b"), node("b")},
			map[string]string{"b": "weakened"},
		},
		"promoted the other way": {
			[]related.Node{node("a", "contrasts-with:b"), node("b")},
			[]related.Node{node("a", "composes-with:b"), node("b")},
			map[string]string{"b": "strengthened"},
		},
		"gained its first inbound edge": {
			[]related.Node{node("a"), node("b")},
			[]related.Node{node("a", "informs:b"), node("b")},
			map[string]string{"b": "covered"},
		},
		"orphaned in both is pre-existing, not a regression": {
			[]related.Node{node("a"), node("b")},
			[]related.Node{node("a"), node("b")},
			map[string]string{},
		},
		"a skill absent from the baseline cannot be newly anything": {
			// b must arrive *covered*, or the was == now short-circuit skips it before the
			// rule is reached and the row proves nothing. The first version of this case
			// had b arrive uncovered and passed with the rule deleted.
			[]related.Node{node("a")},
			[]related.Node{node("a", "depends-on:b"), node("b")},
			map[string]string{},
		},
		"a superseded skill is suppressed even when orphaned": {
			[]related.Node{node("a", "depends-on:b"), node("b", "superseded-by:c"), node("c")},
			[]related.Node{node("a"), node("b", "superseded-by:c"), node("c")},
			map[string]string{},
		},
		"a skill removed from the tree is gone, not orphaned": {
			[]related.Node{node("a", "depends-on:b"), node("b")},
			[]related.Node{node("a")},
			map[string]string{},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rep := orphan.Compare(tc.base, tc.cur)
			if !rep.BaseAvailable {
				t.Error("BaseAvailable is false for a comparison that had a baseline")
			}
			if len(rep.Changes) != len(tc.want) {
				t.Fatalf("Changes = %+v, want %d of them %v", rep.Changes, len(tc.want), tc.want)
			}
			for _, c := range rep.Changes {
				assertPredicate(t, c, tc.want)
			}
		})
	}
}

// TestSurveyCannotBeReadAsARegression keeps the no-baseline mode honest. It answers a
// question worth asking and would be a terrible gate — 47% of the real corpus is orphaned —
// so the thing that must hold is that nothing it emits looks like a regression.
func TestSurveyCannotBeReadAsARegression(t *testing.T) {
	t.Parallel()
	rep := orphan.Survey([]related.Node{node("a", "depends-on:b"), node("b"), node("c")})
	if rep.BaseAvailable {
		t.Error("BaseAvailable is true with nothing to compare against")
	}
	if len(rep.Changes) != 3 {
		t.Fatalf("Changes = %+v, want one per skill so the denominator is right", rep.Changes)
	}
	for _, c := range rep.Changes {
		if c.Was != "" {
			t.Errorf("%q carries a baseline coverage %q there was no baseline for", c.Slug, c.Was)
		}
		if c.Orphaned() || c.Weakened() {
			t.Errorf("%q reads as a regression in a survey: %+v", c.Slug, c)
		}
	}
}

// TestUnadoptedNamesTheKindsTheCorpusDoesNotWrite is the reporting remnant of Convention.
// The check needs no gate — a kind nobody writes produces no inbound edges and so can never
// be a tier — but a reader comparing two corpora needs to know the ladder had fewer rungs.
func TestUnadoptedNamesTheKindsTheCorpusDoesNotWrite(t *testing.T) {
	t.Parallel()
	rep := orphan.Survey([]related.Node{node("a", "depends-on:b"), node("b")})
	want := []related.Kind{related.ComposesWith, related.Informs, related.ContrastsWith}
	if len(rep.Unadopted) != len(want) {
		t.Fatalf("Unadopted = %v, want %v", rep.Unadopted, want)
	}
	for i, w := range want {
		if rep.Unadopted[i] != w {
			t.Errorf("Unadopted[%d] = %q, want %q (strongest first)", i, rep.Unadopted[i], w)
		}
	}
}
