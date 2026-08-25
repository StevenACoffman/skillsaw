// Package scores reads the judge-supplied dimension bases that turn skillsaw's
// deterministic floor into the full rubric total.
//
// A base is a number a reader assigned after reading a particular version of a skill.
// Nothing about it survives an edit, so an entry records the content hash it was judged
// against and is only applied to a skill that still hashes to it. Without that binding a
// file judged before an edit produces a full total for the text after it, silently --
// and in the optimize loop that total is what the keep-or-revert gate compares.
//
// Parsing and lookup are pure; the caller reads the file.
package scores

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"github.com/StevenACoffman/skillsaw/internal/noise"
)

// MinBase and MaxBase bound a judged dimension base.
const (
	MinBase = 1
	MaxBase = 10
)

// MaxDimension is the highest rubric dimension a base may name.
const MaxDimension = 9

// Aggregate summarises a set of per-case soft scores as one dimension base.
//
// Base is a point estimate and BaseLow..BaseHigh is the range the sample actually
// supports. They are reported together because rounding a mean to an integer hides how
// little a handful of cases pins it: three cases agreeing exactly still leave most of the
// scale open, and a Base read on its own looks like a measurement rather than a guess.
// Resolved() is the question a caller should ask before treating Base as one number.
type Aggregate struct {
	Cases    int        // how many cases went into it
	MeanSoft float64    // the mean of their soft scores, in [0,1]
	Base     int        // that mean on the rubric's MinBase..MaxBase scale
	Interval [2]float64 // conservative 95% interval on MeanSoft
	BaseLow  int        // Interval's lower end on the MinBase..MaxBase scale
	BaseHigh int        // Interval's upper end on the same scale
}

// Entry is one skill version's bases: the numbers a reader assigned, and the content
// they were assigned to.
type Entry struct {
	Skill string      `json:"skill,omitempty"` // a label for reports; the hash is the key
	Hash  string      `json:"hash"`
	Bases map[int]int `json:"bases"`

	// Baseline records what was observed with the skill *absent*.
	//
	// A skill written against a failure the model does not actually exhibit is pure
	// context cost, and no rubric dimension can detect one: the document is about a
	// real-sounding problem and is perfectly well-formed. The only thing that separates
	// the two is having watched the model fail without it. That observation is evidence
	// about a version, so it lives here beside the bases, hash-bound the same way.
	//
	// Empty means **unmeasured**, which is a third state beside pass and fail and not a
	// synonym for either. It blocks nothing -- consistent with finding.Unexamined,
	// noise.VerdictUnknown, and an unresolved base, all of which name a gap without
	// pricing it.
	Baseline string `json:"baseline,omitempty"`

	// Rubric is the rubric edition these bases were assigned under.
	//
	// A base is a reader's answer to a question the rubric asked. Change what the
	// rubric asks and the answer is to a different question, however unchanged the
	// skill is -- so the edition binds a base to its rules exactly as Hash binds it to
	// its text, and Bases refuses a mismatch the same way.
	//
	// Empty means **unknown**, not current. An entry written before editions existed
	// cannot claim to match today's rules, and reading it as a match is the silent
	// failure this field exists to stop.
	Rubric string `json:"rubric,omitempty"`
}

// File is a parsed scores document.
type File struct {
	// Entries is empty for the legacy shape, which carries no hash.
	Entries []Entry

	// Unbound holds the bases of a legacy document: a bare dimension-to-base object
	// with nothing recording which text it describes. It applies to whatever skill it
	// is handed, because there is no way to tell whether it belongs to that one.
	Unbound map[int]int
}

// Resolved reports whether the sample pins the base to a single value. A false result is
// not a failure: it says the cases scored are too few or too inconsistent to distinguish
// the neighbouring bases, and the fix is more cases rather than a different skill.
func (a *Aggregate) Resolved() bool { return a.Cases > 0 && a.BaseLow == a.BaseHigh }

