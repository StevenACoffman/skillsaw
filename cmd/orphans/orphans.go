// Package orphans implements the "orphans" command: report the skills a change
// disconnected from the rest of the corpus, or -- with no baseline -- how connected the
// corpus is.
package orphans

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/skillet/finding"
	"github.com/StevenACoffman/skillet/related"
	"github.com/StevenACoffman/skillsaw/cmd/root"
	"github.com/StevenACoffman/skillsaw/internal/inventory"
	"github.com/StevenACoffman/skillsaw/internal/orphan"
)

// CategoryOrphaned and CategoryWeakened name the two findings.
const (
	CategoryOrphaned = "newly-orphaned"
	CategoryWeakened = "weakened-inbound-edge"
)

// Config holds the orphans command configuration.
type Config struct {
	*root.Config
	Tree     string
	BaseTree string
	Strict   bool
	JSON     bool
	Flags    *ff.FlagSet
	Command  *ff.Command
}

// New creates and registers the orphans command.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("orphans").SetParent(parent.Flags)
	cfg.Flags.StringVar(&cfg.Tree, 0, "tree", ".", "the skill tree to scan")
	cfg.Flags.StringVar(&cfg.BaseTree, 0, "base-tree", "",
		"a checkout of the tree to compare against; without it this only surveys")
	cfg.Flags.BoolVar(&cfg.Strict, 0, "strict",
		"promote the findings to errors, so a regression fails the build")
	cfg.Flags.BoolVar(&cfg.JSON, 0, "json", "emit the report as JSON")
	cfg.Command = &ff.Command{
		Name:      "orphans",
		Usage:     "skillsaw orphans [--tree DIR] [--base-tree DIR] [--strict] [--json]",
		ShortHelp: "report skills a change disconnected from the rest of the corpus",
		LongHelp: `Read the "## Related Skills" graph and report which skills nothing points at.

The absolute question is worth asking and useless as a gate. Measured over a
285-skill corpus, 135 skills have no inbound edge of any kind — 47% — so a gate
on "is orphaned" fires on half the tree and teaches people to bypass it. Without
--base-tree this prints the distribution across tiers and exits 0: a fact about
the corpus's shape, not a verdict.

With --base-tree it asks the useful question instead: did this change remove the
last thing pointing at a skill, or weaken it. A skill absent from the baseline is
new and cannot be newly anything.

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

The findings are advisory and this exits 0: an intentional removal legitimately
orphans something, and a gate that fires on those gets bypassed. Pass --strict to
promote them to errors and have the exit follow.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, args []string) error {
	if bad := root.MisplacedFlag(args); bad != "" {
		return fmt.Errorf(
			"orphans: %q looks like a flag after arguments; put flags before positional arguments",
			bad,
		)
	}
	if len(args) > 0 {
		return errors.New("orphans: takes no positional arguments; pass --tree DIR")
	}
	cur, err := inventory.Graph(cfg.Tree)
	if err != nil {
		return fmt.Errorf("orphans: %w", err)
	}
	rep := orphan.Survey(cur)
	if cfg.BaseTree != "" {
		base, baseErr := inventory.Graph(cfg.BaseTree)
		if baseErr != nil {
			return fmt.Errorf("orphans: base tree: %w", baseErr)
		}
		rep = orphan.Compare(base, cur)
	}
	if err := cfg.emit(&rep); err != nil {
		return err
	}
	if cfg.Strict && blocking(&rep) {
		return root.ExitError(1)
	}
	return nil
}

// blocking reports whether anything regressed. Only the two backward directions count: a
// skill that gained coverage is not a reason to fail a build.
func blocking(rep *orphan.Report) bool {
	if !rep.BaseAvailable {
		return false
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
	finding.Sort(out)
	return out
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
	return nil
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
