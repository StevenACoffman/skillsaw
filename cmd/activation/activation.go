// Package activation implements the "activation" command: report trigger
// accuracy for a skill from its type-tagged test-prompts (S3). This signal is
// surfaced on its own and is deliberately NOT part of "eval"'s 9-dimension total.
package activation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/skillet/ratchet"
	"github.com/StevenACoffman/skillet/skill"
	"github.com/StevenACoffman/skillet/testprompts"
	"github.com/StevenACoffman/skillsaw/cmd/root"
	"github.com/StevenACoffman/skillsaw/internal/noise"
)

// promptsFile is the type-tagged prompt set activation reads.
const promptsFile = "test-prompts.json"

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
	Gate       noise.Verdict  `json:"gate"`
	Bound      [2]float64     `json:"net_utility_bound"`

	// Unmeasured says why this skill could not be scored, and is empty when it was.
	//
	// A skill with no SKILL.md or no readable test-prompts is not a bad skill, it is one
	// nobody measured -- the same third state as an unresolved bound, an unexamined
	// aspect, or a base with no baseline. Carried per skill rather than returned as an
	// error because one missing file used to abort the run and discard every other
	// result in it.
	Unmeasured string `json:"unmeasured,omitempty"`

	// Evidence says which halves of the confusion matrix this sample supports. A skill can
	// be measured and still have an unevidenced half.
	Evidence noise.Evidenced `json:"evidence"`
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
		"exit non-zero unless every skill's net_utility bound clears this (range -1..1; 0 = fires on decoys as much as targets)",
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
(whose weights are fixed at 100). Use --min to gate on net_utility in CI.

The gate is on a conservative interval around net_utility, not on the point
estimate: a skill whose interval spans --min is reported UNRESOLVED and exits
non-zero, because a sample too small to separate the two must not read as a
pass. Adding test-prompts, not editing the skill, is what resolves that.`,
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
	failed := false
	for _, dir := range args {
		rep, err := scoreDir(dir)
		if err != nil {
			// Reported, not returned. A gap in one skill says nothing about the others,
			// and failing here threw away every result already computed.
			rep = report{Skill: filepath.Base(dir), Unmeasured: err.Error()}
		}
		rep.Bound = noise.UtilityBound(&rep.Activation)
		rep.Evidence = noise.EvidenceIn(&rep.Activation)
		rep.Gate = noise.Gate(rep.Bound, cfg.Min)
		reports = append(reports, rep)
		// Anything that is not an affirmative pass fails the gate, so an unresolved
		// result and an unmeasured one both cost what a failure costs -- but only after
		// everything has been reported.
		if rep.Unmeasured != "" || rep.Gate != noise.VerdictClears {
			failed = true
		}
	}
	if err := cfg.emit(reports); err != nil {
		return err
	}
	if failed {
		return root.ExitError(1)
	}
	return nil
}

// scoreDir reads one skill and scores its trigger accuracy.
//
// Its errors describe why a skill could not be measured and are shown to a reader beside
// the skills that were, so they say what is missing rather than restating the path the
// caller just typed -- three copies of it is what os.Open's wrapped chain produces here.
func scoreDir(dir string) (report, error) {
	s, err := skill.Load(dir)
	if err != nil {
		return report{}, errors.New("no readable SKILL.md")
	}
	if _, err := os.Stat(filepath.Join(dir, promptsFile)); err != nil {
		return report{}, errors.New("no " + promptsFile)
	}
	// Scoring trigger vocabulary needs vocabulary. Without a description ratchet.Score
	// compares every prompt against the empty string, misses all of them, and reports a
	// full confusion matrix that is indistinguishable in the output from one computed over
	// a real description -- while explaining each miss as "description vocabulary misses
	// it", which is the opposite of what happened.
	//
	// Two causes, named apart because they want different repairs. skillet has carried
	// FrontmatterErr since v0.9.0 for exactly this guard; lint, redlines and speclint all
	// take it, and this was the consumer that did not. eval prices both defects already
	// (dim 1, 6 points and 3); this only declines to pretend it measured something.
	if s.FrontmatterErr != nil {
		return report{}, errors.New(
			"frontmatter did not parse, so there is no description to score",
		)
	}
	if strings.TrimSpace(s.Description) == "" {
		return report{}, errors.New("no description to score")
	}
	f, err := testprompts.Load(filepath.Join(dir, promptsFile))
	if err != nil {
		return report{}, fmt.Errorf("%s is unreadable: %w", promptsFile, err)
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
		if r.Unmeasured != "" {
			_, _ = fmt.Fprintf(cfg.Stdout, "%s: UNMEASURED — %s\n", r.Skill, r.Unmeasured)
			continue
		}
		a := r.Activation
		_, _ = fmt.Fprintf(cfg.Stdout,
			"%s: net_utility %+.2f  TPR %.2f [%.2f,%.2f] (%d/%d)  FPR %.2f [%.2f,%.2f] (%d/%d)\n",
			r.Skill, a.NetUtility,
			a.TPR, a.TPRInterval[0], a.TPRInterval[1], a.TP, a.Targets,
			a.FPR, a.FPRInterval[0], a.FPRInterval[1], a.FP, a.Distractors)
		_, _ = fmt.Fprintf(cfg.Stdout, "  gate: %s\n", explain(r, cfg.Min))
		if caveat := r.Evidence.Caveat(); caveat != "" {
			_, _ = fmt.Fprintf(cfg.Stdout, "  unevidenced: %s\n", caveat)
		}
		for _, w := range a.Why {
			_, _ = fmt.Fprintf(cfg.Stdout, "  - %s\n", w)
		}
	}
	return nil
}
