// Package judge implements the "judge" command: score an output against a set of
// deterministic rule checks (darwin spec §8.5). It is the standalone behavioral
// scorer a harness (or a human pasting a model's output) feeds; it never invents
// the output under test, so it does not fake dim 8 inside "eval".
package judge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/peterbourgon/ff/v4"

	judgelib "github.com/StevenACoffman/skillet/judge"
	"github.com/StevenACoffman/skillet/testprompts"
	"github.com/StevenACoffman/skillsaw/cmd/root"
)

const (
	judgeUsage     = "skillsaw judge (--checks checks.json | --from-test-prompts tp.json --id N) [--output out.txt]"
	judgeShortHelp = "score an output against deterministic rule checks"
	judgeLongHelp  = `Score an output against a set of rule checks (darwin spec §8.5) — the
deterministic first-line dim-8 mechanism, ported from SkillOpt-Sleep's judges.

Provide the checks one of two ways:
  --checks            a JSON array of {"op","arg"} rules
  --from-test-prompts a test-prompts.json (the exegesis/book2skill contract);
                      --id selects the case. Its embedded "checks" are used if
                      present, otherwise checks are derived from the case's
                      "expected" text. Fails if neither yields a check.

Supported ops:
  section_present  a heading line contains arg
  regex            arg is an RE2 pattern that matches the output
  contains         arg is a substring of the output
  tool_called      the output names the tool arg (heuristic: substring)
  max_chars        rune count <= arg
  min_chars        rune count >= arg
  boolean          last "ANSWER: yes/no/true/false" equals arg (yes/no/true/false)
  multiple_choice  last "ANSWER: A-E" equals arg (a letter A-E)
  numeric_order_of_magnitude  |log10(answer/gold)| <= tol; arg = "gold[:tol]" (tol default 1)

The output under test is read from --output (default stdin). Reports hard
(1.0 iff every check passes), soft (passed/total), and a per-check reason.
Exit code is 1 when hard is 0, so a harness can branch on the verdict.`
)

// Config holds the judge command configuration.
type Config struct {
	*root.Config
	Checks          string
	FromTestPrompts string
	ID              int
	Output          string
	JSON            bool
	Flags           *ff.FlagSet
	Command         *ff.Command
}

// New creates and registers the judge command.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("judge").SetParent(parent.Flags)
	cfg.Flags.StringVar(
		&cfg.Checks,
		'c',
		"checks",
		"",
		"path to a JSON array of rule checks",
	)
	cfg.Flags.StringVar(
		&cfg.FromTestPrompts,
		0,
		"from-test-prompts",
		"",
		"path to a test-prompts.json; checks come from a case's embedded checks or its expected text",
	)
	cfg.Flags.IntVar(
		&cfg.ID,
		0,
		"id",
		1,
		"case id to judge when --from-test-prompts is set",
	)
	cfg.Flags.StringVar(
		&cfg.Output,
		'o',
		"output",
		"-",
		"path to the output under test (\"-\" = stdin)",
	)
	cfg.Flags.BoolVar(&cfg.JSON, 0, "json", "emit the result as JSON")
	cfg.Command = &ff.Command{
		Name:      "judge",
		Usage:     judgeUsage,
		ShortHelp: judgeShortHelp,
		LongHelp:  judgeLongHelp,
		Flags:     cfg.Flags,
		Exec:      cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, _ []string) error {
	checks, err := cfg.resolveChecks()
	if err != nil {
		return err
	}
	output, err := cfg.readOutput()
	if err != nil {
		return err
	}

	res, err := judgelib.Score(output, checks)
	if err != nil {
		return fmt.Errorf("judge: %w", err)
	}

	if err := cfg.emit(res); err != nil {
		return err
	}
	if res.Hard == 0 {
		return root.ExitError(1)
	}
	return nil
}

// resolveChecks obtains the checks from --checks or, failing that, from a case
// in a --from-test-prompts file (embedded checks, else derived from expected).
func (cfg *Config) resolveChecks() ([]judgelib.Check, error) {
	switch {
	case cfg.FromTestPrompts != "":
		return cfg.checksFromTestPrompts()
	case cfg.Checks != "":
		return cfg.loadChecks()
	default:
		return nil, errors.New("judge: one of --checks or --from-test-prompts is required")
	}
}

func (cfg *Config) checksFromTestPrompts() ([]judgelib.Check, error) {
	f, err := testprompts.Load(cfg.FromTestPrompts)
	if err != nil {
		return nil, fmt.Errorf("judge: %w", err)
	}
	c, ok := f.Find(cfg.ID)
	if !ok {
		return nil, fmt.Errorf("judge: no case with id %d in %s", cfg.ID, cfg.FromTestPrompts)
	}
	checks, derived := testprompts.ChecksFor(&c)
	if len(checks) == 0 {
		return nil, fmt.Errorf(
			"judge: case %d has no checks and none derivable from its expected text", cfg.ID)
	}
	if derived {
		_, _ = fmt.Fprintf(cfg.Stderr,
			"judge: derived %d check(s) from case %d expected text\n", len(checks), cfg.ID)
	}
	return checks, nil
}

func (cfg *Config) loadChecks() ([]judgelib.Check, error) {
	data, err := os.ReadFile(cfg.Checks)
	if err != nil {
		return nil, fmt.Errorf("judge: read checks: %w", err)
	}
	var checks []judgelib.Check
	if err := json.Unmarshal(data, &checks); err != nil {
		return nil, fmt.Errorf("judge: parse checks %s: %w", cfg.Checks, err)
	}
	return checks, nil
}

func (cfg *Config) readOutput() (string, error) {
	if cfg.Output == "-" {
		data, err := io.ReadAll(cfg.Stdin)
		if err != nil {
			return "", fmt.Errorf("judge: read stdin: %w", err)
		}
		return string(data), nil
	}
	data, err := os.ReadFile(cfg.Output)
	if err != nil {
		return "", fmt.Errorf("judge: read output: %w", err)
	}
	return string(data), nil
}

func (cfg *Config) emit(res judgelib.Result) error {
	if cfg.JSON {
		enc := json.NewEncoder(cfg.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			return fmt.Errorf("judge: encode json: %w", err)
		}
		return nil
	}
	_, _ = fmt.Fprintf(cfg.Stdout, "hard: %.0f  soft: %.2f\n", res.Hard, res.Soft)
	for _, w := range res.Why {
		_, _ = fmt.Fprintf(cfg.Stdout, "  - %s\n", w)
	}
	return nil
}
