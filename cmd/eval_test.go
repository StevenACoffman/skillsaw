package cmd_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/StevenACoffman/skillsaw/cmd/root"
)

// allBases scores every rubric dimension at v, which is enough for eval to report a
// FULL total rather than a dash.
func allBases(v int) map[string]int {
	out := make(map[string]int, 9)
	for d := 1; d <= 9; d++ {
		out[strconv.Itoa(d)] = v
	}
	return out
}

// writeScores marshals a scores document to a fresh file.
func writeScores(t *testing.T, doc any) string {
	t.Helper()
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "scores.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// hashOf returns a skill's content identity via the CLI, so the fixture cannot drift
// from what eval computes.
func hashOf(t *testing.T, dir string) string {
	t.Helper()
	out, err := run(t, "hash", dir)
	if err != nil {
		t.Fatalf("hash %s: %v", dir, err)
	}
	return strings.Fields(out)[0]
}

// fullOf returns the FULL column for skill name, or "" when eval printed no row.
func fullOf(out, name string) string {
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) > 2 && f[0] == name {
			return f[2]
		}
	}
	return ""
}

func TestEvalStillAcceptsTheLegacyScoresShape(t *testing.T) {
	t.Parallel()
	// The shape skillsaw-skill's Phase 1 writes. Breaking it would break the loop.
	tree := makeTree(t, "alpha")
	dir := filepath.Join(tree, "alpha")
	out, err := run(t, "eval", "--scores", writeScores(t, allBases(10)), dir)
	if err != nil {
		t.Fatalf("eval failed: %v\n%s", err, out)
	}
	if got := fullOf(out, "alpha"); got == "" || got == "-" {
		t.Errorf("legacy bases produced no FULL total:\n%s", out)
	}
}

func TestEvalGivesEachSkillItsOwnBases(t *testing.T) {
	t.Parallel()
	// One bases map used to be applied to every directory in the run, so "--all
	// --scores" gave all 233 skills one skill's judge bases and called every total
	// comparable. Judging these two very differently makes that visible.
	tree := makeTree(t, "alpha", "beta")
	alpha, beta := filepath.Join(tree, "alpha"), filepath.Join(tree, "beta")
	path := writeScores(t, map[string]any{"entries": []any{
		map[string]any{"skill": "alpha", "hash": hashOf(t, alpha), "bases": allBases(10)},
		map[string]any{"skill": "beta", "hash": hashOf(t, beta), "bases": allBases(2)},
	}})
	out, err := run(t, "eval", "--scores", path, alpha, beta)
	if err != nil {
		t.Fatalf("eval failed: %v\n%s", err, out)
	}
	a, b := fullOf(out, "alpha"), fullOf(out, "beta")
	if a == "" || b == "" || a == "-" || b == "-" {
		t.Fatalf("expected a FULL total for both:\n%s", out)
	}
	if a == b {
		t.Errorf("both skills got the same total (%s) despite different bases:\n%s", a, out)
	}
}

func TestEvalRefusesBasesJudgedAgainstAnotherVersion(t *testing.T) {
	t.Parallel()
	// The failure the hash binding exists to prevent: bases judged before an edit
	// producing a FULL total for the text after it. In the optimize loop that total is
	// what the keep-or-revert gate compares.
	tree := makeTree(t, "alpha")
	dir := filepath.Join(tree, "alpha")
	path := writeScores(t, map[string]any{"entries": []any{
		map[string]any{"skill": "alpha", "hash": hashOf(t, dir), "bases": allBases(10)},
	}})
	if out, err := run(t, "eval", "--scores", path, dir); err != nil {
		t.Fatalf("a matching hash should score: %v\n%s", err, out)
	}

	f := filepath.Join(dir, "SKILL.md")
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, append(b, []byte("\nedited after judging\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "eval", "--scores", path, dir)
	if err == nil {
		t.Fatalf("stale bases were applied to an edited skill:\n%s", out)
	}
	var exit root.ExitError
	if !errors.As(err, &exit) {
		t.Errorf("want an ExitError, got %T", err)
	}
	if !strings.Contains(out, "re-judge it") {
		t.Errorf("the report does not say what to do:\n%s", out)
	}
	if got := fullOf(out, "alpha"); got != "-" {
		t.Errorf("a FULL total was still reported (%q) from stale bases:\n%s", got, out)
	}
}

func TestEvalScoresEveryOtherSkillBesideAStaleOne(t *testing.T) {
	t.Parallel()
	// With --all a single stale entry must not hide the other verdicts, even though
	// the run still fails.
	tree := makeTree(t, "alpha", "beta")
	alpha, beta := filepath.Join(tree, "alpha"), filepath.Join(tree, "beta")
	path := writeScores(t, map[string]any{"entries": []any{
		map[string]any{"skill": "alpha", "hash": "stale-hash", "bases": allBases(10)},
		map[string]any{"skill": "beta", "hash": hashOf(t, beta), "bases": allBases(9)},
	}})
	out, err := run(t, "eval", "--scores", path, alpha, beta)
	if err == nil {
		t.Fatalf("the run should fail when anything is stale:\n%s", out)
	}
	if got := fullOf(out, "beta"); got == "" || got == "-" {
		t.Errorf("beta was not scored despite its bases matching:\n%s", out)
	}
	if got := fullOf(out, "alpha"); got != "-" {
		t.Errorf("alpha was scored from stale bases (%q):\n%s", got, out)
	}
}

func TestEvalWithoutScoresIsUnchanged(t *testing.T) {
	t.Parallel()
	tree := makeTree(t, "alpha")
	out, err := run(t, "eval", filepath.Join(tree, "alpha"))
	if err != nil {
		t.Fatalf("eval failed: %v\n%s", err, out)
	}
	if got := fullOf(out, "alpha"); got != "-" {
		t.Errorf("no bases should mean no FULL total, got %q:\n%s", got, out)
	}
}
