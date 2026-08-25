package rubric_test

import (
	"strings"
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/rubric"
)

// TestRulesAreSound is the soundness half of the eval-suite question: not "does the rubric
// catch defects" -- the mutation table asks that -- but "does it fire on ordinary work".
// Trust is more sensitive to false alarms than to misses: a rule that flags normal prose
// gets the whole tool switched off, where a rule that misses something gets it fixed.
func TestRulesAreSound(t *testing.T) {
	t.Parallel()
	if err := rubric.DefaultConfig().SelfTest(); err != nil {
		t.Errorf("%v", err)
	}
}

// TestSelfTestSeesAnUnsoundTerm is the negative control, in the test rather than by hand:
// a self-test never seen to fail proves nothing, and this one guards every rule in the
// rubric. The added term matches ordinary prose in the quiet corpus.
func TestSelfTestSeesAnUnsoundTerm(t *testing.T) {
	t.Parallel()
	cfg := rubric.DefaultConfig()
	cfg.Slop = append(cfg.Slop, "retry budget")
	err := cfg.SelfTest()
	if err == nil {
		t.Fatal("SelfTest passed a term that fires on ordinary prose")
	}
	for _, want := range []string{"Slop", "retry budget", "unsound"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not name %q: %v", want, err)
		}
	}
}

// TestAToleratedFalseAlarmMustStillFire pins the other direction. Four terms are recorded
// as firing on ordinary prose and tolerated with a reason; if one stops firing, the
// exception has outlived the problem and must be removed, or it sits there hiding the next
// regression in the same rule.
func TestAToleratedFalseAlarmMustStillFire(t *testing.T) {
	t.Parallel()
	cfg := rubric.DefaultConfig()
	cases := map[string]struct {
		hits func(string) []rubric.Hit
		text string
		term string
	}{
		"首先 in step ordering":  {cfg.SlopHits, "首先检查配置文件是否存在。其次运行迁移脚本。", "首先"},
		"其次 in step ordering":  {cfg.SlopHits, "首先检查配置文件是否存在。其次运行迁移脚本。", "其次"},
		"STOP as a verb":       {cfg.MarkerHits, "Do not STOP the deployment midway", "STOP"},
		"CHECKPOINT as a noun": {cfg.MarkerHits, "The CHECKPOINT table stores state", "CHECKPOINT"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, h := range tc.hits(tc.text) {
				if h.Term == tc.term {
					return
				}
			}
			t.Errorf("%q no longer fires on %q; the tolerated exception in quiet.go has "+
				"outlived the defect and must be removed", tc.term, tc.text)
		})
	}
}

// TestFillerTailIsAnchored keeps the dim-1 rule from becoming a substring search. The
// defect is a description trailing off into vagueness, not the words appearing at all.
func TestFillerTailIsAnchored(t *testing.T) {
	t.Parallel()
	cfg := rubric.DefaultConfig()
	if _, ok := cfg.HasFillerTail("Run it as needed by the operator's schedule."); ok {
		t.Error("fired mid-sentence; the rule is anchored at the end for a reason")
	}
	if tail, ok := cfg.HasFillerTail("Do the thing as needed."); !ok || tail != "as needed" {
		t.Errorf("HasFillerTail = %q, %v; want the trailing phrase", tail, ok)
	}
}

// TestTheChecksUseTheMatchersTheSelfTestChecks is the property that makes any of this
// meaningful. If scoring matched by its own rules, a sound self-test would say nothing
// about the rule as applied -- so a term the matcher reports must move the score.
func TestTheChecksUseTheMatchersTheSelfTestChecks(t *testing.T) {
	t.Parallel()
	cfg := rubric.DefaultConfig()
	body := "## Workflow\n\n说白了 this is the step.\n"
	if len(cfg.SlopHits(body)) == 0 {
		t.Fatal("the matcher does not see the planted term; the fixture is wrong")
	}
	ev := rubric.Evaluate(mkSkill(t, "ok-skill", "d", body, nil), cfg)
	if finalOf(ev, 7) >= 10 {
		t.Errorf("dim 7 scored %d; a term the matcher reports did not reach the score",
			finalOf(ev, 7))
	}
}
