package cmd_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenACoffman/skillsaw/cmd/root"
	"github.com/StevenACoffman/skillsaw/internal/auditlog"
)

func TestLogWritesAHeaderForAFreshFile(t *testing.T) {
	t.Parallel()
	// A new log needs no separate setup step; the header comes from auditlog.Columns()
	// rather than being spelled out a second time by the caller.
	f := filepath.Join(t.TempDir(), "results.tsv")
	if out, err := run(t, "log", "--file", f, "--skill", "alpha", "--status", "baseline",
		"--new", "80.0", "--commit", "baseline", "--note", "initial"); err != nil {
		t.Fatalf("log failed: %v\n%s", err, out)
	}
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want header + 1 row, got %d lines:\n%s", len(lines), b)
	}
	if got, want := lines[0], strings.Join(auditlog.Columns(), "\t"); got != want {
		t.Errorf("header = %q, want %q", got, want)
	}
	if n := len(strings.Split(lines[1], "\t")); n != len(auditlog.Columns()) {
		t.Errorf("row has %d columns, want %d", n, len(auditlog.Columns()))
	}
}

func TestLogAppendsWithoutRepeatingTheHeader(t *testing.T) {
	t.Parallel()
	f := filepath.Join(t.TempDir(), "results.tsv")
	for _, s := range []string{"baseline", "keep", "revert"} {
		if out, err := run(t, "log", "--file", f, "--skill", "alpha", "--status", s); err != nil {
			t.Fatalf("log %s failed: %v\n%s", s, err, out)
		}
	}
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "timestamp\t"); n != 1 {
		t.Errorf("header written %d times, want once:\n%s", n, b)
	}
	if n := len(strings.Split(strings.TrimRight(string(b), "\n"), "\n")); n != 4 {
		t.Errorf("want header + 3 rows, got %d lines:\n%s", n, b)
	}
}

func TestLogRowsReadBackThroughHistory(t *testing.T) {
	t.Parallel()
	// The point of the command: the writer and the reader share one Row type, so a
	// column-order drift between them is impossible rather than merely unlikely.
	f := filepath.Join(t.TempDir(), "results.tsv")
	if out, err := run(t, "log", "--file", f, "--skill", "alpha", "--status", "keep",
		"--old", "80.0", "--new", "84.5", "--dimension", "dim3",
		"--note", "tightened the failure branch", "--eval-mode", "full_test",
		"--commit", "abc1234"); err != nil {
		t.Fatalf("log failed: %v\n%s", err, out)
	}
	out, err := run(t, "history", "--file", f)
	if err != nil {
		t.Fatalf("history failed: %v\n%s", err, out)
	}
	for _, want := range []string{"alpha", "84.5", "dim3", "tightened the failure branch", "abc1234"} {
		if !strings.Contains(out, want) {
			t.Errorf("history lost %q:\n%s", want, out)
		}
	}
}

func TestLogRejectsAStatusTheLogDoesNotAdmit(t *testing.T) {
	t.Parallel()
	// Validated once, inside auditlog.Append, and nothing is written for a bad row.
	f := filepath.Join(t.TempDir(), "results.tsv")
	out, err := run(t, "log", "--file", f, "--skill", "alpha", "--status", "invented")
	if err == nil {
		t.Fatalf("accepted an unknown status:\n%s", out)
	}
	if _, statErr := os.Stat(f); statErr == nil {
		b, _ := os.ReadFile(f)
		if strings.Contains(string(b), "invented") {
			t.Errorf("a rejected row still reached the file:\n%s", b)
		}
	}
}

func TestLogRequiresASkill(t *testing.T) {
	t.Parallel()
	f := filepath.Join(t.TempDir(), "results.tsv")
	if out, err := run(t, "log", "--file", f, "--status", "keep"); err == nil {
		t.Errorf("wrote a row with no skill:\n%s", out)
	}
}

