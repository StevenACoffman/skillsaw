package edit

import (
	"github.com/StevenACoffman/skillet/finding"
	"github.com/StevenACoffman/skillet/manifest"
	"github.com/StevenACoffman/skillsaw/internal/inventory"
)

// CategoryUncoupled names the finding: prose moved and its behavioural assertions did not.
const CategoryUncoupled = "uncoupled-test-prompts"

// Uncoupled reports the skills whose SKILL.md changed while their test-prompts did not.
//
// The defect is invisible in a snapshot. A SKILL.md rewritten under assertions that still
// describe the previous version passes every structural check in this family, because each
// document is internally fine; only the pair is wrong, and only relative to what it was.
//
// Two states deliberately produce nothing. A location absent from the baseline is new and
// has nothing to be uncoupled from. A location with no test-prompts on either side is
// untested rather than uncoupled -- a real defect, and a different one, which this gate
// reporting would drown its own signal in on the first corpus-wide run.
//
// The result is advisory. An editorial fix to one sentence legitimately needs no test
// change, and a gate that fires on those teaches people to bypass it; blocking is the
// caller's decision, taken by promoting the severity.
//
// Requires: base and cur describe the same tree, cur built by inventory.
// Ensures:  pure. One diagnostic per location whose skill hash moved and whose
// test-prompts hash did not, in the order manifest.Delta reports them.
func Uncoupled(base, cur manifest.Manifest) []finding.Diagnostic {
	// Axes cannot tell "has prompts, unchanged" from "has none on either side": both are
	// TestPrompts=false, and only the first is a stale pair. The second is an untested
	// skill, which is a real defect and a different one.
	tested := make(map[string]bool, len(cur.Skills))
	for _, s := range cur.Skills {
		tested[inventory.Location(cur.Tree, s.Dir)] = s.TestPrompts != "" || s.TestPromptsHash != ""
	}
	delta := manifest.Diff(base, cur)
	out := make([]finding.Diagnostic, 0, len(delta.Changed))
	for _, loc := range delta.Changed {
		axes, known := delta.ChangedAxes[loc]
		if !known || !axes.Skill || axes.TestPrompts || !tested[loc] {
			continue
		}
		out = append(out, finding.Diagnostic{
			Severity: finding.SeverityWarning,
			Category: CategoryUncoupled,
			Path:     loc,
			// Action, not severity, carries the reason this cannot be automated: whether
			// an edit needed a test change is a judgement about what the edit meant.
			Action: finding.ActionHuman,
			Message: "SKILL.md changed but test-prompts.json did not; its assertions may " +
				"still describe the previous version",
		})
	}
	return out
}
