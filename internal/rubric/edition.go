package rubric

import (
	"strconv"

	"github.com/StevenACoffman/skillet/identity"
)

// scoringRevision is turned by hand when a **penalty amount** changes — the 6 for
// unparseable frontmatter, the 3 for a missing failure branch, the 1 per slop occurrence.
//
// Those live in eight check functions rather than in rubric.toml, so the document's hash
// cannot see them. Everything else that decides a score — weights, needs-judge, the
// scoring bands, the word lists — is in the document and is covered automatically.
//
// It is safe to hand-maintain only because TestScoresAreStable pins fixture scores, so a
// penalty change fails a test that tells the author to turn this. Without that test this
// constant would be a comment.
const scoringRevision = 1

// Edition identifies the rules a score was produced under.
//
// A base judged under one edition and reapplied under another is yesterday's grade served
// as today's, and the failure is silent: a number comes back, it is the right shape, and
// nothing says which rules made it. scores.Entry and the audit log both record this so the
// mismatch is caught the way content drift already is.
//
// It hashes the rubric document verbatim, which means a comment edit moves it too. That is
// the deliberate trade: the alternative is picking out the scoring-relevant fields by hand
// and silently omitting whichever one gets added next. Re-judging after a comment change
// is cheap; applying a stale base is not, and the document is not edited often enough for
// the difference to matter.
//
// It hangs off the Rubric rather than off a Config: it identifies a set of rules, and a
// Config holds a copy of two of their fields. Edition() below is the shipped one.
//
// Requires: r came from Load, so raw is the document it was parsed from.
// Ensures:  pure; equal for equal rules and different for different ones.
func (r *Rubric) Edition() string {
	return identity.Hash("rev=" + strconv.Itoa(scoringRevision) + ";" + string(r.raw))
}

// Edition is the edition of the compiled-in rubric: the one every command reports and
// records.
func Edition() string { return mustLoadEmbedded().Edition() }
