// Package activation implements the "activation" command: report trigger
// accuracy for a skill from its type-tagged test-prompts (S3). This signal is
// surfaced on its own and is deliberately NOT part of "eval"'s 9-dimension total.
package activation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/skillet/ratchet"
	"github.com/StevenACoffman/skillet/skill"
	"github.com/StevenACoffman/skillet/testprompts"
	"github.com/StevenACoffman/skillsaw/cmd/root"
)

// Config holds the activation command configuration.
type Config struct {
	*root.Config
	Min     float64
	JSON    bool
	Flags   *ff.FlagSet
	Command *ff.Command
}

// report pairs a skill name with its activation result for output.
type report struct {
	Skill      string         `json:"skill"`
	Activation ratchet.Report `json:"activation"`
}

// New creates and registers the activation command.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("activation").SetParent(parent.Flags)
	cfg.Flags.Float64Var(
		&cfg.Min,
		0,
		"min",
		0,
		"exit non-zero if any skill's net_utility is below this (range -1..1; 0 = fires on decoys as much as targets)",
	)
	cfg.Flags.BoolVar(&cfg.JSON, 0, "json", "emit the report as JSON")
	cfg.Command = &ff.Command{
		Name:      "activation",
		Usage:     "skillsaw activation [--min N] [--json] SKILL_DIR ...",
		ShortHelp: "report trigger accuracy from a skill's type-tagged test-prompts",
		LongHelp: `Score how well each skill's description trigger vocabulary matches its
should_trigger prompts (targets) and excludes its should_not_trigger decoys
(distractors), using the type-tagged test-prompts.json (the exegesis/book2skill
contract).

Reports a routing confusion matrix — TPR/FPR/FNR with Wilson 95% intervals and
net_utility = (TP-FP)/total in [-1,1]. This is a deterministic, explainable proxy
reported on its own; it is NOT folded into eval's weighted 9-dimension total
(whose weights are fixed at 100). Use --min to gate on net_utility in CI.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, args []string) error {
	if bad := root.MisplacedFlag(args); bad != "" {
		return fmt.Errorf(
			"activation: %q looks like a flag after arguments; put flags before positional arguments",
			bad,
		)
	}
	if len(args) == 0 {
		return errors.New("activation: need at least one skill directory")
	}
	reports := make([]report, 0, len(args))
	below := false
	for _, dir := range args {
		rep, err := scoreDir(dir)
		if err != nil {
			return err
		}
		reports = append(reports, rep)
		if rep.Activation.NetUtility < cfg.Min {
			below = true
		}
	}
	if err := cfg.emit(reports); err != nil {
		return err
	}
	if below {
		return root.ExitError(1)
	}
	return nil
}

func scoreDir(dir string) (report, error) {
	s, err := skill.Load(dir)
	if err != nil {
		return report{}, fmt.Errorf("activation: %w", err)
	}
	f, err := testprompts.Load(filepath.Join(dir, "test-prompts.json"))
	if err != nil {
		return report{}, fmt.Errorf("activation: %w", err)
	}
	triggers, decoys := split(f)
	return report{
		Skill:      filepath.Base(dir),
		Activation: ratchet.Score(s.Description, triggers, decoys),
	}, nil
}

func split(f *testprompts.File) (triggers, decoys []string) {
	for _, c := range f.Tests {
		switch c.Type {
		case testprompts.TypeShouldTrigger:
			triggers = append(triggers, c.Prompt)
		case testprompts.TypeShouldNotTrigger:
			decoys = append(decoys, c.Prompt)
		}
	}
	return triggers, decoys
}

func (cfg *Config) emit(reports []report) error {
	if cfg.JSON {
		enc := json.NewEncoder(cfg.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(reports); err != nil {
			return fmt.Errorf("activation: encode json: %w", err)
		}
		return nil
	}
	for i := range reports {
		r := &reports[i]
		a := r.Activation
		_, _ = fmt.Fprintf(cfg.Stdout,
			"%s: net_utility %+.2f  TPR %.2f [%.2f,%.2f] (%d/%d)  FPR %.2f [%.2f,%.2f] (%d/%d)\n",
			r.Skill, a.NetUtility,
			a.TPR, a.TPRInterval[0], a.TPRInterval[1], a.TP, a.Targets,
			a.FPR, a.FPRInterval[0], a.FPRInterval[1], a.FP, a.Distractors)
		for _, w := range a.Why {
			_, _ = fmt.Fprintf(cfg.Stdout, "  - %s\n", w)
		}
	}
	return nil
}
