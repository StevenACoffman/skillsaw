// Package orphans implements the "orphans" command: report the skills a change
// disconnected from the rest of the corpus, or -- with no baseline -- how connected the
// corpus is.
package orphans

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/skillet/finding"
	"github.com/StevenACoffman/skillet/manifest"
	"github.com/StevenACoffman/skillet/related"
	"github.com/StevenACoffman/skillsaw/cmd/root"
	"github.com/StevenACoffman/skillsaw/internal/inventory"
	"github.com/StevenACoffman/skillsaw/internal/orphan"
)

// CategoryOrphaned, CategoryWeakened and CategoryDemoted name the findings.
//
// The first two are about a skill's coverage; the third is about one edge. They are
// separate categories because they want different repairs: a target losing coverage is a
// question for whoever owns the target, and a demoted edge is a question for whoever wrote
// the bullet.
const (
	CategoryOrphaned = "newly-orphaned"
	CategoryWeakened = "weakened-inbound-edge"
	CategoryDemoted  = "demoted-outbound-edge"
)

// longHelp is the command's prose, kept out of New so the constructor reads as wiring.
// It is data, not composition, and it is most of what a reader of New would page past.
const longHelp = `Read the "## Related Skills" graph and report which skills nothing points at.

The absolute question is worth asking and useless as a gate. Measured over a
285-skill corpus, 135 skills have no inbound edge of any kind — 47% — so a gate
on "is orphaned" fires on half the tree and teaches people to bypass it. Without
a baseline this prints the distribution across tiers and exits 0: a fact about
the corpus's shape, not a verdict.

With --base-tree it asks the useful question instead: did this change remove the
last thing pointing at a skill, or weaken it. A skill absent from the baseline is
new and cannot be newly anything.

--base-manifest reads the baseline out of a skills-manifest.json instead, for the
case that flag exists for: the baseline is a published artifact and the checkout
is gone. Prefer --base-tree when both trees are on disk. Two trees are read by
one parser at one version, which cannot drift from itself; a recorded edge is a
snapshot that can — the dialects a later parser learns are invisible to a
manifest already written. A manifest whose producer never read the graph is
refused rather than reported clean, because a tree that declares no edges and a
tree nobody asked look identical in the file.

Coverage is a tier, not a boolean, because the edge kinds are not equally strong
claims that a skill is still used:

    depends-on      something needs it first
    composes-with   something is used together with it
    informs         something shapes how it is applied
    contrasts-with  something names it as the alternative

So an edit rewriting a "composes-with" as a "contrasts-with" leaves the skill
covered while dropping a real use relationship. A boolean gate cannot see that,
and it is the likelier way a skill decays: not deleted, demoted.

"superseded-by" counts for nothing — its target is the replacement, which was
never at risk — and a skill that is itself superseded is left out entirely, since
merge-skills keeps it as an audit trail and its edges decaying is the expected
end of that life.

Every run reports what the reader could see beside what it found: the skills and
edges read, and how many of those edges name a slug absent from this tree, which
are read and still cover nothing. That line is a floor, not a total. A bullet
written in a kind outside the vocabulary is dropped by the parser without a
record, and a bullet naming a skill by its display title is not read as an edge
either; neither can be counted from here, so an orphan count is only ever as good
as the graph beneath it.

The findings are advisory and this exits 0: an intentional removal legitimately
orphans something, and a gate that fires on those gets bypassed. Pass --strict to
promote them to errors and have the exit follow.`

// Config holds the orphans command configuration.
type Config struct {
	*root.Config
	Tree         string
	BaseTree     string
	BaseManifest string
	Strict       bool
	JSON         bool
	Flags        *ff.FlagSet
	Command      *ff.Command
}

