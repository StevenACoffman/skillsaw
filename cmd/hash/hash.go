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

	"github.com/StevenACoffman/skillsaw/cmd/root"
	"github.com/StevenACoffman/skillsaw/internal/skill"
)

// Config holds the hash command configuration.
type Config struct {
	*root.Config
	Flags   *ff.FlagSet
	Command *ff.Command
}

// New creates and registers the hash command.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("hash").SetParent(parent.Flags)
	cfg.Command = &ff.Command{
		Name:      "hash",
		Usage:     "skillsaw hash SKILL_DIR|SKILL.md [...]",
		ShortHelp: "print a skill's content identity hash",
		LongHelp: `Print the content identity hash of each argument (darwin spec §8.7): the
first 16 hex chars of sha256(content), byte-identical to SkillOpt's skill_hash.

Each argument may be a skill directory (its SKILL.md is hashed) or a path to a
SKILL.md file directly. Output is "<hash>  <path>", one per line. Two skills with
the same hash are byte-identical — use this as a cache key or to confirm an edit
changed anything.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, args []string) error {
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
		_, _ = fmt.Fprintf(cfg.Stdout, "%s  %s\n", skill.Hash(string(b)), path)
	}
	if failed {
		return root.ExitError(1)
	}
	return nil
}
