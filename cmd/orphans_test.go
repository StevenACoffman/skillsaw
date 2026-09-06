package cmd_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenACoffman/skillsaw/cmd/root"
	"github.com/StevenACoffman/skillsaw/internal/inventory"
)

// recordedManifest writes a manifest describing tree, through the real producer.
//
// Hand-building the edges would be a fixture that can encode a graph the parser never
// produces, which is the drift the shared reader exists to prevent — so the file under
// test is written by the same code that writes a real one.
//
// stripped drops the edges and the flag, standing in for a manifest from a producer that
// predates the field. It is the negative control, not a variant: the whole question is
// whether the consumer can tell that document from a tree with no edges in it.
func recordedManifest(t *testing.T, tree string, stripped bool) string {
	t.Helper()
	m, err := inventory.Tree(tree)
	if err != nil {
		t.Fatalf("inventory.Tree: %v", err)
	}
	m.Tool = "skillsaw"
	if stripped {
		m.EdgesRecorded = false
		for i := range m.Skills {
			m.Skills[i].Edges = nil
		}
	}
	b, err := m.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "skills-manifest.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

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

// TestOrphansReadsABaselineOutOfAManifest is the path the recorded edges exist for: the
// baseline is a published artifact and the checkout is gone. It asserts the same demotion
// the two-tree test does, so the two baselines are shown to answer the same question
// rather than merely both producing output.
func TestOrphansReadsABaselineOutOfAManifest(t *testing.T) {
	t.Parallel()
	base := recordedManifest(t, writeGraph(t, "a: composes-with:b", "b:"), false)
	cur := writeGraph(t, "a: contrasts-with:b", "b:")

	out, err := run(t, "orphans", "--tree", cur, "--base-manifest", base)
	if err != nil {
		t.Fatalf("advisory findings must not fail the run: %v\n%s", err, out)
	}
	for _, want := range []string{"b", "weakened-inbound-edge", "composes-with", "contrasts-with"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
}

// TestOrphansRefusesAManifestThatRecordedNoGraph is the negative control, and it is the
// one that matters most: a manifest whose producer never read the graph is byte-identical
// to one describing a tree with no edges at all. A consumer that cannot tell them apart
// prints "nothing was disconnected or demoted" about a baseline that never existed — a
// green report over a question nobody asked, which is the failure this file records
// against itself four times.
//
// The scenario is a real orphaning, so a fallback-to-empty implementation would report a
// clean run here and pass every other test in this file.
func TestOrphansRefusesAManifestThatRecordedNoGraph(t *testing.T) {
	t.Parallel()
	tree := writeGraph(t, "a: depends-on:b", "b:")
	cur := writeGraph(t, "a:", "b:")

	out, err := run(t, "orphans", "--tree", cur, "--base-manifest", recordedManifest(t, tree, true))
	if err == nil {
		t.Fatalf("a manifest with no recorded graph was accepted as a baseline:\n%s", out)
	}
	if strings.Contains(out, "nothing was disconnected or demoted") {
		t.Errorf("an unrecorded graph was reported as a clean comparison:\n%s", out)
	}
	// The message has to name the repair. "Unavailable" sends the reader hunting through a
	// tree for a graph defect that is in the producer.
	if !strings.Contains(err.Error(), "did not record skill edges") {
		t.Errorf("the refusal does not name the cause: %v", err)
	}
}

// TestOrphansRefusesTwoBaselines keeps the choice with the caller. The two are not equally
// trustworthy — one is re-read by this parser, the other was recorded by another tool at
// another version — so a precedence rule would silently decide which answer is given.
func TestOrphansRefusesTwoBaselines(t *testing.T) {
	t.Parallel()
	tree := writeGraph(t, "a: depends-on:b", "b:")
	out, err := run(t, "orphans",
		"--tree", tree,
		"--base-tree", tree,
		"--base-manifest", recordedManifest(t, tree, false))
	if err == nil {
		t.Fatalf("two baselines were accepted, so one of them was silently ignored:\n%s", out)
	}
}

// TestOrphansSeesADemotionTheTierCannot is the gap the tier left, and the reason a
// per-edge grain exists beside the per-target one. Two skills point at "c"; one demotes.
// "c" is still covered at depends-on by the other, so its strongest inbound edge has not
// moved and no Change is produced — yet the corpus lost a use relationship.
//
// This is not hypothetical: it is what happened on the real corpus when
// go-beyond-packages-as-layers' composes-with edge was demoted and the run reported
// nothing, because two other skills still pointed at the target with depends-on.
func TestOrphansSeesADemotionTheTierCannot(t *testing.T) {
	t.Parallel()
	base := writeGraph(t, "a: composes-with:c", "b: depends-on:c", "c:")
	cur := writeGraph(t, "a: contrasts-with:c", "b: depends-on:c", "c:")

	out, err := run(t, "orphans", "--tree", cur, "--base-tree", base)
	if err != nil {
		t.Fatalf("advisory findings must not fail the run: %v\n%s", err, out)
	}
	// The finding is keyed on "a" — the file to open. A target-keyed finding could not
	// name it, which is half the reason the grain is worth having.
	for _, want := range []string{"a: demoted-outbound-edge", "composes-with", "contrasts-with"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
	// The target-level view is genuinely unmoved, which is what makes this the gap
	// rather than a duplicate of the weakening finding.
	if strings.Contains(out, "weakened-inbound-edge") {
		t.Errorf("the target's strongest edge did not move; nothing should say it did:\n%s", out)
	}
	if strings.Contains(out, "nothing was disconnected or demoted") {
		t.Errorf("a real demotion was reported as a clean run:\n%s", out)
	}
	// It is a regression, so --strict must fail on it; that is the whole use of the grain.
	if _, err = run(t, "orphans", "--tree", cur, "--base-tree", base, "--strict"); err == nil {
		t.Error("--strict passed over a demoted edge")
	}
}

// TestOrphansDoesNotCallADeletionADemotion keeps the deletion legible. A skill that is
// removed takes its edges with it; listing each of them as a demotion would bury the one
// fact that matters under a list of relationships that went with it.
func TestOrphansDoesNotCallADeletionADemotion(t *testing.T) {
	t.Parallel()
	base := writeGraph(t, "a: composes-with:c", "b: depends-on:c", "c:")
	cur := writeGraph(t, "b: depends-on:c", "c:") // "a" is gone entirely

	out, err := run(t, "orphans", "--tree", cur, "--base-tree", base)
	if err != nil {
		t.Fatalf("unexpected failure: %v\n%s", err, out)
	}
	if strings.Contains(out, "demoted-outbound-edge") {
		t.Errorf("a deleted skill's edges were reported as demotions:\n%s", out)
	}
}

// TestOrphansAlwaysSaysHowMuchOfTheGraphItRead is the caveat made structural. An orphan
// count is only as good as the graph beneath it, and the readable graph of the real corpus
// moved from 222 to 357 edges in one day — so the two figures have to arrive together or
// the verdict gets quoted without them.
//
// Both modes are checked because the survey is the one most likely to be read as a fact
// about the corpus rather than about the parser.
func TestOrphansAlwaysSaysHowMuchOfTheGraphItRead(t *testing.T) {
	t.Parallel()
	// "a" points at "gone", which is not in the tree: read, and covering nothing.
	tree := writeGraph(t, "a: depends-on:b, informs:gone", "b:")
	cases := map[string][]string{
		"survey":     {"orphans", "--tree", tree},
		"comparison": {"orphans", "--tree", tree, "--base-tree", tree},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out, err := run(t, args...)
			if err != nil {
				t.Fatalf("unexpected failure: %v\n%s", err, out)
			}
			want := "read 2 skills and 2 edges, 1 of which name a slug not in this tree."
			if !strings.Contains(out, want) {
				t.Errorf("missing the coverage line %q:\n%s", want, out)
			}
			// Saying the number without saying it is a floor invites reading it as a total.
			if !strings.Contains(out, "a floor") {
				t.Errorf("the coverage line does not say it is a floor:\n%s", out)
			}
		})
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
