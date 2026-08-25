// Package diagnose implements the "diagnose" command: the deterministic half of
// a darwin Phase 2 optimization round (spec §11.3 Step 1). It evaluates a skill,
// names the weakest dimension, warns about the dim2/3/4 cluster, and routes to a
// strategy-library priority (§12) — telling a model exactly what to target next.
package diagnose

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/skillet/skill"
	"github.com/StevenACoffman/skillsaw/cmd/root"
	"github.com/StevenACoffman/skillsaw/internal/rubric"
)

// Config holds the diagnose command configuration.
type Config struct {
	*root.Config
	JSON    bool
	Flags   *ff.FlagSet
	Against string
	Command *ff.Command
}

// New creates and registers the diagnose command.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("diagnose").SetParent(parent.Flags)
	cfg.Flags.BoolVar(&cfg.JSON, 0, "json", "emit diagnoses as JSON")
	cfg.Flags.StringVar(&cfg.Against, 0, "against", "",
		"an earlier \"eval --json\" output to compare the target dimension against")
	cfg.Command = &ff.Command{
		Name:      "diagnose",
		Usage:     "skillsaw diagnose [FLAGS] SKILL_DIR [SKILL_DIR ...]",
		ShortHelp: "recommend the next dimension to improve",
		LongHelp: `Evaluate a skill and recommend what to optimize next — the deterministic half
of a darwin Phase 2 round (spec §11.3 Step 1). Reports the weakest dimension, the
strategy-library priority (P0-P3, §12), a rationale, and — when the target is in
the dim2/3/4 correlated cluster — a note to inspect all three together.

A runtime-neutrality hit forces a P0 "runtime drift" target ahead of any
dimension (spec §9.3). This command performs no edits; it scopes the edit a
model should make.

With --against, pointed at an earlier "eval --json", the target dimension is also
compared with its previous score. A dimension that scored higher before and lower
now is a different event from one that has never been clean, and the two want
opposite next moves: a regression has a known-good previous version, so the cheap
step is reading the diff since then rather than reworking the dimension. Without
--against nothing is compared, and the diagnosis says so by leaving the transition
unset rather than by implying stability.

Evaluations produced under different rubric editions are not compared at all —
scores from different rules are not a before and an after.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, args []string) error {
	if bad := root.MisplacedFlag(args); bad != "" {
		return fmt.Errorf(
			"diagnose: %q looks like a flag after arguments; put flags before positional arguments",
			bad,
		)
	}
	if len(args) == 0 {
		return errors.New("diagnose: pass at least one SKILL_DIR")
	}
	previous, err := cfg.loadAgainst()
	if err != nil {
		return err
	}
	rcfg := rubric.DefaultConfig()
	var diags []rubric.Diagnosis
	for _, dir := range args {
		s, err := skill.Load(dir)
		if err != nil {
			_, _ = fmt.Fprintf(cfg.Stderr, "skip %s: %v\n", dir, err)
			continue
		}
		ev := rubric.Evaluate(s, rcfg)
		diags = append(diags, rubric.DiagnoseAgainst(ev, previous[ev.Skill]))
	}
	if len(diags) == 0 {
		return root.ExitError(1)
	}

	if cfg.JSON {
		enc := json.NewEncoder(cfg.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(diags); err != nil {
			return fmt.Errorf("diagnose: encode json: %w", err)
		}
		return nil
	}

	cfg.render(diags)
	return nil
}

// render writes the human form of each diagnosis. Split from exec because it is the whole
// of the presentation and none of the decision -- and because the optional lines push exec
// past the complexity the linter allows.
func (cfg *Config) render(diags []rubric.Diagnosis) {
	for i := range diags {
		d := &diags[i]
		_, _ = fmt.Fprintf(cfg.Stdout, "%s\n", d.Skill)
		_, _ = fmt.Fprintf(cfg.Stdout, "  target:    %s  [%s]\n", d.Target, d.Priority)
		// Omitted rather than printed empty when there is no target to act on: the diagnosis
		// says nothing needs doing, so naming an actor would contradict it.
		if d.Action != "" {
			_, _ = fmt.Fprintf(cfg.Stdout, "  who acts:  %s\n", d.Action)
		}
		_, _ = fmt.Fprintf(cfg.Stdout, "  rationale: %s\n", d.Rationale)
		if d.ClusterNote != "" {
			_, _ = fmt.Fprintf(cfg.Stdout, "  cluster:   %s\n", d.ClusterNote)
		}
		for _, f := range d.Findings {
			_, _ = fmt.Fprintf(cfg.Stdout, "  - %s\n", f)
		}
	}
}

// loadAgainst reads the earlier evaluations, keyed by skill so a run over several
// directories compares each with its own baseline rather than with whichever came first.
//
// A file that names skills this run does not is not an error: an optimize loop diagnoses a
// subset of what it last evaluated, and refusing would make the flag unusable exactly when
// it is most useful.
func (cfg *Config) loadAgainst() (map[string]*rubric.Evaluation, error) {
	out := map[string]*rubric.Evaluation{}
	if cfg.Against == "" {
		return out, nil
	}
	b, err := os.ReadFile(cfg.Against)
	if err != nil {
		return nil, fmt.Errorf("diagnose: read %s: %w", cfg.Against, err)
	}
	var evals []*rubric.Evaluation
	if err := json.Unmarshal(b, &evals); err != nil {
		return nil, fmt.Errorf("diagnose: parse %s as \"eval --json\" output: %w", cfg.Against, err)
	}
	for _, ev := range evals {
		out[ev.Skill] = ev
	}
	return out, nil
}
