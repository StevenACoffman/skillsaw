// Package scan implements the "scan" command: the runtime-neutrality red-light
// scan (darwin spec §9). It flags wording or paths that bind a skill to a single
// agent runtime. Exit code is non-zero when any hit is found, so it can gate CI.
package scan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/skillet/neutrality"
	"github.com/StevenACoffman/skillet/skill"
	"github.com/StevenACoffman/skillsaw/cmd/root"
)

// Config holds the scan command configuration.
type Config struct {
	*root.Config
	All     bool
	Roots   string
	JSON    bool
	Flags   *ff.FlagSet
	Command *ff.Command
}

// dirHits pairs a skill with its red-light hits, for reporting.
type dirHits struct {
	Skill string           `json:"skill"`
	Hits  []neutrality.Hit `json:"hits"`
}

// New creates and registers the scan command.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("scan").SetParent(parent.Flags)
	cfg.Flags.BoolVar(&cfg.All, 'a', "all", "scan skill roots instead of listing dirs")
	cfg.Flags.StringVar(&cfg.Roots, 0, "roots", strings.Join(skill.DefaultRoots(), ","),
		"comma-separated skill roots for --all")
	cfg.Flags.BoolVar(&cfg.JSON, 0, "json", "emit hits as JSON")
	cfg.Command = &ff.Command{
		Name:      "scan",
		Usage:     "skillsaw scan [FLAGS] [SKILL_DIR ...]",
		ShortHelp: "runtime-neutrality red-light scan (CI gate)",
		LongHelp: `Scan SKILL.md (and README.md) for wording or paths that bind a skill to a
single agent runtime (darwin spec §9) — e.g. "在 Claude Code", a single-runtime
badge, or a hard-coded ~/.claude/skills/ path. Other agents refuse to install
such skills.

Prints one line per hit as "<file>:<line>: <text>". Exit code is 1 when any hit
is found (0 when clean), so it can run as a CI gate. Legitimate occurrences
(frontmatter trigger words, labeled runtime-specific sections, commit messages —
spec §9.2) are reported verbatim here; classify them downstream.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, args []string) error {
	if bad := root.MisplacedFlag(args); bad != "" {
		return fmt.Errorf(
			"scan: %q looks like a flag after arguments; put flags before positional arguments",
			bad,
		)
	}
	dirs := args
	if cfg.All {
		found, err := skill.DiscoverRoots(".", root.SplitRoots(cfg.Roots))
		if err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		dirs = found
	}
	if len(dirs) == 0 {
		return errors.New("scan: no skills found; pass SKILL_DIR arguments or use --all")
	}

	var results []dirHits
	totalHits := 0
	for _, dir := range dirs {
		hits := neutrality.Scan(readScanFiles(dir))
		totalHits += len(hits)
		results = append(results, dirHits{Skill: filepath.Base(dir), Hits: hits})
	}

	if err := cfg.emit(results); err != nil {
		return err
	}

	if totalHits > 0 {
		return root.ExitError(1)
	}
	return nil
}

// emit writes the scan results as JSON (--json) or human-readable text.
func (cfg *Config) emit(results []dirHits) error {
	if cfg.JSON {
		enc := json.NewEncoder(cfg.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(results); err != nil {
			return fmt.Errorf("scan: encode json: %w", err)
		}
		return nil
	}
	for _, r := range results {
		if len(r.Hits) == 0 {
			_, _ = fmt.Fprintf(cfg.Stdout, "%s: ok (runtime-neutral)\n", r.Skill)
			continue
		}
		_, _ = fmt.Fprintf(cfg.Stdout, "%s: %d hit(s)\n", r.Skill, len(r.Hits))
		for _, h := range r.Hits {
			_, _ = fmt.Fprintf(cfg.Stdout, "  %s:%d: %s\n", h.File, h.Line, h.Text)
		}
	}
	return nil
}

func readScanFiles(dir string) []neutrality.NamedFile {
	var files []neutrality.NamedFile
	for _, name := range []string{"SKILL.md", "README.md"} {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			files = append(files, neutrality.NamedFile{Name: name, Content: string(b)})
		}
	}
	return files
}
