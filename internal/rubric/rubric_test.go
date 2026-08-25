package rubric_test

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/StevenACoffman/skillet/finding"
	"github.com/StevenACoffman/skillet/skill"
	"github.com/StevenACoffman/skillsaw/internal/rubric"
)

// markers is the three explicit markers a well-formed skill uses to satisfy
// dim4 (>=3 explicit). blacklist is a dim9 section with three concrete items.
const (
	markers   = "🔴 CHECKPOINT STOP"
	blacklist = "## 反例黑名单\n- do not A\n- do not B\n- do not C\n"
)

// mkSkill writes a hermetic SKILL.md (plus any extra files) into a temp dir and
// loads it, so tests exercise the real parser and on-disk resource checks.
func mkSkill(t *testing.T, name, desc, body string, files map[string]string) *skill.Skill {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	raw := "---\nname: " + name + "\ndescription: " + desc + "\n---\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(raw), 0o600); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	s, err := skill.Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return s
}

func dim(ev *rubric.Evaluation, num int) rubric.DimScore {
	for _, d := range ev.Dims {
		if d.Num == num {
			return d
		}
	}
	return rubric.DimScore{}
}

func hasFlag(flags []string, substr string) bool {
	for _, f := range flags {
		if strings.Contains(f, substr) {
			return true
		}
	}
	return false
}

