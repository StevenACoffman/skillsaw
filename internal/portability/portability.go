// Package portability checks that a skill name means one thing everywhere it appears.
//
// Two repositories sharing skills declare a convention in prose: an unprefixed name is
// portable and keeps its canonical content, a name prefixed with its repository's is
// repo-specific and free to diverge. The convention holds while one person maintains both,
// which is exactly the condition that stops holding.
//
// Everything here is pure. Walking trees is the caller's job.
package portability

import (
	"fmt"
	"sort"
	"strings"
)

// The two ways a name can stop identifying one artifact. They are separate because they
// want different fixes: a divergence is two copies that drifted, a collision is one
// repository that named two things alike, and reporting both as "portability" would send
// the reader to the wrong repair.
const (
	KindDiverged  Kind = "diverged"
	KindCollision Kind = "collision"
)

// Kind is what went wrong with a name.
type Kind string

// Skill is one declared skill, as read from a repository.
type Skill struct {
	// Name is what the skill calls itself, from frontmatter -- not its directory. A
	// runtime routes on the declared name, and keying this check on the path would make
	// it agree with the filesystem instead of with the contract.
	Name string `json:"name"`
	Repo string `json:"repo"` // the repository root it was found in
	Dir  string `json:"dir"`  // where in that repository, for the report
	Hash string `json:"hash"` // content identity
}

// Place is one skill's location and content, for reporting.
type Place struct {
	Repo string `json:"repo"`
	Dir  string `json:"dir"`
	Hash string `json:"hash"`
}

// Finding is one name that no longer identifies a single artifact.
type Finding struct {
	Name   string  `json:"name"`
	Kind   Kind    `json:"kind"`
	Places []Place `json:"places"`
}

// Check reports every name that has stopped identifying one artifact.
//
// A name beginning with its own repository's directory name plus "-" is repo-specific and
// exempt from the cross-repository comparison. That rule is derived rather than configured
// so the convention lives in one place, and it has a limit worth stating plainly: **the
// check is exactly as good as the naming.** A skill that ought to have been prefixed and
// was not will report as a portability violation when the real defect is the name, and
// nothing mechanical can tell those apart. The message says so.
//
// Requires: every Skill carries a non-empty Name, Repo, and Hash.
// Ensures:  pure; one finding per affected name, ordered by name so a report does not
// reshuffle between runs over the same input. Agreement and exempt names yield nothing.
func Check(skills []Skill) []Finding {
	byName := make(map[string][]Skill, len(skills))
	for _, s := range skills {
		byName[s.Name] = append(byName[s.Name], s)
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)

	var out []Finding
	for _, n := range names {
		if f, ok := findingFor(n, byName[n]); ok {
			out = append(out, f)
		}
	}
	return out
}

// findingFor judges one name's occurrences.
func findingFor(name string, group []Skill) (Finding, bool) {
	if repo, dup := duplicatedWithin(group); dup {
		return Finding{Name: name, Kind: KindCollision, Places: placesIn(group, repo)}, true
	}
	if isRepoSpecific(group[0]) || len(group) < 2 {
		return Finding{}, false
	}
	first := group[0].Hash
	for _, s := range group[1:] {
		if s.Hash != first {
			return Finding{Name: name, Kind: KindDiverged, Places: placesIn(group, "")}, true
		}
	}
	return Finding{}, false
}

// duplicatedWithin reports the first repository declaring this name more than once with
// **differing content**.
//
// Identical copies are not a collision, and measuring the real trees is what settled that:
// one of them keeps every skill twice, under skills/ and again under internal/assets/ for
// embedding, byte-for-byte. The name still identifies one artifact there. Reporting those
// would have produced five findings on a repository with no identity problem, and a check
// that fires on ordinary work is one somebody turns off.
func duplicatedWithin(group []Skill) (repo string, dup bool) {
	seen := make(map[string]string, len(group))
	for _, s := range group {
		if prior, ok := seen[s.Repo]; ok && prior != s.Hash {
			return s.Repo, true
		}
		seen[s.Repo] = s.Hash
	}
	return "", false
}

// placesIn renders a group's locations, narrowed to one repository when repo is set.
func placesIn(group []Skill, repo string) []Place {
	out := make([]Place, 0, len(group))
	for _, s := range group {
		if repo != "" && s.Repo != repo {
			continue
		}
		out = append(out, Place{Repo: s.Repo, Dir: s.Dir, Hash: s.Hash})
	}
	return out
}

// isRepoSpecific reports whether a name claims its own repository as a prefix.
func isRepoSpecific(s Skill) bool {
	return strings.HasPrefix(s.Name, strings.ToLower(s.Repo)+"-")
}

// Explain renders a finding as the line a reader needs to act on it.
func (f *Finding) Explain() string {
	var b strings.Builder
	switch f.Kind {
	case KindDiverged:
		fmt.Fprintf(&b, "%s: DIVERGED — the same unprefixed name with different content in "+
			"%d repositories. Either sync them, or rename the repo-specific one to carry "+
			"its repository's prefix; this cannot tell which you meant.",
			f.Name, len(f.Places))
	case KindCollision:
		fmt.Fprintf(&b, "%s: COLLISION — declared %d times inside one repository with "+
			"different content, so the name no longer picks out a skill. Nothing to do "+
			"with portability.", f.Name, len(f.Places))
	default:
		fmt.Fprintf(&b, "%s: %s", f.Name, f.Kind)
	}
	for _, p := range f.Places {
		fmt.Fprintf(&b, "\n    %s  %s", p.Hash, p.Dir)
	}
	return b.String()
}
