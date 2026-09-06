// Package orphan reports skills that nothing else points at, and -- given a baseline --
// the ones a change disconnected.
//
// The absolute question is nearly useless on its own: measured over a 285-skill corpus, 135
// skills have no inbound edge of any kind, so a gate on "is orphaned" fires on 47% of the
// tree and teaches people to bypass it. The useful question is regression-relative: did this
// change remove the last thing pointing at a skill, or weaken it.
//
// Weakening is why coverage is a tier rather than a boolean. The five edge kinds are not
// equally strong claims that a skill is still used, so an edit that rewrites "composes-with"
// as "contrasts-with" leaves the skill covered while genuinely dropping a use relationship.
// A boolean gate cannot see that, and it is the most likely way a skill decays in practice:
// not deleted, demoted.
//
// Everything here is pure. Reading trees is inventory's job.
package orphan

import (
	"sort"

	"github.com/StevenACoffman/skillet/related"
)

// Change is how one skill's strongest inbound edge moved between two trees.
//
// Was and Now are the strongest kind pointing at it on each side, and the zero Kind means
// nothing did. Keeping both rather than a verdict is what lets a caller tell a skill that
// lost its last edge from one that merely lost its best, which is the distinction the tier
// exists for.
type Change struct {
	Slug string       `json:"slug"`
	Was  related.Kind `json:"was,omitempty"`
	Now  related.Kind `json:"now,omitempty"`
}

// Coverage says how much of the current tree's graph the reader could actually see.
//
// It is a field on Report rather than something a caller computes beside it, because an
// orphan count is only as good as the graph beneath it and the two must not be separable:
// a caller free to print one without the other will eventually print one without the
// other. The measurement that motivated this found the readable graph moving from 222 to
// 357 edges in a single day.
//
// **It is a floor, not a total.** Two things are unread and neither is counted here. A
// bullet written in a kind outside the vocabulary is dropped by the parser without a
// record, so nothing downstream can count it. A bullet naming a skill by display title is
// likewise not an edge, and related.TitleRefs cannot be used to count those: measured over
// the real 288-skill tree it returns 339 refs against 426 edges, and what it names are the
// *kinds* of well-formed bullets -- its filter excludes an underscored bold token and the
// canonical kinds are hyphenated. A wrong number under a heading that claims to say what
// the reader missed is worse than an absent one, which is the whole point of this type.
type Coverage struct {
	// Skills is how many skills' documents were read.
	Skills int `json:"skills"`

	// EdgesRead is how many bullets the parser understood as edges.
	EdgesRead int `json:"edges_read"`

	// EdgesDangling is how many of those name a slug absent from this tree. They are read
	// and still cover nothing, so they inflate EdgesRead without ever making a skill
	// covered -- which is exactly the gap between "the graph looks populated" and "the
	// orphan count means something". Targets naming another tree are excluded, since those
	// are deliberate and resolvable only on disk.
	EdgesDangling int `json:"edges_dangling"`
}

// Demotion is one skill's outbound edge losing rank between two trees -- rewritten to a
// weaker kind, or dropped outright, which is Now == "".
//
// **A separate type from Change, because it answers a different question.** Change asks
// "is this skill still covered"; a Demotion asks "did this relationship weaken". Folding
// the second into the first would leave Orphaned and Weakened ambiguous about which they
// mean, and the two diverge exactly where it matters: measured on the real corpus,
// demoting go-beyond-packages-as-layers' composes-with edge produced **no** Change,
// because two other skills still point at the target with depends-on and its strongest
// inbound was unmoved. The corpus lost a use relationship and the tier could not see it.
//
// It also names *which file to open*, which a target-keyed Change cannot: the target
// knows it was demoted, not by whom.
type Demotion struct {
	From string       `json:"from"`
	To   string       `json:"to"`
	Was  related.Kind `json:"was"`
	Now  related.Kind `json:"now,omitempty"`
}

