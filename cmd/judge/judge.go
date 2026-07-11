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

	"github.com/StevenACoffman/skillsaw/cmd/root"
	judgelib "github.com/StevenACoffman/skillsaw/internal/judge"
)

// Config holds the judge command configuration.
type Config struct {
	*root.Config
	Checks  string
	Output  string
	JSON    bool
	Flags   *ff.FlagSet
	Command *ff.Command
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
		"path to a JSON array of rule checks (required)",
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
		Usage:     "skillsaw judge --checks checks.json [--output out.txt]",
		ShortHelp: "score an output against deterministic rule checks",
		LongHelp: `Score an output against a set of rule checks (darwin spec §8.5) — the
deterministic first-line dim-8 mechanism, ported from SkillOpt-Sleep's judges.

--checks is a JSON array of {"op","arg"} rules. Supported ops:
  section_present  a heading line contains arg
  regex            arg is an RE2 pattern that matches the output
  contains         arg is a substring of the output
  tool_called      the output names the tool arg (heuristic: substring)
  max_chars        rune count <= arg
  min_chars        rune count >= arg

The output under test is read from --output (default stdin). Reports hard
(1.0 iff every check passes), soft (passed/total), and a per-check reason.
Exit code is 1 when hard is 0, so a harness can branch on the verdict.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, _ []string) error {
	if cfg.Checks == "" {
		return errors.New("judge: --checks is required")
	}
	checks, err := cfg.loadChecks()
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
