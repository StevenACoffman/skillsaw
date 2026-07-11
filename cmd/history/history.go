// Package history implements the "history" command: read and render the darwin
// results.tsv optimization log (spec §13). The log is the ratchet's audit trail —
// one baseline/keep/revert/error row per experiment, 9 tab-separated columns.
package history

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/skillsaw/cmd/root"
	"github.com/StevenACoffman/skillsaw/internal/store"
)

// Config holds the history command configuration.
type Config struct {
	*root.Config
	File    string
	Skill   string
	Flags   *ff.FlagSet
	Command *ff.Command
}

// New creates and registers the history command.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("history").SetParent(parent.Flags)
	cfg.Flags.StringVar(&cfg.File, 'f', "file", "results.tsv", "path to the results.tsv log")
	cfg.Flags.StringVar(&cfg.Skill, 's', "skill", "", "show only rows for this skill")
	cfg.Command = &ff.Command{
		Name:      "history",
		Usage:     "skillsaw history [--file results.tsv] [--skill NAME]",
		ShortHelp: "show the optimization log (results.tsv)",
		LongHelp: `Render the darwin optimization log (spec §13): a tab-separated file with nine
columns — timestamp, commit, skill, old_score, new_score, status, dimension,
note, eval_mode. Each row is one experiment: a baseline, a kept edit, a reverted
edit, or an error.

The log location is configurable (--file); the darwin source keeps it under
.claude/skills/darwin-skill/, but skillsaw defaults to ./results.tsv to stay
runtime-neutral. Use --skill to filter to a single skill's history.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, _ []string) error {
	f, err := os.Open(cfg.File)
	if err != nil {
		if os.IsNotExist(err) {
			// Not a usage error — report cleanly without dumping command help.
			_, _ = fmt.Fprintf(
				cfg.Stderr,
				"history: no log at %s (run an optimization first, or pass --file)\n",
				cfg.File,
			)
			return root.ExitError(1)
		}
		return fmt.Errorf("history: %w", err)
	}
	defer func() { _ = f.Close() }()

	rows, err := store.Read(f)
	if err != nil {
		return fmt.Errorf("history: %w", err)
	}
	cfg.render(rows)
	return nil
}

// render writes the header and matching rows to stdout and prints a tally.
func (cfg *Config) render(rows []store.Row) {
	tw := tabwriter.NewWriter(cfg.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, strings.ToUpper(strings.Join(store.Columns(), "\t")))

	shown, kept, reverted := 0, 0, 0
	for i := range rows {
		r := &rows[i]
		if cfg.Skill != "" && r.Skill != cfg.Skill {
			continue
		}
		shown++
		switch r.Status {
		case store.StatusKeep:
			kept++
		case store.StatusRevert:
			reverted++
		case store.StatusBaseline, store.StatusError:
			// counted in shown only
		}
		_, _ = fmt.Fprintln(tw, strings.Join(r.Fields(), "\t"))
	}
	_ = tw.Flush()
	_, _ = fmt.Fprintf(cfg.Stdout, "\n%d row(s): %d kept, %d reverted\n", shown, kept, reverted)
}
