// Package changed implements the "changed" command: report which skills differ from a
// base manifest, so a campaign re-judges only those.
//
// The comparison itself is skillet's (manifest.Diff); this command reads the base
// manifest, walks the tree, and prints. There is no internal package here on purpose --
// one that only forwarded to skillet would be a layer to see through, not a core.
package changed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/skillet/manifest"
	"github.com/StevenACoffman/skillet/skill"
	"github.com/StevenACoffman/skillsaw/cmd/root"
)

// Config holds the changed command configuration.
type Config struct {
	*root.Config
	Manifest string
	Tree     string
	JSON     bool
	Flags    *ff.FlagSet
	Command  *ff.Command
}

// report is the --json document: the locations to reprocess, and what was skipped.
type report struct {
	Stale     []string `json:"stale"`
	Added     []string `json:"added"`
	Changed   []string `json:"changed"`
	Removed   []string `json:"removed"`
	Unchanged int      `json:"unchanged"`
}

// New creates and registers the changed command.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("changed").SetParent(parent.Flags)
	cfg.Flags.StringVar(&cfg.Manifest, 0, "manifest", "",
		"the base skills-manifest.json to compare against")
	cfg.Flags.StringVar(&cfg.Tree, 0, "tree", ".", "the skill tree to scan")
	cfg.Flags.BoolVar(&cfg.JSON, 0, "json", "emit the report as JSON")
	cfg.Command = &ff.Command{
		Name:      "changed",
		Usage:     "skillsaw changed --manifest base.json [--tree DIR] [--json]",
		ShortHelp: "list the skills that differ from a base manifest",
		LongHelp: `Compare a skill tree against the manifest a previous run wrote, and list the skills
worth reprocessing: those added since, and those whose content changed.

The point is one tier up from this CLI. skillsaw never calls a model, so its own
commands are cheap; what a hash pin saves is the expensive half of the loop, where the
agent hand-scores the judge-only dimensions and runs the skill. This says which skills
that has to happen for.

Locations are printed relative to the tree, not as slugs. A slug is not unique -- a
multi-root scan sees .claude/skills/foo and .cursor/skills/foo as two distinct skills
sharing one name -- so a slug-keyed list could not say which of them to re-judge.

How the tree path is spelled does not matter: a manifest written with --tree . and one
written with an absolute path compare equal, because each side's locations are taken
relative to its own recorded tree.

A skill that cannot be read is reported as changed rather than skipped. Its content is
unknown, and treating unknown as unchanged would drop it from the campaign for good.

This is a query, not a gate: it exits 0 whether or not anything is stale. Use
"skillsaw verified" for the gate.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, args []string) error {
	if bad := root.MisplacedFlag(args); bad != "" {
		return fmt.Errorf(
			"changed: %q looks like a flag after arguments; put flags before positional arguments",
			bad,
		)
	}
	if len(args) > 0 {
		return errors.New("changed: takes no positional arguments; pass --tree DIR")
	}
	if cfg.Manifest == "" {
		return errors.New("changed: --manifest is required")
	}
	base, err := loadManifest(cfg.Manifest)
	if err != nil {
		return err
	}
	cur, err := scan(cfg.Tree)
	if err != nil {
		return err
	}
	delta := manifest.Diff(base, cur)
	return cfg.emit(&delta)
}

// loadManifest reads and parses the base manifest.
func loadManifest(path string) (manifest.Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return manifest.Manifest{}, fmt.Errorf("changed: read %s: %w", path, err)
	}
	m, err := manifest.Parse(b)
	if err != nil {
		return manifest.Manifest{}, fmt.Errorf("changed: %w", err)
	}
	return m, nil
}

// scan walks tree into the manifest shape Diff compares against.
//
// A struct literal rather than manifest.Build: Build also takes the emitting tool and
// whether every gate passed, and neither has a meaning for a tree that has just been
// walked. Diff reads only Tree and Skills.
//
// A skill that fails to load is still recorded, with no hash. Diff counts an unknown
// hash as changed, so it lands in the campaign; dropping it here would remove it from
// the tree's inventory entirely and it would never be looked at again. One unreadable
// skill also must not abort the walk -- the other two hundred still need triaging.
func scan(tree string) (manifest.Manifest, error) {
	dirs, err := skill.Discover(tree)
	if err != nil {
		return manifest.Manifest{}, fmt.Errorf("changed: %w", err)
	}
	skills := make([]manifest.Skill, 0, len(dirs))
	for _, dir := range dirs {
		entry := manifest.Skill{Slug: filepath.Base(dir), Dir: dir}
		if s, err := skill.Load(dir); err == nil {
			entry.Hash = s.Hash()
		}
		skills = append(skills, entry)
	}
	return manifest.Manifest{Tree: tree, Skills: skills}, nil
}

// emit renders the delta as JSON (--json) or one location per line.
//
// d is a pointer only to avoid copying four slice headers; emit does not mutate it.
func (cfg *Config) emit(d *manifest.Delta) error {
	stale := d.Stale()
	if cfg.JSON {
		enc := json.NewEncoder(cfg.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report{
			Stale:     stale,
			Added:     d.Added,
			Changed:   d.Changed,
			Removed:   d.Removed,
			Unchanged: len(d.Unchanged),
		}); err != nil {
			return fmt.Errorf("changed: encode json: %w", err)
		}
		return nil
	}
	for _, loc := range stale {
		_, _ = fmt.Fprintln(cfg.Stdout, loc)
	}
	// The counts go out too, and name what was skipped: a bare list of three locations
	// gives no way to tell a tree where three skills changed from one where the other
	// two hundred were never scanned.
	_, _ = fmt.Fprintf(cfg.Stdout, "%d to reprocess (%d added, %d changed), %d unchanged",
		len(stale), len(d.Added), len(d.Changed), len(d.Unchanged))
	if len(d.Removed) > 0 {
		_, _ = fmt.Fprintf(cfg.Stdout, ", %d removed", len(d.Removed))
	}
	_, _ = fmt.Fprintln(cfg.Stdout)
	return nil
}
