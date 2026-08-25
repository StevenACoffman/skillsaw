// Package log implements the "log" command: append one row to the optimization log
// (results.tsv) that "skillsaw history" reads back.
//
// The row layout and its validation are skillet's (auditlog); this command turns flags
// into a Row, opens the file, and writes the header when the file is new.
package log

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/skillsaw/cmd/root"
	"github.com/StevenACoffman/skillsaw/internal/auditlog"
	"github.com/StevenACoffman/skillsaw/internal/rubric"
)

// defaultFile is the log every other command in this family reads and writes.
const defaultFile = "results.tsv"

// timeFormat is the timestamp precision the log records: minutes, because two rows in
// the same minute are still ordered by their position in the file, and seconds would
// only add noise to a log a person reads.
const timeFormat = "2006-01-02T15:04"

// Config holds the log command configuration.
type Config struct {
	*root.Config
	File    string
	Row     auditlog.Row
	Status  string
	Flags   *ff.FlagSet
	Command *ff.Command
}

// New creates and registers the log command.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("log").SetParent(parent.Flags)
	cfg.Flags.StringVar(&cfg.File, 0, "file", defaultFile, "the log to append to")
	cfg.Flags.StringVar(&cfg.Row.Skill, 0, "skill", "", "the skill this row is about")
	cfg.Flags.StringVar(&cfg.Status, 0, "status", "",
		"the outcome: "+strings.Join(statuses(), ", "))
	cfg.Flags.StringVar(&cfg.Row.OldScore, 0, "old", "-", "the score before this experiment")
	cfg.Flags.StringVar(&cfg.Row.NewScore, 0, "new", "-", "the score after it")
	cfg.Flags.StringVar(&cfg.Row.Dimension, 0, "dimension", "-", "the dimension targeted")
	cfg.Flags.StringVar(&cfg.Row.Note, 0, "note", "-", "a one-line summary of the edit")
	cfg.Flags.StringVar(&cfg.Row.EvalMode, 0, "eval-mode", "-", "full_test or dry_run")
	cfg.Flags.StringVar(&cfg.Row.Commit, 0, "commit", "-",
		"the commit this row describes; a baseline row records \"baseline\"")
	cfg.Flags.StringVar(&cfg.Row.Timestamp, 0, "timestamp", "",
		"override the recorded time (default: now)")
	cfg.Flags.StringVar(&cfg.Row.Rubric, 0, "rubric", "",
		"the rubric edition this score was produced under (default: the current one)")
	cfg.Command = &ff.Command{
		Name:      "log",
		Usage:     "skillsaw log --skill NAME --status STATUS [flags]",
		ShortHelp: "append one row to the optimization log",
		LongHelp: `Append a row to results.tsv, the log "skillsaw history" renders.

The ten columns and their order are this package's, and so is the check that the status is
one the log admits. Writing the row by hand -- which is what an optimize loop did before
this command existed -- spells that order out a second time, in a printf, where it can
drift from the reader's without anything noticing.

The header is written when the file does not yet exist or is empty, so a fresh log needs
no separate setup step.

The timestamp defaults to now, because a caller should not have to format one
consistently. The commit does not default to anything derived: resolving it would mean
running git from inside skillsaw, and a baseline row deliberately records the word
"baseline" in that column rather than a sha, so there is no single right answer. Pass
what the row means.

The rubric edition defaults to the current one, so a row records which rules produced
its score without the caller thinking about it. That is what lets "skillsaw regression"
refuse to average across a rubric change: an edit that raised every score is otherwise
indistinguishable, in this file, from skills that got better.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, args []string) error {
	if bad := root.MisplacedFlag(args); bad != "" {
		return fmt.Errorf(
			"log: %q looks like a flag after arguments; put flags before positional arguments",
			bad,
		)
	}
	if len(args) > 0 {
		return errors.New("log: takes no positional arguments; every field is a flag")
	}
	if strings.TrimSpace(cfg.Row.Skill) == "" {
		return errors.New("log: --skill is required")
	}
	if cfg.Row.Timestamp == "" {
		cfg.Row.Timestamp = time.Now().Format(timeFormat)
	}
	if cfg.Row.Rubric == "" {
		// Defaulted rather than required: a caller that had to supply it would eventually
		// supply a stale one, and the current edition is the honest answer for a score
		// this binary just produced.
		cfg.Row.Rubric = rubric.Edition()
	}
	// The status is validated by auditlog.Append, which rejects an unknown one before
	// writing anything. Re-checking here would put the same rule in two places, and the
	// two would eventually disagree about what the log admits.
	cfg.Row.Status = auditlog.Status(cfg.Status)
	return cfg.append()
}

// append opens the log and writes the row, preceded by the header when the file is new.
func (cfg *Config) append() error {
	fresh, err := isEmpty(cfg.File)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(cfg.File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("log: open %s: %w", cfg.File, err)
	}
	defer func() { _ = f.Close() }()

	if fresh {
		if _, err := fmt.Fprintln(f, strings.Join(auditlog.Columns(), "\t")); err != nil {
			return fmt.Errorf("log: write header: %w", err)
		}
	}
	if err := auditlog.Append(f, cfg.Row); err != nil {
		return fmt.Errorf("log: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("log: close %s: %w", cfg.File, err)
	}
	return nil
}

// isEmpty reports whether path needs a header: it does not exist, or holds nothing.
func isEmpty(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("log: stat %s: %w", path, err)
	}
	return info.Size() == 0, nil
}

// statuses lists the outcomes the log admits, for the flag's help text.
func statuses() []string {
	return []string{
		string(auditlog.StatusBaseline), string(auditlog.StatusKeep),
		string(auditlog.StatusRevert), string(auditlog.StatusError),
	}
}
