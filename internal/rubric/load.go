package rubric

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/BurntSushi/toml"
)

// The shape every rubric document must have, asserted at load rather than in prose, which
// is where the weight invariant used to live.
const (
	dimensionCount = 9
	totalWeight    = 100
)

//go:embed rubric.toml
var embedded []byte

// Rubric is a loaded, validated rubric document.
//
// It is embedded rather than read from a path, and that is a decision rather than an
// omission. Everything wanted from externalising it -- a reviewable diff, a note recording
// why each number has its value, an edition derived from the bytes -- arrives without a
// load path, and a load path is the one change that would let a candidate edit relax the
// gate judging it: the tool and the thing it grades live in one repository, so a rubric
// read from the tree under evaluation is a rubric the optimiser can write. Compiled in, a
// rubric change is a committed diff and the edition moves with it.
//
// The loader is still strict and still earns it: the embedded bytes go through it on every
// run, so unknown-key rejection is exercised constantly rather than sitting behind a flag.
type Rubric struct {
	Thresholds Thresholds  `toml:"thresholds"`
	Dimension  []Dimension `toml:"dimension"`
	Lists      Lists       `toml:"lists"`

	// raw is the document exactly as read. Edition hashes it, so a Rubric carries the
	// bytes that identify it rather than reassembling an identity from parsed fields --
	// which would silently omit whichever field is added next.
	raw []byte
}

// Thresholds are the scoring bands that are data rather than Go.
//
// The per-defect penalty amounts are deliberately not here: they are scattered through
// eight check functions and moving them is a refactor nothing has asked for. scoringRevision
// still covers those.
type Thresholds struct {
	CheckpointMinMarkers int `toml:"checkpoint_min_markers"`
	CheckpointBase       int `toml:"checkpoint_base"`
	BlacklistEmptyBase   int `toml:"blacklist_empty_base"`
	BlacklistThinBase    int `toml:"blacklist_thin_base"`
	BlacklistFullBase    int `toml:"blacklist_full_base"`
	BlacklistFullUnits   int `toml:"blacklist_full_units"`
}

// Lists are the three word lists the deterministic checks match against.
type Lists struct {
	FillerTails       []string `toml:"filler_tails"`
	Slop              []string `toml:"slop"`
	CheckpointMarkers []string `toml:"checkpoint_markers"`
}

// Load reads and validates a rubric document.
//
// TOML rather than JSON because toml.Decode reports MetaData.Undecoded(), so a key spelled
// `blacklist_thin_bass` is named rather than decoded to zero. A threshold somebody believes
// they changed is the expensive failure here, and it is exactly the one JSON hides.
//
// Requires: r yields TOML.
// Ensures:  a fully validated rubric, or an error naming the first problem and no rubric.
// There is no partial result and no fallback to a compiled default: scoring against
// yesterday's rubric while reporting success is the failure this document exists to
// prevent, and a lenient loader gives up the property that made a hardcoded table
// trustworthy.
func Load(r io.Reader) (*Rubric, error) {
	doc, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("rubric: read: %w", err)
	}
	var rb Rubric
	md, err := toml.Decode(string(doc), &rb)
	if err != nil {
		return nil, fmt.Errorf("rubric: decode: %w", err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, 0, len(undecoded))
		for _, k := range undecoded {
			keys = append(keys, k.String())
		}
		return nil, fmt.Errorf("rubric: unknown key(s): %s", strings.Join(keys, ", "))
	}
	if err := rb.validate(); err != nil {
		return nil, err
	}
	rb.raw = doc
	return &rb, nil
}

// validate enforces what the compiler used to: nine dimensions, each number once, weights
// summing to totalWeight, every response defined, no empty list.
func (r *Rubric) validate() error {
	if len(r.Dimension) != dimensionCount {
		return fmt.Errorf("rubric: %d dimensions, want %d", len(r.Dimension), dimensionCount)
	}
	seen := make(map[int]bool, len(r.Dimension))
	sum := 0
	for i := range r.Dimension {
		d := &r.Dimension[i]
		if err := validateDimension(d); err != nil {
			return err
		}
		if seen[d.Num] {
			return fmt.Errorf("rubric: dimension %d declared twice", d.Num)
		}
		seen[d.Num] = true
		sum += d.Weight
	}
	if sum != totalWeight {
		return fmt.Errorf("rubric: weights sum to %d, want %d", sum, totalWeight)
	}
	return r.Lists.validate()
}

// validateDimension checks one entry in isolation. Uniqueness and the weight sum belong to
// the document rather than to any single dimension, so they stay with the caller.
func validateDimension(d *Dimension) error {
	switch {
	case d.Num < 1 || d.Num > dimensionCount:
		return fmt.Errorf("rubric: dimension number %d out of range 1..%d", d.Num, dimensionCount)
	case d.Key == "" || d.Name == "":
		return fmt.Errorf("rubric: dimension %d has no key or name", d.Num)
	case !d.Response.Valid() || d.Response == ResponseUnclassified:
		return fmt.Errorf("rubric: dimension %d has response %q; want neutral, additive, "+
			"or subtractive", d.Num, d.Response)
	case d.Note == "":
		// The note is the whole reason this is a document. A weight without one is a magic
		// number that has merely changed file.
		return fmt.Errorf("rubric: dimension %d has no note recording why its weight is %d",
			d.Num, d.Weight)
	}
	return nil
}

// validate rejects an empty word list, which would silently disable a check.
func (l *Lists) validate() error {
	for name, list := range map[string][]string{
		"filler_tails":       l.FillerTails,
		"slop":               l.Slop,
		"checkpoint_markers": l.CheckpointMarkers,
	} {
		if len(list) == 0 {
			return errors.New("rubric: list " + name + " is empty, which turns its check off")
		}
	}
	return nil
}

// mustLoadEmbedded parses the compiled-in document.
//
// It re-parses on every call rather than caching. Measured at ~120µs, against one call per
// skill scored, so the corpus costs tens of milliseconds; caching would mean a package
// global or a sync.Once, both of which this codebase avoids for reasons that outlast the
// saving. If it ever matters the fix is local to this function.
//
// It panics rather than returning an error, and that is right exactly once: the bytes are
// compiled into the binary, so a failure here is a malformed source file rather than
// anything a caller did or could recover from. TestEmbeddedRubricLoads catches it at build
// time. Making every caller handle an impossible error would spread a runtime error path
// through code that has no answer for it.
func mustLoadEmbedded() *Rubric {
	r, err := Load(bytes.NewReader(embedded))
	if err != nil {
		panic("rubric: the embedded rubric.toml is malformed: " + err.Error())
	}
	return r
}
