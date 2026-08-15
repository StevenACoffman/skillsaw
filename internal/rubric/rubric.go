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
)

// Dimension is one rubric axis.
type Dimension struct {
	Num    int
	Key    string
	Name   string
	Weight int
	// NeedsJudge is true when the dimension's base 1-10 quality cannot be
	// determined without a model (spec §8.1). Deterministic penalties still apply.
	NeedsJudge bool
}

// DimScore is the per-dimension result.
type DimScore struct {
	Num        int      `json:"num"`
	Name       string   `json:"name"`
	Weight     int      `json:"weight"`
	Base       int      `json:"base"`        // 1..10 when known
	HasBase    bool     `json:"has_base"`    // false => NeedsJudge and no judge supplied
	Penalty    int      `json:"penalty"`     // deterministic penalty (>=0)
	Final      int      `json:"final"`       // clamp(effectiveBase - penalty, 1, 10)
	NeedsJudge bool     `json:"needs_judge"` // base is an LLM judgment
	Flags      []string `json:"flags,omitempty"`
}

// Evaluation is the full deterministic evaluation of one skill.
type Evaluation struct {
	Skill              string     `json:"skill"`
	Hash               string     `json:"hash"`
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
}

// DefaultConfig returns the banned/marker/heading lists. It covers both the
// Chinese darwin-source vocabulary and English equivalents, because the checks
// must fire on English skills too (a China-only list scores every English skill
// as defect-free, which is the opposite of useful).
func DefaultConfig() *Config {
	return &Config{
		FillerTails: []string{
			"灵活应用", "根据情况判断", "视情况而定", "灵活把握",
			"as appropriate", "use your judgment", "as needed",
			"depending on context", "your mileage may vary",
		},
		Slop: []string{
			"说白了", "换句话说", "综上", "首先", "其次",
			"in other words", "that said", "at the end of the day",
			"needless to say", "it's worth noting", "simply put", "in essence",
		},
		CheckpointMarkers: []string{
			"🔴", "🛑", "⚠️", "🚨", "🚦", "STOP", "CHECKPOINT", "HALT",
		},
	}
}

// Dimensions returns the authoritative rubric. Weights use the reconciled table
// from spec D1 (dim6 = 5) so the nine weights sum to exactly 100. Dims 3, 5, and 9
// are the SkillLens dimensions (see the package doc); the rest are darwin's spec §8.
func Dimensions() []Dimension {
	return []Dimension{
		{1, "frontmatter", "Frontmatter quality", 7, true},
		{2, "workflow", "Workflow clarity", 12, true},
		{3, "failure", "Failure-mode encoding", 12, true},
		{4, "checkpoint", "Checkpoint design", 6, false},
		{5, "specificity", "Actionable specificity", 17, true},
		{6, "resources", "Resource integration", 5, false},
		{7, "architecture", "Overall architecture", 12, true},
		{8, "effectiveness", "Real-world test performance", 23, true},
		{9, "blacklist", "Counter-examples / blacklist", 6, false},
	}
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
	ev := &Evaluation{Skill: s.Name, Hash: identity.Hash(s.Raw), Bytes: s.Bytes}
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
func fullScore(dims []DimScore, bases map[int]int) (float64, bool) {
	if bases == nil {
		return 0, false
	}
	sum := 0
	for i := range dims {
		d := &dims[i]
		base := d.Base
		if d.NeedsJudge {
			b, ok := bases[d.Num]
			if !ok {
				return 0, false
			}
			base = b
		}
		sum += clamp(base-d.Penalty, 1, 10) * d.Weight
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

// applyChecks runs the deterministic sub-check for a dimension. Dims 2 and 8 have
// no deterministic check (pure needs-judge) and fall through.
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
	case 9:
		deriveBlacklist(doc, ds)
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
	for _, tail := range cfg.FillerTails {
		if strings.HasSuffix(strings.TrimRight(desc, "。.\" '"), tail) {
			ds.Penalty++
			ds.Flags = append(ds.Flags, "filler tail: "+tail)
			break
		}
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

func deriveCheckpoint(doc *markdown.Doc, cfg *Config, ds *DimScore) {
	n := 0
	for _, m := range cfg.CheckpointMarkers {
		n += strings.Count(doc.Prose, m)
	}
	// Only substantial marker usage (>=3) is positive evidence of checkpoint
	// discipline (base 9). Zero-to-two markers is neither strong evidence nor a
	// defect — a lone ⚠️ warning must not score WORSE than no markers at all — so
	// dim 4 defers to a judge. (Absence is legitimate for knowledge/decision
	// skills; pinning it low made "add checkpoints" a useless universal diagnosis.)
	if n < 3 {
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
	ds.Base = 9
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
	ds.Base = clamp(10-broken, 1, 10)
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
	for _, w := range cfg.Slop {
		c := strings.Count(doc.Prose, w)
		if c > 0 {
			n += c
			ds.Flags = append(ds.Flags, "AI-slop: "+w+" ×"+strconv.Itoa(c))
		}
	}
	ds.Penalty += n // §8.2: each occurrence -1
}

func deriveBlacklist(doc *markdown.Doc, ds *DimScore) {
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
		ds.Base = 2
		ds.Flags = append(ds.Flags, "no counter-example / boundary section")
	case units < 3:
		ds.Base = 6
		ds.Flags = append(ds.Flags, "thin counter-example section ("+strconv.Itoa(units)+" points)")
	default:
		ds.Base = 9
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
	ds.Final = clamp(base-ds.Penalty, 1, 10)
}

// total computes the weighted rubric total (§8.3): Σ(Final × weight) / 10.
func total(dims []DimScore) float64 {
	sum := 0
	for _, d := range dims {
		sum += d.Final * d.Weight
	}
	return float64(sum) / 10.0
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