// Parse reads either shape of scores document.
//
// The legacy shape is a bare object of dimension bases, `{"1": 8, "2": 7}`, which is what
// skillsaw-skill's Phase 1 writes today. The current shape wraps entries that each name
// the hash they were judged against. They are told apart by the presence of "entries",
// the same way testprompts distinguishes its accepted shapes.
//
// A key that is not a dimension number, or a base outside MinBase..MaxBase, is an error
// rather than a silent default: a partially understood scores file yields a total that
// looks comparable to a correct one.
//
// Ensures: exactly one of Entries and Unbound is populated; it is pure.
func Parse(data []byte) (*File, error) {
	var probe struct {
		Entries []json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("parse scores: %w", err)
	}
	if probe.Entries == nil {
		bases, err := parseBases(data)
		if err != nil {
			return nil, err
		}
		return &File{Unbound: bases}, nil
	}
	return parseEntries(probe.Entries)
}

// Marshal renders entries as the hash-bound document Parse reads.
//
// The writer lives beside the reader so the shape has one definition. A caller that
// assembled this JSON itself would be the second, and the two would drift the first time
// either changed -- the defect File.Rewrites exists to catch on the way in.
//
// Bases are validated here rather than by the caller: an out-of-range base must be
// unwritable, not merely rejected later by whatever reads the file back.
//
// Ensures: Parse(Marshal(e)) yields e; it is pure.
func Marshal(entries []Entry) ([]byte, error) {
	for i := range entries {
		if entries[i].Hash == "" {
			return nil, fmt.Errorf(
				"marshal scores: entry %d has no hash; an entry that names no version "+
					"cannot be checked against one", i+1)
		}
		for dim, base := range entries[i].Bases {
			if dim < 1 || dim > MaxDimension {
				return nil, fmt.Errorf(
					"marshal scores: entry %d: invalid dimension %d (want 1..%d)",
					i+1,
					dim,
					MaxDimension,
				)
			}
			if base < MinBase || base > MaxBase {
				return nil, fmt.Errorf(
					"marshal scores: entry %d: dimension %d base %d out of range %d..%d",
					i+1,
					dim,
					base,
					MinBase,
					MaxBase,
				)
			}
		}
	}
	b, err := json.MarshalIndent(struct {
		Entries []Entry `json:"entries"`
	}{Entries: entries}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal scores: %w", err)
	}
	return append(b, '\n'), nil
}

// Bases returns the bases judged against hash.
//
// stale reports the case that must not pass silently: this skill was judged, but at some
// other version. It is a different answer from never having been judged -- one means
// "re-judge it", the other means "judge it" -- and a caller that could not tell them
// apart would see an unexplained missing total in both.
//
// Ensures: bases is nil when stale is true; it is pure.
func (f *File) Bases(skill, hash, edition string) (bases map[int]int, stale bool) {
	if len(f.Unbound) > 0 {
		return f.Unbound, false
	}
	for i := range f.Entries {
		if f.Entries[i].Hash != hash {
			continue
		}
		// Right text, wrong rules is as stale as wrong text. It routes to the same
		// verdict deliberately: the caller already refuses stale bases and names both
		// versions, and a second disposition for a second cause would need a second
		// place that remembers to handle it.
		if f.Entries[i].Rubric != edition {
			return nil, true
		}
		return f.Entries[i].Bases, false
	}
	for i := range f.Entries {
		if f.Entries[i].Skill != "" && f.Entries[i].Skill == skill {
			return nil, true
		}
	}
	return nil, false
}

// Measured reports whether a no-guidance control was recorded for this exact content.
//
// It answers a different question from Bases, and the difference is the point: bases say
// how good the skill is, this says whether anyone established there was a problem to solve.
// A skill can be well scored and unmeasured, and that combination is the one worth naming.
//
// Requires: hash identifies the version being asked about.
// Ensures:  false for a legacy unbound document, which records no hash and therefore
// cannot claim an observation belongs to any particular version.
func (f *File) Measured(hash string) bool {
	for i := range f.Entries {
		if f.Entries[i].Hash == hash {
			return f.Entries[i].Baseline != ""
		}
	}
	return false
}

// RubricAt returns the rubric edition this skill was last judged under, or "" for an entry
// written before editions existed. Like JudgedAt, it is for naming both sides of a stale
// verdict rather than for deciding one.
func (f *File) RubricAt(skill string) string {
	for i := range f.Entries {
		if f.Entries[i].Skill == skill {
			return f.Entries[i].Rubric
		}
	}
	return ""
}

