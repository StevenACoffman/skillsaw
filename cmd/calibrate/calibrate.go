// Package calibrate implements the "calibrate" command: it reports whether the
// agent is systematically over- or under-confident, by comparing each confidence it
// stated in advance against what the outcome turned out to be. The arithmetic is
// pure and shared (skillet's calibration); this command does the file I/O and
// rendering.
package calibrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/skillet/calibration"
	"github.com/StevenACoffman/skillsaw/cmd/root"
	"github.com/StevenACoffman/skillsaw/internal/calibrate"
)

// thinSample is the count below which a ten-bin calibration report says more about
// sampling noise than about the judge. It gates a warning, never the exit code.
const thinSample = 20

// Config holds the calibrate command configuration.
type Config struct {
	*root.Config
	JSON    bool
	Flags   *ff.FlagSet
	Command *ff.Command
}

// file is the input document: the judgments recorded across a run.
type file struct {
	Judgments []calibrate.Judgment `json:"judgments"`
}

// New creates and registers the calibrate command.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("calibrate").SetParent(parent.Flags)
	cfg.Flags.BoolVar(&cfg.JSON, 0, "json", "emit the calibration report as JSON")
	cfg.Command = &ff.Command{
		Name:      "calibrate",
		Usage:     "skillsaw calibrate [--json] JUDGMENTS.json",
		ShortHelp: "report whether stated confidences match what actually happened",
		LongHelp: `Report how well stated confidences matched outcomes: Expected and Maximum
Calibration Error, the Brier score, and the per-bin breakdown they derive from.

The input is either {"judgments":[{skill, dim, base, passed}]} or the same objects one
per line (JSONL), which is what an optimize loop appends as it goes. base is the 1-10
confidence the agent stated before an outcome was known; passed is what the outcome
turned out to be.

The intended source is the optimize loop's edit prediction: the agent states, when it
proposes an edit targeting a dimension, how strongly it expects that edit to pass the
validation gate; "skillsaw gate" later returns accept or reject on a strict ">"
comparison the agent does not control. The confidence is fixed before the re-score
and the verdict is computed after, so neither derives from the other.

Dimension 8 is deliberately NOT the source, though it looks like one. The dim-8 base
is itself computed from the judge output (round(10 x mean soft)), so scoring it
against judge's hard verdict measures how the checks were written, not how well the
agent judges. A calibration needs a prediction the outcome cannot see.

The deterministic dimensions are not the subject either: a deterministic score is a
function of the file and has no variance to calibrate.

Reading the numbers: a well-calibrated judge has accuracy close to confidence in
every bin. Accuracy consistently BELOW confidence means systematic overconfidence —
the agent expects its edits to land more often than they do. Lower ECE, MCE and Brier
are better.

The 1-10 base is mapped onto [0,1] by a stated convention, not as a probability: a
base of 8 does not assert an 80% chance of passing. The relative signal — accuracy
against confidence across bins — is what the report supports; do not read the Brier
score as a probability claim.

This command reports; it does not gate. A judgment whose base falls outside 1-10 is
dropped rather than clamped, and the count of dropped judgments is reported.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, args []string) error {
	if err, bad := root.MisplacedFlag("calibrate", args); bad {
		return err
	}
	if len(args) != 1 {
		return root.Usagef("calibrate: pass exactly one judgments JSON file")
	}
	judgments, err := load(args[0])
	if err != nil {
		return err
	}
	samples := calibrate.Samples(judgments)
	if len(samples) == 0 {
		return errors.New("calibrate: no judgments with a base in 1-10 to score")
	}
	return cfg.emit(calibration.Compute(samples), len(judgments)-len(samples))
}

// load reads and parses the judgments file, in either accepted shape.
//
// The optimize loop appends one judgment per line as it goes, so JSONL is what it
// naturally has; the wrapped {"judgments":[...]} form is what it used to assemble with
// paste before this read it directly.
//
// The shapes are told apart by probing for the "judgments" key, the way manifest.Parse
// probes for "tool" -- not by trying the wrapper first. A single-line JSONL file is
// itself valid JSON and unmarshals into the wrapper with zero judgments, so "did it
// parse" cannot distinguish them and would read that file as empty.
func load(path string) ([]calibrate.Judgment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("calibrate: read judgments: %w", err)
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err == nil {
		if _, wrapped := probe["judgments"]; wrapped {
			var f file
			if err := json.Unmarshal(data, &f); err != nil {
				return nil, fmt.Errorf("calibrate: parse judgments %s: %w", path, err)
			}
			return f.Judgments, nil
		}
	}
	return parseLines(path, data)
}

// parseLines reads the JSONL shape: one judgment per non-blank line.
//
// A malformed line is an error naming its number, never a skip. A silently dropped
// judgment skews the report toward whatever survived, which is the one thing a
// calibration report must not do.
func parseLines(path string, data []byte) ([]calibrate.Judgment, error) {
	var out []calibrate.Judgment
	for i, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var j calibrate.Judgment
		if err := json.Unmarshal([]byte(line), &j); err != nil {
			return nil, fmt.Errorf("calibrate: parse %s line %d: %w", path, i+1, err)
		}
		out = append(out, j)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("calibrate: %s holds no judgments", path)
	}
	return out, nil
}

// emit writes the report as JSON (--json) or human-readable text. dropped is the
// number of judgments excluded for an out-of-range base.
func (cfg *Config) emit(rep calibration.Report, dropped int) error {
	if cfg.JSON {
		enc := json.NewEncoder(cfg.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return fmt.Errorf("calibrate: encode json: %w", err)
		}
		return nil
	}
	_, _ = fmt.Fprintf(cfg.Stdout, "samples: %d  ECE %.3f  MCE %.3f  Brier %.3f\n",
		rep.Samples, rep.ECE, rep.MCE, rep.Brier)
	if dropped > 0 {
		_, _ = fmt.Fprintf(cfg.Stdout,
			"  dropped %d judgment(s) with a base outside 1-10\n", dropped)
	}
	// Ten bins over a handful of samples produces a confident-looking error that is
	// mostly sampling noise, so say so rather than let the number be trusted.
	if rep.Samples < thinSample {
		_, _ = fmt.Fprintf(cfg.Stdout,
			"  note: %d samples is thin for a 10-bin report — treat these as indicative\n",
			rep.Samples)
	}
	_, _ = fmt.Fprintln(cfg.Stdout, "  CONFIDENCE  ACCURACY  COUNT")
	for _, b := range rep.Buckets {
		gap := ""
		if b.Accuracy < b.Confidence {
			gap = "  (overconfident)"
		}
		_, _ = fmt.Fprintf(cfg.Stdout, "  %10.2f  %8.2f  %5d%s\n",
			b.Confidence, b.Accuracy, b.Count, gap)
	}
	return nil
}