// Report is the comparison of two trees' inbound coverage.
//
// One list rather than four, with the direction derivable per Change. Partitioning here
// would make the type as complicated as the question, and the only caller that wants the
// grouping is the one rendering it.
type Report struct {
	// BaseAvailable distinguishes "compared against nothing" from "compared and found
	// nothing", the way timeseries.Verdict.Compared does. False means every Change carries
	// only a Now, and none of them is a regression.
	BaseAvailable bool `json:"base_available"`

	// Changes are the skills whose coverage differs between the trees, sorted by slug.
	Changes []Change `json:"changes,omitempty"`

	// Unadopted names the ranked kinds the current tree never writes, strongest first, so
	// a reader knows the ladder had fewer rungs than it looks.
	Unadopted []related.Kind `json:"unadopted,omitempty"`

	// Demotions are the individual edges that weakened, sorted by source then target.
	//
	// **An edit can appear here and in Changes both**, when the demoted edge was also the
	// target's strongest. That is not double-counting to be suppressed: the two say
	// different things, and only this one names the skill whose bullet changed. Suppressing
	// it would make Demotions mean "the demotions the tier missed" -- a type defined by
	// what another type failed to catch, which is a worse thing to hand a reader than one
	// edit described twice.
	//
	// Empty when there is no baseline, like Changes' regressions.
	Demotions []Demotion `json:"demotions,omitempty"`

	// Coverage describes the **current** tree alone. A baseline read out of a manifest has
	// no documents to measure, and a figure spanning two differently-known sides would be
	// a number about neither.
	Coverage Coverage `json:"coverage"`
}

// Orphaned reports that the skill had inbound coverage and now has none.
func (c Change) Orphaned() bool { return c.Was != "" && c.Now == "" }

// Covered reports that the skill had no inbound coverage and now has some.
func (c Change) Covered() bool { return c.Was == "" && c.Now != "" }

// Weakened reports that the strongest thing pointing at the skill got weaker without
// disappearing -- a composes-with rewritten as a contrasts-with, which a boolean gate
// cannot see and which is the likelier way a skill decays: not deleted, demoted.
func (c Change) Weakened() bool {
	return c.Was != "" && c.Now != "" && Rank(c.Now) < Rank(c.Was)
}

// Strengthened is Weakened's mirror, reported for the same reason the gate reports Covered:
// a check that only ever names regressions cannot show a repair working.
func (c Change) Strengthened() bool {
	return c.Was != "" && c.Now != "" && Rank(c.Now) > Rank(c.Was)
}

// ladder orders the edge kinds by how strongly being their target says a skill is still
// used. Absent from the ladder means an inbound edge of that kind counts for nothing.
//
// The two ends are not in doubt: depends-on means something needs this first, and
// contrasts-with means something merely names it as the alternative. Composes-with over
// informs is a judgement, and skillet's own reasoning is the argument -- Informs exists as a
// separate kind because ComposesWith "would claim a symmetry 26 of the 38 do not have", so
// composes-with asserts a mutual working relationship that informs explicitly does not.
// Measured, composes-with is also the load-bearing kind, 201 edges against informs' 38.
//
// superseded-by is deliberately absent, and for a reason unrelated to strength: its target
// is the *replacement*, which was never the skill at risk. Counting it would mark the
// survivor as covered by the thing it replaced.
func ladder() map[related.Kind]int {
	return map[related.Kind]int{
		related.DependsOn:     4,
		related.ComposesWith:  3,
		related.Informs:       2,
		related.ContrastsWith: 1,
	}
}

// Rank returns how strongly an inbound edge of this kind says a skill is still used, higher
// being stronger, and 0 for a kind that says nothing -- including the zero value, which is
// how a skill with no inbound edge at all is spelled.
//
// Ensures: pure. Rank("") == 0, so an uncovered skill compares correctly against every
// covered one without the caller special-casing it.
func Rank(k related.Kind) int { return ladder()[k] }

