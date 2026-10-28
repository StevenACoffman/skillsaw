// Package verified implements the "verified" command: refuse to proceed when the
// manifest exegesis wrote says the tree's structure did not pass its gates.
package verified

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/skillet/manifest"
	"github.com/StevenACoffman/skillsaw/cmd/root"
	"github.com/StevenACoffman/skillsaw/internal/gate"
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
looking for structural defects in a tree that was never examined.

A manifest covering no skills is refused for the same reason and reported differently.
structure_verified is true on it -- vacuously, since no gate failed -- so gating on that
alone let a campaign start against nothing. The refusal says the tree was not examined
rather than found defective, because it has no defects; it has no skills.

A manifest written before exegesis recorded an examined count is honoured on the verdict
it does carry: it lists skills and cannot say how many were read, and refusing those
would fail every manifest stored before the field existed.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, args []string) error {
	if err, bad := root.MisplacedFlag("verified", args); bad {
		return err
	}
	if len(args) != 1 {
		return root.Usagef("verified: pass exactly one skills-manifest.json")
	}
	m, err := load(args[0])
	if err != nil {
		return err
	}
	// Name what was gated, not just the verdict: a bare pass or fail leaves the reader
	// unable to tell whether the manifest they checked is the one they meant.
	cov := gate.Of(&m)
	_, _ = fmt.Fprintf(cfg.Stdout,
		"%s: tool=%s tree=%s skills=%d examined=%s structure_verified=%t\n",
		args[0], m.Tool, m.Tree, len(m.Skills), examined(&m, cov), m.StructureVerified)
	// Coverage is asked before the verdict, because a manifest covering nothing has no
	// meaningful verdict to report either way -- structure_verified is true on it, and
	// saying so is the misdirection this command's help already warns about for the
	// neighbouring case.
	if cov == gate.CoverageNone {
		_, _ = fmt.Fprintf(cfg.Stderr,
			"verified: %s covers no skills, so there is nothing to optimize; "+
				"the tree was not examined rather than found defective\n", args[0])
		return root.ExitError(1)
	}
	if !m.StructureVerified {
		return root.ExitError(1)
	}
	return nil
}

// examined renders the manifest's examined count for the report line. It prints the count
// itself rather than the coverage name, since "examined=examined" restates the key; only a
// manifest older than the field, which cannot say, prints "unknown".
func examined(m *manifest.Manifest, cov gate.Coverage) string {
	if cov == gate.CoverageUnknown {
		return cov.String()
	}
	return strconv.Itoa(m.Examined)
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
