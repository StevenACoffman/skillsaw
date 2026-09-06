// Package rubric implements the darwin 9-dimension rubric (spec §8) as far as it
// can be scored deterministically. It computes the deterministic sub-checks
// (§8.2), the fully-derivable dimensions (4, 6, 9 — §8.4), the runtime-neutrality
// hit count (§9), and the weighted total (§8.3).
//
// Dimensions whose *base* quality is an irreducible textual judgment (2, 3, 5,
// 7 and the effectiveness dimension 8) are marked NeedsJudge: skillsaw reports
// what a model would still need to score, and computes a "deterministic score"
// that assumes a perfect base for those dims and docks only objectively
// detectable defects (a linter-style lower bound on quality loss).
//
// Three of the nine come from a different source than darwin's spec §8. Dimensions
// 3 (failure-mode encoding), 5 (actionable specificity), and 9 (counter-examples /
// blacklist) are the microsoft/SkillLens quality rubric (arXiv:2605.23899), each
// validated at 65-66% predictive accuracy against downstream skill utility. Their
// detectors are skillet/skilllens (FailureMechanisms, SofteningPhrases,
// BlacklistSections), which adh also scores, so the two tools cannot drift; the weights
// and 1-10 mapping stay skillsaw's.
//
// # What dims 3, 5 and 9 are valid for
//
// Those three reward an encoded failure branch, concrete rather than hedged steps, and an
// explicit boundary section. Those are the right signals for a skill whose baseline
// failure is a model doing the wrong thing under pressure -- a discipline skill, where the
// text has to hold a line the model would otherwise cross.
//
// They are not the right signals for every skill, and the form that fixes one failure type
// can measurably worsen another: in a controlled comparison of guidance forms, a
// prohibition-shaped arm produced clearly more of the unwanted content than a
// recipe-shaped arm, with fully separated distributions, and trended worse than giving no
// guidance at all. A skill whose baseline failure is wrong-shaped output or an omitted
// field wants a recipe, and scoring it against a boundary section rewards the wrong shape.
//
// The consequence for a reader of these scores: a low 3, 5 or 9 on a reference skill is a
// statement about form, not about quality, and three empty detector results are not three
// passes. The scores here are reported per dimension and never collapsed into one number
// precisely so that this distinction survives to whoever acts on them. The dimensions are
// not conditioned on skill class because skillsaw has no trustworthy way to infer the
// class, and guessing it would silently change what a score means.
package rubric

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/StevenACoffman/skillet/identity"
	"github.com/StevenACoffman/skillet/markdown"
	"github.com/StevenACoffman/skillet/neutrality"
	"github.com/StevenACoffman/skillet/skill"
	"github.com/StevenACoffman/skillet/skilllens"
	"github.com/StevenACoffman/skillet/speclint"
	"github.com/StevenACoffman/skillet/testprompts"
)

// How a dimension's deterministic score responds to feeding it more of what it counts.
const (
	// ResponseUnclassified is the zero value and means nobody has established the
	// direction. It is not "neutral": an unmeasured dimension must not read as one
	// that was measured and found inert, which is the same rule finding.Action's
	// absent zero follows. Dimensions() must classify every dimension, and a test
	// enforces it.
	ResponseUnclassified Response = ""
	// ResponseNeutral means adding does not move the deterministic score.
	ResponseNeutral Response = "neutral"
	// ResponseAdditive means adding raises it -- an optimiser target.
	ResponseAdditive Response = "additive"
	// ResponseSubtractive means adding LOWERS it, so the cheap move is to remove.
	// Dim 4 is the live instance and it is the surprising one: an unscored
	// needs-judge dimension is optimistically assumed to be a perfect 10, so
	// deriving any real base can only match or reduce it.
	ResponseSubtractive Response = "subtractive"
)

// Response is the direction a dimension's deterministic score moves when the artifact
// gains more of what that dimension counts.
type Response string

