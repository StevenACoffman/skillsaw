package cmd_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeJudgments writes a judgments file and returns its path.
func writeJudgments(t *testing.T, doc string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "judgments.json")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatalf("write judgments: %v", err)
	}
	return path
}

func TestCalibrateReportsOverconfidence(t *testing.T) {
	t.Parallel()
	// The signal the command exists for: the agent rated dim 8 at the ceiling and the
	// deterministic evidence disagreed most of the time.
	var b strings.Builder
	b.WriteString(`{"judgments":[`)
	for i := range 10 {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"skill":"s%d","dim":8,"base":10,"passed":%t}`, i, i < 3)
	}
	b.WriteString(`]}`)

	out, err := run(t, "calibrate", writeJudgments(t, b.String()))
	if err != nil {
		t.Fatalf("calibrate: %v\n%s", err, out)
	}
	if !strings.Contains(out, "(overconfident)") {
		t.Errorf("expected the top bin to be flagged overconfident, got:\n%s", out)
	}
	if !strings.Contains(out, "samples: 10") {
		t.Errorf("expected the sample count to be reported, got:\n%s", out)
	}
}

func TestCalibrateReportsDroppedAndThinSamples(t *testing.T) {
	t.Parallel()
	// An out-of-range base is dropped rather than clamped, and the drop must be
	// visible — a silently smaller denominator would misstate the calibration.
	path := writeJudgments(t, `{"judgments":[
	  {"skill":"a","dim":8,"base":10,"passed":true},
	  {"skill":"b","dim":8,"base":11,"passed":true}
	]}`)
	out, err := run(t, "calibrate", path)
	if err != nil {
		t.Fatalf("calibrate: %v\n%s", err, out)
	}
	if !strings.Contains(out, "dropped 1 judgment") {
		t.Errorf("expected the dropped judgment to be reported, got:\n%s", out)
	}
	if !strings.Contains(out, "thin for a 10-bin report") {
		t.Errorf("expected a thin-sample note, got:\n%s", out)
	}
}

func TestCalibrateJSONIsWellFormed(t *testing.T) {
	t.Parallel()
	path := writeJudgments(t,
		`{"judgments":[{"skill":"a","dim":8,"base":10,"passed":true}]}`)
	out, err := run(t, "calibrate", "--json", path)
	if err != nil {
		t.Fatalf("calibrate --json: %v\n%s", err, out)
	}
	var rep struct {
		ECE     float64 `json:"ECE"`
		Samples int     `json:"Samples"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if rep.Samples != 1 {
		t.Errorf("Samples = %d, want 1", rep.Samples)
	}
}

func TestCalibrateNeedsScorableJudgments(t *testing.T) {
	t.Parallel()
	// Reporting a zero calibration for an unscorable file would look like a verdict.
	path := writeJudgments(t, `{"judgments":[{"skill":"a","dim":8,"base":42}]}`)
	out, err := run(t, "calibrate", path)
	if err == nil {
		t.Fatalf("expected an error when nothing is scorable\n%s", out)
	}
	if !strings.Contains(err.Error(), "no judgments with a base in 1-10") {
		t.Errorf("error should name the problem, got %v", err)
	}
}
