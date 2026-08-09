package cmd_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenACoffman/skillsaw/cmd/root"
)

// writeTP writes a test-prompts.json into an existing skill dir.
func writeTP(t *testing.T, tree, skill, body string) {
	t.Helper()
	p := filepath.Join(tree, skill, "test-prompts.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write test-prompts: %v", err)
	}
}

func TestChecksReportsAndGatesMissing(t *testing.T) {
	t.Parallel()
	tree := makeTree(t, "alpha")
	// One should_trigger case with no checks and nothing derivable from its expected
	// prose, so ChecksFor returns none: a genuine gap.
	writeTP(t, tree, "alpha", `{"tests":[
		{"id":1,"type":"should_trigger","prompt":"do the thing","expected":"it activates the thing"}]}`)
	out, err := run(t, "checks", "--tree", tree)
	var exit root.ExitError
	if !errors.As(err, &exit) || int(exit) != 1 {
		t.Fatalf("a check-less behavioral case must gate (ExitError 1); got %v\n%s", err, out)
	}
	if !strings.Contains(out, "alpha: 1/1 behavioral case(s) lack checks") {
		t.Errorf("gap not reported:\n%s", out)
	}
}

func TestChecksCleanWhenEveryCaseHasACheck(t *testing.T) {
	t.Parallel()
	tree := makeTree(t, "alpha")
	writeTP(t, tree, "alpha", `{"tests":[
		{"id":1,"type":"should_trigger","prompt":"p","expected":"e",
		 "checks":[{"op":"contains","arg":"x"}]}]}`)
	out, err := run(t, "checks", "--tree", tree)
	if err != nil {
		t.Fatalf("a fully-checked tree must pass: %v\n%s", err, out)
	}
	if !strings.Contains(out, "0 of 1 behavioral case(s)") {
		t.Errorf("expected a clean 0-of-1 report:\n%s", out)
	}
}

// TestChecksIgnoresDecoys pins the §406 rule: a should_not_trigger decoy needs no check,
// so counting it would make the remaining work look permanently unfinishable.
func TestChecksIgnoresDecoys(t *testing.T) {
	t.Parallel()
	tree := makeTree(t, "alpha")
	writeTP(t, tree, "alpha", `{"tests":[
		{"id":1,"type":"should_not_trigger","prompt":"unrelated","expected":"does not fire"}]}`)
	out, err := run(t, "checks", "--tree", tree)
	if err != nil {
		t.Fatalf("a decoy-only skill has no behavioral gap: %v\n%s", err, out)
	}
	if !strings.Contains(out, "0 of 0 behavioral case(s)") {
		t.Errorf("decoys must not be counted as behavioral:\n%s", out)
	}
}
