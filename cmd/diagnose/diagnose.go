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
	Command *ff.Command
}

// New creates and registers the diagnose command.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("diagnose").SetParent(parent.Flags)
	cfg.Flags.BoolVar(&cfg.JSON, 0, "json", "emit diagnoses as JSON")
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
model should make.`,
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
	rcfg := rubric.DefaultConfig()
	var diags []rubric.Diagnosis
	for _, dir := range args {
		s, err := skill.Load(dir)
		if err != nil {
			_, _ = fmt.Fprintf(cfg.Stderr, "skip %s: %v\n", dir, err)
			continue
		}
		diags = append(diags, rubric.Diagnose(rubric.Evaluate(s, rcfg)))
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

	for _, d := range diags {
		_, _ = fmt.Fprintf(cfg.Stdout, "%s\n", d.Skill)
		_, _ = fmt.Fprintf(cfg.Stdout, "  target:    %s  [%s]\n", d.Target, d.Priority)
		_, _ = fmt.Fprintf(cfg.Stdout, "  rationale: %s\n", d.Rationale)
		if d.ClusterNote != "" {
			_, _ = fmt.Fprintf(cfg.Stdout, "  cluster:   %s\n", d.ClusterNote)
		}
		for _, f := range d.Findings {
			_, _ = fmt.Fprintf(cfg.Stdout, "  - %s\n", f)
		}
	}
	return nil
}
