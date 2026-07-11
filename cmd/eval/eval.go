// Package eval implements the "eval" command: score one or more skills against
// the darwin 9-dimension rubric (spec §8, Phase 1 baseline). It reports the
// deterministic score and, per dimension, the deterministic findings plus which
// dimensions still need a model judge.
package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/skillsaw/cmd/root"
	"github.com/StevenACoffman/skillsaw/internal/rubric"
	"github.com/StevenACoffman/skillsaw/internal/skill"
)

// Config holds the eval command configuration.
type Config struct {
	*root.Config
	All     bool
	Roots   string
	Scores  string
	JSON    bool
	Verbose bool
	Flags   *ff.FlagSet
	Command *ff.Command
}

// New creates and registers the eval command.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("eval").SetParent(parent.Flags)
	cfg.Flags.BoolVar(&cfg.All, 'a', "all", "scan skill roots instead of listing dirs")
	cfg.Flags.StringVar(&cfg.Roots, 0, "roots", strings.Join(skill.DefaultRoots(), ","),
		"comma-separated skill roots for --all")
	cfg.Flags.StringVar(&cfg.Scores, 's', "scores", "",
		"path to judge-supplied per-dimension bases (JSON) enabling the full total")
	cfg.Flags.BoolVar(&cfg.JSON, 0, "json", "emit evaluations as JSON")
	cfg.Flags.BoolVar(&cfg.Verbose, 'v', "verbose", "show per-dimension breakdown")
	cfg.Command = &ff.Command{
		Name:      "eval",
		Usage:     "skillsaw eval [FLAGS] [SKILL_DIR ...]",
		ShortHelp: "score skills against the 9-dimension rubric",
		LongHelp: `Score one or more skills against the darwin 9-dimension rubric (spec §8).

The "deterministic score" is computed from what can be checked without a model:
frontmatter/description limits, explicit checkpoint markers, resource-link
reachability, softening-phrase and AI-slop counts, failure-branch presence, a
counter-example section, and the runtime-neutrality scan. Dimensions whose base
quality is an irreducible textual judgment (workflow clarity, failure-mode
encoding, actionable specificity, overall architecture, and real-world test
performance) are marked NEEDS-JUDGE: for the deterministic score they are
assumed perfect and docked only for objectively detectable defects, so the score
is a lower bound on quality loss — a lint-style floor, not the full rubric total.

Pass skill directories, or --all to scan the roots (default covers Claude Code,
Cursor, Codex, and .agents layouts).`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, args []string) error {
	dirs := args
	if cfg.All {
		found, err := skill.DiscoverRoots(cfg.Roots)
		if err != nil {
			return fmt.Errorf("eval: %w", err)
		}
		dirs = found
	}
	if len(dirs) == 0 {
		return errors.New("eval: no skills found; pass SKILL_DIR arguments or use --all")
	}

	bases, err := cfg.loadScores()
	if err != nil {
		return err
	}

	rcfg := rubric.DefaultConfig()
	evals := make([]*rubric.Evaluation, 0, len(dirs))
	for _, dir := range dirs {
		s, loadErr := skill.Load(dir)
		if loadErr != nil {
			_, _ = fmt.Fprintf(cfg.Stderr, "skip %s: %v\n", dir, loadErr)
			continue
		}
		evals = append(evals, rubric.EvaluateWithBases(s, rcfg, bases))
	}
	if len(evals) == 0 {
		return root.ExitError(1)
	}

	if cfg.JSON {
		enc := json.NewEncoder(cfg.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(evals); err != nil {
			return fmt.Errorf("eval: encode json: %w", err)
		}
		return nil
	}

	cfg.printTable(evals)
	if cfg.Verbose {
		for _, ev := range evals {
			cfg.printBreakdown(ev)
		}
	}
	return nil
}

// loadScores reads and parses the optional judge-supplied bases file. When no
// file is set it returns an empty (non-nil) map — "no bases", which yields no
// full score without tripping the nil-nil return rule.
func (cfg *Config) loadScores() (map[int]int, error) {
	if cfg.Scores == "" {
		return map[int]int{}, nil
	}
	data, err := os.ReadFile(cfg.Scores)
	if err != nil {
		return nil, fmt.Errorf("eval: read scores: %w", err)
	}
	bases, err := rubric.ParseScores(data)
	if err != nil {
		return nil, fmt.Errorf("eval: %w", err)
	}
	return bases, nil
}

func (cfg *Config) printTable(evals []*rubric.Evaluation) {
	tw := tabwriter.NewWriter(cfg.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "SKILL\tDET.SCORE\tFULL\tRUNTIME\tWEAKEST\tNEEDS-JUDGE")
	for _, ev := range evals {
		_, _ = fmt.Fprintf(tw, "%s\t%.1f/100\t%s\t%s\t%s\t%s\n",
			ev.Skill, ev.DeterministicScore, fullCell(ev),
			runtimeCell(ev.RuntimeWarn), weakestDim(ev), needsJudge(ev))
	}
	_ = tw.Flush()
	_, _ = fmt.Fprintln(
		cfg.Stdout,
		"\nDET.SCORE = deterministic lower bound (NEEDS-JUDGE dims assume a perfect base; "+
			"only detectable defects are docked). FULL = total with --scores judge bases.",
	)
}

// fullCell renders the full rubric total, or "-" when no judge bases cover all
// needs-judge dimensions.
func fullCell(ev *rubric.Evaluation) string {
	if ev.HasFullScore {
		return fmt.Sprintf("%.1f/100", ev.FullScore)
	}
	return "-"
}

func (cfg *Config) printBreakdown(ev *rubric.Evaluation) {
	_, _ = fmt.Fprintf(cfg.Stdout, "\n%s  (hash %s, %d bytes, runtime_warn=%d)\n",
		ev.Skill, ev.Hash, ev.Bytes, ev.RuntimeWarn)
	tw := tabwriter.NewWriter(cfg.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "  DIM\tWEIGHT\tFINAL\tPENALTY\tSOURCE\tFINDINGS")
	for _, d := range ev.Dims {
		source := "derived"
		if d.NeedsJudge {
			source = "needs-judge"
		}
		_, _ = fmt.Fprintf(tw, "  %d %s\t%d\t%d/10\t-%d\t%s\t%s\n",
			d.Num, d.Name, d.Weight, d.Final, d.Penalty, source, strings.Join(d.Flags, "; "))
	}
	_ = tw.Flush()
}

func runtimeCell(n int) string {
	if n == 0 {
		return "ok"
	}
	return fmt.Sprintf("warn=%d", n)
}

func weakestDim(ev *rubric.Evaluation) string {
	d := rubric.Diagnose(ev)
	if d.TargetNum == 0 {
		return d.Target
	}
	return fmt.Sprintf("%d %s", d.TargetNum, d.Target)
}

func needsJudge(ev *rubric.Evaluation) string {
	var nums []string
	for _, d := range ev.Dims {
		if d.NeedsJudge {
			nums = append(nums, strconv.Itoa(d.Num))
		}
	}
	if len(nums) == 0 {
		return "-"
	}
	return "dims " + strings.Join(nums, ",")
}
