package cmd_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenACoffman/skillsaw/cmd/root"
)

// tpCases builds a test-prompts.json body with n targets that share the skill's
// description vocabulary and n decoys that share none of it, so every case classifies
// correctly and only the sample size varies.
func tpCases(n int) string {
	cases := make([]string, 0, 2*n)
	for i := range n {
		cases = append(cases,
			fmt.Sprintf(`{"id":%d,"type":"should_trigger",`+
				`"prompt":"please demo the thing needed here","expected":"it activates"}`, i+1),
			fmt.Sprintf(`{"id":%d,"type":"should_not_trigger",`+
				`"prompt":"unrelated quarterly payroll spreadsheet","expected":"quiet"}`, n+i+1),
		)
	}
	return `{"tests":[` + strings.Join(cases, ",") + `]}`
}

// TestActivationGatesOnTheBoundNotThePointEstimate is the behaviour change L967 asked
// for, checked through the real CLI. Both trees classify every prompt correctly, so the
// point estimate is identical and positive; only the sample size differs. Gating on the
// estimate would pass both, which is the failure -- a two-prompt sample cannot tell a
// working trigger from a lucky one.
func TestActivationGatesOnTheBoundNotThePointEstimate(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name      string
		n         int
		wantExit  bool
		wantInOut string
	}{
		{"a sample too small to resolve", 1, true, "UNRESOLVED"},
		{"a sample large enough", 30, false, "clears"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			tree := makeTree(t, "router")
			writeTP(t, tree, "router", tpCases(c.n))
			out, err := run(t, "activation", tree+"/router")

			var exit root.ExitError
			switch {
			case c.wantExit && !errors.As(err, &exit):
				t.Fatalf("an unresolved gate must exit non-zero; got %v\n%s", err, out)
			case !c.wantExit && err != nil:
				t.Fatalf("a resolved pass was rejected: %v\n%s", err, out)
			}
			if !strings.Contains(out, c.wantInOut) {
				t.Errorf("output does not report %q:\n%s", c.wantInOut, out)
			}
			if !strings.Contains(out, "net_utility +") {
				t.Errorf("both cases classify everything correctly, so the point estimate "+
					"must be positive -- otherwise this test is not about the bound:\n%s", out)
			}
		})
	}
}

// TestActivationExplainsWhatToDoAboutAnUnresolvedGate covers the operator-facing half. A
// non-zero exit that does not distinguish "this skill is bad" from "your sample is too
// small" sends the reader to the wrong fix.
func TestActivationExplainsWhatToDoAboutAnUnresolvedGate(t *testing.T) {
	t.Parallel()
	tree := makeTree(t, "router")
	writeTP(t, tree, "router", tpCases(1))
	out, _ := run(t, "activation", tree+"/router")
	for _, want := range []string{"UNRESOLVED", "bound [", "against min +0.00", "Add test-prompts."} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
}

// TestActivationJSONCarriesTheGate keeps the machine-readable path in step with the text
// one, since a CI job reading JSON would otherwise see the old shape and its own idea of
// whether the run passed.
func TestActivationJSONCarriesTheGate(t *testing.T) {
	t.Parallel()
	tree := makeTree(t, "router")
	writeTP(t, tree, "router", tpCases(1))
	out, _ := run(t, "activation", "--json", tree+"/router")
	for _, want := range []string{`"gate": "unresolved"`, `"net_utility_bound"`} {
		if !strings.Contains(out, want) {
			t.Errorf("JSON output is missing %s:\n%s", want, out)
		}
	}
}

// TestOneGapDoesNotDiscardTheBatch is the defect this closes. A corpus run over sixty
// skills used to be aborted by the first one missing a file, throwing away every result
// already computed — and it aborted with a usage dump, as though the caller had mistyped
// something.
func TestOneGapDoesNotDiscardTheBatch(t *testing.T) {
	t.Parallel()
	tree := makeTree(t, "scored", "noprompts")
	writeTP(t, tree, "scored", tpCases(30))
	missing := filepath.Join(t.TempDir(), "not-a-skill")
	if err := os.MkdirAll(missing, 0o750); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, "activation",
		filepath.Join(tree, "scored"), filepath.Join(tree, "noprompts"), missing)

	var exit root.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("a run containing unmeasured skills must still gate: %v\n%s", err, out)
	}
	for _, want := range []string{
		"scored: net_utility",   // the measurable one was measured
		"noprompts: UNMEASURED", // and the gaps are named
		"no test-prompts.json",  // each by its own cause
		"not-a-skill: UNMEASURED",
		"no readable SKILL.md",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "USAGE") {
		t.Errorf("a missing file was reported as a usage error:\n%s", out)
	}
}

