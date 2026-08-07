// Package edit holds the pure invariants an optimize/apply step must enforce
// before committing a skill edit: the darwin 150% growth guard (spec §11.3
// E-size), the hash-based no-op check (spec §8.7; SkillOpt's
// test_no_op_when_already_optimal), and the structural defects that must reject a
// proposal outright. Every function is pure; none performs I/O.
package edit

import (
	"github.com/StevenACoffman/skillet/finding"
	"github.com/StevenACoffman/skillet/identity"
	"github.com/StevenACoffman/skillet/redlines"
	"github.com/StevenACoffman/skillet/skill"
	"github.com/StevenACoffman/skillet/speclint"
)

// WithinSizeBudget reports whether an edited skill of newBytes stays within
// ratio × origBytes (darwin's default ratio is 1.5). A non-positive origBytes
// admits only an empty result, since any growth from nothing is unbounded.
func WithinSizeBudget(origBytes, newBytes int, ratio float64) bool {
	if origBytes <= 0 {
		return newBytes == 0
	}
	return float64(newBytes) <= float64(origBytes)*ratio
}

// IsNoOp reports whether before and after are the same skill under the content
// identity used everywhere (identity.Hash, spec §8.7). An edit that leaves the
// hash unchanged did nothing and need not be re-evaluated.
func IsNoOp(before, after string) bool {
	return identity.Hash(before) == identity.Hash(after)
}

// StructuralDefects returns the defects that must block adopting s as an edit,
// whatever it scored. The agentskills.io frontmatter rules (speclint) always apply,
// because every Agent Skill is bound by the spec. book2skill's Quality Red Lines
// (redlines) apply only when withRedlines is set.
//
// The red lines are opt-in for the same reason `exegesis lint --check redlines` is:
// they encode book2skill's house structure — the six RIA-TV++ segments above all —
// and a skill written by hand rather than distilled from a book has no reason to
// carry it. Enforcing them by default would reject every such skill and make this
// gate useless for most of what skillsaw optimizes. Turn them on when optimizing a
// book tree, where the structure is the contract.
//
// This is the gate's definition of "structurally sound", which is why it is one
// named function rather than two calls repeated at each site. It is deliberately
// narrower than the rubric. The rubric *scores* a defect, so a blown description cap
// costs points that a gain elsewhere can outweigh; these *reject*, because an edit
// that buys a higher score by breaking structure is the failure this gate exists to
// prevent. Runtime neutrality is not included — `skillsaw scan` gates that
// separately, and folding it in here would give one command two unrelated verdicts.
//
// Requires: s is a loaded skill.
// Ensures:  the result is empty iff s carries no blocking structural defect under
//
//	the selected rules; it is pure.
func StructuralDefects(s *skill.Skill, withRedlines bool) []finding.Diagnostic {
	ds := speclint.Frontmatter(s)
	if withRedlines {
		ds = append(ds, redlines.Check(s)...)
	}
	return ds
}
