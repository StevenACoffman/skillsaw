// Package hash implements the "hash" command: print the content identity hash
// of a skill (darwin spec §8.7) — the first 16 hex chars of sha256, byte-identical
// to SkillOpt's skill_hash. Useful as an evaluation-cache key and to detect
// whether an edit or rewrite actually changed the skill (a no-op has the same hash).
package hash

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/skillet/identity"
	"github.com/StevenACoffman/skillsaw/cmd/root"
	"github.com/StevenACoffman/skillsaw/internal/rubric"
)

// Config holds the hash command configuration.
type Config struct {
	*root.Config
	Rubric  bool
	Flags   *ff.FlagSet
	Command *ff.Command
}

// New creates and registers the hash command.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("hash").SetParent(parent.Flags)
	cfg.Flags.BoolVar(&cfg.Rubric, 0, "rubric",
		"print the rubric edition instead: the other half of an evaluation cache key")
	cfg.Command = &ff.Command{
		Name:      "hash",
		Usage:     "skillsaw hash SKILL_DIR|SKILL.md [...] | skillsaw hash --rubric",
		ShortHelp: "print a skill's content identity hash",
		LongHelp: `Print the content identity hash of each argument (darwin spec §8.7): the
first 16 hex chars of sha256(content), byte-identical to SkillOpt's skill_hash.

Each argument may be a skill directory (its SKILL.md is hashed) or a path to a
SKILL.md file directly. Output is "<hash>  <path>", one per line. Two skills with
the same hash are byte-identical — use this as a cache key or to confirm an edit
changed anything.

A cached grade has two inputs and this prints both. The content hash says which
text was scored; --rubric says which rules scored it, and changes when a weight,
a word list, or a threshold does. A key carrying only the first serves yesterday's
grade under today's rules, and says nothing about having done so — which is why
"skillsaw eval" treats a base recorded under a different edition as stale.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, args []string) error {
	if bad := root.MisplacedFlag(args); bad != "" {
		return fmt.Errorf(
			"hash: %q looks like a flag after arguments; put flags before positional arguments",
			bad,
		)
	}
	if cfg.Rubric {
		if len(args) > 0 {
			return errors.New("hash: --rubric describes the rules, not a skill; pass no paths")
		}
		_, _ = fmt.Fprintln(cfg.Stdout, rubric.Edition())
		return nil
	}
	if len(args) == 0 {
		return errors.New("hash: pass at least one SKILL_DIR or SKILL.md path")
	}
	failed := false
	for _, arg := range args {
		path := arg
		if info, err := os.Stat(arg); err == nil && info.IsDir() {
			path = filepath.Join(arg, "SKILL.md")
		}
		b, err := os.ReadFile(path)
		if err != nil {
			_, _ = fmt.Fprintf(cfg.Stderr, "hash: %v\n", err)
			failed = true
			continue
		}
		_, _ = fmt.Fprintf(cfg.Stdout, "%s  %s\n", identity.Hash(string(b)), path)
	}
	if failed {
		return root.ExitError(1)
	}
	return nil
}