// TestAnUnmeasuredSkillIsNotAPass keeps the third state from becoming a loophole. Nobody
// looked, so the run cannot go green — but it says so after reporting, not instead of it.
func TestAnUnmeasuredSkillIsNotAPass(t *testing.T) {
	t.Parallel()
	tree := makeTree(t, "noprompts")
	out, err := run(t, "activation", filepath.Join(tree, "noprompts"))
	var exit root.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("a skill nobody could measure passed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "UNMEASURED") {
		t.Errorf("output does not say the skill was unmeasured:\n%s", out)
	}
}

// writeSkill puts a SKILL.md with the given frontmatter body into its own directory,
// alongside prompts that would score if there were a description to score them against.
func writeSkill(t *testing.T, frontmatter string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "s")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	doc := "---\n" + frontmatter + "---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	// Enough cases to clear the noise floor, so a scoreable skill genuinely passes and
	// this fixture tests the description guard rather than the sample size.
	if err := os.WriteFile(filepath.Join(dir, "test-prompts.json"),
		[]byte(tpCases(30)), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestAnUnscoreableDescriptionIsNotScored is the defect this closes. Trigger accuracy is
// vocabulary overlap, and without a description every prompt is compared against the empty
// string: the matrix comes out full, reads exactly like one computed over a real
// description, and explains each miss as "description vocabulary misses it" — the opposite
// of what happened.
//
// The two causes are separate rows because they were separately measured over 60 book
// skills (22 unparsed, 8 parsed-but-empty) and because a guard on the parse error alone
// would have left the second scoring silently.
func TestAnUnscoreableDescriptionIsNotScored(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		frontmatter string
		wantReason  string
	}{
		"frontmatter did not parse": {
			// A quoted scalar followed by unquoted text: the shape the books corpus
			// carries in source_book, and the one dim 1's own test already names.
			"name: s\nsource_book: \"Legacy Code\" by Feathers (2005) + \"Talks\"\n" +
				"description: Use when the demo thing is needed here.\n",
			"frontmatter did not parse",
		},
		"description is empty": {
			"name: s\ndescription: \"\"\n", "no description to score",
		},
		"description is only whitespace": {
			"name: s\ndescription: \"   \"\n", "no description to score",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out, err := run(t, "activation", writeSkill(t, tc.frontmatter))
			var exit root.ExitError
			if !errors.As(err, &exit) {
				t.Fatalf("an unscoreable skill passed: %v\n%s", err, out)
			}
			if !strings.Contains(out, "UNMEASURED — "+tc.wantReason) {
				t.Errorf("output does not name the cause %q:\n%s", tc.wantReason, out)
			}
			for _, absent := range []string{"net_utility", "TPR", "description vocabulary"} {
				if strings.Contains(out, absent) {
					t.Errorf("a matrix computed from no description was reported (%q):\n%s",
						absent, out)
				}
			}
		})
	}
}

// TestAReadableDescriptionStillScores is the other half: the guard must not swallow the
// skills it exists to protect the reading of.
func TestAReadableDescriptionStillScores(t *testing.T) {
	t.Parallel()
	out, err := run(t, "activation",
		writeSkill(t, "name: s\ndescription: please demo the thing needed here\n"))
	if err != nil {
		t.Fatalf("a scoreable skill was refused: %v\n%s", err, out)
	}
	if !strings.Contains(out, "net_utility") {
		t.Errorf("a skill with a real description was not scored:\n%s", out)
	}
}

// TestNoDecoyCaveatWithoutADescription is L1737: the "write harder decoys" advice is
// correct only for a report scored against a real description, and an author following it
// on one of these would write harder decoys and watch the number not move.
//
// Two independent things prevent it, which is worth knowing because neither alone is
// obvious from the code. emit returns before the caveat for an unmeasured skill; and
// scoreDir returns before scoring, so the report carries no counts and EvidenceIn reports
// "nothing here was measured" rather than anything about decoys. Removing either leaves
// the property intact — measured by planting both — so this test pins the user-visible
// outcome and deliberately does not claim to guard a particular mechanism.
func TestNoDecoyCaveatWithoutADescription(t *testing.T) {
	t.Parallel()
	out, _ := run(t, "activation", writeSkill(t, "name: s\ndescription: \"\"\n"))
	if strings.Contains(out, "harder decoys") {
		t.Errorf("an author was told to write harder decoys when the description is "+
			"empty and nothing could have fired:\n%s", out)
	}
}
