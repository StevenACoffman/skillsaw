// Package preflight implements the "preflight" command: the structural gate that
// runs before an edit is adopted. It rejects a proposal whose structure is broken
// however well it scored, so the optimizer cannot trade structure for points. The
// checks are pure and shared (skillet's speclint + redlines, composed by
// internal/edit); this command does the file I/O and decides the exit code.
package preflight

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/skillet/finding"
	"github.com/StevenACoffman/skillet/manifest"
	"github.com/StevenACoffman/skillet/skill"
	"github.com/StevenACoffman/skillsaw/cmd/root"
	"github.com/StevenACoffman/skillsaw/internal/edit"
	"github.com/StevenACoffman/skillsaw/internal/inventory"
)

// Config holds the preflight command configuration.
type Config struct {
	*root.Config
	JSON      bool
	Redlines  bool
	Against   string
	Manifest  string
	Coupling  bool
	MaxGrowth float64
	Flags     *ff.FlagSet
	Command   *ff.Command
}

// skillDefects is one skill's structural verdict, for reporting.
type skillDefects struct {
	Skill   string               `json:"skill"`
	Defects []finding.Diagnostic `json:"defects"`
}

// registerFlags declares preflight's flags. Split from New only because New had grown past
// what reads as one thing: flag declaration and command construction are separate phases.
func (cfg *Config) registerFlags(parent *root.Config) {
	cfg.Flags = ff.NewFlagSet("preflight").SetParent(parent.Flags)
	cfg.Flags.BoolVar(&cfg.JSON, 0, "json", "emit the defects as JSON")
	cfg.Flags.BoolVar(&cfg.Redlines, 0, "redlines",
		"also enforce book2skill's Quality Red Lines (RIA-TV++ segments, quote limit, trigger)")
	cfg.Flags.StringVar(&cfg.Against, 0, "against", "",
		"the pre-edit SKILL.md to judge the edit against (one SKILL_DIR only)")
	cfg.Flags.Float64Var(&cfg.MaxGrowth, 0, "max-growth", edit.DefaultMaxGrowth,
		"size ceiling with --against, as a multiple of the original")
	cfg.Flags.StringVar(&cfg.Manifest, 0, "manifest", "",
		"baseline skills-manifest.json; warns when a SKILL.md changed and its test-prompts did not")
	cfg.Flags.BoolVar(&cfg.Coupling, 0, "require-coupling",
		"with --manifest, make the uncoupled-test-prompts warning block instead of advise")
}

