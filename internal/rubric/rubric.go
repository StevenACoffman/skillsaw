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

	"github.com/StevenACoffman/skillsaw/internal/neutrality"
	"github.com/StevenACoffman/skillsaw/internal/skill"
)

// descCharLimit is the Agent Skills description length cap (spec §8.2 dim1).
const descCharLimit = 1024

var (
	// resourceRef matches an actual resource file path — a known resource
	// directory followed by a path ending in a file extension. Requiring the
	// extension avoids matching prose that merely names the directories (e.g.
	// the rubric's own "references/scripts/assets引用正确").
	resourceRef = regexp.MustCompile(
		`(references|scripts|assets|templates)/[A-Za-z0-9._/-]*\.[A-Za-z0-9]+`,
	)
	failureCN = regexp.MustCompile(`如果.{0,24}(失败|错误|不可用|超时|冲突|缺失|找不到|异常|没有)`)
	failureEN = regexp.MustCompile(
		`(?i)if\s.{0,40}(fail|error|missing|unavailable|timeout|not found)`,
	)
	workflowMark = regexp.MustCompile(`(?i)(步骤|phase\s|step\s|^\s*\d+[.)、])`)
	headingLine  = regexp.MustCompile(`^#{1,6}\s`)
	listItemLine = regexp.MustCompile(`^\s*([-*]|\d+[.)、]|\|)`)
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
	BlacklistHeadings []string // dim9: section-title signals
}

// DefaultConfig returns the banned/marker lists from the darwin source, plus a
// few English equivalents to keep the checks runtime- and language-neutral.
func DefaultConfig() *Config {
	return &Config{
		FillerTails:       []string{"灵活应用", "根据情况判断", "视情况而定", "灵活把握"},
		Softening:         []string{"建议", "可以考虑", "根据情况", "灵活把握", "视情况而定"},
		Slop:              []string{"说白了", "换句话说", "综上", "首先", "其次"},
		CheckpointMarkers: []string{"🔴", "🛑", "STOP", "CHECKPOINT"},
		BlacklistHeadings: []string{"反例", "黑名单", "反模式", "不要做", "不要", "don't", "avoid", "blacklist"},
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
	ev := &Evaluation{Skill: s.Name, Hash: skill.Hash(s.Raw), Bytes: s.Bytes}
	if ev.Skill == "" {
		ev.Skill = filepath.Base(s.Dir)
	}
	ev.RuntimeWarn = len(neutrality.Scan(scanFiles(s)))

	for _, d := range Dimensions() {
		ds := DimScore{Num: d.Num, Name: d.Name, Weight: d.Weight, NeedsJudge: d.NeedsJudge}
		applyChecks(d.Num, s, cfg, &ds)
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
func applyChecks(num int, s *skill.Skill, cfg *Config, ds *DimScore) {
	switch num {
	case 1:
		checkFrontmatter(s, cfg, ds)
	case 3:
		checkFailure(s.Body, ds)
	case 4:
		deriveCheckpoint(s.Body, cfg, ds)
	case 5:
		checkSoftening(s.Body, cfg, ds)
	case 6:
		deriveResources(s, ds)
	case 7:
		checkSlop(s.Body, cfg, ds)
	case 9:
		deriveBlacklist(s.Body, cfg, ds)
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
	if len([]rune(desc)) > descCharLimit {
		ds.Penalty += 2
		ds.Flags = append(ds.Flags, "description over 1024 chars")
	}
	for _, tail := range cfg.FillerTails {
		if strings.HasSuffix(strings.TrimRight(desc, "。.\" '"), tail) {
			ds.Penalty++
			ds.Flags = append(ds.Flags, "filler tail: "+tail)
			break
		}
	}
}

func checkFailure(body string, ds *DimScore) {
	branches := len(failureCN.FindAllString(body, -1)) +
		len(failureEN.FindAllString(body, -1)) +
		strings.Count(strings.ToLower(body), "fallback") +
		strings.Count(body, "兜底")
	hasWorkflow := workflowMark.MatchString(body)
	if branches == 0 && hasWorkflow {
		ds.Penalty += 3
		ds.Flags = append(ds.Flags, "forward-only workflow: no failure branches")
		return
	}
	ds.Flags = append(ds.Flags, strconv.Itoa(branches)+" failure branch(es) detected")
}

func deriveCheckpoint(body string, cfg *Config, ds *DimScore) {
	n := 0
	for _, m := range cfg.CheckpointMarkers {
		n += strings.Count(body, m)
	}
	switch {
	case n == 0:
		ds.Base = 2
	case n <= 2:
		ds.Base = 6
	default:
		ds.Base = 9
	}
	ds.HasBase = true
	ds.Flags = append(ds.Flags, strconv.Itoa(n)+" explicit checkpoint marker(s)")
}

func checkSoftening(body string, cfg *Config, ds *DimScore) {
	n := 0
	for _, w := range cfg.Softening {
		n += strings.Count(body, w)
	}
	switch {
	case n >= 3:
		ds.Penalty += 3
		ds.Flags = append(ds.Flags, strconv.Itoa(n)+" softening phrase(s) (>=3)")
	case n > 0:
		ds.Flags = append(ds.Flags, strconv.Itoa(n)+" softening phrase(s)")
	}
}

func deriveResources(s *skill.Skill, ds *DimScore) {
	refs := resourceRef.FindAllString(s.Body, -1)
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

func checkSlop(body string, cfg *Config, ds *DimScore) {
	n := 0
	for _, w := range cfg.Slop {
		c := strings.Count(body, w)
		if c > 0 {
			n += c
			ds.Flags = append(ds.Flags, "AI-slop: "+w+" ×"+strconv.Itoa(c))
		}
	}
	ds.Penalty += n // §8.2: each occurrence -1
}

func deriveBlacklist(body string, cfg *Config, ds *DimScore) {
	inSection := false
	items := 0
	found := false
	for _, line := range strings.Split(body, "\n") {
		if headingLine.MatchString(line) {
			inSection = headingIsBlacklist(line, cfg.BlacklistHeadings)
			found = found || inSection
			continue
		}
		if inSection && listItemLine.MatchString(line) {
			items++
		}
	}
	switch {
	case !found:
		ds.Base = 2
		ds.Flags = append(ds.Flags, "no counter-example / blacklist section")
	case items < 3:
		ds.Base = 6
		ds.Flags = append(ds.Flags, "blacklist section present ("+strconv.Itoa(items)+" items)")
	default:
		ds.Base = 9
		ds.Flags = append(ds.Flags, "blacklist section with "+strconv.Itoa(items)+" concrete items")
	}
	ds.HasBase = true
}

func headingIsBlacklist(line string, signals []string) bool {
	lower := strings.ToLower(line)
	for _, h := range signals {
		if strings.Contains(lower, strings.ToLower(h)) {
			return true
		}
	}
	return false
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
