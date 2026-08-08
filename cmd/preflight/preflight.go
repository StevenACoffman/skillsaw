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
	"os"
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
	JSON      bool
	Redlines  bool
	Against   string
	MaxGrowth float64
	Flags     *ff.FlagSet
	Command   *ff.Command
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
	cfg.Flags.StringVar(&cfg.Against, 0, "against", "",
		"the pre-edit SKILL.md to judge the edit against (one SKILL_DIR only)")
	cfg.Flags.Float64Var(&cfg.MaxGrowth, 0, "max-growth", edit.DefaultMaxGrowth,
		"size ceiling with --against, as a multiple of the original")
	cfg.Command = &ff.Command{
		Name:      "preflight",
		Usage:     "skillsaw preflight [--redlines] [--against ORIG] [--json] SKILL_DIR ...",
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

With --against, the edit is also judged against the text it replaced: an edit that
left the content hash unchanged did nothing, and one that grew past --max-growth
(default 1.5, darwin's ratio) has stopped being an edit and become a rewrite. Both
were previously left to the caller as shell arithmetic that only printed a warning;
here they fail the command like every other defect.

--against takes exactly one SKILL_DIR. One original cannot describe several edits,
and comparing every directory to one file would report defects that are arithmetic
accidents rather than real ones.

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
	if cfg.Against != "" && len(args) != 1 {
		return errors.New(
			"preflight: --against compares one edit to one original; pass a single SKILL_DIR")
	}
	original, err := cfg.loadOriginal()
	if err != nil {
		return err
	}

	results := make([]skillDefects, 0, len(args))
	total := 0
	for _, dir := range args {
		s, err := skill.Load(dir)
		if err != nil {
			return fmt.Errorf("preflight: %w", err)
		}
		defects := edit.StructuralDefects(s, cfg.Redlines)
		if cfg.Against != "" {
			defects = append(defects, edit.AgainstOriginal(original, s.Raw, cfg.MaxGrowth)...)
		}
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

// loadOriginal reads the pre-edit text, or returns "" when --against was not given.
func (cfg *Config) loadOriginal() (string, error) {
	if cfg.Against == "" {
		return "", nil
	}
	b, err := os.ReadFile(cfg.Against)
	if err != nil {
		return "", fmt.Errorf("preflight: read original %s: %w", cfg.Against, err)
	}
	return string(b), nil
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