// Inbound returns the strongest kind of edge pointing at each skill in nodes.
//
// A skill present in nodes but pointed at by nothing maps to the zero Kind rather than being
// absent, so a caller iterating the map sees every skill and never has to ask whether a
// missing key means uncovered or unknown.
//
// Requires: nodes are the skills of one tree; a node's Slug is its directory name.
// Ensures:  pure. Three rules, each of which a corpus will exercise:
//   - A self-edge does not cover its own skill. A document must not vouch for itself, which
//     is the one thing an orphan check cannot allow.
//   - An edge naming a slug absent from nodes covers nothing and creates no entry. Those are
//     dangling edges, which related.DanglingEdges reports and this deliberately does not.
//   - Where several edges point at one skill, the strongest wins.
func Inbound(nodes []related.Node) map[string]related.Kind {
	present := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		present[n.Slug] = true
	}
	out := make(map[string]related.Kind, len(nodes))
	for _, n := range nodes {
		out[n.Slug] = ""
	}
	for _, n := range nodes {
		for _, e := range n.Edges {
			if e.Target == n.Slug || !present[e.Target] || Rank(e.Kind) == 0 {
				continue
			}
			if Rank(e.Kind) > Rank(out[e.Target]) {
				out[e.Target] = e.Kind
			}
		}
	}
	return out
}

// retired reports the skills a merge run replaced, which are the sources of superseded-by
// edges.
//
// They are suppressed from orphan reporting because their disconnection is intentional:
// merge-skills keeps a superseded skill as the audit trail of what was merged, and the
// edges into it decaying is the expected end of that life rather than a regression.
func retired(nodes []related.Node) map[string]bool {
	out := map[string]bool{}
	for _, n := range nodes {
		for _, e := range n.Edges {
			if e.Kind == related.SupersededBy {
				out[n.Slug] = true
				break
			}
		}
	}
	return out
}