func TestEvaluateDimensions(t *testing.T) {
	t.Parallel()
	longDesc := strings.Repeat("a", 1100)
	tests := []struct {
		name         string
		skillName    string
		desc         string
		body         string
		files        map[string]string
		dimNum       int
		wantFinal    int
		wantPenalty  int
		wantFlagPart string
	}{
		{
			name: "dim1 missing name", skillName: "", desc: "does x, use when y",
			dimNum: 1, wantPenalty: 3, wantFlagPart: "missing name",
		},
		{
			name: "dim1 non-kebab name", skillName: "Bad_Name", desc: "does x",
			dimNum: 1, wantPenalty: 2, wantFlagPart: "kebab",
		},
		{
			// A quoted scalar followed by unquoted text — the shape that appears in
			// real book frontmatter. Nothing can be read out of the block.
			name: "dim1 frontmatter did not parse", skillName: "ok-skill",
			desc:   `"Site Reliability Engineering" by Betsy Beyer`,
			dimNum: 1, wantPenalty: 6, wantFlagPart: "did not parse",
		},
		{
			name: "dim1 description over 1024", skillName: "ok-skill", desc: longDesc,
			dimNum: 1, wantPenalty: 2, wantFlagPart: "over 1024",
		},
		{
			name: "dim1 filler tail", skillName: "ok-skill", desc: "do stuff 灵活应用",
			dimNum: 1, wantPenalty: 1, wantFlagPart: "filler tail",
		},
		{
			// The fence is load-bearing: dim 3 docks a skill that runs something and never
			// says what to do when it fails. Without it this body executes nothing, there is
			// no runtime failure to encode, and the penalty correctly does not apply.
			name: "dim3 forward-only workflow penalised", skillName: "ok-skill", desc: "d",
			body:   "Step 1: do the thing\nStep 2: do the next thing\n\n```sh\nrun --it\n```\n",
			dimNum: 3, wantFinal: 7, wantPenalty: 3, wantFlagPart: "runs commands",
		},
		{
			name: "dim3 with fallback not penalised", skillName: "ok-skill", desc: "d",
			body:   "Step 1: do it\n如果失败 → 回退\nfallback: retry",
			dimNum: 3, wantFinal: 10, wantPenalty: 0, wantFlagPart: "failure branch",
		},
		{
			// No markers is not a deterministic defect — dim4 defers to a judge
			// (final = floor 10) so it stops being the universal diagnosis target.
			// Both dim-4 flags defer to a judge and dock nothing; they differ in what they
			// tell the judge. A skill with nothing to run has no step to pause between.
			name: "dim4 no markers, executes nothing", skillName: "ok-skill", desc: "d",
			body:   "no visual markers here",
			dimNum: 4, wantFinal: 10, wantFlagPart: "executes nothing to checkpoint",
		},
		{
			name: "dim4 no markers, runs commands", skillName: "ok-skill", desc: "d",
			body:   "no visual markers here\n\n```sh\nrun --it\n```\n",
			dimNum: 4, wantFinal: 10, wantFlagPart: "runs commands, so judge whether",
		},
		{
			name: "dim4 three markers scores high", skillName: "ok-skill", desc: "d",
			body:   markers,
			dimNum: 4, wantFinal: 9, wantFlagPart: "3 explicit",
		},
		{
			// Corpus fix: a lone ⚠️ warning is too few to derive a checkpoint score
			// and must not score worse than zero markers — it defers to judgment.
			name: "dim4 one marker defers to judge", skillName: "ok-skill", desc: "d",
			body:   "note: ⚠️ be careful with concurrency",
			dimNum: 4, wantFinal: 10, wantFlagPart: "too few to derive",
		},
		{
			name: "dim5 softening phrases penalised", skillName: "ok-skill", desc: "d",
			body:   "建议这样 可以考虑那样 视情况而定",
			dimNum: 5, wantFinal: 7, wantPenalty: 3, wantFlagPart: ">=3",
		},
		{
			name: "dim6 broken markdown link", skillName: "ok-skill", desc: "d",
			body:   "see [ref](references/missing.md) for details",
			dimNum: 6, wantFinal: 9, wantFlagPart: "broken link",
		},
		{
			name: "dim6 reachable backtick link", skillName: "ok-skill", desc: "d",
			body:   "see `references/present.md` for details",
			files:  map[string]string{"references/present.md": "hi"},
			dimNum: 6, wantFinal: 10, wantFlagPart: "reachable",
		},
		{
			// Corpus fix: intra-skill links into non-standard dirs (methodology/,
			// extractors/, agents/) are now checked — a broken one is caught.
			name: "dim6 broken methodology backtick link", skillName: "ok-skill", desc: "d",
			body:   "run `methodology/99-missing.md` next",
			files:  map[string]string{"methodology/00-overview.md": "x"},
			dimNum: 6, wantFinal: 9, wantFlagPart: "broken link",
		},
		{
			// Placeholder/example paths (first segment is neither conventional nor
			// an existing subdir) must NOT be treated as broken links.
			name: "dim6 ignores placeholder path", skillName: "ok-skill", desc: "d",
			body:   "put it at `path/to/output.md` or see https://example.com/x.md",
			dimNum: 6, wantFinal: 10, wantFlagPart: "0 resource ref",
		},
		{
			// Parent-relative links (cross-skill / example paths) are outside the
			// skill dir and must not be flagged as broken.
			name: "dim6 ignores parent-relative link", skillName: "ok-skill", desc: "d",
			body:   "avoid `[...](../other/SKILL.md)` links",
			dimNum: 6, wantFinal: 10, wantFlagPart: "0 resource ref",
		},
		{
			name: "dim7 ai-slop penalised per occurrence", skillName: "ok-skill", desc: "d",
			body:   "说白了 换句话说 综上",
			dimNum: 7, wantFinal: 7, wantPenalty: 3, wantFlagPart: "AI-slop",
		},
		{
			name: "dim9 no blacklist section", skillName: "ok-skill", desc: "d",
			body:   "only positive guidance here",
			dimNum: 9, wantFinal: 2, wantFlagPart: "no counter-example",
		},
		{
			name: "dim9 concrete blacklist section", skillName: "ok-skill", desc: "d",
			body:   blacklist,
			dimNum: 9, wantFinal: 9, wantFlagPart: "3 points",
		},
		{
			// Corpus fix: an English "Boundary" section (book2skill's RIA
			// convention) with bullets under a subheading is fully credited.
			name: "dim9 boundary with subheading bullets", skillName: "ok-skill", desc: "d",
			body:   "## B — Boundary\n### Do Not Use When\n- do not A\n- avoid B\n- never C\n",
			dimNum: 9, wantFinal: 9, wantFlagPart: "3 points",
		},
		{
			// Corpus fix: a "Quality Red Line" section (violations that stop
			// output) is a counter-example/blacklist section.
			name:         "dim9 quality red line section",
			skillName:    "ok-skill",
			desc:         "d",
			body:         "## Quality Red Line\n- never ship without tests\n- do not skip the gate\n- avoid hand-editing\n",
			dimNum:       9,
			wantFinal:    9,
			wantFlagPart: "points",
		},
		{
			// Corpus fix: "Required Flags" must NOT match the "red flag" signal
			// (substring bug: "requi[red flag]s"). With no real section, dim9 = 2.
			name:         "dim9 required-flags is not a blacklist heading",
			skillName:    "ok-skill",
			desc:         "d",
			body:         "## Required Flags\n- must set --name and --port\n- must set --config path\n",
			dimNum:       9,
			wantFinal:    2,
			wantFlagPart: "no counter-example",
		},
		{
			// Corpus fix: best-match — a thin early "Caution" note must not hide a
			// later rich "Common Mistakes" table.
			name: "dim9 richest section wins", skillName: "ok-skill", desc: "d",
			body: "## Caution\nshort\n## Common Mistakes\n| a | b |\n| --- | --- |\n" +
				"| c | d |\n| e | f |\n| g | h |\n",
			dimNum: 9, wantFinal: 9, wantFlagPart: "points",
		},
		{
			// Supersedes an earlier rule that a boundary section is itself failure-mode
			// encoding. It is a container: 154 of the 233 corpus skills have one with no
			// inline branch under it, so accepting the heading scored the absence as
			// present. What actually protects these skills is that they execute nothing.
			name:         "dim3 boundary section on a skill that executes nothing",
			skillName:    "ok-skill",
			desc:         "d",
			body:         "Step 1: act\nStep 2: act more\n## Boundary\n- when not to apply this\n",
			dimNum:       3,
			wantFinal:    10,
			wantPenalty:  0,
			wantFlagPart: "executes nothing",
		},
		{
			// The case the change exists for: the same section, on a skill that does run
			// something. The heading no longer buys immunity.
			name:      "dim3 boundary section does not excuse a skill that runs commands",
			skillName: "ok-skill",
			desc:      "d",
			body: "Step 1: act\n## Boundary\n- when not to apply this\n\n" +
				"```sh\nrun --it\n```\n",
			dimNum:       3,
			wantFinal:    7,
			wantPenalty:  3,
			wantFlagPart: "runs commands",
		},
		{
			// Corpus fix: a "## Common Failures" section is failure-mode encoding.
			name: "dim3 common failures section satisfies", skillName: "ok-skill", desc: "d",
			body:   "Step 1: act\nStep 2: act more\n## Common Failures\n- forgot to rebuild\n",
			dimNum: 3, wantFinal: 10, wantPenalty: 0, wantFlagPart: "failure-handling section",
		},
		{
			// Corpus fix: inline "when ... blocked/fails" is failure handling, not
			// just "if ... fail" (executing-plans' "Stop when blocked").
			name: "dim3 inline when-blocked counts", skillName: "ok-skill", desc: "d",
			body:   "Step 1: act\nStep 2: stop when blocked, don't guess",
			dimNum: 3, wantFinal: 10, wantPenalty: 0, wantFlagPart: "failure branch",
		},
		{
			// Corpus fix: English softening phrases fire (dims were Chinese-only).
			name: "dim5 english softening penalised", skillName: "ok-skill", desc: "d",
			body:   "do it as appropriate; it depends; you might want to",
			dimNum: 5, wantFinal: 7, wantPenalty: 3, wantFlagPart: ">=3",
		},
		{
			// Corpus fix: English AI-slop fires (dims were Chinese-only).
			name: "dim7 english slop penalised", skillName: "ok-skill", desc: "d",
			body:   "in other words, that said, in essence this works",
			dimNum: 7, wantFinal: 7, wantPenalty: 3, wantFlagPart: "AI-slop",
		},
		{
			// Inflection regression: "boundary" must match the "Boundaries" plural
			// (y->ies rewrites the stem, so a prefix match alone misses it). This is
			// the matryer/test-helper "B — Boundaries and Blind Spots" convention,
			// under-scored across 12 corpus skills before the iesPlural probe.
			name:      "dim9 boundaries plural matches boundary signal",
			skillName: "ok-skill",
			desc:      "d",
			body: "## B — Boundaries and Blind Spots\n- fits single-service servers\n" +
				"- be cautious with very large dependency lists\n" +
				"- shared mutable state still needs synchronisation\n",
			dimNum:       9,
			wantFinal:    9,
			wantFlagPart: "points",
		},
		{
			// Vocabulary regression: a "## Known Limitations" section is a boundary
			// section ("limitation" added to BlacklistHeadings).
			name: "dim9 known limitations section", skillName: "ok-skill", desc: "d",
			body: "## Known Limitations\n- does not support workspace mode\n" +
				"- no Windows support yet\n- requires Go 1.26 or newer\n",
			dimNum: 9, wantFinal: 9, wantFlagPart: "points",
		},
		{
			// Fence-bug regression: a "## Common Mistakes" line inside a fenced code
			// block is NOT a heading, so it must not create a phantom dim9 section.
			// The old line-regex parser mistook it for a real counter-example section.
			name:      "dim9 heading inside code fence is not a section",
			skillName: "ok-skill",
			desc:      "d",
			body: "## Usage\n\n```bash\n## Common Mistakes\necho avoid this pattern\n```\n\n" +
				"This skill applies the transformation described above to input files.\n",
			dimNum:       9,
			wantFinal:    2,
			wantFlagPart: "no counter-example",
		},
	}
	cfg := rubric.DefaultConfig()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := mkSkill(t, tt.skillName, tt.desc, tt.body, tt.files)
			ev := rubric.Evaluate(s, cfg)
			d := dim(ev, tt.dimNum)
			if tt.wantFinal != 0 && d.Final != tt.wantFinal {
				t.Errorf(
					"dim%d Final = %d, want %d (flags: %v)",
					tt.dimNum,
					d.Final,
					tt.wantFinal,
					d.Flags,
				)
			}
			if d.Penalty != tt.wantPenalty {
				t.Errorf(
					"dim%d Penalty = %d, want %d (flags: %v)",
					tt.dimNum,
					d.Penalty,
					tt.wantPenalty,
					d.Flags,
				)
			}
			if tt.wantFlagPart != "" && !hasFlag(d.Flags, tt.wantFlagPart) {
				t.Errorf(
					"dim%d missing flag containing %q; got %v",
					tt.dimNum,
					tt.wantFlagPart,
					d.Flags,
				)
			}
		})
	}
}

