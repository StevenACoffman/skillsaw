// Package regression implements the "regression" command: fail when a skill's score has
// dropped against its own recent history, rather than against a fixed threshold.
//
// A fixed threshold answers "is this good enough" and says nothing about a skill that was
// at 92 and is now at 84. This answers the other question, from the log the ratchet already
// writes.
package regression

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/skillet/timeseries"
	"github.com/StevenACoffman/skillsaw/cmd/root"
	"github.com/StevenACoffman/skillsaw/internal/auditlog"
)

// Config holds the regression command configuration.
type Config struct {
	*root.Config
	File       string
	Skill      string
	Tolerance  float64
	Window     int
	MinHistory int
	JSON       bool
	Flags      *ff.FlagSet
	Command    *ff.Command
}

// report is the command's output shape.
type report struct {
	Skill    string             `json:"skill"`
	Verdict  timeseries.Verdict `json:"verdict"`
	Measured bool               `json:"measured"`
	Editions []string           `json:"editions,omitempty"`
}

// New creates and registers the regression command.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.registerFlags(parent)
	cfg.Command = &ff.Command{
		Name:      "regression",
		Usage:     "skillsaw regression --skill NAME [--file results.tsv] [--tolerance T]",
		ShortHelp: "fail when a skill's score dropped against its own recent history",
		LongHelp: `Compare a skill's most recent score in results.tsv against the mean of the
scores before it, and exit 1 when the drop exceeds --tolerance.

This is the check a fixed threshold cannot make. "Above 80" says nothing about a
skill that was at 92 and is now at 84, and in an optimize loop that is exactly the
movement worth catching.

Rows whose new_score is not a number are skipped, not read as zero: a baseline row
records "-", and folding that in would manufacture a regression out of a run that
measured nothing.

With too little history the command reports that it did not compare, and exits 0.
That is not a pass — it is the absence of an opinion, and the two are printed
differently. Failing the first run of a metric is the failure --min-history exists
to prevent, because the only way to fix it is to stop measuring.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

// registerFlags declares the command's flags.
func (cfg *Config) registerFlags(parent *root.Config) {
	cfg.Flags = ff.NewFlagSet("regression").SetParent(parent.Flags)
	cfg.Flags.StringVar(&cfg.File, 0, "file", "results.tsv", "the optimization log to read")
	cfg.Flags.StringVar(&cfg.Skill, 0, "skill", "", "the skill to judge (required)")
	cfg.Flags.Float64Var(
		&cfg.Tolerance,
		0,
		"tolerance",
		0,
		"how far below the baseline is still acceptable, in score points; 0 means any drop regresses",
	)
	cfg.Flags.IntVar(&cfg.Window, 0, "window", 0,
		"how many recent measurements form the baseline; 0 means all of them")
	cfg.Flags.IntVar(&cfg.MinHistory, 0, "min-history", 0,
		"fewest measurements that may form a baseline; below it nothing is compared")
	cfg.Flags.BoolVar(&cfg.JSON, 0, "json", "emit the verdict as JSON")
}

func (cfg *Config) exec(_ context.Context, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf(
			"regression: unexpected argument %q; the skill is named by --skill",
			args[0],
		)
	}
	if cfg.Skill == "" {
		return errors.New("regression: --skill is required; one log holds many skills")
	}
	rows, err := cfg.read()
	if err != nil {
		return err
	}
	history, current, ok := auditlog.Series(rows)
	editions := auditlog.Editions(rows)
	// Two editions in one history are two different questions, and averaging the answers
	// produces a number about neither. This is the guard that stops a rubric change from
	// being laundered through the ratchet's own log: the history refuses to span one.
	if len(editions) > 1 {
		ok = false
	}
	rep := report{Skill: cfg.Skill, Measured: ok, Editions: editions}
	if ok {
		rep.Verdict = timeseries.Detect(history, current, timeseries.Config{
			Window: cfg.Window, MinHistory: cfg.MinHistory, Tolerance: cfg.Tolerance,
		})
	}
	if err := cfg.emit(&rep); err != nil {
		return err
	}
	if rep.Verdict.Regressed {
		return root.ExitError(1)
	}
	return nil
}

// read loads the log and keeps only the named skill's rows.
func (cfg *Config) read() ([]auditlog.Row, error) {
	f, err := os.Open(cfg.File)
	if err != nil {
		return nil, fmt.Errorf("regression: open %s: %w", cfg.File, err)
	}
	defer func() { _ = f.Close() }()
	all, err := auditlog.Read(f)
	if err != nil {
		return nil, fmt.Errorf("regression: %w", err)
	}
	rows := make([]auditlog.Row, 0, len(all))
	for i := range all {
		if all[i].Skill == cfg.Skill {
			rows = append(rows, all[i])
		}
	}
	return rows, nil
}

// emit renders the verdict, keeping "did not compare" visibly distinct from "no
// regression": one is an absent opinion and the other is a held one, and a reader who
// cannot tell them apart will read a silent run as a pass.
func (cfg *Config) emit(rep *report) error {
	if cfg.JSON {
		enc := json.NewEncoder(cfg.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return fmt.Errorf("regression: encode json: %w", err)
		}
		return nil
	}
	v := rep.Verdict
	switch {
	case len(rep.Editions) > 1:
		_, _ = fmt.Fprintf(cfg.Stdout,
			"%s: NOT COMPARED — the history spans %d rubric editions (%s); scores produced "+
				"under different rules are not a series. Re-score under one.\n",
			rep.Skill, len(rep.Editions), strings.Join(rep.Editions, ", "))
	case !rep.Measured:
		_, _ = fmt.Fprintf(cfg.Stdout, "%s: NOT COMPARED — no scored rows in %s\n",
			rep.Skill, cfg.File)
	case !v.Compared:
		_, _ = fmt.Fprintf(cfg.Stdout,
			"%s: NOT COMPARED — too little history to form a baseline; current %.2f\n",
			rep.Skill, v.Current)
	case v.Regressed:
		_, _ = fmt.Fprintf(cfg.Stdout,
			"%s: REGRESSED — %.2f against a baseline of %.2f over %d run(s), a drop of %.2f "+
				"past a tolerance of %.2f\n",
			rep.Skill, v.Current, v.Baseline, v.N, v.Drop, cfg.Tolerance)
	default:
		_, _ = fmt.Fprintf(cfg.Stdout,
			"%s: ok — %.2f against a baseline of %.2f over %d run(s)\n",
			rep.Skill, v.Current, v.Baseline, v.N)
	}
	return nil
}