// unadopted returns the ranked kinds that appear nowhere in nodes, sorted strongest first.
//
// It is reported rather than enforced. The source entry carried this across as a gate --
// skip the check for a kind the corpus does not use -- but worked through, the gate has
// nothing to do: a kind nobody writes produces no inbound edges, so it can never be a
// skill's strongest tier, and nothing misfires. What is left is worth saying out loud, so a
// reader knows the ladder had fewer rungs than it looks.
func unadopted(nodes []related.Node) []related.Kind {
	seen := map[related.Kind]bool{}
	for _, n := range nodes {
		for _, e := range n.Edges {
			seen[e.Kind] = true
		}
	}
	var out []related.Kind
	for k := range ladder() {
		if !seen[k] {
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool { return Rank(out[i]) > Rank(out[j]) })
	return out
}

// outbound maps each skill to the strongest kind it points at each target with.
//
// Strongest, because a skill may state one relationship twice: the reader dedupes on kind
// and target, so two bullets naming the same pair with different kinds survive as two
// edges, and comparing an arbitrary one of them against the baseline would report a
// demotion whenever the pair happened to be visited in a different order.
//
// Requires: nodes are the skills of one tree.
// Ensures:  pure. A self-edge is excluded, matching Inbound -- a document that stops
// pointing at itself has not demoted a relationship anyone else relied on.
func outbound(nodes []related.Node) map[Demotion]related.Kind {
	out := make(map[Demotion]related.Kind, len(nodes))
	for _, n := range nodes {
		for _, e := range n.Edges {
			if e.Target == n.Slug {
				continue
			}
			pair := Demotion{From: n.Slug, To: e.Target}
			if Rank(e.Kind) > Rank(out[pair]) {
				out[pair] = e.Kind
			}
		}
	}
	return out
}

// demotions reports the edges that lost rank between two trees.
//
// Requires: base and cur are the skills of two versions of one tree.
// Ensures:  pure. Sorted by source then target. Three rules, each of which a corpus
// exercises:
//   - Both endpoints must exist in **both** trees. A skill that arrived or was deleted
//     takes its edges with it, and reporting those as demotions would bury the deletion
//     under a list of relationships that went with it.
//   - Only a strict rank decrease. A dropped edge falls out for free, since Rank("") is 0,
//     and arrives with an empty Now.
//   - A source a merge run superseded is suppressed, the same rule retired applies to
//     targets and for the same reason: its edges decaying is the expected end of that life.
func demotions(base, cur []related.Node) []Demotion {
	was, now := outbound(base), outbound(cur)
	inBoth := both(base, cur)
	gone := retired(cur)
	var out []Demotion
	for pair, w := range was {
		if !inBoth[pair.From] || !inBoth[pair.To] || gone[pair.From] {
			continue
		}
		if n := now[pair]; Rank(n) < Rank(w) {
			out = append(out, Demotion{From: pair.From, To: pair.To, Was: w, Now: n})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		return out[i].To < out[j].To
	})
	return out
}

// both returns the slugs present in each of two trees.
func both(base, cur []related.Node) map[string]bool {
	inBase := make(map[string]bool, len(base))
	for _, n := range base {
		inBase[n.Slug] = true
	}
	out := make(map[string]bool, len(cur))
	for _, n := range cur {
		if inBase[n.Slug] {
			out[n.Slug] = true
		}
	}
	return out
}

// measure counts how much of nodes the reader could see. See Coverage for what it
// deliberately does not count, and why counting it would be worse than the gap.
//
// Ensures: pure. EdgesDangling <= EdgesRead, since a dangling edge is one that was read.
func measure(nodes []related.Node) Coverage {
	c := Coverage{Skills: len(nodes), EdgesDangling: len(related.DanglingEdges(nodes))}
	for _, n := range nodes {
		c.EdgesRead += len(n.Edges)
	}
	return c
}

// Compare reports how inbound coverage moved between a baseline tree and the current one.
//
// Two rules carry over unchanged from edit.Uncoupled, and re-deriving them is the whole
// risk of siting this check here:
//
//   - A skill absent from the baseline is new and cannot be *newly* anything. Without this
//     the first corpus-wide run reports every pre-existing orphan as a regression: measured,
//     135 of 285 skills have no inbound edge, so the check would open by crying wolf 135
//     times and be switched off before it ever caught the one that mattered.
//   - The result is advisory. An intentional removal legitimately orphans something, and a
//     gate that fires on those teaches people to bypass it. Blocking is the caller's
//     decision, taken by promoting the severity.
//
// A skill that a merge run superseded is suppressed even when orphaned -- see retired.
//
// It reports two things, at two grains, and the second is not derivable from the first.
// Changes is per target: did this skill's coverage move. Demotions is per edge: did this
// relationship weaken. A demotion by one of several skills pointing at a target leaves the
// target's strongest kind unmoved, so it produces a Demotion and no Change -- which is the
// commonest way a corpus loses a relationship and the case a tier alone cannot see.
//
// Requires: base and cur are the skills of two versions of one tree.
// Ensures:  pure. Changes holds only slugs present in both sides whose coverage differs,
// sorted by slug. A skill absent from cur is gone rather than orphaned and is not reported.
func Compare(base, cur []related.Node) Report {
	was, now := Inbound(base), Inbound(cur)
	gone := retired(cur)
	rep := Report{
		BaseAvailable: true,
		Unadopted:     unadopted(cur),
		Coverage:      measure(cur),
		Demotions:     demotions(base, cur),
	}
	for slug, n := range now {
		w, inBase := was[slug]
		if !inBase || w == n || gone[slug] {
			continue
		}
		rep.Changes = append(rep.Changes, Change{Slug: slug, Was: w, Now: n})
	}
	sort.Slice(rep.Changes, func(i, j int) bool {
		return rep.Changes[i].Slug < rep.Changes[j].Slug
	})
	return rep
}

// Survey reports the current tree's coverage with nothing to compare against.
//
// It exists because the absolute question is worth answering and worthless as a gate: 47% of
// a real corpus has no inbound edge, so a list of those is noise, while the distribution
// across tiers is a fact about the corpus's shape. BaseAvailable is false and every Change
// carries only a Now, so nothing here can be read as a regression.
//
// Ensures: pure. One Change per skill, sorted by slug, including the uncovered ones -- the
// caller is counting a distribution and a missing skill would silently shrink the
// denominator.
func Survey(cur []related.Node) Report {
	now := Inbound(cur)
	rep := Report{Unadopted: unadopted(cur), Coverage: measure(cur)}
	for slug, n := range now {
		rep.Changes = append(rep.Changes, Change{Slug: slug, Now: n})
	}
	sort.Slice(rep.Changes, func(i, j int) bool {
		return rep.Changes[i].Slug < rep.Changes[j].Slug
	})
	return rep
}