// TestWeightsSumTo100 guards the D1 reconciliation (dim6 = 5) so a max-scoring
// skill totals exactly 100 (spec §8.3).
func TestWeightsSumTo100(t *testing.T) {
	t.Parallel()
	if got := len(rubric.Dimensions()); got != 9 {
		t.Fatalf("expected 9 dimensions, got %d", got)
	}
	sum := 0
	for _, d := range rubric.Dimensions() {
		sum += d.Weight
	}
	if sum != 100 {
		t.Errorf("dimension weights sum to %d, want 100", sum)
	}
}

// TestDeterministicScoreBoundsAndMonotonicity: scores stay in [10,100] and a
// skill with detectable defects scores strictly below a clean one.
func TestDeterministicScoreBoundsAndMonotonicity(t *testing.T) {
	t.Parallel()
	cfg := rubric.DefaultConfig()

	clean := mkSkill(t, "clean-skill", "does x, use when y",
		"Step 1: act\n如果失败 → fallback: retry\n"+markers+"\n"+blacklist, nil)
	cleanEv := rubric.Evaluate(clean, cfg)

	defective := mkSkill(t, "Bad_Name", "do stuff 灵活应用",
		"Step 1: act with no fallback\n说白了 换句话说 综上\n建议 可以考虑 视情况而定", nil)
	defectiveEv := rubric.Evaluate(defective, cfg)

	for _, ev := range []*rubric.Evaluation{cleanEv, defectiveEv} {
		if ev.DeterministicScore < 10 || ev.DeterministicScore > 100 {
			t.Errorf("%s score %.1f out of [10,100]", ev.Skill, ev.DeterministicScore)
		}
	}
	if defectiveEv.DeterministicScore >= cleanEv.DeterministicScore {
		t.Errorf("defective %.1f should be < clean %.1f",
			defectiveEv.DeterministicScore, cleanEv.DeterministicScore)
	}
}

