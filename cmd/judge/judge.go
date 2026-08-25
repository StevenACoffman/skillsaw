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
	"path/filepath"

	"github.com/peterbourgon/ff/v4"

	judgelib "github.com/StevenACoffman/skillet/judge"
	"github.com/StevenACoffman/skillet/testprompts"
	"github.com/StevenACoffman/skillsaw/cmd/root"
	"github.com/StevenACoffman/skillsaw/internal/scores"
)

const (
	judgeUsage     = "skillsaw judge (--checks c.json | --from-test-prompts tp.json (--id N | --all --outputs DIR)) [--output out.txt]"
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
Exit code is 1 when hard is 0, so a harness can branch on the verdict.

With --all, score every behavioral case in --from-test-prompts and report the
dimension base their mean implies -- the arithmetic an optimize loop otherwise does
by hand for dim 8, where the result reaches the keep/revert gate unchecked. Each
case's output is read from DIR/out-<id>.txt under --outputs.

Only should_trigger and edge_case cases are scored. A should_not_trigger decoy has
no good output to score, so including it would drag the base down for a skill
behaving correctly by declining to fire.

A case with no output file is an error, not a skip: the base is a mean, and dropping
a case silently changes the denominator.

--all reports rather than gates, so it exits 0 even when cases fail their checks --
most skills fail some, which is why the base is below 10. It exits non-zero only
when something prevented scoring.`
)

// caseScore is one behavioral case's verdict, for reporting.
type caseScore struct {
	ID   int     `json:"id"`
	Hard float64 `json:"hard"`
	Soft float64 `json:"soft"`
}

// allReport is the --all document: every case's verdict and the base they imply.
type allReport struct {
	Cases    []caseScore `json:"cases"`
	MeanSoft float64     `json:"mean_soft"`
	Base     int         `json:"base"`
	BaseLow  int         `json:"base_low"`
	BaseHigh int         `json:"base_high"`
	Resolved bool        `json:"resolved"`
}

// Config holds the judge command configuration.
type Config struct {
	*root.Config
	Checks          string
	FromTestPrompts string
	ID              int
	All             bool
	Outputs         string
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
	cfg.Flags.BoolVar(&cfg.All, 0, "all",
		"score every behavioral case and report the dimension base their mean implies")
	cfg.Flags.StringVar(&cfg.Outputs, 0, "outputs", "",
		"directory holding out-<id>.txt per case, required with --all")
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
	if cfg.All {
		return cfg.scoreAll()
	}
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

// scoreAll scores every behavioral case and reports the base their mean implies.
func (cfg *Config) scoreAll() error {
	if cfg.FromTestPrompts == "" {
		return errors.New("judge: --all needs --from-test-prompts; there is no per-case " +
			"notion with a single --checks set")
	}
	if cfg.Outputs == "" {
		return errors.New("judge: --all needs --outputs DIR holding out-<id>.txt per case")
	}
	f, err := testprompts.Load(cfg.FromTestPrompts)
	if err != nil {
		return fmt.Errorf("judge: %w", err)
	}
	// Behavioral, not every case: a should_not_trigger decoy has no good output to
	// score, so counting it would lower the base for a skill correctly declining to fire.
	behavioral := f.Behavioral()
	if len(behavioral) == 0 {
		return fmt.Errorf("judge: %s has no should_trigger or edge_case cases to score",
			cfg.FromTestPrompts)
	}
	// Cases specifying nothing to check are collected and reported together rather than
	// failing at the first one. Across the real corpus this is the common state, and a
	// message about case 1 reads as a per-case slip when the file as a whole specifies
	// no checks; naming all of them says what to fix.
	var unscorable []int
	for i := range behavioral {
		if checks, _ := testprompts.ChecksFor(&behavioral[i]); len(checks) == 0 {
			unscorable = append(unscorable, behavioral[i].ID)
		}
	}
	if len(unscorable) > 0 {
		for _, id := range unscorable {
			_, _ = fmt.Fprintf(cfg.Stderr,
				"judge: case %d has no checks and none derivable from its expected text\n", id)
		}
		// Not "score the rest": a base averaged over an unstated subset is not
		// comparable to one averaged over every case, and comparability is the whole
		// point of the total this feeds. Refuse rather than emit a lookalike.
		return fmt.Errorf(
			"judge: %d of %d behavioral case(s) in %s specify no checks, so no dim-8 base "+
				"can be computed; add checks to those cases first",
			len(unscorable), len(behavioral), cfg.FromTestPrompts)
	}
	rep := allReport{Cases: make([]caseScore, 0, len(behavioral))}
	softs := make([]float64, 0, len(behavioral))
	for i := range behavioral {
		score, err := cfg.scoreCase(&behavioral[i])
		if err != nil {
			return err
		}
		rep.Cases = append(rep.Cases, score)
		softs = append(softs, score.Soft)
	}
	agg := scores.Aggregated(softs)
	rep.MeanSoft, rep.Base = agg.MeanSoft, agg.Base
	rep.BaseLow, rep.BaseHigh, rep.Resolved = agg.BaseLow, agg.BaseHigh, agg.Resolved()
	return cfg.emitAll(&rep)
}

// scoreCase judges one case against its output file.
func (cfg *Config) scoreCase(c *testprompts.Case) (caseScore, error) {
	checks, _ := testprompts.ChecksFor(c)
	path := filepath.Join(cfg.Outputs, fmt.Sprintf("out-%d.txt", c.ID))
	data, err := os.ReadFile(path)
	if err != nil {
		// Not a skip: the base is a mean, so dropping a case silently changes the
		// denominator and reports a number computed over fewer cases than it claims.
		return caseScore{}, fmt.Errorf("judge: case %d: read %s: %w", c.ID, path, err)
	}
	res, err := judgelib.Score(string(data), checks)
	if err != nil {
		return caseScore{}, fmt.Errorf("judge: case %d: %w", c.ID, err)
	}
	return caseScore{ID: c.ID, Hard: res.Hard, Soft: res.Soft}, nil
}

// emitAll renders the aggregate as JSON (--json) or one line per case plus the base.
func (cfg *Config) emitAll(rep *allReport) error {
	if cfg.JSON {
		enc := json.NewEncoder(cfg.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return fmt.Errorf("judge: encode json: %w", err)
		}
		return nil
	}
	for _, c := range rep.Cases {
		_, _ = fmt.Fprintf(cfg.Stdout, "case %d: hard %.0f  soft %.2f\n", c.ID, c.Hard, c.Soft)
	}
	_, _ = fmt.Fprintf(cfg.Stdout, "%d case(s), mean soft %.3f, base %d\n",
		len(rep.Cases), rep.MeanSoft, rep.Base)
	if !rep.Resolved {
		// Said plainly rather than as a footnote on the number: a base copied into a
		// scores file is applied as if it were measured, and nothing downstream can tell
		// that this sample could not separate it from its neighbours.
		_, _ = fmt.Fprintf(cfg.Stdout,
			"  unresolved: %d case(s) support base %d-%d; score more cases before "+
				"treating %d as measured\n",
			len(rep.Cases), rep.BaseLow, rep.BaseHigh, rep.Base)
	}
	return nil
}