// Dimension is one rubric axis.
type Dimension struct {
	Num    int    `toml:"num"`
	Key    string `toml:"key"`
	Name   string `toml:"name"`
	Weight int    `toml:"weight"`
	// NeedsJudge is true when the dimension's base 1-10 quality cannot be
	// determined without a model (spec §8.1). Deterministic penalties still apply.
	NeedsJudge bool `toml:"needs_judge"`
	// Response says which way this dimension's deterministic score moves when the
	// artifact gains something the dimension counts. Measured, not assumed -- see
	// TestDimensionResponseMatchesBehaviour, which feeds each dimension the thing it
	// counts and asserts the direction.
	//
	// It records a mechanism, not a verdict. Every dimension here is defensible and
	// none is being called wrong; dims 4 and 9 were tuned against the 233-skill corpus
	// and the comments on their checks say why. What this captures is that an
	// optimiser pointed at the total has a cheap move available, and which way it
	// runs -- a fact a person editing one of these thresholds needs and cannot get
	// from the weight.
	//
	// The concrete instance it exists for: a surveyed self-optimising loop raised a
	// harness score by 37% with no capability change, by adding files a
	// presence-counting metric rewarded, and logged the artefacts as "legitimate".
	Response Response `toml:"response"`

	// Note records why this dimension has the weight it has. The loader refuses a
	// dimension without one: a weight with no warrant is a magic number that has
	// merely changed file, and recording the warrant is the reason the rubric is a
	// document at all.
	Note string `toml:"note"`
}

// DimScore is the per-dimension result.
type DimScore struct {
	Num        int      `json:"num"`
	Name       string   `json:"name"`
	Weight     int      `json:"weight"`
	Base       int      `json:"base"`        // 1..10 when known
	HasBase    bool     `json:"has_base"`    // false => NeedsJudge and no judge supplied
	Penalty    int      `json:"penalty"`     // deterministic penalty (>=0)
	Final      int      `json:"final"`       // clampScore(effectiveBase - penalty)
	NeedsJudge bool     `json:"needs_judge"` // base is an LLM judgment
	Flags      []string `json:"flags,omitempty"`
}

// Evaluation is the full deterministic evaluation of one skill.
type Evaluation struct {
	Skill string `json:"skill"`
	Hash  string `json:"hash"`

	// Rubric is the edition that produced these scores.
	//
	// Two evaluations of the same skill under different rules are not a before and an
	// after: dim 4 scoring 9 then 7 says nothing if the checkpoint band moved in between.
	// Recorded here so a comparison can refuse, the way eval already refuses a judge base
	// from another edition. Empty means unknown, which is not the same as matching.
	Rubric string `json:"rubric"`

	Bytes              int        `json:"bytes"`
	Dims               []DimScore `json:"dims"`
	RuntimeWarn        int        `json:"runtime_warn"`
	DeterministicScore float64    `json:"deterministic_score"`  // §8.3, assumed-perfect judge bases
	FullScore          float64    `json:"full_score,omitempty"` // §8.3, using supplied judge bases
	HasFullScore       bool       `json:"has_full_score"`       // true iff bases covered all judge dims
}

// Config holds the tunable, deterministic knobs (all config-driven per §8.2).
//
// Dims 3, 5, and 9 no longer carry vocabularies here: their detectors moved to
// skillet/skilllens, which owns the failure-section, softening, and blacklist term
// lists so skillsaw and adh cannot drift. Only the dims with no second consumer keep
// their lists local.
type Config struct {
	FillerTails       []string // dim1: banned trailing filler
	Slop              []string // dim7: AI-slop words (each -> -1)
	CheckpointMarkers []string // dim4: explicit visual markers

	// Thresholds are the scoring bands, carried alongside the lists so a check has one
	// place to read its policy from.
	Thresholds Thresholds
}

// Valid reports whether r is a classified direction. The zero value is not one: it
// means nobody established the direction, which is a different claim from "neutral".
func (r Response) Valid() bool {
	switch r {
	case ResponseNeutral, ResponseAdditive, ResponseSubtractive:
		return true
	default:
		return false
	}
}

// DefaultConfig returns the word lists the deterministic checks match against.
//
// They come from the embedded rubric document, which covers both the Chinese darwin-source
// vocabulary and English equivalents -- a China-only list scores every English skill as
// defect-free, which is the opposite of useful.
func DefaultConfig() *Config {
	r := mustLoadEmbedded()
	return &Config{
		FillerTails:       r.Lists.FillerTails,
		Slop:              r.Lists.Slop,
		CheckpointMarkers: r.Lists.CheckpointMarkers,
		Thresholds:        r.Thresholds,
	}
}

// Dimensions returns the authoritative rubric, read from the embedded document.
//
// The weights sum to exactly 100 and each carries a note recording why it has the value it
// has; the loader refuses a document where either fails. What used to be a Go literal with
// its warrant in a comment is now a reviewable diff with its warrant beside it.
func Dimensions() []Dimension {
	return mustLoadEmbedded().Dimension
}

