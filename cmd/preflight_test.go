package cmd_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
