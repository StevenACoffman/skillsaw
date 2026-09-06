package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDiagnoseAgainstNamesARegression is L1077 through the real CLI: a dimension that
// scored well and now does not is reported as that, and the recommendation changes.
func TestDiagnoseAgainstNamesARegression(t *testing.T) {
	t.Parallel()
	tree := makeTree(t, "alpha")
	dir := filepath.Join(tree, "alpha")

	// The baseline is a real "eval --json" of a skill with a counter-example section; the
	// current tree has none, so dim 9 falls from 9 to 2.
	rich := filepath.Join(t.TempDir(), "rich")
	if err := os.MkdirAll(rich, 0o750); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	withBoundary := string(body) + "\n## 反例黑名单\n- do not A\n- do not B\n- do not C\n"
	if err := os.WriteFile(
		filepath.Join(rich, "SKILL.md"),
		[]byte(withBoundary),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	baseline := evalJSON(t, rich)

	out, err := run(t, "diagnose", "--against", baseline, dir)
	if err != nil {
		t.Fatalf("diagnose failed: %v\n%s", err, out)
	}
	for _, want := range []string{"scored 9 in the evaluation compared against", "read the diff"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not say %q:\n%s", want, out)
		}
	}
}

// TestDiagnoseWithoutABaselineClaimsNothing is the case a reader is most likely to
// misread. No comparison was made, and the output must not imply the dimension has always
// been this way — an absent opinion is not a finding.
func TestDiagnoseWithoutABaselineClaimsNothing(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(makeTree(t, "alpha"), "alpha")
	out, err := run(t, "diagnose", dir)
	if err != nil {
		t.Fatalf("diagnose failed: %v\n%s", err, out)
	}
	for _, absent := range []string{"read the diff", "compared against"} {
		if strings.Contains(out, absent) {
			t.Errorf("an uncompared diagnosis says %q:\n%s", absent, out)
		}
	}
}

// TestDiagnoseRefusesToCompareAcrossRubrics guards the same rule "regression" enforces for
// totals: two evaluations under different rules are not a before and an after.
func TestDiagnoseRefusesToCompareAcrossRubrics(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(makeTree(t, "alpha"), "alpha")
	stale := filepath.Join(t.TempDir(), "stale.json")
	doc := `[{"skill":"alpha","hash":"aaaa","rubric":"an-earlier-edition",
		"dims":[{"num":9,"name":"Counter-examples / blacklist","final":9}]}]`
	if err := os.WriteFile(stale, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "diagnose", "--against", stale, dir)
	if err != nil {
		t.Fatalf("diagnose failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "different rubric edition") {
		t.Errorf("output does not say why it declined to compare:\n%s", out)
	}
	if strings.Contains(out, "read the diff") {
		t.Errorf("it recommended diffing against an incomparable baseline:\n%s", out)
	}
}

// evalJSON writes an "eval --json" of dir and returns the path, so the fixture is the real
// output format rather than a hand-written approximation of it.
func evalJSON(t *testing.T, dir string) string {
	t.Helper()
	out, err := run(t, "eval", "--json", dir)
	if err != nil {
		t.Fatalf("eval --json: %v\n%s", err, out)
	}
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