func TestDiagnose(t *testing.T) {
	t.Parallel()
	cfg := rubric.DefaultConfig()
	tests := []struct {
		name          string
		skillName     string
		body          string
		wantTargetNum int // 0 => runtime P0 (no dimension)
		wantPriority  string
		wantCluster   bool
	}{
		{
			name: "runtime hit forces P0", skillName: "ok-skill",
			body:          "本 skill 在 Claude Code 里使用",
			wantTargetNum: 0, wantPriority: "P0",
		},
		{
			name: "cluster dim3 lowest", skillName: "ok-skill",
			// Forward-only workflow with markers + a "## Blacklist" section that
			// satisfies dim9 but is NOT a failure-handling heading, so dim3 stays min.
			// The fence is what makes dim 3's penalty applicable at all: without a skill
			// that runs something there is no failure to encode and nothing to diagnose.
			body: "Step 1: act\nStep 2: act more\n" + markers +
				"\n## Blacklist\n- do not A\n- do not B\n- do not C\n" +
				"\n```sh\nrun --it\n```\n",
			wantTargetNum: 3, wantPriority: "P2", wantCluster: true,
		},
		{
			// A structurally healthy skill (markers + a substantial boundary
			// section, no penalties) has no deterministic weakness to flag.
			name: "healthy skill defers to judge", skillName: "ok-skill",
			body:          markers + "\n## Boundary\n- do not A\n- avoid B\n- never C\n- also D\n",
			wantTargetNum: 0, wantPriority: "judge",
		},
		{
			name:      "non-cluster dim7 lowest",
			skillName: "ok-skill",
			// dim7 slop penalised; dim3 has fallback, dim4/dim9 satisfied.
			body:          "Step 1: act\nfallback: retry\n说白了 换句话说 综上\n" + markers + "\n" + blacklist,
			wantTargetNum: 7,
			wantPriority:  "P3",
			wantCluster:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := mkSkill(t, tt.skillName, "does x, use when y", tt.body, nil)
			d := rubric.Diagnose(rubric.Evaluate(s, cfg))
			if d.TargetNum != tt.wantTargetNum {
				t.Errorf("TargetNum = %d (%s), want %d", d.TargetNum, d.Target, tt.wantTargetNum)
			}
			if d.Priority != tt.wantPriority {
				t.Errorf("Priority = %q, want %q", d.Priority, tt.wantPriority)
			}
			if (d.ClusterNote != "") != tt.wantCluster {
				t.Errorf(
					"ClusterNote present = %v, want %v (note: %q)",
					d.ClusterNote != "",
					tt.wantCluster,
					d.ClusterNote,
				)
			}
		})
	}
}

