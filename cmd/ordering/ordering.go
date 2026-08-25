// Package ordering implements the "ordering" command: given an agent transcript, report
// whether a skill loaded before any work began.
package ordering

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/skillsaw/cmd/root"
	"github.com/StevenACoffman/skillsaw/internal/transcript"
)

// Config holds the ordering command configuration.
type Config struct {
	*root.Config
	Skill     string
	JSON      bool
	Agreement bool
	Flags     *ff.FlagSet
	Command   *ff.Command
}

// report is one transcript's verdict.
type report struct {
	Transcript string           `json:"transcript"`
	Skill      string           `json:"skill"`
	Order      transcript.Order `json:"order"`
	Tools      int              `json:"tools"`
	Before     []string         `json:"before,omitempty"`
	Err        string           `json:"error,omitempty"`
}

// New creates and registers the ordering command.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("ordering").SetParent(parent.Flags)
	cfg.Flags.StringVar(
		&cfg.Skill,
		0,
		"skill",
		"",
		"the skill that should have loaded first (required)",
	)
	cfg.Flags.BoolVar(&cfg.JSON, 0, "json", "emit the verdicts as JSON")
	cfg.Flags.BoolVar(&cfg.Agreement, 0, "agreement",
		"group repeated runs of one phrasing and report whether they agree")
	cfg.Command = &ff.Command{
		Name:      "ordering",
		Usage:     "skillsaw ordering --skill NAME TRANSCRIPT [TRANSCRIPT ...]",
		ShortHelp: "report whether a skill loaded before the agent started working",
		LongHelp: `Read an agent transcript and report where the named skill's invocation
sits relative to the work around it.

"activation" asks whether a skill would trigger. This asks something the trigger
vocabulary cannot answer: whether anything happened first. An agent that starts
editing and then loads the skill has used it — a reply describes that as success,
truthfully — but part of the artifact was produced outside the guidance it appears
to follow. That is a distinct and worse outcome than never loading it, and only
the event sequence separates the two.

Ordering is not something a prose reply can be trusted to report about itself, so
this reads the structured log rather than the answer: the line-delimited JSON that
"claude -p --output-format stream-json" writes. skillsaw does not run the agent —
see superpowers/tests/explicit-skill-requests/run-test.sh, which does, and writes
this format.

Planning tools (TodoWrite, TaskCreate, TaskUpdate, TaskList, TaskGet) do not count
as having started work: writing down an intention is not acting on one.

Exit is non-zero unless every transcript reports "first", on the same rule the
rest of this tool follows — a result nobody could establish costs what a failure
costs.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, args []string) error {
	if bad := root.MisplacedFlag(args); bad != "" {
		return fmt.Errorf(
			"ordering: %q looks like a flag after arguments; put flags before positional arguments",
			bad,
		)
	}
	if cfg.Skill == "" {
		return errors.New("ordering: --skill is required; there is nothing to look for without it")
	}
	if len(args) == 0 {
		return errors.New("ordering: pass at least one transcript")
	}
	reports := make([]report, 0, len(args))
	failed := false
	for _, path := range args {
		rep := cfg.judge(path)
		reports = append(reports, rep)
		if rep.Order != transcript.OrderFirst {
			failed = true
		}
	}
	if cfg.Agreement {
		if err := cfg.emitAgreement(reports); err != nil {
			return err
		}
	} else if err := cfg.emit(reports); err != nil {
		return err
	}
	if failed {
		return root.ExitError(1)
	}
	return nil
}

// phrasingOf groups a transcript with the other runs of the same phrasing, by the filename
// stem before the first dot: 03-imperative.2.json belongs with 03-imperative.1.json. The
// convention lives here rather than in a flag because the runner writes the names, and a
// tool that had to be told how its own input is laid out would be one more thing to keep
// in step.
func phrasingOf(path string) string {
	base := filepath.Base(path)
	if i := strings.Index(base, "."); i > 0 {
		return base[:i]
	}
	return base
}

// emitAgreement reports, per phrasing, whether its runs converged.
//
// A split is the finding, not an error: it says the wording is not binding, whatever the
// individual verdicts were. So this changes what is printed and never the exit rule --
// making disagreement fail the build would be a new gate, and one that a caller measuring
// bindingness has not asked for.
func (cfg *Config) emitAgreement(reports []report) error {
	names := make([]string, 0, len(reports))
	byName := map[string][]transcript.Order{}
	for i := range reports {
		n := phrasingOf(reports[i].Transcript)
		if _, seen := byName[n]; !seen {
			names = append(names, n)
		}
		byName[n] = append(byName[n], reports[i].Order)
	}
	sort.Strings(names)

	type row struct {
		Phrasing  string               `json:"phrasing"`
		Agreement transcript.Agreement `json:"agreement"`
	}
	rows := make([]row, 0, len(names))
	for _, n := range names {
		rows = append(rows, row{Phrasing: n, Agreement: transcript.Agree(byName[n])})
	}

	if cfg.JSON {
		enc := json.NewEncoder(cfg.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rows); err != nil {
			return fmt.Errorf("ordering: encode json: %w", err)
		}
		return nil
	}
	for _, r := range rows {
		a := r.Agreement
		switch {
		case a.Runs == 1:
			// One sample is not convergence, and calling it unanimous would invite a
			// reader to treat a single observation as a property.
			_, _ = fmt.Fprintf(cfg.Stdout, "%-24s %s (1 run — not repeated)\n",
				r.Phrasing, a.Tally[0].Order)
		case a.Unanimous:
			_, _ = fmt.Fprintf(cfg.Stdout, "%-24s %s in all %d runs\n",
				r.Phrasing, a.Tally[0].Order, a.Runs)
		default:
			parts := make([]string, 0, len(a.Tally))
			for _, v := range a.Tally {
				parts = append(parts, fmt.Sprintf("%d %s", v.Count, v.Order))
			}
			_, _ = fmt.Fprintf(cfg.Stdout, "%-24s SPLIT over %d runs: %s\n",
				r.Phrasing, a.Runs, strings.Join(parts, ", "))
		}
	}
	return nil
}

// judge reads one transcript and reports its verdict.
//
// An unreadable transcript is reported rather than returned, so one bad path does not
// discard the verdicts already computed -- the same rule "activation" follows.
func (cfg *Config) judge(path string) report {
	rep := report{Transcript: path, Skill: cfg.Skill}
	f, err := os.Open(path)
	if err != nil {
		rep.Err = err.Error()
		return rep
	}
	defer func() { _ = f.Close() }()
	uses, err := transcript.Read(f)
	if err != nil {
		rep.Err = err.Error()
		return rep
	}
	rep.Tools = len(uses)
	rep.Order = transcript.OrderOf(uses, cfg.Skill)
	rep.Before = transcript.Before(uses, cfg.Skill)
	return rep
}

// emit renders the verdicts, naming what ran first when something did -- a verdict that
// says work happened without saying what is a verdict nobody can act on.
func (cfg *Config) emit(reports []report) error {
	if cfg.JSON {
		enc := json.NewEncoder(cfg.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(reports); err != nil {
			return fmt.Errorf("ordering: encode json: %w", err)
		}
		return nil
	}
	for i := range reports {
		r := &reports[i]
		switch {
		case r.Err != "":
			_, _ = fmt.Fprintf(cfg.Stdout, "%s: UNREAD — %s\n", r.Transcript, r.Err)
		case r.Order == transcript.OrderAfterAction:
			_, _ = fmt.Fprintf(cfg.Stdout,
				"%s: AFTER ACTION — %s loaded, but %v ran first over %d tool use(s)\n",
				r.Transcript, r.Skill, r.Before, r.Tools)
		case r.Order == transcript.OrderNotTriggered:
			_, _ = fmt.Fprintf(
				cfg.Stdout,
				"%s: NOT TRIGGERED — %s never loaded in %d tool use(s)\n",
				r.Transcript,
				r.Skill,
				r.Tools,
			)
		default:
			_, _ = fmt.Fprintf(cfg.Stdout, "%s: first — %s loaded before any work\n",
				r.Transcript, r.Skill)
		}
	}
	return nil
}