// New creates and registers the orphans command.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("orphans").SetParent(parent.Flags)
	cfg.Flags.StringVar(&cfg.Tree, 0, "tree", ".", "the skill tree to scan")
	cfg.Flags.StringVar(&cfg.BaseTree, 0, "base-tree", "",
		"a checkout of the tree to compare against; without it this only surveys")
	cfg.Flags.StringVar(&cfg.BaseManifest, 0, "base-manifest", "",
		"a skills-manifest.json to compare against, for when the checkout is gone")
	cfg.Flags.BoolVar(&cfg.Strict, 0, "strict",
		"promote the findings to errors, so a regression fails the build")
	cfg.Flags.BoolVar(&cfg.JSON, 0, "json", "emit the report as JSON")
	cfg.Command = &ff.Command{
		Name: "orphans",
		Usage: "skillsaw orphans [--tree DIR] [--base-tree DIR | --base-manifest FILE] " +
			"[--strict] [--json]",
		ShortHelp: "report skills a change disconnected from the rest of the corpus",
		LongHelp:  longHelp,
		Flags:     cfg.Flags,
		Exec:      cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, args []string) error {
	if err, bad := root.MisplacedFlag("orphans", args); bad {
		return err
	}
	if len(args) > 0 {
		return root.Usagef("orphans: takes no positional arguments; pass --tree DIR")
	}
	if cfg.BaseTree != "" && cfg.BaseManifest != "" {
		// Refused rather than given a precedence rule. The two baselines are not equally
		// trustworthy -- one is re-read by this parser, the other was recorded by another
		// tool at another version -- so silently preferring either would decide which
		// answer the caller gets on a rule nobody would remember.
		return root.Usagef(
			"orphans: pass --base-tree or --base-manifest, not both; they are different baselines")
	}
	rep, err := cfg.report()
	if err != nil {
		return err
	}
	if err := cfg.emit(&rep); err != nil {
		return err
	}
	if cfg.Strict && blocking(&rep) {
		return root.ExitError(1)
	}
	return nil
}

// report reads the current tree and whichever baseline was asked for.
//
// Split out so exec stays flag validation, report, emit, exit. The choice of baseline is
// one subtask and is understandable without reading the rest.
func (cfg *Config) report() (orphan.Report, error) {
	cur, err := inventory.Graph(cfg.Tree)
	if err != nil {
		return orphan.Report{}, fmt.Errorf("orphans: %w", err)
	}
	switch {
	case cfg.BaseTree != "":
		base, baseErr := inventory.Graph(cfg.BaseTree)
		if baseErr != nil {
			return orphan.Report{}, fmt.Errorf("orphans: base tree: %w", baseErr)
		}
		return orphan.Compare(base, cur), nil
	case cfg.BaseManifest != "":
		base, baseErr := recordedBase(cfg.BaseManifest)
		if baseErr != nil {
			return orphan.Report{}, baseErr
		}
		return orphan.Compare(base, cur), nil
	default:
		return orphan.Survey(cur), nil
	}
}

// recordedBase reads the baseline graph a manifest recorded.
//
// A manifest whose producer never read the graph is **refused**, not surveyed and not
// reported clean. The two are indistinguishable in the document -- a tree declaring no
// edges and a tree nobody asked are the same bytes, which is why skillet put the flag on
// the manifest -- so the only honest answers are this graph or no answer. Falling back
// would print "nothing was disconnected or demoted" about a baseline that never existed.
func recordedBase(path string) ([]related.Node, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("orphans: read %s: %w", path, err)
	}
	m, err := manifest.Parse(b)
	if err != nil {
		return nil, fmt.Errorf("orphans: %w", err)
	}
	nodes, err := inventory.RecordedGraph(m)
	if errors.Is(err, inventory.ErrEdgesUnrecorded) {
		return nil, fmt.Errorf(
			"orphans: %s: %w; re-run the producer with a version that records them, "+
				"or compare two checkouts with --base-tree", path, err)
	}
	if err != nil {
		return nil, fmt.Errorf("orphans: %s: %w", path, err)
	}
	return nodes, nil
}

// blocking reports whether anything regressed. Only backward directions count: a skill
// that gained coverage is not a reason to fail a build.
//
// A demotion blocks on the same footing as the other two. It is regression-relative, so it
// cannot fire on a pre-existing state -- which is the property that made the other two
// safe to gate on, and the reason the absolute orphan count is not gated at all.
func blocking(rep *orphan.Report) bool {
	if !rep.BaseAvailable {
		return false
	}
	if len(rep.Demotions) > 0 {
		return true
	}
	for _, c := range rep.Changes {
		if c.Orphaned() || c.Weakened() {
			return true
		}
	}
	return false
}

// diagnostics renders the regressions as findings, advisory unless promoted.
//
// Severity carries the strict decision rather than the exit code deciding separately: the
// rule the source of this check states is that blocking is "the caller's decision, taken by
// promoting the severity", and two places that could disagree about whether a run failed is
// the defect that produces a green build over a red report.
func diagnostics(rep *orphan.Report, strict bool) []finding.Diagnostic {
	sev := finding.SeverityWarning
	if strict {
		sev = finding.SeverityError
	}
	var out []finding.Diagnostic
	for _, c := range rep.Changes {
		switch {
		case c.Orphaned():
			out = append(out, finding.Diagnostic{
				Severity: sev, Category: CategoryOrphaned, Path: c.Slug,
				Action:  finding.ActionHuman,
				Message: fmt.Sprintf("nothing points at it any more; it was %q", c.Was),
			})
		case c.Weakened():
			out = append(out, finding.Diagnostic{
				Severity: sev, Category: CategoryWeakened, Path: c.Slug,
				Action: finding.ActionHuman,
				Message: fmt.Sprintf("the strongest edge into it fell from %q to %q",
					c.Was, c.Now),
			})
		}
	}
	// Keyed on the skill whose bullet changed, not the target: that is the file to open,
	// and it is the thing a target-keyed finding cannot name.
	for _, d := range rep.Demotions {
		out = append(out, finding.Diagnostic{
			Severity: sev, Category: CategoryDemoted, Path: d.From,
			Action:  finding.ActionHuman,
			Message: demotionMessage(d),
		})
	}
	finding.Sort(out)
	return out
}