// Evaluate scores a skill deterministically (no judge bases). It is the common
// case; use EvaluateWithBases to fold in a model's per-dimension scores.
func Evaluate(s *skill.Skill, cfg *Config) *Evaluation {
	return EvaluateWithBases(s, cfg, nil)
}

// EvaluateWithBases scores a skill deterministically and, when bases supply a
// score for every needs-judge dimension, also computes the full rubric total.
// It reads a sibling README.md (if present) for the runtime-neutrality scan.
func EvaluateWithBases(s *skill.Skill, cfg *Config, bases map[int]int) *Evaluation {
	ev := &Evaluation{
		Skill: s.Name, Hash: identity.Hash(s.Raw), Bytes: s.Bytes, Rubric: Edition(),
	}
	if ev.Skill == "" {
		ev.Skill = filepath.Base(s.Dir)
	}
	ev.RuntimeWarn = len(neutrality.Scan(scanFiles(s)))

	// Parse the body once (goldmark). Every structural check reads this Doc rather
	// than re-scanning the raw text with regexes.
	doc := markdown.Parse(s.Body)
	for _, d := range Dimensions() {
		ds := DimScore{Num: d.Num, Name: d.Name, Weight: d.Weight, NeedsJudge: d.NeedsJudge}
		applyChecks(d.Num, s, doc, cfg, &ds)
		finalize(&ds)
		ev.Dims = append(ev.Dims, ds)
	}
	ev.DeterministicScore = total(ev.Dims)
	ev.FullScore, ev.HasFullScore = fullScore(ev.Dims, bases)
	return ev
}

// fullScore computes the weighted total using judge-supplied bases for the
// needs-judge dimensions. It reports ok=false unless every needs-judge dim has a
// base — a partial judge cannot produce a trustworthy full total.
//
// A judge-supplied base supersedes this dimension's deterministic penalty rather
// than stacking with it. The judge read the same skill and the same flags, so a
// base already prices the defect the penalty describes; subtracting both charges
// for it twice. Dim 3 makes this concrete — it flags "runs commands but encodes no
// failure branch" and docks 3, and a judge who lowers the base on the strength of
// that flag would have the skill pay 3 more.
//
// A dimension the judge did not score keeps its penalty: there the deterministic
// check is the only reading of the skill there is.
func fullScore(dims []DimScore, bases map[int]int) (float64, bool) {
	if bases == nil {
		return 0, false
	}
	sum := 0
	for i := range dims {
		d := &dims[i]
		if d.NeedsJudge {
			b, ok := bases[d.Num]
			if !ok {
				return 0, false
			}
			sum += clampScore(b) * d.Weight
			continue
		}
		sum += clampScore(d.Base-d.Penalty) * d.Weight
	}
	return float64(sum) / 10.0, true
}

// scanFiles gathers the files the runtime-neutrality scan reads.
func scanFiles(s *skill.Skill) []neutrality.NamedFile {
	files := []neutrality.NamedFile{{Name: "SKILL.md", Content: s.Raw}}
	if readme, err := os.ReadFile(filepath.Join(s.Dir, "README.md")); err == nil {
		files = append(files, neutrality.NamedFile{Name: "README.md", Content: string(readme)})
	}
	return files
}

// applyChecks runs the deterministic sub-check for a dimension. Dim 2 has no
// deterministic check (pure needs-judge) and falls through.
func applyChecks(num int, s *skill.Skill, doc *markdown.Doc, cfg *Config, ds *DimScore) {
	switch num {
	case 1:
		checkFrontmatter(s, cfg, ds)
	case 3:
		checkFailure(doc, ds)
	case 4:
		deriveCheckpoint(doc, cfg, ds)
	case 5:
		checkSoftening(doc, ds)
	case 6:
		deriveResources(s, doc, ds)
	case 7:
		checkSlop(doc, cfg, ds)
	case 8:
		checkScorable(s, ds)
	case 9:
		deriveBlacklist(doc, &cfg.Thresholds, ds)
	}
}

