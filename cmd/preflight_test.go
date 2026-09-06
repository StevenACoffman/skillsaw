package cmd_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenACoffman/skillet/identity"
	"github.com/StevenACoffman/skillsaw/cmd/root"
)

// soundSkill satisfies both structural families: allowed frontmatter keys, a
// description that states a trigger and fits the cap, and all six RIA-TV++ segments.
const soundSkill = `---
name: demo-skill
description: Use when the reader needs the demo thing done in a particular way.
---

## R

Recognition.

## I

Interpretation.

## A1

Past application.

## A2

Future trigger.

## E

Execution.

## B

Boundary.
`

// writePreflightSkill writes content as a "demo-skill" directory, matching the
// frontmatter name every case uses, and returns its path.
func writePreflightSkill(t *testing.T, content string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "demo-skill")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return dir
}

func TestPreflightPassesAStructurallySoundSkill(t *testing.T) {
	t.Parallel()
	dir := writePreflightSkill(t, soundSkill)

	out, err := run(t, "preflight", dir)
	if err != nil {
		t.Fatalf("preflight returned error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ok (structurally sound)") {
		t.Errorf("expected a clean verdict, got:\n%s", out)
	}
}

func TestPreflightRejectsAMissingRIASegment(t *testing.T) {
	t.Parallel()
	// Drop the Boundary segment: a red line, and the case the gate exists for —
	// an edit can delete a segment while still scoring well.
	dir := writePreflightSkill(t,
		strings.Replace(soundSkill, "## B\n\nBoundary.\n", "", 1))

	out, err := run(t, "preflight", "--redlines", dir)
	var exit root.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("expected root.ExitError, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "RIA segment") {
		t.Errorf("expected the missing segment to be named, got:\n%s", out)
	}
}

func TestPreflightRejectsABlownDescriptionCap(t *testing.T) {
	t.Parallel()
	// Proves the speclint family is wired in as well as the redlines one.
	long := "Use when " + strings.Repeat("x", 1100)
	dir := writePreflightSkill(t,
		strings.Replace(soundSkill,
			"description: Use when the reader needs the demo thing done in a particular way.",
			"description: "+long, 1))

	out, err := run(t, "preflight", dir)
	var exit root.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("expected root.ExitError, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "frontmatter") {
		t.Errorf("expected a frontmatter defect, got:\n%s", out)
	}
}

func TestPreflightNeedsASkillDirectory(t *testing.T) {
	t.Parallel()
	out, err := run(t, "preflight")
	if err == nil {
		t.Fatalf("expected an error with no arguments\n%s", out)
	}
	if !strings.Contains(err.Error(), "at least one SKILL_DIR") {
		t.Errorf("error should name the problem, got %v", err)
	}
}

func TestPreflightSkipsRedlinesByDefault(t *testing.T) {
	t.Parallel()
	// A hand-written skill with no RIA segments is legitimate; the default gate must
	// not reject it, or preflight is useless outside a book tree.
	dir := writePreflightSkill(t, `---
name: demo-skill
description: Use when the reader needs the demo thing done in a particular way.
---

# Body

A perfectly good skill that is not RIA-structured.
`)
	out, err := run(t, "preflight", dir)
	if err != nil {
		t.Fatalf("default preflight must pass a non-RIA skill: %v\n%s", err, out)
	}

	var exit root.ExitError
	if _, err = run(t, "preflight", "--redlines", dir); !errors.As(err, &exit) {
		t.Error("--redlines must reject the same skill")
	}
}

// writeBaseline writes a manifest describing tree with the given per-skill hashes, in the
// shape inventory.Entry produces, so the test exercises the real reader.
func writeBaseline(t *testing.T, tree string, skills []map[string]string) string {
	t.Helper()
	entries := make([]map[string]any, 0, len(skills))
	for _, s := range skills {
		e := map[string]any{
			"slug":   s["slug"],
			"dir":    filepath.Join(tree, s["slug"]),
			"sha256": s["hash"],
		}
		if s["prompts"] != "" {
			e["test_prompts"] = filepath.Join(tree, s["slug"], "test-prompts.json")
			e["test_prompts_sha256"] = s["prompts"]
		}
		entries = append(entries, e)
	}
	b, err := json.Marshal(map[string]any{
		"tool": "exegesis", "tree": tree, "structure_verified": true, "skills": entries,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "skills-manifest.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestPreflightWarnsWhenProseOutrunsItsAssertions is L1305 through the real CLI. The
// baseline records a SKILL.md hash that no longer matches, with a prompts hash that still
// does -- the edit landed and the assertions did not move with it.
func TestPreflightWarnsWhenProseOutrunsItsAssertions(t *testing.T) {
	t.Parallel()
	tree := makeTree(t, "alpha")
	writeTP(
		t,
		tree,
		"alpha",
		`{"tests":[{"id":1,"type":"should_trigger","prompt":"p","expected":"e"}]}`,
	)
	promptsHash := fileHash(t, filepath.Join(tree, "alpha", "test-prompts.json"))

	base := writeBaseline(t, tree, []map[string]string{
		{"slug": "alpha", "hash": "0000000000000000", "prompts": promptsHash},
	})
	out, err := run(t, "preflight", "--manifest", base, filepath.Join(tree, "alpha"))
	if err != nil {
		t.Fatalf("an advisory warning must not gate: %v\n%s", err, out)
	}
	if !strings.Contains(out, "test-prompts.json did not") {
		t.Errorf("the uncoupled edit was not reported:\n%s", out)
	}

	// The same run with --require-coupling is the caller opting in to a block.
	out, err = run(t, "preflight", "--manifest", base, "--require-coupling",
		filepath.Join(tree, "alpha"))
	var exit root.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("--require-coupling did not gate: %v\n%s", err, out)
	}
}

// TestPreflightIsSilentOnACoupledEdit is the case that keeps the warning worth reading. A
// baseline whose prompts hash is also stale means both moved together, which is the edit
// being done properly.
func TestPreflightIsSilentOnACoupledEdit(t *testing.T) {
	t.Parallel()
	tree := makeTree(t, "alpha")
	writeTP(
		t,
		tree,
		"alpha",
		`{"tests":[{"id":1,"type":"should_trigger","prompt":"p","expected":"e"}]}`,
	)
	base := writeBaseline(t, tree, []map[string]string{
		{"slug": "alpha", "hash": "0000000000000000", "prompts": "1111111111111111"},
	})
	out, err := run(t, "preflight", "--manifest", base, filepath.Join(tree, "alpha"))
	if err != nil {
		t.Fatalf("preflight failed: %v\n%s", err, out)
	}
	if strings.Contains(out, "test-prompts.json did not") {
		t.Errorf("an edit that moved both reported an uncoupled pair:\n%s", out)
	}
}

// fileHash returns the identity hash of one file, the way inventory.Entry computes it.
// Distinct from eval_test's hashOf, which asks the CLI for a whole skill's identity.
func fileHash(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return identity.Hash(string(b))
}

// TestOneUnloadableDirectoryDoesNotDiscardTheBatch is the defect this closes. Running
// preflight over 285 real skill directories returned nothing at all, because one of them —
// a support folder holding a script and an inputs/ tree — has no SKILL.md. Worse, the error
// reached ff's handler, so a missing file printed the full usage text and read as though
// the caller had mistyped a flag.
func TestOneUnloadableDirectoryDoesNotDiscardTheBatch(t *testing.T) {
	t.Parallel()
	tree := makeTree(t, "alpha", "beta")
	gap := filepath.Join(t.TempDir(), "not-a-skill")
	if err := os.MkdirAll(gap, 0o750); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, "preflight",
		filepath.Join(tree, "alpha"), gap, filepath.Join(tree, "beta"))

	var exit root.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("an unloadable directory must still gate: %v\n%s", err, out)
	}
	for _, want := range []string{"alpha", "not-a-skill", "beta", "cannot be read as a skill"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q; the batch was discarded:\n%s", want, out)
		}
	}
	if strings.Contains(out, "USAGE") {
		t.Errorf("a missing SKILL.md was reported as a usage error:\n%s", out)
	}
}