func TestEvaluateWithBases(t *testing.T) {
	t.Parallel()
	cfg := rubric.DefaultConfig()
	// A clean skill: judge dims carry no deterministic penalty, derived dims
	// are dim4=9 (3 markers), dim6=10 (no refs), dim9=9 (3 blacklist items).
	s := mkSkill(t, "clean-skill", "does x, use when y",
		"Step 1: act\n如果失败 → fallback: retry\n"+markers+"\n"+blacklist, nil)

	// All six needs-judge dims supplied -> full score computable.
	// Σ(final×weight)/10 = (9·7+8·12+7·12+9·6+8·17+10·5+9·12+7·23+9·6)/10 = 80.6
	full := map[int]int{1: 9, 2: 8, 3: 7, 5: 8, 7: 9, 8: 7}
	ev := rubric.EvaluateWithBases(s, cfg, full)
	if !ev.HasFullScore {
		t.Fatal("expected HasFullScore with all judge dims supplied")
	}
	if math.Abs(ev.FullScore-80.6) > 1e-9 {
		t.Errorf("FullScore = %.4f, want 80.6", ev.FullScore)
	}

	// Missing one judge dim (8) -> no full score.
	partial := map[int]int{1: 9, 2: 8, 3: 7, 5: 8, 7: 9}
	if rubric.EvaluateWithBases(s, cfg, partial).HasFullScore {
		t.Error("expected no full score when a judge dim base is missing")
	}
}

// TestFrontmatterParseFailureReplacesTheSymptoms pins the reason for the guard: an
// unparsed block must be reported as itself, not as the two missing fields it causes.
// skillsaw's own preflight already names the YAML position; eval disagreeing with it on
// the same file is the defect this closes.
func TestFrontmatterParseFailureReplacesTheSymptoms(t *testing.T) {
	t.Parallel()
	cfg := rubric.DefaultConfig()
	s := mkSkill(t, "ok-skill", `"Site Reliability Engineering" by Betsy Beyer`, "# Body\n", nil)
	d := dim(rubric.Evaluate(s, cfg), 1)

	if !hasFlag(d.Flags, "did not parse") {
		t.Fatalf("expected the parse failure to be named, got %v", d.Flags)
	}
	for _, symptom := range []string{"missing name", "missing description"} {
		if hasFlag(d.Flags, symptom) {
			t.Errorf("must not report %q as a separate defect; got %v", symptom, d.Flags)
		}
	}
	// The penalty equals what the two symptom flags summed to, so the diagnosis
	// changed and no score moved — results.tsv compares totals across runs.
	if d.Penalty != 6 {
		t.Errorf("Penalty = %d, want 6 (unchanged from the flags it replaces)", d.Penalty)
	}
}

// TestEmptyFrontmatterFieldsStillReported is the over-suppression guard. A skill with an
// empty name and description parses fine (yaml.Unmarshal of an empty value succeeds), so
// an author who really did omit them must still be told. Keying the guard on the wrong
// condition would trade a false message for a missing one.
func TestEmptyFrontmatterFieldsStillReported(t *testing.T) {
	t.Parallel()
	s := mkSkill(t, "", "", "# Body\n", nil)
	if s.FrontmatterErr != nil {
		t.Fatalf("empty fields must still parse; got %v", s.FrontmatterErr)
	}
	d := dim(rubric.Evaluate(s, rubric.DefaultConfig()), 1)
	for _, want := range []string{"missing name", "missing description"} {
		if !hasFlag(d.Flags, want) {
			t.Errorf("expected %q to still be reported; got %v", want, d.Flags)
		}
	}
}

// TestJudgeBaseSupersedesPenalty pins that a judge-supplied base is not also charged the
// dimension's deterministic penalty. The judge read the same skill and the same flags, so a
// base already prices the defect the penalty describes; subtracting both bills it twice.
func TestJudgeBaseSupersedesPenalty(t *testing.T) {
	t.Parallel()
	cfg := rubric.DefaultConfig()
	// Runs a command and encodes no failure branch, so dim 3 carries its penalty of 3.
	s := mkSkill(t, "docked-skill", "does x, use when y",
		"Step 1: act\n\n```sh\nrun --it\n```\n"+markers+"\n"+blacklist, nil)

	ev := rubric.EvaluateWithBases(s, cfg, map[int]int{1: 9, 2: 8, 3: 7, 5: 8, 7: 9, 8: 7})
	if !ev.HasFullScore {
		t.Fatal("expected HasFullScore with all judge dims supplied")
	}
	// The penalty must still be reported -- suppressing it from the full total is not the
	// same as pretending it was not found, and the deterministic floor still uses it.
	var dim3 rubric.DimScore
	for _, d := range ev.Dims {
		if d.Num == 3 {
			dim3 = d
		}
	}
	if dim3.Penalty != 3 {
		t.Fatalf("dim3 Penalty = %d, want 3 (the fixture must actually be docked)", dim3.Penalty)
	}
	// Same weights as TestEvaluateWithBases: dim 3 contributes its base of 7, not 7-3.
	// (9·7+8·12+7·12+9·6+8·17+10·5+9·12+7·23+9·6)/10 = 80.6
	if math.Abs(ev.FullScore-80.6) > 1e-9 {
		t.Errorf("FullScore = %.4f, want 80.6 (base 7 used as-is, not 7-3)", ev.FullScore)
	}
}

