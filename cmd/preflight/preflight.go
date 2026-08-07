// Package preflight implements the "preflight" command: the structural gate that
// runs before an edit is adopted. It rejects a proposal whose structure is broken
// however well it scored, so the optimizer cannot trade structure for points. The
// checks are pure and shared (skillet's speclint + redlines, composed by
// internal/edit); this command does the file I/O and decides the exit code.
package preflight

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/skillet/finding"
	"github.com/StevenACoffman/skillet/skill"
	"github.com/StevenACoffman/skillsaw/cmd/root"
	"github.com/StevenACoffman/skillsaw/internal/edit"
)

// Config holds the preflight command configuration.
type Config struct {
	*root.Config
	JSON     bool
	Redlines bool
	Flags    *ff.FlagSet
	Command  *ff.Command
}

// skillDefects is one skill's structural verdict, for reporting.
type skillDefects struct {
	Skill   string               `json:"skill"`
	Defects []finding.Diagnostic `json:"defects"`
}

// New creates and registers the preflight command.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("preflight").SetParent(parent.Flags)
	cfg.Flags.BoolVar(&cfg.JSON, 0, "json", "emit the defects as JSON")
	cfg.Flags.BoolVar(&cfg.Redlines, 0, "redlines",
		"also enforce book2skill's Quality Red Lines (RIA-TV++ segments, quote limit, trigger)")
	cfg.Command = &ff.Command{
		Name:      "preflight",
		Usage:     "skillsaw preflight [--redlines] [--json] SKILL_DIR ...",
		ShortHelp: "structural gate: reject an edit that breaks structure, whatever it scored",
		LongHelp: `Check each SKILL_DIR against the structural rules an edit must satisfy
before it is adopted. Exit code is 1 when any defect is found, so an optimize
loop can run this between writing an edit and deciding whether to keep it.

By default only the agentskills.io frontmatter rules apply, since every Agent
Skill is bound by the spec. With --redlines, book2skill's Quality Red Lines are
enforced too: the six RIA-TV++ body segments, the quotation ceiling, and a
description that states a trigger. Those are opt-in because they encode
book2skill's house structure — a hand-written skill has no reason to carry it,
and enforcing them by default would reject nearly every skill outside a book
tree. Use --redlines when optimizing a book tree, where that structure is the
contract. This mirrors "exegesis lint --check redlines".

This is the structural half of a double-gated pipeline. "skillsaw gate" decides
on score; this decides on structure, and it is deliberately the stricter of the
two: "skillsaw eval" only *penalises* a blown description cap, so a gain
elsewhere can outweigh it, whereas an edit that fails here is rejected outright.

Runtime neutrality is not checked here — "skillsaw scan" gates that separately.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, args []string) error {
	if bad := root.MisplacedFlag(args); bad != "" {
		return fmt.Errorf(
			"preflight: %q looks like a flag after arguments; put flags before positional arguments",
			bad,
		)
	}
	if len(args) == 0 {
		return errors.New("preflight: pass at least one SKILL_DIR")
	}

	results := make([]skillDefects, 0, len(args))
	total := 0
	for _, dir := range args {
		s, err := skill.Load(dir)
		if err != nil {
			return fmt.Errorf("preflight: %w", err)
		}
		defects := edit.StructuralDefects(s, cfg.Redlines)
		total += len(defects)
		results = append(results, skillDefects{Skill: filepath.Base(dir), Defects: defects})
	}

	if err := cfg.emit(results); err != nil {
		return err
	}
	if total > 0 {
		return root.ExitError(1)
	}
	return nil
}

// emit writes the verdicts as JSON (--json) or human-readable text.
func (cfg *Config) emit(results []skillDefects) error {
	if cfg.JSON {
		enc := json.NewEncoder(cfg.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(results); err != nil {
			return fmt.Errorf("preflight: encode json: %w", err)
		}
		return nil
	}
	for _, r := range results {
		if len(r.Defects) == 0 {
			_, _ = fmt.Fprintf(cfg.Stdout, "%s: ok (structurally sound)\n", r.Skill)
			continue
		}
		_, _ = fmt.Fprintf(cfg.Stdout, "%s: %d defect(s)\n", r.Skill, len(r.Defects))
		for _, d := range r.Defects {
			_, _ = fmt.Fprintf(cfg.Stdout, "  %s: %s\n", d.Severity, d.Message)
		}
	}
	return nil
}
