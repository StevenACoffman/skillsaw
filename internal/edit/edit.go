// Package edit holds the pure invariants an optimize/apply step must enforce
// before committing a skill edit: the darwin 150% growth guard (spec §11.3
// E-size) and the hash-based no-op check (spec §8.7; SkillOpt's
// test_no_op_when_already_optimal). Both are pure functions, ready to wire into
// an edit loop; neither performs I/O.
package edit

import "github.com/StevenACoffman/skillsaw/internal/skill"

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
// identity used everywhere in skillsaw (skill.Hash, spec §8.7). An edit that
// leaves the hash unchanged did nothing and need not be re-evaluated.
func IsNoOp(before, after string) bool {
	return skill.Hash(before) == skill.Hash(after)
}
