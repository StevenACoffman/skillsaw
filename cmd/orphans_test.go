package cmd_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenACoffman/skillsaw/cmd/root"
)

// writeGraph builds a skill tree from "slug: kind:target, kind:target" specs, writing each
// skill's edges as a Related Skills section in the bullet dialect skillet renders.
func writeGraph(t *testing.T, specs ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, spec := range specs {
		slug, edges, _ := strings.Cut(spec, ":")
		slug = strings.TrimSpace(slug)
		body := "---\nname: " + slug + "\ndescription: Use when demoing " + slug +
			".\n---\n\n# " + slug + "\n\nbody\n"
		if strings.TrimSpace(edges) != "" {
			body += "\n## Related Skills\n\n"
			var bodySb25 strings.Builder
			for _, e := range strings.Split(edges, ",") {
				kind, target, _ := strings.Cut(strings.TrimSpace(e), ":")
				bodySb25.WriteString("- **" + kind + "** → `" + target + "`: because.\n")
			}
			body += bodySb25.String()
		}
		sd := filepath.Join(dir, slug)
		if err := os.MkdirAll(sd, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sd, "SKILL.md"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestOrphansSeesADemotionABooleanGateCannot is the reason this check tiers coverage rather
// than answering yes or no. Both trees leave "b" covered, so a gate asking only "does
// anything point at it" reports nothing, while a real use relationship was dropped —
// composes-with says two skills are used together, contrasts-with only names an alternative.
//
// Demotion is also the likelier way a skill decays: not deleted, rewritten.
func TestOrphansSeesADemotionABooleanGateCannot(t *testing.T) {
	t.Parallel()
	base := writeGraph(t, "a: composes-with:b", "b:")
	cur := writeGraph(t, "a: contrasts-with:b", "b:")

	out, err := run(t, "orphans", "--tree", cur, "--base-tree", base)
	if err != nil {
		t.Fatalf("advisory findings must not fail the run: %v\n%s", err, out)
	}
	for _, want := range []string{"b", "weakened-inbound-edge", "composes-with", "contrasts-with"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "newly-orphaned") {
		t.Errorf("a demotion was reported as an orphaning:\n%s", out)
	}
}

// TestOrphansDoesNotReportThePreexisting is the rule that decides whether this is usable at
// all. 135 of 285 skills in the real corpus have no inbound edge; a check that opened by
// reporting all of them would be switched off before it caught the one that mattered.
func TestOrphansDoesNotReportThePreexisting(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ base, cur []string }{
		"orphaned on both sides": {
			[]string{"a:", "b:"},
			[]string{"a:", "b:"},
		},
		"a new skill arriving covered": {
			[]string{"a:"},
			[]string{"a: depends-on:b", "b:"},
		},
		"a skill deleted outright is gone, not orphaned": {
			[]string{"a: depends-on:b", "b:"},
			[]string{"a:"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out, err := run(t, "orphans",
				"--tree", writeGraph(t, tc.cur...),
				"--base-tree", writeGraph(t, tc.base...))
			if err != nil {
				t.Fatalf("unexpected failure: %v\n%s", err, out)
			}
			if !strings.Contains(out, "nothing was disconnected or demoted") {
				t.Errorf("a pre-existing state was reported as a regression:\n%s", out)
			}
		})
	}
}

// TestOrphansIsAdvisoryUntilPromoted pins the exit rule to the severity rather than to a
// second decision that could disagree with it. An intentional removal legitimately orphans
// something, so the default cannot be a failure.
func TestOrphansIsAdvisoryUntilPromoted(t *testing.T) {
	t.Parallel()
	base := writeGraph(t, "a: depends-on:b", "b:")
	cur := writeGraph(t, "a:", "b:")

	out, err := run(t, "orphans", "--tree", cur, "--base-tree", base)
	if err != nil {
		t.Fatalf("the default must not fail the run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "newly-orphaned") {
		t.Fatalf("the regression was not reported at all:\n%s", out)
	}

	out, err = run(t, "orphans", "--tree", cur, "--base-tree", base, "--strict")
	var exit root.ExitError
	if !errors.As(err, &exit) {
		t.Errorf("--strict did not fail on a regression: %v\n%s", err, out)
	}
}

// TestOrphansSurveyCannotBeMistakenForAGate keeps the no-baseline mode honest. It answers a
// question worth asking and would be a terrible gate at 47% orphaned, so what must hold is
// that it neither fails nor phrases anything as a regression.
func TestOrphansSurveyCannotBeMistakenForAGate(t *testing.T) {
	t.Parallel()
	out, err := run(t, "orphans", "--tree", writeGraph(t, "a: depends-on:b", "b:", "c:"))
	if err != nil {
		t.Fatalf("a survey failed the run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "no baseline") {
		t.Errorf("the survey did not say it had nothing to compare against:\n%s", out)
	}
	for _, absent := range []string{"newly-orphaned", "weakened"} {
		if strings.Contains(out, absent) {
			t.Errorf("a survey used regression language (%q):\n%s", absent, out)
		}
	}
}
