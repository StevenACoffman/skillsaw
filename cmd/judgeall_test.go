package cmd_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// behavioralTP has two should_trigger cases, one edge_case, and a decoy that must not
// be scored. Each behavioral case checks for a distinct marker.
const behavioralTP = `{"tests":[
{"id":1,"type":"should_trigger","prompt":"p","expected":"e","checks":[{"op":"contains","arg":"ALPHA"}]},
{"id":2,"type":"should_trigger","prompt":"p","expected":"e","checks":[{"op":"contains","arg":"BETA"}]},
{"id":3,"type":"should_not_trigger","prompt":"p","expected":"e","checks":[{"op":"contains","arg":"DECOY"}]},
{"id":4,"type":"edge_case","prompt":"p","expected":"e","checks":[{"op":"contains","arg":"GAMMA"}]}]}`

// judgeInputs writes the test-prompts file and an outputs dir holding the given
// id → content mapping.
func judgeInputs(t *testing.T, outputs map[int]string) (tp, dir string) {
	t.Helper()
	base := t.TempDir()
	tp = filepath.Join(base, "test-prompts.json")
	if err := os.WriteFile(tp, []byte(behavioralTP), 0o600); err != nil {
		t.Fatal(err)
	}
	dir = filepath.Join(base, "out")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for id, body := range outputs {
		f := filepath.Join(dir, "out-"+strconv.Itoa(id)+".txt")
		if err := os.WriteFile(f, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return tp, dir
}

func TestJudgeAllReportsTheBaseTheMeanImplies(t *testing.T) {
	t.Parallel()
	// Two of the three behavioral cases pass → mean soft 2/3 → base round(6.67) = 7.
	tp, dir := judgeInputs(t, map[int]string{
		1: "ALPHA here", 2: "BETA here", 4: "nothing matching",
	})
	out, err := run(t, "judge", "--from-test-prompts", tp, "--all", "--outputs", dir)
	if err != nil {
		t.Fatalf("judge --all failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "base 7") {
		t.Errorf("want base 7 from a 2-of-3 mean, got:\n%s", out)
	}
	if !strings.Contains(out, "3 case(s)") {
		t.Errorf("want 3 behavioral cases, got:\n%s", out)
	}
}

func TestJudgeAllDoesNotScoreDecoys(t *testing.T) {
	t.Parallel()
	// The decoy (id 3) has no output file at all. If it were scored, the run would
	// fail on the missing file — and if it were skipped-but-counted, the denominator
	// would be 4 rather than 3.
	tp, dir := judgeInputs(t, map[int]string{
		1: "ALPHA", 2: "BETA", 4: "GAMMA",
	})
	out, err := run(t, "judge", "--from-test-prompts", tp, "--all", "--outputs", dir)
	if err != nil {
		t.Fatalf("a decoy was scored: %v\n%s", err, out)
	}
	if strings.Contains(out, "case 3:") {
		t.Errorf("the decoy appears in the report:\n%s", out)
	}
	if !strings.Contains(out, "base 10") {
		t.Errorf("all behavioral cases passed, want base 10:\n%s", out)
	}
}

func TestJudgeAllRefusesAMissingOutput(t *testing.T) {
	t.Parallel()
	// Skipping would change the denominator and report a base computed over fewer
	// cases than it claims.
	tp, dir := judgeInputs(t, map[int]string{1: "ALPHA", 2: "BETA"}) // id 4 missing
	out, err := run(t, "judge", "--from-test-prompts", tp, "--all", "--outputs", dir)
	if err == nil {
		t.Fatalf("a missing output was silently skipped:\n%s", out)
	}
	if !strings.Contains(err.Error(), "case 4") {
		t.Errorf("the error should name the case, got: %v", err)
	}
}

func TestJudgeAllReportsRatherThanGates(t *testing.T) {
	t.Parallel()
	// Every case fails its checks. Single-case judge exits 1 on hard==0; --all is a
	// measurement, and most skills fail some checks, so it must still report the base.
	tp, dir := judgeInputs(t, map[int]string{
		1: "nothing", 2: "nothing", 4: "nothing",
	})
	out, err := run(t, "judge", "--from-test-prompts", tp, "--all", "--outputs", dir)
	if err != nil {
		t.Fatalf("--all gated instead of reporting: %v\n%s", err, out)
	}
	// A zero mean maps to the rubric floor, not to 0 — 0 is not on the scale.
	if !strings.Contains(out, "base 1") {
		t.Errorf("want the floor base 1 for an all-failing set, got:\n%s", out)
	}
}

func TestJudgeAllJSON(t *testing.T) {
	t.Parallel()
	tp, dir := judgeInputs(t, map[int]string{1: "ALPHA", 2: "BETA", 4: "GAMMA"})
	out, err := run(t, "judge", "--from-test-prompts", tp, "--all", "--outputs", dir, "--json")
	if err != nil {
		t.Fatalf("judge --all --json failed: %v\n%s", err, out)
	}
	var got struct {
		Cases []struct {
			ID int `json:"id"`
		} `json:"cases"`
		MeanSoft float64 `json:"mean_soft"`
		Base     int     `json:"base"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if len(got.Cases) != 3 || got.Base != 10 {
		t.Errorf("got %d cases and base %d, want 3 and 10", len(got.Cases), got.Base)
	}
}

func TestJudgeAllNeedsItsInputs(t *testing.T) {
	t.Parallel()
	tp, dir := judgeInputs(t, map[int]string{1: "ALPHA"})
	if out, err := run(t, "judge", "--all", "--outputs", dir); err == nil {
		t.Errorf("--all ran without --from-test-prompts:\n%s", out)
	}
	if out, err := run(t, "judge", "--from-test-prompts", tp, "--all"); err == nil {
		t.Errorf("--all ran without --outputs:\n%s", out)
	}
}

func TestJudgeSingleCaseIsUnchanged(t *testing.T) {
	t.Parallel()
	// The gate contract stays: hard==0 exits 1.
	tp, dir := judgeInputs(t, map[int]string{1: "ALPHA"})
	if out, err := run(t, "judge", "--from-test-prompts", tp, "--id", "1",
		"--output", filepath.Join(dir, "out-1.txt")); err != nil {
		t.Fatalf("a passing case should exit 0: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "out-1.txt"), []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := run(t, "judge", "--from-test-prompts", tp, "--id", "1",
		"--output", filepath.Join(dir, "out-1.txt")); err == nil {
		t.Errorf("a failing case should still gate:\n%s", out)
	}
}

func TestCalibrateReadsJSONL(t *testing.T) {
	t.Parallel()
	// What the loop appends line by line, with no assembly step.
	dir := t.TempDir()
	jsonl := filepath.Join(dir, "judgments.jsonl")
	lines := `{"skill":"a","dim":5,"base":9,"passed":true}
{"skill":"b","dim":3,"base":8,"passed":false}

{"skill":"c","dim":2,"base":10,"passed":true}
`
	if err := os.WriteFile(jsonl, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "calibrate", jsonl)
	if err != nil {
		t.Fatalf("calibrate failed on JSONL: %v\n%s", err, out)
	}
	if !strings.Contains(out, "samples: 3") {
		t.Errorf("want 3 samples (the blank line skipped), got:\n%s", out)
	}
}

func TestCalibrateGivesTheSameReportForBothShapes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	objs := []string{
		`{"skill":"a","dim":5,"base":9,"passed":true}`,
		`{"skill":"b","dim":3,"base":8,"passed":false}`,
	}
	jsonl := filepath.Join(dir, "j.jsonl")
	if err := os.WriteFile(jsonl, []byte(strings.Join(objs, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wrapped := filepath.Join(dir, "j.json")
	if err := os.WriteFile(wrapped,
		[]byte(`{"judgments":[`+strings.Join(objs, ",")+`]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := run(t, "calibrate", jsonl)
	if err != nil {
		t.Fatalf("jsonl: %v\n%s", err, a)
	}
	b, err := run(t, "calibrate", wrapped)
	if err != nil {
		t.Fatalf("wrapped: %v\n%s", err, b)
	}
	if a != b {
		t.Errorf("the two shapes disagree:\n--- jsonl ---\n%s\n--- wrapped ---\n%s", a, b)
	}
}

func TestCalibrateReadsASingleLineJSONL(t *testing.T) {
	t.Parallel()
	// The subtle case: one line is itself valid JSON and unmarshals into the wrapper
	// with zero judgments, so "did it parse" cannot tell the shapes apart.
	f := filepath.Join(t.TempDir(), "one.jsonl")
	if err := os.WriteFile(f,
		[]byte(`{"skill":"a","dim":5,"base":9,"passed":true}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "calibrate", f)
	if err != nil {
		t.Fatalf("a one-line JSONL file was read as empty: %v\n%s", err, out)
	}
	if !strings.Contains(out, "samples: 1") {
		t.Errorf("want 1 sample, got:\n%s", out)
	}
}

func TestCalibrateRefusesAMalformedLine(t *testing.T) {
	t.Parallel()
	// A dropped judgment skews the report toward whatever survived.
	f := filepath.Join(t.TempDir(), "bad.jsonl")
	if err := os.WriteFile(f, []byte(
		`{"skill":"a","dim":5,"base":9,"passed":true}`+"\nnot json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "calibrate", f)
	if err == nil {
		t.Fatalf("a malformed line was skipped:\n%s", out)
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("the error should name the line, got: %v", err)
	}
}

func TestJudgeAllNamesEveryUnscorableCase(t *testing.T) {
	t.Parallel()
	// Driven by real data: across the corpus, files where *no* behavioral case carries
	// checks are the common state, and failing at the first one reads as a per-case
	// slip rather than a property of the file. Name them all, and say what to fix.
	tp := filepath.Join(t.TempDir(), "tp.json")
	body := `{"tests":[
{"id":1,"type":"should_trigger","prompt":"p","expected":"prose with nothing checkable"},
{"id":2,"type":"edge_case","prompt":"p","expected":"also nothing checkable"}]}`
	if err := os.WriteFile(tp, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "judge", "--from-test-prompts", tp, "--all", "--outputs", t.TempDir())
	if err == nil {
		t.Fatalf("a file specifying no checks produced a base:\n%s", out)
	}
	if !strings.Contains(err.Error(), "2 of 2") {
		t.Errorf("the error should count them, got: %v", err)
	}
	for _, want := range []string{"case 1", "case 2"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q not named among the unscorable:\n%s", want, out)
		}
	}
}

func TestJudgeAllRefusesAPartiallyScorableFile(t *testing.T) {
	t.Parallel()
	// A base averaged over an unstated subset is not comparable to one averaged over
	// every case, and comparability is the point of the total it feeds.
	tp := filepath.Join(t.TempDir(), "tp.json")
	body := `{"tests":[
{"id":1,"type":"should_trigger","prompt":"p","expected":"e","checks":[{"op":"contains","arg":"A"}]},
{"id":2,"type":"edge_case","prompt":"p","expected":"nothing checkable"}]}`
	if err := os.WriteFile(tp, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "out-1.txt"), []byte("A"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "judge", "--from-test-prompts", tp, "--all", "--outputs", dir)
	if err == nil {
		t.Fatalf("scored a subset and called it a base:\n%s", out)
	}
	if !strings.Contains(err.Error(), "1 of 2") {
		t.Errorf("want the partial count, got: %v", err)
	}
}
