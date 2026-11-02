package rubric_test

import (
	"testing"
	"testing/quick"

	"github.com/StevenACoffman/skillet/skill"
	"github.com/StevenACoffman/skillet/speclint"
)

// TestKebabRuleMatchesSlugIdentity pins the claim that moving dim 7's name check from
// skill.Slug(name) != name to speclint.ValidSkillName moved no score: for every non-empty
// name the two agree. results.tsv compares totals across runs, so a silent disagreement
// would read as a quality change.
func TestKebabRuleMatchesSlugIdentity(t *testing.T) {
	t.Parallel()
	agree := func(name string) bool {
		if name == "" {
			return true // reported as "missing name" before either rule runs
		}
		return (skill.Slug(name) == name) == speclint.ValidSkillName(name)
	}
	if err := quick.Check(agree, nil); err != nil {
		t.Error(err)
	}
	for _, name := range []string{"a", "a-b", "-a", "a-", "a--b", "A", "a_b", "a.b", "x9"} {
		if !agree(name) {
			t.Errorf("Slug identity and ValidSkillName disagree on %q", name)
		}
	}
}