func TestLogTimestampsByDefault(t *testing.T) {
	t.Parallel()
	f := filepath.Join(t.TempDir(), "results.tsv")
	if out, err := run(t, "log", "--file", f, "--skill", "alpha", "--status", "keep"); err != nil {
		t.Fatalf("log failed: %v\n%s", err, out)
	}
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	row := strings.Split(strings.Split(strings.TrimRight(string(b), "\n"), "\n")[1], "\t")
	// 2006-01-02T15:04 — a date, not an empty column.
	if len(row[0]) != len("2006-01-02T15:04") || !strings.Contains(row[0], "T") {
		t.Errorf("timestamp column = %q, want a minute-precision stamp", row[0])
	}
}

func TestPreflightAgainstOriginal(t *testing.T) {
	t.Parallel()
	tree := makeTree(t, "alpha")
	dir := filepath.Join(tree, "alpha")
	orig := filepath.Join(t.TempDir(), "skill.orig")
	b, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orig, b, 0o600); err != nil {
		t.Fatal(err)
	}

	// An unchanged skill is a no-op edit: previously a bash line that only echoed.
	out, err := run(t, "preflight", "--against", orig, dir)
	if err == nil {
		t.Fatalf("a no-op edit passed the gate:\n%s", out)
	}
	if !strings.Contains(out, "changed nothing") {
		t.Errorf("expected the no-op defect, got:\n%s", out)
	}
	var exit root.ExitError
	if !errors.As(err, &exit) {
		t.Errorf("want an ExitError, got %T", err)
	}

	// A real, modest edit passes.
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"),
		append(b, []byte("\na real edit\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := run(t, "preflight", "--against", orig, dir); err != nil {
		t.Fatalf("a modest edit was rejected: %v\n%s", err, out)
	}
}

func TestPreflightAgainstRejectsRunawayGrowth(t *testing.T) {
	t.Parallel()
	tree := makeTree(t, "alpha")
	dir := filepath.Join(tree, "alpha")
	orig := filepath.Join(t.TempDir(), "skill.orig")
	b, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orig, b, 0o600); err != nil {
		t.Fatal(err)
	}
	bloated := append(b, []byte(strings.Repeat("padding padding padding\n", 50))...)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), bloated, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "preflight", "--against", orig, dir)
	if err == nil {
		t.Fatalf("a rewrite passed the size ceiling:\n%s", out)
	}
	if !strings.Contains(out, "past the") {
		t.Errorf("expected the growth defect, got:\n%s", out)
	}
	// The ceiling is tunable, and a generous one admits the same edit.
	if out, err := run(t, "preflight", "--against", orig, "--max-growth", "100", dir); err != nil {
		t.Errorf("--max-growth did not raise the ceiling: %v\n%s", err, out)
	}
}

func TestPreflightAgainstNeedsExactlyOneSkillDir(t *testing.T) {
	t.Parallel()
	// One original cannot describe two edits; comparing both to it would report
	// defects that are arithmetic accidents.
	tree := makeTree(t, "alpha", "beta")
	orig := filepath.Join(t.TempDir(), "skill.orig")
	if err := os.WriteFile(orig, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "preflight", "--against", orig,
		filepath.Join(tree, "alpha"), filepath.Join(tree, "beta"))
	if err == nil {
		t.Fatalf("accepted two SKILL_DIRs with one original:\n%s", out)
	}
	// Assert the reason, not just that something failed: comparing both skills to a
	// one-byte original also fails, on the size ceiling, so "err != nil" alone would
	// pass even if this check were removed.
	if !strings.Contains(err.Error(), "one edit to one original") {
		t.Errorf("failed for the wrong reason: %v", err)
	}
}

func TestPreflightWithoutAgainstIsUnchanged(t *testing.T) {
	t.Parallel()
	tree := makeTree(t, "alpha")
	if out, err := run(t, "preflight", filepath.Join(tree, "alpha")); err != nil {
		t.Fatalf("a sound skill was rejected: %v\n%s", err, out)
	}
}