// New creates and registers the preflight command.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.registerFlags(parent)

	cfg.Command = &ff.Command{
		Name:      "preflight",
		Usage:     "skillsaw preflight [--redlines] [--against ORIG] [--json] SKILL_DIR ...",
		ShortHelp: "structural gate: reject an edit that breaks structure, whatever it scored",
		LongHelp: `Check each SKILL_DIR against the structural rules an edit must satisfy
before it is adopted. Exit code is 1 when any defect is found, so an optimize
loop can run this between writing an edit and deciding whether to keep it.

By default only the agentskills.io frontmatter rules apply, since every Agent
Skill is bound by the spec. With --redlines, book2skill's Quality Red Lines are
enforced too: the six RIA-TV++ body segments, the quotation ceiling, and a
description that states a trigger. Those are opt-in because they encode
book2skill's house structure — a hand-written skill has no reason to carry it,
and enforcing them by default would reject nearly every skill outside a book
tree. Use --redlines when optimizing a book tree, where that structure is the
contract. This mirrors "exegesis lint --check redlines".

This is the structural half of a double-gated pipeline. "skillsaw gate" decides
on score; this decides on structure, and it is deliberately the stricter of the
two: "skillsaw eval" only *penalises* a blown description cap, so a gain
elsewhere can outweigh it, whereas an edit that fails here is rejected outright.

With --against, the edit is also judged against the text it replaced: an edit that
left the content hash unchanged did nothing, and one that grew past --max-growth
(default 1.5, darwin's ratio) has stopped being an edit and become a rewrite. Both
were previously left to the caller as shell arithmetic that only printed a warning;
here they fail the command like every other defect.

--against takes exactly one SKILL_DIR. One original cannot describe several edits,
and comparing every directory to one file would report defects that are arithmetic
accidents rather than real ones.

With --manifest, each SKILL_DIR is also compared against a baseline manifest, and a
skill whose SKILL.md moved while its test-prompts.json did not is reported. That
pair cannot be judged from one snapshot: both documents are individually fine and
only their relationship is stale, so nothing else in this family can see it.

It is a warning, not a defect. An editorial fix to one sentence legitimately needs
no test change, and a gate that fired on those would teach people to route around
it. Pass --require-coupling to make it block, in a tree where the coupling is the
contract.

Runtime neutrality is not checked here — "skillsaw scan" gates that separately.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, args []string) error {
	if err, bad := root.MisplacedFlag("preflight", args); bad {
		return err
	}
	if len(args) == 0 {
		return root.Usagef("preflight: pass at least one SKILL_DIR")
	}
	if cfg.Against != "" && len(args) != 1 {
		return errors.New(
			"preflight: --against compares one edit to one original; pass a single SKILL_DIR")
	}
	original, err := cfg.loadOriginal()
	if err != nil {
		return err
	}

	uncoupled, err := cfg.couplingFindings(args)
	if err != nil {
		return err
	}

	results := make([]skillDefects, 0, len(args))
	total := 0
	for _, dir := range args {
		s, err := skill.Load(dir)
		if err != nil {
			// Reported, not returned. A directory that will not load is a defect about
			// that directory; returning here discarded every result already computed and,
			// because the error reached ff's handler, printed the full usage text -- so a
			// folder with no SKILL.md read as though the caller had mistyped a flag.
			// activation took this same fix on 2026-08-23.
			results = append(results, skillDefects{
				Skill: filepath.Base(dir),
				Defects: []finding.Diagnostic{{
					Severity: finding.SeverityError,
					Category: "unloadable",
					Path:     dir,
					Action:   finding.ActionHuman,
					Message:  "cannot be read as a skill: " + err.Error(),
				}},
			})
			total++
			continue
		}
		defects := edit.StructuralDefects(s, cfg.Redlines)
		if cfg.Against != "" {
			defects = append(defects, edit.AgainstOriginal(original, s.Raw, cfg.MaxGrowth)...)
		}
		defects = append(defects, uncoupled[dir]...)
		total += blocking(defects)
		results = append(results, skillDefects{Skill: filepath.Base(dir), Defects: defects})
	}

	if err := cfg.emit(results); err != nil {
		return err
	}
	if total > 0 {
		return root.ExitError(1)
	}
	return nil
}

// blocking counts the defects that decide the exit code.
//
// skillet's finding package is explicit that only SeverityError blocks, and this command
// used to count every diagnostic instead. Nothing reachable from here emits a warning
// today, so this moves no behaviour -- but it is what makes an advisory finding
// expressible, and a gate that blocks on everything it is handed cannot be given one.
func blocking(defects []finding.Diagnostic) int {
	n := 0
	for i := range defects {
		if defects[i].Severity == finding.SeverityError {
			n++
		}
	}
	return n
}

// couplingFindings compares each directory against the baseline manifest, keyed by the
// directory the caller named so the caller does not have to re-derive the location Diff
// used. It returns nothing at all when --manifest was not given.
//
// The severity is decided here rather than in edit.Uncoupled: whether an uncoupled edit
// blocks is a property of the tree being checked, not of the finding.
func (cfg *Config) couplingFindings(dirs []string) (map[string][]finding.Diagnostic, error) {
	if cfg.Manifest == "" {
		return map[string][]finding.Diagnostic{}, nil
	}
	b, err := os.ReadFile(cfg.Manifest)
	if err != nil {
		return nil, fmt.Errorf("preflight: read manifest %s: %w", cfg.Manifest, err)
	}
	base, err := manifest.Parse(b)
	if err != nil {
		return nil, fmt.Errorf("preflight: %w", err)
	}
	byLocation := make(map[string]string, len(dirs))
	skills := make([]manifest.Skill, 0, len(dirs))
	for _, dir := range dirs {
		entry := inventory.Entry(dir)
		skills = append(skills, entry)
		byLocation[inventory.Location(base.Tree, dir)] = dir
	}
	cur := manifest.Manifest{Tree: base.Tree, Skills: skills}

	out := make(map[string][]finding.Diagnostic)
	for _, d := range edit.Uncoupled(base, cur) {
		if cfg.Coupling {
			d.Severity = finding.SeverityError
		}
		dir, ok := byLocation[d.Path]
		if !ok {
			// Diff keyed this finding to a location none of the arguments produced. Report
			// it against the first rather than dropping it: a finding nobody owns is one
			// nobody sees, and silence here would read as a clean run.
			dir = dirs[0]
		}
		out[dir] = append(out[dir], d)
	}
	return out, nil
}

// loadOriginal reads the pre-edit text, or returns "" when --against was not given.
func (cfg *Config) loadOriginal() (string, error) {
	if cfg.Against == "" {
		return "", nil
	}
	b, err := os.ReadFile(cfg.Against)
	if err != nil {
		return "", fmt.Errorf("preflight: read original %s: %w", cfg.Against, err)
	}
	return string(b), nil
}

// emit writes the verdicts as JSON (--json) or human-readable text.
func (cfg *Config) emit(results []skillDefects) error {
	if cfg.JSON {
		enc := json.NewEncoder(cfg.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(results); err != nil {
			return fmt.Errorf("preflight: encode json: %w", err)
		}
		return nil
	}
	for _, r := range results {
		if len(r.Defects) == 0 {
			_, _ = fmt.Fprintf(cfg.Stdout, "%s: ok (structurally sound)\n", r.Skill)
			continue
		}
		_, _ = fmt.Fprintf(cfg.Stdout, "%s: %d defect(s)\n", r.Skill, len(r.Defects))
		for _, d := range r.Defects {
			_, _ = fmt.Fprintf(cfg.Stdout, "  %s: %s\n", d.Severity, d.Message)
		}
	}
	return nil
}
