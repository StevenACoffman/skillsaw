// Package scores implements the "scores" command: emit the judge-supplied bases for a
// skill, bound to the version they were judged against.
//
// The document shape and its validation are internal/scores'; this command reads the
// skill to capture its hash and renders.
package scores

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/skillet/skill"
	"github.com/StevenACoffman/skillsaw/cmd/root"
	"github.com/StevenACoffman/skillsaw/internal/rubric"
	scoreslib "github.com/StevenACoffman/skillsaw/internal/scores"
)

// Config holds the scores command configuration.
type Config struct {
	*root.Config
	Skill   string
	Bases   string
	Flags   *ff.FlagSet
	Command *ff.Command
}

// New creates and registers the scores command.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("scores").SetParent(parent.Flags)
	cfg.Flags.StringVar(&cfg.Skill, 0, "skill", "", "the skill directory these bases describe")
	cfg.Flags.StringVar(&cfg.Bases, 0, "bases", "",
		"comma-separated DIM=BASE pairs, e.g. 1=8,2=7,5=9")
	cfg.Command = &ff.Command{
		Name:      "scores",
		Usage:     "skillsaw scores --skill DIR --bases 1=8,2=7,... > scores.json",
		ShortHelp: "emit judge bases bound to the skill version they were judged against",
		LongHelp: `Write the scores document "skillsaw eval --scores" reads, to stdout.

A base is a number someone assigned after reading a particular version of a skill, and
nothing about it survives an edit. This reads the skill to capture its content hash and
records it beside the bases, so "eval" can refuse to reuse them once the skill changes.

The hash is taken from the skill itself rather than accepted as a flag: passing it in
reintroduces exactly the mistake the binding exists to catch -- a file that names the
version before an edit while describing the text after it.

Supply one DIM=BASE pair per dimension "eval" marks needs_judge; that set varies per
skill, and a missing base means no FULL total at all rather than a partial one. Bases are
1-10, dimensions 1-9; anything else is refused here rather than written and rejected on
the way back in.

Redirect to a file: this writes to stdout so it composes, and the caller names the file
it wants.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, args []string) error {
	if bad := root.MisplacedFlag(args); bad != "" {
		return fmt.Errorf(
			"scores: %q looks like a flag after arguments; put flags before positional arguments",
			bad,
		)
	}
	if len(args) > 0 {
		return errors.New("scores: takes no positional arguments; pass --skill DIR")
	}
	if cfg.Skill == "" {
		return errors.New("scores: --skill is required")
	}
	bases, err := parseBases(cfg.Bases)
	if err != nil {
		return err
	}
	s, err := skill.Load(cfg.Skill)
	if err != nil {
		return fmt.Errorf("scores: %w", err)
	}
	name := s.Name
	if name == "" {
		name = filepath.Base(cfg.Skill)
	}
	// The edition is recorded here for the same reason the hash is: a base answers the
	// question the rubric asked, and eval refuses one whose rules no longer match. A
	// writer that omitted it would emit a document its own reader rejects.
	doc, err := scoreslib.Marshal([]scoreslib.Entry{
		{Skill: name, Hash: s.Hash(), Bases: bases, Rubric: rubric.Edition()},
	})
	if err != nil {
		return fmt.Errorf("scores: %w", err)
	}
	_, _ = cfg.Stdout.Write(doc)
	return nil
}

// parseBases reads the DIM=BASE list. A malformed pair is an error rather than a skip:
// a quietly dropped dimension yields no FULL total, and the reason would be invisible.
func parseBases(spec string) (map[int]int, error) {
	out := map[int]int{}
	for _, pair := range strings.Split(spec, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue // tolerate stray or trailing commas
		}
		dim, base, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, fmt.Errorf("scores: %q is not DIM=BASE", pair)
		}
		d, err := strconv.Atoi(strings.TrimSpace(dim))
		if err != nil {
			return nil, fmt.Errorf("scores: %q has a non-numeric dimension", pair)
		}
		b, err := strconv.Atoi(strings.TrimSpace(base))
		if err != nil {
			return nil, fmt.Errorf("scores: %q has a non-numeric base", pair)
		}
		if _, dup := out[d]; dup {
			return nil, fmt.Errorf("scores: dimension %d given twice", d)
		}
		out[d] = b
	}
	if len(out) == 0 {
		return nil, errors.New("scores: --bases is required, e.g. --bases 1=8,2=7")
	}
	return out, nil
}