// TestDim8ReportsScorabilityWithoutDocking pins the flag-don't-dock contract for dim 8.
// The absence lives in the test prompts rather than in the skill, so docking would price
// someone else's unfinished work — and the deterministic score must stay a statement about
// the skill.
func TestDim8ReportsScorabilityWithoutDocking(t *testing.T) {
	t.Parallel()
	cfg := rubric.DefaultConfig()
	cases := map[string]struct {
		prompts  string
		wantFlag string
	}{
		"no file at all": {"", "no readable test-prompts.json"},
		"cases carry no checks": {
			`{"tests":[{"id":1,"type":"should_trigger","prompt":"p","expected":"invoke"}]}`,
			"none of 1 behavioral case(s) specify checks",
		},
		"every case carries checks": {
			`{"tests":[{"id":1,"type":"should_trigger","prompt":"p","expected":"e",` +
				`"checks":[{"op":"contains","arg":"x"}]}]}`,
			"all 1 behavioral case(s) specify checks",
		},
		// A decoy has no good output to score, so it must not count against scorability.
		"a decoy alongside a checked case is not counted": {
			`{"tests":[{"id":1,"type":"should_trigger","prompt":"p","expected":"e",` +
				`"checks":[{"op":"contains","arg":"x"}]},` +
				`{"id":2,"type":"should_not_trigger","prompt":"q","expected":"skip"}]}`,
			"all 1 behavioral case(s) specify checks",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dim8 := scoreDim8(t, cfg, tc.prompts)
			if dim8.Penalty != 0 {
				t.Errorf("dim 8 Penalty = %d, want 0 — it reports, it does not dock", dim8.Penalty)
			}
			if !strings.Contains(strings.Join(dim8.Flags, " | "), tc.wantFlag) {
				t.Errorf("flags = %v, want one containing %q", dim8.Flags, tc.wantFlag)
			}
		})
	}
}

// scoreDim8 evaluates a minimal skill carrying prompts as its test-prompts.json (omitted
// entirely when prompts is empty) and returns its dim-8 score.
func scoreDim8(t *testing.T, cfg *rubric.Config, prompts string) rubric.DimScore {
	t.Helper()
	files := map[string]string{}
	if prompts != "" {
		files["test-prompts.json"] = prompts
	}
	s := mkSkill(t, "ok-skill", "does x, use when y", "Step 1: act\n", files)
	for _, d := range rubric.Evaluate(s, cfg).Dims {
		if d.Num == 8 {
			return d
		}
	}
	t.Fatal("dim 8 missing from the evaluation")
	return rubric.DimScore{}
}

// TestDiagnosisSaysWhoActs pins the axis the loop was deciding implicitly: it picks one edit
// per round, so "is this safe to apply unattended" has to be stated rather than assumed.
func TestDiagnosisSaysWhoActs(t *testing.T) {
	t.Parallel()
	cfg := rubric.DefaultConfig()
	// Dim 3 is the only dimension producing a deterministic penalty, but it is not
	// automatically the lowest: the derived dims cap at 9, so dim 9 wins unless the skill
	// carries a blacklist section. With one, dim 9 scores 9 and dim 3's penalty puts it at 7.
	s := mkSkill(t, "ok-skill", "does x, use when y",
		"Step 1: do the thing\n\n```sh\nrun --it\n```\n"+blacklist, nil)
	d := rubric.Diagnose(rubric.Evaluate(s, cfg))
	if d.TargetNum != 3 {
		t.Fatalf("fixture diagnosed dim %d, not 3; it cannot pin the classification", d.TargetNum)
	}
	if d.Action != finding.ActionHuman {
		t.Errorf("dim 3 Action = %q, want human — a failure mechanism needs domain knowledge",
			d.Action)
	}
	// Orthogonal to Priority: neither may be derived from the other.
	if d.Priority == "" {
		t.Error("Priority went missing")
	}
}