func checkFrontmatter(s *skill.Skill, cfg *Config, ds *DimScore) {
	// A block that did not parse leaves every field below zero, so scoring them
	// reports "missing name" and "missing description" for prose the author wrote and
	// the parser could not reach. Unlike the guards in exegesis's lint and skillet's
	// redlines — which spare only the checks that read parsed fields — every check in
	// this function reads one, so there is nothing left to say honestly.
	//
	// The penalty is exactly what the two flags it replaces summed to, so this changes
	// the diagnosis without moving any score; results.tsv compares totals across runs.
	// Whether an unparseable block deserves a harsher penalty is a scoring question,
	// filed separately rather than smuggled in with a message fix.
	if s.FrontmatterErr != nil {
		ds.Penalty += 6
		ds.Flags = append(ds.Flags, "frontmatter did not parse")
		return
	}
	switch {
	case strings.TrimSpace(s.Name) == "":
		ds.Penalty += 3
		ds.Flags = append(ds.Flags, "missing name")
	case skill.Slug(s.Name) != s.Name:
		ds.Penalty += 2
		ds.Flags = append(ds.Flags, "name not kebab-case")
	}
	desc := strings.TrimSpace(s.Description)
	if desc == "" {
		ds.Penalty += 3
		ds.Flags = append(ds.Flags, "missing description")
	}
	if len([]rune(desc)) > speclint.DescriptionMaxRunes {
		ds.Penalty += 2
		ds.Flags = append(ds.Flags, fmt.Sprintf(
			"description over %d chars", speclint.DescriptionMaxRunes))
	}
	if tail, ok := cfg.HasFillerTail(desc); ok {
		ds.Penalty++
		ds.Flags = append(ds.Flags, "filler tail: "+tail)
	}
}

func checkFailure(doc *markdown.Doc, ds *DimScore) {
	// skilllens.FailureMechanisms is the shared definition of an inline "if X fails"
	// branch (KindProse) and a dedicated failure/boundary section (KindSection). A
	// section is itself failure-mode encoding even without inline branches, which is how
	// most decision-framework skills document their limits.
	branches, hasSection := 0, false
	for _, sp := range skilllens.FailureMechanisms(doc) {
		switch sp.Kind {
		case skilllens.KindProse:
			branches++
		case skilllens.KindSection:
			hasSection = true
		}
	}
	detail := strconv.Itoa(branches) + " failure branch(es)"
	if hasSection {
		detail += " + failure-handling section"
	}
	// A matching section title is not itself an encoded failure mechanism: it is a
	// container, and 154 of the 233 corpus skills have one with no inline branch under it.
	// Whether that absence is a defect depends on the skill, and doc.HasCodeBlock is the
	// question that decides it -- a skill that runs commands and never says what to do when
	// they fail is the defect this dimension exists to catch, while one that executes
	// nothing has no runtime failure to encode and docking it would be a category error.
	//
	// This replaces an older HasOrderedList/workflowMark proxy for the same question.
	// Answering "does this dimension apply" from two signals in two places let the weaker
	// one silently decide every case they disagreed on.
	if branches == 0 && doc.HasCodeBlock {
		ds.Penalty += 3
		ds.Flags = append(ds.Flags, detail+" — runs commands but encodes no failure branch")
		return
	}
	if branches == 0 {
		// Reported, never docked: a mis-derived category must not hide a real gap, and dim 3
		// is judged anyway, so the base is where this belongs.
		ds.Flags = append(ds.Flags, detail+" — executes nothing to fail; judge the base")
		return
	}
	ds.Flags = append(ds.Flags, detail+" detected")
}

// checkScorable reports whether dim 8 can be scored at all, and never docks for it.
//
// The dim-8 base is computed from "judge --all" over each behavioral case's checks. A
// skill whose cases carry none -- and whose "expected" text yields none either -- has no
// base available, and today that is silent: the deterministic score assumes a perfect
// base, so a skill nothing can score reads exactly like one that scored perfectly.
//
// Reported, never penalised, following dims 4 and 9: the absence is in the test prompts
// rather than in the skill, so docking the skill would price someone else's unfinished
// work. It is also not a category error the way a decision skill's missing failure branch
// is -- see the note on checkFailure. Every skill has outputs worth checking; these
// prompts just do not say what a good one contains yet.
//
// It asks testprompts.ChecksFor, which is the same call "judge --all" makes, so this
// cannot report a case scorable that judge would then skip.
func checkScorable(s *skill.Skill, ds *DimScore) {
	f, err := testprompts.Load(filepath.Join(s.Dir, "test-prompts.json"))
	if err != nil {
		ds.Flags = append(ds.Flags, "no readable test-prompts.json; dim 8 cannot be scored")
		return
	}
	behavioral := f.Behavioral()
	scorable := 0
	for i := range behavioral {
		if checks, _ := testprompts.ChecksFor(&behavioral[i]); len(checks) > 0 {
			scorable++
		}
	}
	switch {
	case len(behavioral) == 0:
		ds.Flags = append(ds.Flags, "no behavioral test cases; dim 8 cannot be scored")
	case scorable == 0:
		ds.Flags = append(ds.Flags,
			"none of "+strconv.Itoa(len(behavioral))+" behavioral case(s) specify checks; "+
				"dim 8 cannot be scored until they do")
	case scorable < len(behavioral):
		ds.Flags = append(ds.Flags, strconv.Itoa(scorable)+" of "+
			strconv.Itoa(len(behavioral))+" behavioral case(s) specify checks; "+
			"a base over part of a skill's cases is not comparable with one over all of them")
	default:
		ds.Flags = append(ds.Flags,
			"all "+strconv.Itoa(len(behavioral))+" behavioral case(s) specify checks")
	}
}

