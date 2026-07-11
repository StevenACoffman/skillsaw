package rubric_test

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/rubric"
	"github.com/StevenACoffman/skillsaw/internal/skill"
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
			name: "dim1 description over 1024", skillName: "ok-skill", desc: longDesc,
			dimNum: 1, wantPenalty: 2, wantFlagPart: "over 1024",
		},
		{
			name: "dim1 filler tail", skillName: "ok-skill", desc: "do stuff 灵活应用",
			dimNum: 1, wantPenalty: 1, wantFlagPart: "filler tail",
		},
		{
			name: "dim3 forward-only workflow penalised", skillName: "ok-skill", desc: "d",
			body:   "Step 1: do the thing\nStep 2: do the next thing",
			dimNum: 3, wantFinal: 7, wantPenalty: 3, wantFlagPart: "forward-only",
		},
		{
			name: "dim3 with fallback not penalised", skillName: "ok-skill", desc: "d",
			body:   "Step 1: do it\n如果失败 → 回退\nfallback: retry",
			dimNum: 3, wantFinal: 10, wantPenalty: 0, wantFlagPart: "failure branch",
		},
		{
			name: "dim4 no markers scores low", skillName: "ok-skill", desc: "d",
			body:   "no visual markers here",
			dimNum: 4, wantFinal: 2, wantFlagPart: "0 explicit",
		},
		{
			name: "dim4 three markers scores high", skillName: "ok-skill", desc: "d",
			body:   markers,
			dimNum: 4, wantFinal: 9, wantFlagPart: "3 explicit",
		},
		{
			name: "dim5 softening phrases penalised", skillName: "ok-skill", desc: "d",
			body:   "建议这样 可以考虑那样 视情况而定",
			dimNum: 5, wantFinal: 7, wantPenalty: 3, wantFlagPart: ">=3",
		},
		{
			name: "dim6 broken resource link", skillName: "ok-skill", desc: "d",
			body:   "see [ref](references/missing.md) for details",
			dimNum: 6, wantFinal: 9, wantFlagPart: "broken link",
		},
		{
			name: "dim6 reachable resource link", skillName: "ok-skill", desc: "d",
			body:   "see references/present.md for details",
			files:  map[string]string{"references/present.md": "hi"},
			dimNum: 6, wantFinal: 10, wantFlagPart: "reachable",
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
			dimNum: 9, wantFinal: 9, wantFlagPart: "concrete items",
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
			// dim3 penalised (forward-only); dim4/dim9 satisfied so dim3 is the min.
			body:          "Step 1: act\nStep 2: act more\n" + markers + "\n" + blacklist,
			wantTargetNum: 3, wantPriority: "P2", wantCluster: true,
		},
		{
			name: "non-cluster dim7 lowest", skillName: "ok-skill",
			// dim7 slop penalised; dim3 has fallback, dim4/dim9 satisfied.
			body:          "Step 1: act\nfallback: retry\n说白了 换句话说 综上\n" + markers + "\n" + blacklist,
			wantTargetNum: 7, wantPriority: "P3", wantCluster: false,
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

func TestParseScores(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      string
		wantErr bool
		want    map[int]int
	}{
		{name: "valid subset", in: `{"2":8,"3":7}`, want: map[int]int{2: 8, 3: 7}},
		{name: "empty object", in: `{}`, want: map[int]int{}},
		{name: "unknown high key", in: `{"10":5}`, wantErr: true},
		{name: "zero key", in: `{"0":5}`, wantErr: true},
		{name: "non-numeric key", in: `{"foo":5}`, wantErr: true},
		{name: "value too high", in: `{"2":11}`, wantErr: true},
		{name: "value too low", in: `{"2":0}`, wantErr: true},
		{name: "array not object", in: `[1,2,3]`, wantErr: true},
		{name: "garbage", in: `not json`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := rubric.ParseScores([]byte(tt.in))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseScores(%s) = %v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseScores(%s) unexpected error: %v", tt.in, err)
			}
			assertScores(t, got, tt.want)
		})
	}
}

func assertScores(t *testing.T, got, want map[int]int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("key %d = %d, want %d", k, got[k], v)
		}
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