// TestDiagnosisLeavesActionUnsetWithNoTarget pins the zero value's meaning. When there is
// nothing to fix, naming an actor would be a false instruction — the same reason finding has
// no ActionUnknown constant.
func TestDiagnosisLeavesActionUnsetWithNoTarget(t *testing.T) {
	t.Parallel()
	cfg := rubric.DefaultConfig()
	// Structurally healthy: no deterministic weakness, so the diagnosis routes to the judge.
	s := mkSkill(t, "ok-skill", "does x, use when y",
		"Step 1: act\n如果失败 → fallback: retry\n"+markers+"\n"+blacklist, nil)
	d := rubric.Diagnose(rubric.Evaluate(s, cfg))
	if d.TargetNum != 0 {
		t.Fatalf("fixture found a weakness (dim %d); it cannot pin the no-target case",
			d.TargetNum)
	}
	if d.Action != "" {
		t.Errorf("Action = %q with no target; it must stay unset", d.Action)
	}
}

// TestDimensionResponseIsTotal guards the classification against rotting rather than
// restating the table, which would test nothing. What can go wrong is a tenth dimension
// arriving unclassified, and the zero value exists so that reads as unclassified rather
// than as neutral.
func TestDimensionResponseIsTotal(t *testing.T) {
	t.Parallel()
	dims := rubric.Dimensions()
	if len(dims) == 0 {
		t.Fatal("no dimensions")
	}
	seen := map[rubric.Response]int{}
	for _, d := range dims {
		if !d.Response.Valid() {
			t.Errorf("dim %d (%s) has response %q, which is not a classified direction",
				d.Num, d.Key, d.Response)
		}
		seen[d.Response]++
	}
	if seen[rubric.ResponseNeutral] == len(dims) {
		t.Error(
			"every dimension is neutral; the classification has rotted and distinguishes nothing",
		)
	}
}

// TestDimensionResponseMatchesBehaviour binds the table to what the scorer actually does:
// feed a dimension the thing it counts and the score must move the way the table claims.
// The first version of this table guessed dim 4 was additive; this test refuted it, which
// is the reason the field is measured rather than declared.
func TestDimensionResponseMatchesBehaviour(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		bare, fed string
		dim       int
		want      rubric.Response
	}{
		"dim 3 gains a failure branch": {
			bare: "# S\n\n```sh\nrun\n```\n",
			fed:  "# S\n\n```sh\nrun\n```\n\nIf the run fails, retry once.\n",
			dim:  3, want: rubric.ResponseAdditive,
		},
		"dim 4 gains checkpoint markers": {
			bare: "# S\n\n```sh\nrun\n```\n",
			fed:  "# S\n\n```sh\nrun\n```\n\n\u26a0\ufe0f a\n\n\u26a0\ufe0f b\n\n\u26a0\ufe0f c\n",
			dim:  4, want: rubric.ResponseSubtractive,
		},
		"dim 9 gains a counter-example section": {
			bare: "# S\n\ndo it.\n",
			fed:  "# S\n\ndo it.\n\n## Common Mistakes\n\n- a\n- b\n- c\n",
			dim:  9, want: rubric.ResponseAdditive,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			before, after := scoreDim(t, tc.bare, tc.dim), scoreDim(t, tc.fed, tc.dim)
			got := rubric.ResponseNeutral
			switch {
			case after > before:
				got = rubric.ResponseAdditive
			case after < before:
				got = rubric.ResponseSubtractive
			}
			if got != tc.want {
				t.Errorf("dim %d scored %d then %d, so it is %q; the table says %q",
					tc.dim, before, after, got, tc.want)
			}
			if declared := responseOf(t, tc.dim); declared != tc.want {
				t.Errorf("dim %d is declared %q; this test measured %q", tc.dim, declared, tc.want)
			}
		})
	}
}

// responseOf returns a dimension's declared response.
func responseOf(t *testing.T, num int) rubric.Response {
	t.Helper()
	for _, d := range rubric.Dimensions() {
		if d.Num == num {
			return d.Response
		}
	}
	t.Fatalf("dimension %d not in the table", num)
	return rubric.ResponseUnclassified
}

// scoreDim evaluates a body and returns one dimension's final score.
func scoreDim(t *testing.T, body string, num int) int {
	t.Helper()
	ev := rubric.Evaluate(&skill.Skill{Body: body, Dir: t.TempDir()}, rubric.DefaultConfig())
	for i := range ev.Dims {
		if ev.Dims[i].Num == num {
			return ev.Dims[i].Final
		}
	}
	t.Fatalf("dimension %d not scored", num)
	return 0
}

