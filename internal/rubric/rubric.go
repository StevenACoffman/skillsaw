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
package rubric

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/StevenACoffman/skillet/identity"
	"github.com/StevenACoffman/skillet/markdown"
	"github.com/StevenACoffman/skillet/neutrality"
	"github.com/StevenACoffman/skillet/skill"
	"github.com/StevenACoffman/skillet/speclint"
)

// These are the *semantic* patterns the rubric still owns. Markdown structure
// (headings, lists, tables, links, code fences) is now parsed by the markdown
// package via goldmark; only meaning-bearing phrase detection lives here.
var (
	// failureCN/failureEN detect inline "if X fails / when Y errors" branches in
	// prose (code already blanked by markdown.Doc.Prose).
	failureCN = regexp.MustCompile(`如果.{0,24}(失败|错误|不可用|超时|冲突|缺失|找不到|异常|没有)`)
	failureEN = regexp.MustCompile(
		`(?i)(if|when)\s.{0,40}(fail|error|missing|unavailable|timeout|not found|goes wrong|blocked|get stuck|gets stuck)`,
	)
	// workflowMark detects step/phase language. Numbered-list detection is handled
	// structurally by markdown.Doc.HasOrderedList, so it is no longer regex'd here.
	workflowMark = regexp.MustCompile(`(?i)(步骤|phase\s|step\s)`)
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
type Config struct {
	FillerTails       []string // dim1: banned trailing filler
	Softening         []string // dim5: softening phrases (>=3 -> penalty)
	Slop              []string // dim7: AI-slop words (each -> -1)
	CheckpointMarkers []string // dim4: explicit visual markers
	BlacklistHeadings []string // dim9: section-title signals for a counter-example section
	FailureSections   []string // dim3: headings that count as failure-mode encoding
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
		Softening: []string{
			"建议", "可以考虑", "根据情况", "灵活把握", "视情况而定",
			"as appropriate", "it depends", "you might want", "feel free",
			"at your discretion", "where appropriate", "as you see fit", "if you prefer",
		},
		Slop: []string{
			"说白了", "换句话说", "综上", "首先", "其次",
			"in other words", "that said", "at the end of the day",
			"needless to say", "it's worth noting", "simply put", "in essence",
		},
		CheckpointMarkers: []string{
			"🔴", "🛑", "⚠️", "🚨", "🚦", "STOP", "CHECKPOINT", "HALT",
		},
		BlacklistHeadings: []string{
			"反例", "黑名单", "反模式", "不要做", "不要",
			"don't", "do not", "avoid", "blacklist", "boundary", "pitfall",
			"anti-pattern", "antipattern", "when not to", "common mistake",
			"common failure", "red flag", "red line", "failure mode", "gotcha",
			"caution", "limitation",
		},
		FailureSections: []string{
			"反例", "边界", "异常", "失败", "回退",
			"boundary", "pitfall", "anti-pattern", "antipattern", "failure mode",
			"common failure", "when not to", "troubleshoot", "if this fails", "limitation",
			"edge case", "recovery", "when it breaks", "gotcha", "fallback", "red line",
		},
	}
}

// Dimensions returns the authoritative rubric. Weights use the reconciled table
// from spec D1 (dim6 = 5) so the nine weights sum to exactly 100.
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

// ParseScores parses an untrusted JSON object of judge-supplied dimension bases,
// e.g. {"2": 8, "3": 7}. Keys must be dimension numbers "1".."9" and values must
// be in 1..10; anything else is an error rather than a silent default (SkillOpt
// extract_json: prefer an error over an ambiguous or partial object).
func ParseScores(data []byte) (map[int]int, error) {
	var raw map[string]int
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse scores: %w", err)
	}
	out := make(map[int]int, len(raw))
	for k, v := range raw {
		n, err := strconv.Atoi(k)
		if err != nil || n < 1 || n > len(Dimensions()) {
			return nil, fmt.Errorf("parse scores: invalid dimension key %q (want \"1\"..\"9\")", k)
		}
		if v < 1 || v > 10 {
			return nil, fmt.Errorf("parse scores: dimension %d base %d out of range 1..10", n, v)
		}
		out[n] = v
	}
	return out, nil
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
		checkFailure(doc, cfg, ds)
	case 4:
		deriveCheckpoint(doc, cfg, ds)
	case 5:
		checkSoftening(doc, cfg, ds)
	case 6:
		deriveResources(s, doc, ds)
	case 7:
		checkSlop(doc, cfg, ds)
	case 9:
		deriveBlacklist(doc, cfg, ds)
	}
}