func deriveCheckpoint(doc *markdown.Doc, cfg *Config, ds *DimScore) {
	n := 0
	for _, h := range cfg.MarkerHits(doc.Prose) {
		n += h.Count
	}
	// Only substantial marker usage (>=3) is positive evidence of checkpoint
	// discipline (base 9). Zero-to-two markers is neither strong evidence nor a
	// defect — a lone ⚠️ warning must not score WORSE than no markers at all — so
	// dim 4 defers to a judge. (Absence is legitimate for knowledge/decision
	// skills; pinning it low made "add checkpoints" a useless universal diagnosis.)
	if n < cfg.Thresholds.CheckpointMinMarkers {
		ds.NeedsJudge = true
		msg := "no explicit checkpoint markers; this skill runs commands, so judge whether " +
			"it needs them"
		switch {
		case n > 0:
			msg = strconv.Itoa(
				n,
			) + " checkpoint marker(s) — too few to derive; judge if more are needed"
		case !doc.HasCodeBlock:
			// The same sentence reached 230 of 233 corpus skills, which asks the judge the
			// same question about a runbook and a decision framework. A skill that executes
			// nothing has no step to pause between, so name that rather than making the
			// judge re-derive it.
			msg = "no explicit checkpoint markers, and this skill executes nothing to " +
				"checkpoint; likely not applicable"
		}
		ds.Flags = append(ds.Flags, msg)
		return
	}
	ds.Base = cfg.Thresholds.CheckpointBase
	ds.HasBase = true
	ds.Flags = append(ds.Flags, strconv.Itoa(n)+" explicit checkpoint marker(s)")
}

func checkSoftening(doc *markdown.Doc, ds *DimScore) {
	n := len(skilllens.SofteningPhrases(doc))
	switch {
	case n >= 3:
		ds.Penalty += 3
		ds.Flags = append(ds.Flags, strconv.Itoa(n)+" softening phrase(s) (>=3)")
	case n > 0:
		ds.Flags = append(ds.Flags, strconv.Itoa(n)+" softening phrase(s)")
	}
}

func deriveResources(s *skill.Skill, doc *markdown.Doc, ds *DimScore) {
	refs := resourceRefs(doc.Links, s.Dir)
	broken := 0
	for _, r := range refs {
		if _, err := os.Stat(filepath.Join(s.Dir, r)); err != nil {
			broken++
			ds.Flags = append(ds.Flags, "broken link: "+r)
		}
	}
	ds.Base = clampScore(10 - broken)
	ds.HasBase = true
	if broken == 0 {
		ds.Flags = append(ds.Flags, strconv.Itoa(len(refs))+" resource ref(s), all reachable")
	}
}

// resourceRefs extracts intra-skill file references (into any conventional or
// existing subdirectory) from the candidate links the markdown package collected
// (Markdown link destinations and code-span contents). This covers the
// methodology/, extractors/, agents/ conventions large multi-file skills use, not
// just references/scripts/assets/templates.
func resourceRefs(links []string, dir string) []string {
	seen := map[string]bool{}
	var refs []string
	for _, raw := range links {
		t := cleanRef(raw)
		if t == "" || seen[t] {
			continue
		}
		seg, _, _ := strings.Cut(t, "/")
		if !conventionalDir(seg) && !isSubdir(dir, seg) {
			continue // an external URL, an example path, or a placeholder
		}
		seen[t] = true
		refs = append(refs, t)
	}
	return refs
}

