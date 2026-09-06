package portability_test

import (
	"strings"
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/portability"
)

// sk is one declared skill in a repository.
func sk(name, repo, hash string) portability.Skill {
	return portability.Skill{Name: name, Repo: repo, Dir: repo + "/" + name, Hash: hash}
}

// TestCheckReportsOnlyWhatStoppedIdentifyingOneThing is the specification. The silent rows
// matter as much as the reported ones: a check that also fired on agreement, on names
// unique to one repository, or on properly prefixed ones would report most of a corpus and
// be switched off.
func TestCheckReportsOnlyWhatStoppedIdentifyingOneThing(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		skills []portability.Skill
		want   portability.Kind // "" means no finding
	}{
		"the same name, the same content": {
			[]portability.Skill{sk("shared", "alpha", "h1"), sk("shared", "beta", "h1")}, "",
		},
		"the same name, different content": {
			[]portability.Skill{sk("shared", "alpha", "h1"), sk("shared", "beta", "h2")},
			portability.KindDiverged,
		},
		"a name only one repository has": {
			[]portability.Skill{sk("solo", "alpha", "h1")}, "",
		},
		"a repo-specific name is exempt": {
			[]portability.Skill{sk("alpha-local", "alpha", "h1"), sk("alpha-local", "beta", "h2")},
			"",
		},
		"declared twice in one repository": {
			[]portability.Skill{sk("twice", "alpha", "h1"), sk("twice", "alpha", "h2")},
			portability.KindCollision,
		},
		"declared twice with identical content is a copy, not a collision": {
			// Settled by measuring the real trees, against my first guess. One of them
			// keeps every skill twice -- under skills/ and again under internal/assets/
			// for embedding -- byte-for-byte. The name still identifies one artifact, and
			// calling that a collision produced five findings on a repository with no
			// identity problem.
			[]portability.Skill{sk("twice", "alpha", "h1"), sk("twice", "alpha", "h1")},
			"",
		},
		"nothing at all": {nil, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := portability.Check(tc.skills)
			if tc.want == "" {
				if len(got) != 0 {
					t.Errorf("got %d finding(s), want none: %+v", len(got), got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("got %d finding(s), want 1: %+v", len(got), got)
			}
			if got[0].Kind != tc.want {
				t.Errorf("Kind = %q, want %q", got[0].Kind, tc.want)
			}
		})
	}
}

// TestACollisionOutranksADivergence pins the precedence. A name declared twice in one repo
// and also present elsewhere is broken locally first: reporting it as a portability problem
// would send the reader to sync two repositories when one of them cannot say what the name
// refers to.
func TestACollisionOutranksADivergence(t *testing.T) {
	t.Parallel()
	got := portability.Check([]portability.Skill{
		sk("both", "alpha", "h1"), sk("both", "alpha", "h2"), sk("both", "beta", "h3"),
	})
	if len(got) != 1 || got[0].Kind != portability.KindCollision {
		t.Fatalf("got %+v, want a single collision", got)
	}
	for _, p := range got[0].Places {
		if p.Repo != "alpha" {
			t.Errorf("the collision report names %s, which is not where the duplicate is", p.Repo)
		}
	}
}

// TestFindingsAreOrdered keeps a report from reshuffling between runs over one input, which
// would make any diff of two runs unreadable.
func TestFindingsAreOrdered(t *testing.T) {
	t.Parallel()
	got := portability.Check([]portability.Skill{
		sk("zulu", "alpha", "h1"), sk("zulu", "beta", "h2"),
		sk("alfa", "alpha", "h1"), sk("alfa", "beta", "h2"),
	})
	if len(got) != 2 {
		t.Fatalf("got %d findings, want 2", len(got))
	}
	if got[0].Name != "alfa" || got[1].Name != "zulu" {
		t.Errorf("findings are %s then %s, want alphabetical", got[0].Name, got[1].Name)
	}
}

// TestExplainNamesBothRepairs covers the operator-facing half and the check's own limit: a
// skill that should have been prefixed and was not looks exactly like a portability
// violation, so the message must offer the rename as well as the sync.
func TestExplainNamesBothRepairs(t *testing.T) {
	t.Parallel()
	got := portability.Check([]portability.Skill{
		sk("shared", "alpha", "h1"), sk("shared", "beta", "h2"),
	})
	msg := got[0].Explain()
	for _, want := range []string{"DIVERGED", "sync", "rename", "h1", "h2"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Explain() is missing %q:\n%s", want, msg)
		}
	}
}

// TestARenamedCopyIsNotADivergence is the reason identity is the declared name. Two
// directories with different paths holding the same declared skill are one skill; two
// directories with the same basename holding differently-named skills are two. Keying on
// the path would get both backwards.
func TestARenamedCopyIsNotADivergence(t *testing.T) {
	t.Parallel()
	same := []portability.Skill{
		{Name: "shared", Repo: "alpha", Dir: "alpha/curated/shared", Hash: "h1"},
		{Name: "shared", Repo: "beta", Dir: "beta/internal/assets/skills/shared", Hash: "h1"},
	}
	if got := portability.Check(same); len(got) != 0 {
		t.Errorf("identical content at different paths reported %+v; the path is not the "+
			"identity", got)
	}
	differentSkills := []portability.Skill{
		{Name: "one", Repo: "alpha", Dir: "alpha/skills/thing", Hash: "h1"},
		{Name: "two", Repo: "beta", Dir: "beta/skills/thing", Hash: "h2"},
	}
	if got := portability.Check(differentSkills); len(got) != 0 {
		t.Errorf("two differently-named skills sharing a directory basename reported %+v", got)
	}
}
