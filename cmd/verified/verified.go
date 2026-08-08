// Package verified implements the "verified" command: refuse to proceed when the
// manifest exegesis wrote says the tree's structure did not pass its gates.
package verified

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/skillet/manifest"
	"github.com/StevenACoffman/skillsaw/cmd/root"
)

// Config holds the verified command configuration.
type Config struct {
	*root.Config
	Flags   *ff.FlagSet
	Command *ff.Command
}

// New creates and registers the verified command.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("verified").SetParent(parent.Flags)
	cfg.Command = &ff.Command{
		Name:      "verified",
		Usage:     "skillsaw verified MANIFEST",
		ShortHelp: "exit non-zero unless exegesis marked the tree structurally verified",
		LongHelp: `Read a skills-manifest.json and exit 0 only if its structure_verified is true.

Optimizing a tree whose structure exegesis rejected wastes the expensive half of the
loop: the agent hand-scores the judge-only dimensions and runs the skill, and a tree
that fails its structural gates will have to be fixed and re-judged anyway. This is the
cheap check that comes first.

skillsaw has no "optimize" subcommand to attach the gate to -- the loop lives in the
skillsaw-skill -- so this is a standalone gate the skill calls and whose exit code it
checks before starting a campaign.

Pointing this at a JSON file that is not a manifest is an error, not a quiet
"unverified": an unrelated document decodes into an empty manifest, whose
structure_verified is false, and reporting that as a failed gate would send the reader
looking for structural defects in a tree that was never examined.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, args []string) error {
	if bad := root.MisplacedFlag(args); bad != "" {
		return fmt.Errorf(
			"verified: %q looks like a flag after arguments; put flags before positional arguments",
			bad,
		)
	}
	if len(args) != 1 {
		return errors.New("verified: pass exactly one skills-manifest.json")
	}
	m, err := load(args[0])
	if err != nil {
		return err
	}
	// Name what was gated, not just the verdict: a bare pass or fail leaves the reader
	// unable to tell whether the manifest they checked is the one they meant.
	_, _ = fmt.Fprintf(cfg.Stdout, "%s: tool=%s tree=%s skills=%d structure_verified=%t\n",
		args[0], m.Tool, m.Tree, len(m.Skills), m.StructureVerified)
	if !m.StructureVerified {
		return root.ExitError(1)
	}
	return nil
}

// load reads and parses a manifest.
func load(path string) (manifest.Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return manifest.Manifest{}, fmt.Errorf("verified: read %s: %w", path, err)
	}
	m, err := manifest.Parse(b)
	if err != nil {
		return manifest.Manifest{}, fmt.Errorf("verified: %w", err)
	}
	return m, nil
}