// demotionMessage words a demotion, which reads differently when the bullet was deleted
// rather than rewritten: "fell to nothing" describes a kind nobody wrote.
func demotionMessage(d orphan.Demotion) string {
	if d.Now == "" {
		return fmt.Sprintf("dropped its %q edge to %q", d.Was, d.To)
	}
	return fmt.Sprintf("its edge to %q fell from %q to %q", d.To, d.Was, d.Now)
}

// tiers counts the skills at each level of coverage, strongest first, for the survey.
func tiers(rep *orphan.Report) []string {
	count := map[related.Kind]int{}
	for _, c := range rep.Changes {
		count[c.Now]++
	}
	kinds := append([]related.Kind{}, related.Kinds()...)
	sort.Slice(kinds, func(i, j int) bool {
		return orphan.Rank(kinds[i]) > orphan.Rank(kinds[j])
	})
	out := make([]string, 0, len(kinds)+1)
	for _, k := range kinds {
		if orphan.Rank(k) == 0 {
			continue
		}
		out = append(out, fmt.Sprintf("  %-16s %4d", k, count[k]))
	}
	out = append(out, fmt.Sprintf("  %-16s %4d", "(none)", count[""]))
	return out
}

// emit renders the report.
func (cfg *Config) emit(rep *orphan.Report) error {
	if cfg.JSON {
		enc := json.NewEncoder(cfg.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return fmt.Errorf("orphans: encode json: %w", err)
		}
		return nil
	}
	if !rep.BaseAvailable {
		_, _ = fmt.Fprintf(cfg.Stdout,
			"no baseline — surveying %d skills by strongest inbound edge:\n",
			len(rep.Changes))
		for _, line := range tiers(rep) {
			_, _ = fmt.Fprintln(cfg.Stdout, line)
		}
		cfg.note(rep)
		cfg.seen(rep)
		return nil
	}
	ds := diagnostics(rep, cfg.Strict)
	for i := range ds {
		_, _ = fmt.Fprintf(cfg.Stdout, "%s: %s — %s\n",
			ds[i].Path, ds[i].Category, ds[i].Message)
	}
	for _, c := range rep.Changes {
		if c.Covered() {
			_, _ = fmt.Fprintf(cfg.Stdout, "%s: covered — %q now points at it\n", c.Slug, c.Now)
		}
		if c.Strengthened() {
			_, _ = fmt.Fprintf(cfg.Stdout, "%s: strengthened — %q rose to %q\n",
				c.Slug, c.Was, c.Now)
		}
	}
	if len(ds) == 0 {
		_, _ = fmt.Fprintln(cfg.Stdout, "nothing was disconnected or demoted.")
	}
	cfg.note(rep)
	cfg.seen(rep)
	return nil
}

// seen prints what the reader could make out of the graph, on every run and beside the
// verdict rather than behind a flag. A count of orphans is only as good as the graph it
// was computed over, and the two are useless apart.
func (cfg *Config) seen(rep *orphan.Report) {
	c := rep.Coverage
	_, _ = fmt.Fprintf(cfg.Stdout,
		"\nread %d skills and %d edges, %d of which name a slug not in this tree.\n",
		c.Skills, c.EdgesRead, c.EdgesDangling)
	_, _ = fmt.Fprintln(cfg.Stdout,
		"a floor: edges in an unknown kind, and bullets naming a skill by display title,"+
			" are not read as edges and are not counted here.")
}

// note names the kinds the corpus never writes, so a reader knows the ladder had fewer
// rungs than the help text implies and does not read their absence as a clean result.
func (cfg *Config) note(rep *orphan.Report) {
	if len(rep.Unadopted) == 0 {
		return
	}
	names := make([]string, 0, len(rep.Unadopted))
	for _, k := range rep.Unadopted {
		names = append(names, string(k))
	}
	_, _ = fmt.Fprintf(cfg.Stdout,
		"\nnot adopted by this corpus, so never a tier here: %v\n", names)
}
