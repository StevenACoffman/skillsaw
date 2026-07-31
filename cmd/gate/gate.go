// Package gate implements the "gate" command: the validation-gate / ratchet
// decision (darwin spec §8.6, a port of SkillOpt's evaluate_gate). Given a
// candidate score and the current/best scores, it decides accept_new_best,
// accept, or reject using strict ">" — ties reject and do not promote.
package gate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/skillsaw/cmd/root"
	gatelib "github.com/StevenACoffman/skillsaw/internal/gate"
)

// Config holds the gate command configuration.
type Config struct {
	*root.Config
	Candidate  string
	Current    string
	Best       string
	BestStep   string
	GlobalStep string
	JSON       bool
	Flags      *ff.FlagSet
	Command    *ff.Command
}

// New creates and registers the gate command.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("gate").SetParent(parent.Flags)
	cfg.Flags.StringVar(&cfg.Candidate, 'c', "candidate", "", "candidate skill score (required)")
	cfg.Flags.StringVar(&cfg.Current, 'u', "current", "", "current skill score (required)")
	cfg.Flags.StringVar(&cfg.Best, 'b', "best", "", "best-so-far score (defaults to current)")
	cfg.Flags.StringVar(&cfg.BestStep, 0, "best-step", "0", "step at which best was set")
	cfg.Flags.StringVar(&cfg.GlobalStep, 0, "global-step", "0", "current step")
	cfg.Flags.BoolVar(&cfg.JSON, 0, "json", "emit the decision as JSON")
	cfg.Command = &ff.Command{
		Name:      "gate",
		Usage:     "skillsaw gate --candidate N --current N [--best N]",
		ShortHelp: "decide keep/revert for a candidate score (validation gate)",
		LongHelp: `Decide whether to keep an edited skill, using the darwin ratchet / SkillOpt
validation gate (spec §8.6). Comparison is strict ">": a candidate is accepted
only if it beats the current score, and becomes the new best only if it also
beats the best score. Ties reject and do not promote.

Scores are the already-projected comparison metric (darwin uses one weighted
rubric total — see "skillsaw eval"). Outcomes:

  accept_new_best   candidate > current AND candidate > best   -> keep, new best
  accept            candidate > current only                   -> keep, best unchanged
  reject            candidate <= current                       -> revert

The action above is the disposition axis. The result also reports a separate
measured axis — status (improved/tie/regressed) and delta (candidate-current) —
so a high or improved score is not conflated with the keep/revert decision.

Exit code is 0 on any accept, 1 on reject, so a script can branch on it.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, _ []string) error {
	if cfg.Candidate == "" || cfg.Current == "" {
		return errors.New("gate: --candidate and --current are required")
	}
	cand, err := parseScore("candidate", cfg.Candidate)
	if err != nil {
		return err
	}
	current, err := parseScore("current", cfg.Current)
	if err != nil {
		return err
	}
	best := current
	if cfg.Best != "" {
		if best, err = parseScore("best", cfg.Best); err != nil {
			return err
		}
	}
	bestStep, err := parseStep("best-step", cfg.BestStep)
	if err != nil {
		return err
	}
	globalStep, err := parseStep("global-step", cfg.GlobalStep)
	if err != nil {
		return err
	}

	res := gatelib.Evaluate(cand, current, best, bestStep, globalStep)

	if cfg.JSON {
		enc := json.NewEncoder(cfg.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			return fmt.Errorf("gate: encode json: %w", err)
		}
	} else {
		_, _ = fmt.Fprintf(cfg.Stdout, "%s (%s, delta %+.1f)\n", res.Action, res.Status, res.Delta)
		_, _ = fmt.Fprintf(cfg.Stdout, "  current -> %.1f\n  best    -> %.1f (step %d)\n",
			res.CurrentScore, res.BestScore, res.BestStep)
	}

	if res.Action == gatelib.Reject {
		return root.ExitError(1)
	}
	return nil
}

func parseScore(name, v string) (float64, error) {
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("gate: --%s: invalid score %q", name, v)
	}
	return f, nil
}

func parseStep(name, v string) (int, error) {
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("gate: --%s: invalid integer %q", name, v)
	}
	return n, nil
}
