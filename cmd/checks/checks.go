// Package checks implements the "checks" command: walk a skill tree and report how
// many behavioral test-prompts cases still lack a scorable check, so a check-authoring
// campaign can see how much is left and gate on it the way `verified` gates structure.
package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/skillet/skill"
	"github.com/StevenACoffman/skillet/testprompts"
	"github.com/StevenACoffman/skillsaw/cmd/root"
)

// Config holds the checks command's flags and ff wiring.
type Config struct {
	*root.Config
	Tree    string
	JSON    bool
	Flags   *ff.FlagSet
	Command *ff.Command
}

// gap is one skill's check coverage: how many behavioral cases carry no check (and none
// derivable) out of how many behavioral cases it has.
type gap struct {
	Skill      string `json:"skill"`
	Missing    int    `json:"missing"`
	Behavioral int    `json:"behavioral"`
	Note       string `json:"note,omitempty"` // set when test-prompts.json could not be read
}

// New creates and registers the checks command under parent.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("checks").SetParent(parent.Flags)
	cfg.Flags.StringVar(&cfg.Tree, 0, "tree", ".", "the skill tree to scan")
	cfg.Flags.BoolVar(&cfg.JSON, 0, "json", "emit the report as JSON")
	cfg.Command = &ff.Command{
		Name:      "checks",
		Usage:     "skillsaw checks [--tree DIR] [--json]",
		ShortHelp: "report behavioral test-prompts cases that still lack checks",
		LongHelp: `Walk a skill tree and report, per skill, how many behavioral cases in
its test-prompts.json carry no check and none derivable from their expected text — the
question "skillsaw judge --all" answers for one skill, asked across many.

Only behavioral cases (should_trigger, edge_case) are counted: a should_not_trigger
decoy has no output to score, so counting it would make the remaining work look
permanently unfinishable. Writing checks is authoring work no tool can derive; this
reports the gap, it does not fill it.

Exits non-zero while any behavioral case lacks a check, so it can gate a check-authoring
campaign the way "verified" gates structure.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, args []string) error {
	if err, bad := root.MisplacedFlag("checks", args); bad {
		return err
	}
	if len(args) > 0 {
		return root.Usagef("checks: takes no positional arguments; pass --tree DIR")
	}
	dirs, err := skill.Discover(cfg.Tree)
	if err != nil {
		return fmt.Errorf("checks: %w", err)
	}
	gaps := make([]gap, 0, len(dirs))
	for _, dir := range dirs {
		gaps = append(gaps, scanSkill(dir))
	}
	return cfg.emit(gaps)
}

// scanSkill counts the behavioral cases in dir's test-prompts.json that lack a check. An
// unreadable file is reported with a note and zero counts rather than aborting the walk:
// its presence is `verified`/`tests`'s gate, not this command's, and one bad skill must
// not hide the coverage of the rest.
func scanSkill(dir string) gap {
	g := gap{Skill: filepath.Base(dir)}
	f, err := testprompts.Load(filepath.Join(dir, "test-prompts.json"))
	if err != nil {
		g.Note = "no readable test-prompts.json"
		return g
	}
	behavioral := f.Behavioral()
	g.Behavioral = len(behavioral)
	for i := range behavioral {
		if checks, _ := testprompts.ChecksFor(&behavioral[i]); len(checks) == 0 {
			g.Missing++
		}
	}
	return g
}

// emit prints the report and returns ExitError(1) while any behavioral case lacks a
// check. An unreadable test-prompts.json is surfaced but does not gate.
func (cfg *Config) emit(gaps []gap) error {
	totalMissing, totalBehavioral := 0, 0
	for _, g := range gaps {
		totalMissing += g.Missing
		totalBehavioral += g.Behavioral
	}
	if cfg.JSON {
		enc := json.NewEncoder(cfg.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(gaps); err != nil {
			return fmt.Errorf("checks: encode json: %w", err)
		}
	} else {
		for _, g := range gaps {
			switch {
			case g.Note != "":
				_, _ = fmt.Fprintf(cfg.Stdout, "%s: %s\n", g.Skill, g.Note)
			case g.Missing > 0:
				_, _ = fmt.Fprintf(cfg.Stdout, "%s: %d/%d behavioral case(s) lack checks\n",
					g.Skill, g.Missing, g.Behavioral)
			}
		}
		_, _ = fmt.Fprintf(
			cfg.Stdout,
			"%d of %d behavioral case(s) across %d skill(s) lack checks\n",
			totalMissing,
			totalBehavioral,
			len(gaps),
		)
	}
	if totalMissing > 0 {
		return root.ExitError(1)
	}
	return nil
}