// JudgedAt returns the hash this skill was last judged against, for a message that can
// name both versions. It is only meaningful when Bases reported stale.
func (f *File) JudgedAt(skill string) string {
	for i := range f.Entries {
		if f.Entries[i].Skill == skill {
			return f.Entries[i].Hash
		}
	}
	return ""
}

// parseEntries decodes the hash-bound shape.
func parseEntries(raw []json.RawMessage) (*File, error) {
	entries := make([]Entry, 0, len(raw))
	for i, r := range raw {
		var e struct {
			Skill    string          `json:"skill"`
			Hash     string          `json:"hash"`
			Bases    json.RawMessage `json:"bases"`
			Baseline string          `json:"baseline"`
			Rubric   string          `json:"rubric"`
		}
		if err := json.Unmarshal(r, &e); err != nil {
			return nil, fmt.Errorf("parse scores: entry %d: %w", i+1, err)
		}
		if e.Hash == "" {
			return nil, fmt.Errorf(
				"parse scores: entry %d has no hash; an entry that names no version "+
					"cannot be checked against one", i+1)
		}
		bases, err := parseBases(e.Bases)
		if err != nil {
			return nil, fmt.Errorf("entry %d: %w", i+1, err)
		}
		entries = append(entries, Entry{
			Skill: e.Skill, Hash: e.Hash, Bases: bases,
			Baseline: e.Baseline, Rubric: e.Rubric,
		})
	}
	return &File{Entries: entries}, nil
}

// parseBases decodes and validates a dimension-to-base object.
func parseBases(data []byte) (map[int]int, error) {
	var raw map[string]int
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse scores: %w", err)
	}
	out := make(map[int]int, len(raw))
	for k, v := range raw {
		n, err := strconv.Atoi(k)
		if err != nil || n < 1 || n > MaxDimension {
			return nil, fmt.Errorf(
				"parse scores: invalid dimension key %q (want \"1\"..\"%d\")", k, MaxDimension)
		}
		if v < MinBase || v > MaxBase {
			return nil, fmt.Errorf("parse scores: dimension %d base %d out of range %d..%d",
				n, v, MinBase, MaxBase)
		}
		out[n] = v
	}
	return out, nil
}

// Aggregated turns per-case soft scores into a dimension base: the mean, placed on the
// rubric's 1-10 scale.
//
// This is the arithmetic an optimize loop otherwise does by hand for dim 8, where the
// result reaches the keep/revert gate with nothing checking it.
//
// A mean of zero maps to MinBase, not to 0. Ten times a zero mean rounds to 0, but 0 is
// not a point on the rubric scale and Parse rejects it — failing every check *is* the
// floor, and 0 is an artefact of the formula rather than a score. That clamp is
// legitimate where internal/calibrate's refusal to clamp was not: there, moving an
// out-of-range value would invent a judgment nobody made; here the judgment is "it failed
// everything", and the floor is what the scale calls that.
//
// Requires: len(softs) > 0 — with no cases there is no base to report, and a caller must
//
//	refuse before asking rather than be handed a fabricated one.
//
// Ensures: Base is in [MinBase, MaxBase]; it is pure.
func Aggregated(softs []float64) Aggregate {
	if len(softs) == 0 {
		return Aggregate{}
	}
	var sum float64
	for _, s := range softs {
		sum += s
	}
	mean := sum / float64(len(softs))
	iv := noise.MeanInterval(softs)
	return Aggregate{
		Cases:    len(softs),
		MeanSoft: mean,
		Base:     baseOf(mean),
		Interval: iv,
		BaseLow:  baseOf(iv[0]),
		BaseHigh: baseOf(iv[1]),
	}
}

// baseOf maps a soft score in [0,1] onto the rubric's base scale.
func baseOf(soft float64) int {
	base := int(math.Round(soft * float64(MaxBase)))
	if base < MinBase {
		return MinBase
	}
	if base > MaxBase {
		return MaxBase
	}
	return base
}
