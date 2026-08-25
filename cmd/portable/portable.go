// Package portable implements the "portable" command: check that a skill name means one
// thing across a set of repositories.
package portable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/skillet/identity"
	"github.com/StevenACoffman/skillet/skill"
	"github.com/StevenACoffman/skillsaw/cmd/root"
	"github.com/StevenACoffman/skillsaw/internal/portability"
)

// skillFile is the marker naming a skill directory.
const skillFile = "SKILL.md"

// Config holds the portable command configuration.
type Config struct {
	*root.Config
	JSON    bool
	Flags   *ff.FlagSet
	Command *ff.Command
}

// report is the command's output shape.
type report struct {
	Repos    int                   `json:"repos"`
	Skills   int                   `json:"skills"`
	Findings []portability.Finding `json:"findings"`
}

// New creates and registers the portable command.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("portable").SetParent(parent.Flags)
	cfg.Flags.BoolVar(&cfg.JSON, 0, "json", "emit the findings as JSON")
	cfg.Command = &ff.Command{
		Name:      "portable",
		Usage:     "skillsaw portable REPO [REPO ...]",
		ShortHelp: "check that a skill name means one thing across repositories",
		LongHelp: `Read every skill under each REPO and report each declared name that has
stopped identifying a single artifact.

Two findings, wanting different repairs. A name declared in more than one
repository with different content is DIVERGED: the portable copy has drifted. A
name declared twice inside one repository is a COLLISION, which has nothing to do
with portability — the name simply no longer picks out a skill.

Identity is the name a skill declares in its frontmatter, not its directory. A
runtime routes on the declared name, and two copies at different paths are one
skill.

A name beginning with its own repository's directory name and a hyphen is treated
as repo-specific and is exempt, which is the convention these trees follow. That
makes this check exactly as good as the naming: a skill that ought to have been
prefixed and was not reports as a portability violation when the real defect is
the name. Nothing mechanical can separate those, so the message offers the rename
alongside the sync.

There are no default repositories. Which checkouts constitute "everywhere" is a
fact about one machine, not about this tool.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, args []string) error {
	if bad := root.MisplacedFlag(args); bad != "" {
		return fmt.Errorf(
			"portable: %q looks like a flag after arguments; put flags before positional arguments",
			bad,
		)
	}
	if len(args) == 0 {
		return errors.New("portable: pass at least one repository root; there are no defaults")
	}
	skills, err := cfg.gather(args)
	if err != nil {
		return err
	}
	rep := report{Repos: len(args), Skills: len(skills), Findings: portability.Check(skills)}
	if err := cfg.emit(&rep); err != nil {
		return err
	}
	if len(rep.Findings) > 0 {
		return root.ExitError(1)
	}
	return nil
}

// gather reads every skill under each repository.
//
// A skill that will not load is reported and skipped rather than failing the run: one
// unreadable SKILL.md among sixty must not stop the other fifty-nine being checked, and
// the note on stderr says it was not considered.
func (cfg *Config) gather(repos []string) ([]portability.Skill, error) {
	var out []portability.Skill
	for _, repo := range repos {
		dirs, err := skillDirs(repo)
		if err != nil {
			return nil, fmt.Errorf("portable: %w", err)
		}
		for _, dir := range dirs {
			s, err := skill.Load(dir)
			if err != nil {
				_, _ = fmt.Fprintf(cfg.Stderr, "skip %s: %v\n", dir, err)
				continue
			}
			name := s.Name
			if name == "" {
				// A skill declaring no name cannot be compared with one that does. Say so
				// rather than falling back to the directory, which would invent an
				// identity the file does not claim.
				_, _ = fmt.Fprintf(cfg.Stderr, "skip %s: declares no name\n", dir)
				continue
			}
			out = append(out, portability.Skill{
				Name: name,
				Repo: filepath.Base(filepath.Clean(repo)),
				Dir:  dir,
				Hash: identity.Hash(s.Raw),
			})
		}
	}
	return out, nil
}

// emit renders the findings, and says plainly when a run had nothing to compare -- that is
// neither a pass nor a failure, and printing "ok" for it would be a claim about
// repositories nobody looked at.
func (cfg *Config) emit(rep *report) error {
	if cfg.JSON {
		enc := json.NewEncoder(cfg.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return fmt.Errorf("portable: encode json: %w", err)
		}
		return nil
	}
	for i := range rep.Findings {
		_, _ = fmt.Fprintln(cfg.Stdout, rep.Findings[i].Explain())
	}
	switch {
	case len(rep.Findings) > 0:
		_, _ = fmt.Fprintf(cfg.Stdout, "\n%d finding(s) across %d skill(s) in %d repositories\n",
			len(rep.Findings), rep.Skills, rep.Repos)
	case rep.Repos < 2:
		_, _ = fmt.Fprintf(cfg.Stdout,
			"%d skill(s) in 1 repository: no collisions, and nothing to compare against. "+
				"Pass a second repository to check portability.\n", rep.Skills)
	default:
		_, _ = fmt.Fprintf(cfg.Stdout, "%d skill(s) across %d repositories: every name "+
			"identifies one artifact\n", rep.Skills, rep.Repos)
	}
	return nil
}

// skipDir names the directories a skill is never found in. A vendored or dependency copy
// of somebody else's skill is not this repository declaring a name.
func skipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "dist", ".idea":
		return true
	default:
		return false
	}
}

// skillDirs walks a repository for every directory holding a SKILL.md.
//
// skillet's skill.Discover looks one level deep, which is right for a skills tree and
// wrong for a repository: the two checked here keep skills under "community/", "curated/",
// and "internal/assets/skills/". Passing those subdirectories separately instead would make
// one repository look like several, which would defeat both the collision check and the
// prefix exemption.
func skillDirs(repo string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(repo, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == skillFile {
			out = append(out, filepath.Dir(path))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", repo, err)
	}
	return out, nil
}