// cleanRef normalizes a raw backtick/link target to a relative intra-skill file
// path (dir/.../name.ext), or "" when it is a URL, anchor, absolute or home
// path, a bare filename, or has no file extension.
func cleanRef(raw string) string {
	t := strings.TrimSpace(raw)
	if isExternalRef(t) {
		return ""
	}
	if i := strings.IndexByte(t, '#'); i >= 0 {
		t = t[:i] // strip a trailing anchor
	}
	if !strings.Contains(t, "/") {
		return "" // bare filename: not a directory reference
	}
	if !hasFileExt(t[strings.LastIndexByte(t, '/')+1:]) {
		return ""
	}
	return t
}

// isExternalRef reports whether a target is empty, a URL, an anchor, an
// absolute/home path, or a parent-relative ("../") path — none of which are
// intra-skill file references (dim 6 only checks files within the skill dir).
func isExternalRef(t string) bool {
	return t == "" || strings.Contains(t, "://") ||
		strings.HasPrefix(t, "#") || strings.HasPrefix(t, "/") ||
		strings.HasPrefix(t, "~") || strings.HasPrefix(t, "..") ||
		strings.HasPrefix(t, "mailto:")
}

// hasFileExt reports whether base ends in a non-empty alphanumeric extension.
func hasFileExt(base string) bool {
	dot := strings.LastIndexByte(base, '.')
	if dot <= 0 || dot == len(base)-1 {
		return false
	}
	for _, r := range base[dot+1:] {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// conventionalDir reports whether seg is a conventional skill resource directory.
// Such refs are checked even when the directory is absent, so a broken link into
// a would-be resource dir is still reported.
func conventionalDir(seg string) bool {
	switch seg {
	case "references", "scripts", "assets", "templates",
		"methodology", "extractors", "agents", "docs", "prompts":
		return true
	default:
		return false
	}
}

func isSubdir(dir, seg string) bool {
	info, err := os.Stat(filepath.Join(dir, seg))
	return err == nil && info.IsDir()
}

func checkSlop(doc *markdown.Doc, cfg *Config, ds *DimScore) {
	n := 0
	for _, h := range cfg.SlopHits(doc.Prose) {
		n += h.Count
		ds.Flags = append(ds.Flags, "AI-slop: "+h.Term+" ×"+strconv.Itoa(h.Count))
	}
	ds.Penalty += n // §8.2: each occurrence -1
}

func deriveBlacklist(doc *markdown.Doc, th *Thresholds, ds *DimScore) {
	units := -1 // -1 => no recognized section at all
	// Score the richest matching section, not the first: a thin early "Caution"
	// note must not hide a later 10-row "Common Mistakes" table. skilllens locates the
	// boundary/counter-example sections; the Units threshold below stays skillsaw policy.
	for _, sp := range skilllens.BlacklistSections(doc) {
		if sp.Units > units {
			units = sp.Units
		}
	}
	switch {
	case units <= 0:
		ds.Base = th.BlacklistEmptyBase
		ds.Flags = append(ds.Flags, "no counter-example / boundary section")
	case units < th.BlacklistFullUnits:
		ds.Base = th.BlacklistThinBase
		ds.Flags = append(ds.Flags, "thin counter-example section ("+strconv.Itoa(units)+" points)")
	default:
		ds.Base = th.BlacklistFullBase
		ds.Flags = append(ds.Flags, "counter-example section ("+strconv.Itoa(units)+" points)")
	}
	ds.HasBase = true
}

// finalize computes Final. For dims with a known base, Final = base - penalty.
// For NeedsJudge dims without a supplied base, the deterministic score assumes a
// perfect base (10) and docks only the deterministic penalty.
func finalize(ds *DimScore) {
	base := ds.Base
	if !ds.HasBase {
		base = 10
	}
	ds.Final = clampScore(base - ds.Penalty)
}

// total computes the weighted rubric total (§8.3): Σ(Final × weight) / 10.
func total(dims []DimScore) float64 {
	sum := 0
	for _, d := range dims {
		sum += d.Final * d.Weight
	}
	return float64(sum) / 10.0
}

// clampScore holds a value to the rubric's 1-10 scale. The bounds are the scale
// itself rather than parameters: every dimension is scored on it, and a caller
// that could pass its own would be inventing a second scale.
//
// The floor is 1, not 0: no dimension contributes nothing, so a catastrophic dim 8
// still carries 2.3 of its 23 points.
func clampScore(v int) int {
	const lo, hi = 1, 10
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
