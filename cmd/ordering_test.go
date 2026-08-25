package cmd_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenACoffman/skillsaw/cmd/root"
)

// writeTranscript writes a stream-json transcript of the given tool uses. A name of the
// form "Skill:x" is a Skill invocation loading x.
func writeTranscript(t *testing.T, tools ...string) string {
	t.Helper()
	var lines []string
	for _, tool := range tools {
		if name, skill, ok := strings.Cut(tool, ":"); ok {
			lines = append(lines, `{"type":"assistant","message":{"content":[{"type":"tool_use",`+
				`"name":"`+name+`","input":{"skill":"`+skill+`"}}]}}`)
			continue
		}
		lines = append(lines, `{"type":"tool_use","name":"`+tool+`","input":{}}`)
	}
	p := filepath.Join(t.TempDir(), "transcript.json")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestOrderingSeparatesTriggeredFromTriggeredInTime is the item's whole point, through the
// real CLI. The first two transcripts both end with the skill loaded and the work done; a
// verdict read off the reply would call both a success.
func TestOrderingSeparatesTriggeredFromTriggeredInTime(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		tools    []string
		wantExit bool
		wantIn   string
	}{
		"loaded before any work": {
			[]string{"Skill:brainstorming", "Bash"}, false, "first",
		},
		"loaded after work began": {
			[]string{"Bash", "Edit", "Skill:brainstorming"}, true, "AFTER ACTION",
		},
		"never loaded": {
			[]string{"Bash"}, true, "NOT TRIGGERED",
		},
		"planning first is not work": {
			[]string{"TodoWrite", "TaskCreate", "Skill:brainstorming"}, false, "first",
		},
		"namespaced invocation matches": {
			[]string{"Skill:superpowers:brainstorming"}, false, "first",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out, err := run(t, "ordering", "--skill", "brainstorming",
				writeTranscript(t, tc.tools...))
			var exit root.ExitError
			switch {
			case tc.wantExit && !errors.As(err, &exit):
				t.Fatalf("a non-first verdict did not gate: %v\n%s", err, out)
			case !tc.wantExit && err != nil:
				t.Fatalf("a clean transcript was rejected: %v\n%s", err, out)
			}
			if !strings.Contains(out, tc.wantIn) {
				t.Errorf("output does not say %q:\n%s", tc.wantIn, out)
			}
		})
	}
}

// TestOrderingNamesWhatRanFirst covers the operator-facing half: "work happened before the
// skill" without saying what happened leaves the reader nothing to act on, and the repair
// differs depending on whether the agent read a file or started editing one.
func TestOrderingNamesWhatRanFirst(t *testing.T) {
	t.Parallel()
	out, _ := run(t, "ordering", "--skill", "brainstorming",
		writeTranscript(t, "TodoWrite", "Bash", "Edit", "Skill:brainstorming"))
	for _, want := range []string{"Bash", "Edit"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not name %q as having run first:\n%s", want, out)
		}
	}
	if strings.Contains(out, "TodoWrite") {
		t.Errorf("a planning tool was reported as work:\n%s", out)
	}
}

// TestOneUnreadableTranscriptDoesNotDiscardTheRest matches the rule the rest of the tool
// follows: a gap in one input says nothing about the others.
func TestOneUnreadableTranscriptDoesNotDiscardTheRest(t *testing.T) {
	t.Parallel()
	good := writeTranscript(t, "Skill:brainstorming")
	out, err := run(t, "ordering", "--skill", "brainstorming",
		good, filepath.Join(t.TempDir(), "absent.json"))
	var exit root.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("an unreadable transcript must still gate: %v\n%s", err, out)
	}
	if !strings.Contains(out, "first") || !strings.Contains(out, "UNREAD") {
		t.Errorf("both transcripts should be reported:\n%s", out)
	}
}

// TestOrderingNeedsASkill pins the required flag: with nothing to look for there is no
// question to answer, and defaulting to something would invent one.
func TestOrderingNeedsASkill(t *testing.T) {
	t.Parallel()
	out, err := run(t, "ordering", writeTranscript(t, "Bash"))
	if err == nil {
		t.Fatalf("ran with no --skill:\n%s", out)
	}
	if !strings.Contains(err.Error(), "--skill is required") {
		t.Errorf("error does not say what is missing: %v", err)
	}
}

// TestAgreementReportsTheSplitNotTheMajority is the bindingness metric through the real
// CLI. Three firsts and two after-actions is not a pass with noise: when guidance lands,
// repetitions converge, so a phrasing whose runs disagree is one the skill does not
// reliably survive — and a majority verdict would report it as clean.
func TestAgreementReportsTheSplitNotTheMajority(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name string, tools ...string) {
		t.Helper()
		src := writeTranscript(t, tools...)
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []string{"1", "2", "3"} {
		write("03-imperative."+r+".json", "Skill:brainstorming")
	}
	for _, r := range []string{"4", "5"} {
		write("03-imperative."+r+".json", "Bash", "Skill:brainstorming")
	}
	write("01-bare.1.json", "Skill:brainstorming")
	write("01-bare.2.json", "Skill:brainstorming")

	out, _ := run(t, "ordering", "--skill", "brainstorming", "--agreement",
		filepath.Join(dir, "01-bare.1.json"), filepath.Join(dir, "01-bare.2.json"),
		filepath.Join(dir, "03-imperative.1.json"), filepath.Join(dir, "03-imperative.2.json"),
		filepath.Join(dir, "03-imperative.3.json"), filepath.Join(dir, "03-imperative.4.json"),
		filepath.Join(dir, "03-imperative.5.json"))

	if !strings.Contains(out, "SPLIT over 5 runs") {
		t.Errorf("the disagreeing phrasing was not reported as split:\n%s", out)
	}
	if !strings.Contains(out, "3 first") || !strings.Contains(out, "2 after-action") {
		t.Errorf("the tally does not show both outcomes:\n%s", out)
	}
	if !strings.Contains(out, "first in all 2 runs") {
		t.Errorf("the agreeing phrasing was not reported as unanimous:\n%s", out)
	}
}

// TestASingleRunIsNotConvergence keeps one observation from reading as a property. It is
// reported, and qualified, rather than counted as agreement.
func TestASingleRunIsNotConvergence(t *testing.T) {
	t.Parallel()
	out, _ := run(t, "ordering", "--skill", "brainstorming", "--agreement",
		writeTranscript(t, "Skill:brainstorming"))
	if !strings.Contains(out, "not repeated") {
		t.Errorf("a single run was not qualified:\n%s", out)
	}
	if strings.Contains(out, "in all 1 runs") {
		t.Errorf("a single run was presented as convergence:\n%s", out)
	}
}
