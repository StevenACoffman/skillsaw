package cmd_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenACoffman/skillsaw/cmd/root"
)

// subject is the skill every case here logs against; one is enough, since what varies is
// the series of scores rather than who produced them.
const subject = "alpha"

// logScores writes one results.tsv row per score through the real "log" command, so the
// fixture cannot drift from the format the reader expects. Rows written this way carry the
// current rubric edition, which is what an ordinary run looks like.
func logScores(t *testing.T, scores []string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "results.tsv")
	for _, s := range scores {
		out, err := run(t, "log", "--file", f, "--skill", subject, "--status", "keep", "--new", s)
		if err != nil {
			t.Fatalf("log %s: %v\n%s", s, err, out)
		}
	}
	return f
}

// TestRegressionJudgesAgainstRecentHistory is the check a fixed threshold cannot make:
// every score here is high, and the last one is a drop the "above 80" style of gate would
// wave through.
func TestRegressionJudgesAgainstRecentHistory(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		scores     []string
		tolerance  string
		wantExit   bool
		wantInText string
	}{
		"a clear drop": {
			[]string{"92", "93", "91", "84"}, "0", true, "REGRESSED",
		},
		"steady": {
			[]string{"92", "93", "91", "92"}, "0", false, "ok",
		},
		"a drop inside tolerance": {
			[]string{"92", "93", "91", "90"}, "3", false, "ok",
		},
		"an improvement": {
			[]string{"84", "85", "86", "95"}, "0", false, "ok",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := logScores(t, tc.scores)
			out, err := run(t, "regression", "--file", f, "--skill", "alpha",
				"--tolerance", tc.tolerance)
			var exit root.ExitError
			switch {
			case tc.wantExit && !errors.As(err, &exit):
				t.Fatalf("a regression did not gate: %v\n%s", err, out)
			case !tc.wantExit && err != nil:
				t.Fatalf("a clean run was rejected: %v\n%s", err, out)
			}
			if !strings.Contains(out, tc.wantInText) {
				t.Errorf("output does not say %q:\n%s", tc.wantInText, out)
			}
		})
	}
}

// TestTooLittleHistoryIsNotAPass is the third state, and the one a caller is most likely to
// misread. It must exit 0 -- failing the first run of a metric is fixable only by not
// measuring -- while saying plainly that it holds no opinion, so nobody records a silent
// run as evidence the score held.
func TestTooLittleHistoryIsNotAPass(t *testing.T) {
	t.Parallel()
	for name, scores := range map[string][]string{
		"one run":            {"92"},
		"two runs":           {"92", "91"},
		"nothing measurable": {"-"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := logScores(t, scores)
			out, err := run(t, "regression", "--file", f, "--skill", "alpha")
			if err != nil {
				t.Fatalf("an absent opinion must not gate: %v\n%s", err, out)
			}
			if !strings.Contains(out, "NOT COMPARED") {
				t.Errorf("a run with no baseline did not say so, so it reads as a pass:\n%s", out)
			}
			if strings.Contains(out, "ok —") {
				t.Errorf("a run with no baseline reported ok:\n%s", out)
			}
		})
	}
}

// TestRegressionIsPerSkill guards the filter. One log holds every skill, and judging alpha
// against beta's history is a comparison of unrelated numbers.
func TestRegressionIsPerSkill(t *testing.T) {
	t.Parallel()
	f := logScores(t, []string{"92", "93", "91", "92"})
	for _, s := range []string{"10", "11", "12"} {
		if out, err := run(t, "log", "--file", f, "--skill", "beta", "--status", "keep",
			"--new", s); err != nil {
			t.Fatalf("log beta: %v\n%s", err, out)
		}
	}
	out, err := run(t, "regression", "--file", f, "--skill", "alpha")
	if err != nil {
		t.Fatalf("alpha was judged against beta's scores: %v\n%s", err, out)
	}
	if !strings.Contains(out, "baseline of 92.00") {
		t.Errorf("the baseline is not alpha's:\n%s", out)
	}
}

// TestRegressionRefusesToSpanARubricChange is the guard L834 asks for, in the place it
// bites. Every score here is high and the last is a drop — but the rules changed partway,
// so the earlier numbers answer a different question. An edit to the rubric that raised
// every score must not be launderable through the ratchet's own history, and a real
// regression must not be reported on the strength of numbers that are not comparable.
func TestRegressionRefusesToSpanARubricChange(t *testing.T) {
	t.Parallel()
	f := filepath.Join(t.TempDir(), "results.tsv")
	for _, r := range []struct{ score, edition string }{
		{"92", "ed-old"}, {"93", "ed-old"}, {"91", "ed-old"}, {"84", "ed-new"},
	} {
		out, err := run(t, "log", "--file", f, "--skill", "alpha", "--status", "keep",
			"--new", r.score, "--rubric", r.edition)
		if err != nil {
			t.Fatalf("log: %v\n%s", err, out)
		}
	}
	out, err := run(t, "regression", "--file", f, "--skill", "alpha")
	if err != nil {
		t.Fatalf("an incomparable history must not gate: %v\n%s", err, out)
	}
	if !strings.Contains(out, "NOT COMPARED") {
		t.Errorf("a history spanning two editions produced a verdict:\n%s", out)
	}
	for _, want := range []string{"ed-old", "ed-new", "not a series"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not say %q, so the reader cannot tell why:\n%s", want, out)
		}
	}
}

// TestOneEditionThroughoutStillCompares is the other half: the guard must not swallow every
// history. Rows written by "log" carry the current edition automatically, so an ordinary
// run is unaffected.
func TestOneEditionThroughoutStillCompares(t *testing.T) {
	t.Parallel()
	f := logScores(t, []string{"92", "93", "91", "84"})
	out, err := run(t, "regression", "--file", f, "--skill", "alpha")
	var exit root.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("a real regression under one rubric was not reported: %v\n%s", err, out)
	}
	if !strings.Contains(out, "REGRESSED") {
		t.Errorf("output does not report the drop:\n%s", out)
	}
}
