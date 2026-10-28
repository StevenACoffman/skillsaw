// Package gate answers what a manifest says about its own coverage, so a caller can tell a
// verdict about a tree from a verdict about nothing.
//
// Everything here is pure; reading files and choosing exit codes belong to the command.
package gate

import "github.com/StevenACoffman/skillet/manifest"

// Coverage values.
//
// CoverageUnknown is the zero value, following quotecheck.Status.Unchecked and
// finding.Action's absent classification: a value nobody populated reads as "we do not
// know", never as a verdict. A caller that forgets to ask therefore gets the answer that
// declines rather than the one that approves.
const (
	CoverageUnknown  Coverage = iota // the manifest predates Examined and cannot say
	CoverageNone                     // the producer examined no skills
	CoverageExamined                 // the producer examined at least one skill
)

// Coverage is what a manifest says about how much of a tree its producer examined.
//
// Three values rather than a bool, because "examined nothing" and "cannot say" call for
// opposite responses: the first is a manifest a gate must refuse, the second is every
// manifest written before skillet v0.30.0 and must still be honoured on the verdict it does
// carry. A bool cannot hold both, and collapsing them either fails the whole stored corpus
// or lets an empty tree through.
type Coverage int

// String renders a coverage for a report.
func (c Coverage) String() string {
	switch c {
	case CoverageNone:
		return "none"
	case CoverageExamined:
		return "examined"
	case CoverageUnknown:
		return "unknown"
	default:
		return "unrecognised"
	}
}

// Of reports what m says about its own coverage.
//
// **The absent-versus-zero split is read from the skills list, and the rule is skillet's
// rather than one invented here.** Examined is omitempty, so a manifest written before the
// field existed and one whose producer examined nothing decode to the same zero. They are
// separable because a producer that examined nothing lists nothing: a zero count beside a
// non-empty skills list can only be the older manifest. skillet records that inference on
// the field and pins it with a test, so this reads it instead of adding a second rule that
// could disagree.
//
// Requires: m is a parsed manifest; manifest.Parse has already refused a document that is
//
//	not one.
//
// Ensures: CoverageUnknown only when the count is zero and skills are listed; pure.
func Of(m *manifest.Manifest) Coverage {
	if m.Examined > 0 {
		return CoverageExamined
	}
	if len(m.Skills) > 0 {
		return CoverageUnknown
	}
	return CoverageNone
}