func checkFrontmatter(s *skill.Skill, cfg *Config, ds *DimScore) {
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

func checkFailure(doc *markdown.Doc, cfg *Config, ds *DimScore) {
	prose := doc.Prose
	branches := len(failureCN.FindAllString(prose, -1)) +
		len(failureEN.FindAllString(prose, -1)) +
		strings.Count(strings.ToLower(prose), "fallback") +
		strings.Count(prose, "兜底")
	// A dedicated boundary / anti-pattern / troubleshooting section is itself
	// failure-mode encoding, even without inline "if X fails" branches — which is
	// how most decision-framework skills document their limits.
	hasSection := sectionTitleMatches(doc, cfg.FailureSections)
	hasWorkflow := doc.HasOrderedList || workflowMark.MatchString(prose)
	if branches == 0 && !hasSection && hasWorkflow {
		ds.Penalty += 3
		ds.Flags = append(
			ds.Flags,
			"forward-only workflow: no failure branches or boundary section",
		)
		return
	}
	detail := strconv.Itoa(branches) + " failure branch(es)"
	if hasSection {
		detail += " + failure-handling section"
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
		msg := "no explicit checkpoint markers (judge if this skill type needs them)"
		if n > 0 {
			msg = strconv.Itoa(
				n,
			) + " checkpoint marker(s) — too few to derive; judge if more are needed"
		}
		ds.Flags = append(ds.Flags, msg)
		return
	}
	ds.Base = 9
	ds.HasBase = true
	ds.Flags = append(ds.Flags, strconv.Itoa(n)+" explicit checkpoint marker(s)")
}

func checkSoftening(doc *markdown.Doc, cfg *Config, ds *DimScore) {
	n := 0
	for _, w := range cfg.Softening {
		n += strings.Count(doc.Prose, w)
	}
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

func deriveBlacklist(doc *markdown.Doc, cfg *Config, ds *DimScore) {
	units := -1 // -1 => no recognized section at all
	// Score the richest matching section, not the first: a thin early "Caution"
	// note must not hide a later 10-row "Common Mistakes" table.
	for _, sec := range doc.Sections {
		if containsAny(strings.ToLower(sec.Title), cfg.BlacklistHeadings) && sec.Units > units {
			units = sec.Units
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

// sectionTitleMatches reports whether any section heading contains one of the
// (case-insensitive) signal substrings.
func sectionTitleMatches(doc *markdown.Doc, signals []string) bool {
	for _, sec := range doc.Sections {
		if containsAny(strings.ToLower(sec.Title), signals) {
			return true
		}
	}
	return false
}

// containsAny reports whether s contains any of the signals. s is already
// lowercased by callers.
func containsAny(s string, signals []string) bool {
	for _, sig := range signals {
		if matchesSignal(s, strings.ToLower(sig)) {
			return true
		}
	}
	return false
}

// matchesSignal reports whether s contains sig or a regular inflection of it
// (both lowercased). An ASCII signal must begin at a word boundary, so "red flag"
// does not match "requi[red flag]s" (the "red" is mid-word). Append-only
// inflections already match because the signal is a prefix of the longer word
// ("mistake" in "mistakes", "troubleshoot" in "troubleshooting"); the one regular
// inflection a prefix match cannot reach — a consonant+"y" pluralizing to "ies" —
// is probed explicitly, so "boundary" matches "boundaries". Non-ASCII (CJK)
// signals have no word boundaries and match as plain substrings.
func matchesSignal(s, sig string) bool {
	if !isASCII(sig) {
		return strings.Contains(s, sig)
	}
	if matchesForm(s, sig) {
		return true
	}
	if plural, ok := iesPlural(sig); ok {
		return matchesForm(s, plural)
	}
	return false
}

// matchesForm reports whether form appears in s beginning at a word boundary.
func matchesForm(s, form string) bool {
	for idx := 0; ; {
		p := strings.Index(s[idx:], form)
		if p < 0 {
			return false
		}
		p += idx
		if p == 0 || !isWordByte(s[p-1]) {
			return true // form starts at a word boundary
		}
		idx = p + 1
	}
}

// iesPlural returns sig with a trailing consonant+"y" rewritten to "ies" — the
// regular English plural that rewrites the stem (boundary->boundaries,
// policy->policies), which a prefix match cannot reach. ok is false otherwise; a
// vowel+"y" ("day"->"days") is append-only and already covered by matchesForm.
func iesPlural(sig string) (plural string, ok bool) {
	if len(sig) < 2 || sig[len(sig)-1] != 'y' || isVowel(sig[len(sig)-2]) {
		return "", false
	}
	return sig[:len(sig)-1] + "ies", true
}

func isVowel(b byte) bool {
	switch b {
	case 'a', 'e', 'i', 'o', 'u':
		return true
	default:
		return false
	}
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func isWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
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
