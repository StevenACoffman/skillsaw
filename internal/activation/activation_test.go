package activation_test

import (
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/activation"
)

func TestScorePerfectSeparation(t *testing.T) {
	t.Parallel()
	desc := "Invoke when the user needs a Planguage specification for a performance requirement."
	triggers := []string{
		"Help me write a Planguage specification for latency",
		"Turn this performance requirement into Planguage",
	}
	decoys := []string{
		"What is the capital of France",
		"Reboot the printer in the office",
	}
	r := activation.Score(desc, triggers, decoys)
	if r.TP != 2 || r.FN != 0 {
		t.Errorf("targets: TP=%d FN=%d, want TP=2 FN=0 (why: %v)", r.TP, r.FN, r.Why)
	}
	if r.FP != 0 || r.TN != 2 {
		t.Errorf("distractors: FP=%d TN=%d, want FP=0 TN=2 (why: %v)", r.FP, r.TN, r.Why)
	}
	if r.TPR != 1.0 || r.FPR != 0.0 {
		t.Errorf("TPR=%v FPR=%v, want TPR=1 FPR=0", r.TPR, r.FPR)
	}
	// net_utility = (TP-FP)/total = (2-0)/4 = 0.5; a balanced set caps at 0.5
	// even under perfect separation (faithful to scoreDistractor).
	if r.NetUtility != 0.5 {
		t.Errorf("NetUtility = %v, want 0.5", r.NetUtility)
	}
	// Wilson interval on a perfect 2/2 TPR is wide but must bracket-ish be valid.
	if r.TPRInterval[0] < 0 || r.TPRInterval[1] > 1 || r.TPRInterval[0] > r.TPRInterval[1] {
		t.Errorf("invalid TPR interval %v", r.TPRInterval)
	}
}

func TestScoreDetectsDecoyFalsePositive(t *testing.T) {
	t.Parallel()
	// A decoy that shares the salient term "performance" fires — a false positive
	// that raises FPR and lowers net_utility.
	desc := "Invoke for performance tuning questions."
	triggers := []string{"optimize performance of this loop"}
	decoys := []string{"write a performance review for my report"}
	r := activation.Score(desc, triggers, decoys)
	if r.FP != 1 {
		t.Errorf("FP = %d, want 1 (decoy shares 'performance')", r.FP)
	}
	if r.FPR != 1.0 {
		t.Errorf("FPR = %v, want 1.0", r.FPR)
	}
	// (TP-FP)/total = (1-1)/2 = 0: the false positive cancels the true positive.
	if r.NetUtility != 0 {
		t.Errorf("NetUtility = %v, want 0 (FP cancels TP)", r.NetUtility)
	}
}

func TestScoreNoPromptsIsZeroUtility(t *testing.T) {
	t.Parallel()
	r := activation.Score("anything", nil, nil)
	if r.NetUtility != 0 {
		t.Errorf("NetUtility with no prompts = %v, want 0", r.NetUtility)
	}
}

func TestScoreMissedTarget(t *testing.T) {
	t.Parallel()
	// A target with no salient-term overlap is a miss (FN); TPR drops to 0.
	desc := "Invoke for database migration planning."
	triggers := []string{"help me choose a color palette"}
	r := activation.Score(desc, triggers, nil)
	if r.TP != 0 || r.FN != 1 {
		t.Errorf("TP=%d FN=%d, want TP=0 FN=1", r.TP, r.FN)
	}
	if r.FNR != 1.0 {
		t.Errorf("FNR = %v, want 1.0", r.FNR)
	}
}
