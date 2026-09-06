package edit_test

import (
	"testing"

	"github.com/StevenACoffman/skillet/manifest"
	"github.com/StevenACoffman/skillsaw/internal/edit"
)

// loc is the single location every case here describes; the table varies the two hashes,
// which is what the check reads, and one skill is enough to say what it does with them.
const loc = "a"

// entry builds one manifest record. An empty prompts hash means the skill has no
// test-prompts file at all, which Diff reads as absent rather than unknown.
func entry(skillHash, promptsHash string) manifest.Skill {
	s := manifest.Skill{Slug: loc, Dir: loc, Hash: skillHash}
	if promptsHash != "" {
		s.TestPrompts = loc + "/test-prompts.json"
		s.TestPromptsHash = promptsHash
	}
	return s
}

// TestUncoupledFindsOnlyTheStalePair is the whole specification. The two silent rows are
// as load-bearing as the reported one: a gate that also fired on new skills and on
// never-tested ones would report most of a corpus on its first run, and a warning that
// fires everywhere is one nobody reads.
func TestUncoupledFindsOnlyTheStalePair(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		base, cur manifest.Skill
		want      bool
	}{
		"prose moved, assertions did not": {
			entry("h1", "p1"), entry("h2", "p1"), true,
		},
		"prose and assertions moved together": {
			entry("h1", "p1"), entry("h2", "p2"), false,
		},
		"nothing moved": {
			entry("h1", "p1"), entry("h1", "p1"), false,
		},
		"only the assertions moved": {
			entry("h1", "p1"), entry("h1", "p2"), false,
		},
		"untested on both sides is not uncoupled": {
			entry("h1", ""), entry("h2", ""), false,
		},
		"prompts appeared alongside the edit": {
			entry("h1", ""), entry("h2", "p1"), false,
		},
		"prompts disappeared": {
			entry("h1", "p1"), entry("h2", ""), false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := edit.Uncoupled(
				manifest.Manifest{Tree: ".", Skills: []manifest.Skill{tc.base}},
				manifest.Manifest{Tree: ".", Skills: []manifest.Skill{tc.cur}},
			)
			if (len(got) > 0) != tc.want {
				t.Errorf("got %d finding(s), want %v: %+v", len(got), tc.want, got)
			}
		})
	}
}

// TestANewSkillHasNothingToBeUncoupledFrom is separate because it is about a location the
// baseline never had, not about two hashes -- Diff routes it to Added, and a check reading
// Changed must not reach it by another path.
func TestANewSkillHasNothingToBeUncoupledFrom(t *testing.T) {
	t.Parallel()
	got := edit.Uncoupled(
		manifest.Manifest{Tree: "."},
		manifest.Manifest{Tree: ".", Skills: []manifest.Skill{entry("h1", "p1")}},
	)
	if len(got) != 0 {
		t.Errorf("a skill absent from the baseline produced %+v", got)
	}
}

// TestTheFindingIsAdvisoryAndNeedsAPerson pins both classifications. Severity is what
// blocks and this must not; Action is who closes it, and deciding whether an edit needed a
// test change is a judgement about what the edit meant.
func TestTheFindingIsAdvisoryAndNeedsAPerson(t *testing.T) {
	t.Parallel()
	got := edit.Uncoupled(
		manifest.Manifest{Tree: ".", Skills: []manifest.Skill{entry("h1", "p1")}},
		manifest.Manifest{Tree: ".", Skills: []manifest.Skill{entry("h2", "p1")}},
	)
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1", len(got))
	}
	if got[0].Severity != "warning" {
		t.Errorf("Severity = %q, want warning; this must not block by default", got[0].Severity)
	}
	if got[0].Action != "human" {
		t.Errorf("Action = %q, want human", got[0].Action)
	}
	if got[0].Category != edit.CategoryUncoupled || got[0].Path != loc {
		t.Errorf("finding does not name what it is about: %+v", got[0])
	}
}