// TestEveryDimensionIsScoredEveryTime pins the property L956 asked after: no dimension is
// skipped and none is carried over. It is true today because Evaluate is pure and iterates
// the whole table, but "true today" is not the same as "guarded" -- a skip path added for
// speed would leave the earlier iteration's score standing, and a hill-climbing loop reads
// the weakest dimension, so a stale one would keep sending it at a defect it already fixed.
func TestEveryDimensionIsScoredEveryTime(t *testing.T) {
	t.Parallel()
	cfg := rubric.DefaultConfig()
	desc := "Use when the demo thing is needed."
	weak := mkSkill(t, "alpha", desc, "## Workflow\n\nrun the thing\n", nil)

	ev := rubric.Evaluate(weak, cfg)
	if len(ev.Dims) != len(rubric.Dimensions()) {
		t.Fatalf("scored %d dimensions, the table has %d", len(ev.Dims), len(rubric.Dimensions()))
	}
	seen := map[int]bool{}
	for _, ds := range ev.Dims {
		if seen[ds.Num] {
			t.Errorf("dimension %d scored twice", ds.Num)
		}
		seen[ds.Num] = true
	}
	for _, d := range rubric.Dimensions() {
		if !seen[d.Num] {
			t.Errorf("dimension %d (%s) was not scored", d.Num, d.Key)
		}
	}

	// Re-scoring an edited skill must move the dimension the edit touched. If any score
	// were reused across evaluations this is where it would show.
	strong := mkSkill(t, "alpha", desc, "## Workflow\n\nrun the thing\n\n"+blacklist, nil)
	after := rubric.Evaluate(strong, cfg)
	if before, now := finalOf(ev, 9), finalOf(after, 9); now <= before {
		t.Errorf("dim 9 scored %d before the blacklist and %d after; the edit was not re-scored",
			before, now)
	}
}

// finalOf returns the final score recorded for one dimension of an evaluation, or -1 when
// the evaluation has no such dimension -- which the caller reports rather than skips.
func finalOf(ev *rubric.Evaluation, num int) int {
	for i := range ev.Dims {
		if ev.Dims[i].Num == num {
			return ev.Dims[i].Final
		}
	}
	return -1
}

// TestEachCheckWritesOnlyItsOwnDimension guards the one place in this package where a
// one-character edit silently moves a score: the `switch num` in applyChecks. A check is
// handed the DimScore of whichever dimension dispatched it, so a penalty can never be
// orphaned -- but an arm under the wrong number puts one dimension's finding on another's
// score, and every other test here would still pass, because they assert on the dimension
// they happen to look at.
//
// The map is the declaration: a dimension is deterministic or it is judge-only, and the
// code has to agree. Dim 2 is judge-only by design ("judged outright" in the Dimensions
// comment); adding a check for it without adding a fixture fails here rather than passing
// unnoticed.
func TestEachCheckWritesOnlyItsOwnDimension(t *testing.T) {
	t.Parallel()
	// Each fixture is built to trip exactly the dimension it is filed under. The body is
	// the trigger; frontmatter stays well-formed unless dim 1 is the subject.
	fixtures := map[int]struct{ name, desc, body string }{
		1: {"", "does x, use when y", "b"},
		3: {"ok-skill", "d", "Step 1: do it\n\n```sh\nrun --it\n```\n"},
		4: {"ok-skill", "d", markers + "\n" + markers + "\n" + markers + "\n"},
		5: {"ok-skill", "d", "建议这样 可以考虑那样 视情况而定"},
		6: {"ok-skill", "d", "see [ref](references/missing.md) for details"},
		7: {"ok-skill", "d", "说白了 换句话说 综上"},
		8: {"ok-skill", "d", "b"},
		9: {"ok-skill", "d", "only positive guidance here"},
	}
	// Judge-only dimensions have no deterministic check and must never carry a finding,
	// on any input. Keeping this as a derived set means the two cannot disagree.
	judgeOnly := map[int]bool{}
	for _, d := range rubric.Dimensions() {
		if _, checked := fixtures[d.Num]; !checked {
			judgeOnly[d.Num] = true
		}
	}
	if len(judgeOnly) != 1 || !judgeOnly[2] {
		t.Fatalf("judge-only set is %v, want exactly {2}; a dimension gained or lost a check",
			judgeOnly)
	}

	for num, f := range fixtures {
		t.Run(strconv.Itoa(num), func(t *testing.T) {
			t.Parallel()
			ev := rubric.Evaluate(
				mkSkill(t, f.name, f.desc, f.body, nil), rubric.DefaultConfig())
			assertFindingsLandOn(t, ev, num, judgeOnly)
		})
	}
}

// assertFindingsLandOn checks that the dimension a fixture targets recorded something and
// that no judge-only dimension did. Both halves are needed: the first catches a check that
// stopped being dispatched, the second catches one dispatched under the wrong number.
func assertFindingsLandOn(t *testing.T, ev *rubric.Evaluation, target int, judgeOnly map[int]bool) {
	t.Helper()
	for i := range ev.Dims {
		ds := &ev.Dims[i]
		switch {
		case ds.Num == target && len(ds.Flags) == 0:
			t.Errorf("dim %d records nothing for a fixture built to trip it; "+
				"its check is not dispatched", target)
		case judgeOnly[ds.Num] && len(ds.Flags) > 0:
			t.Errorf("dim %d is judge-only but recorded %v; a check is dispatched "+
				"under the wrong number", ds.Num, ds.Flags)
		}
	}
}
