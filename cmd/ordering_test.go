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

// refusedTranscript writes the shape 19 of 27 captured runs had: the agent asks for the
// skill and the runtime turns it down, twice, then works anyway. Verbatim causes, because
// the three doors are the finding and a paraphrase would lose which one was shut.
func refusedTranscript(t *testing.T) string {
	t.Helper()
	doc := `{"type":"tool_use","tool_name":"read_file","tool_id":"a",` +
		`"parameters":{"file_path":"/Users/steve/.agents/skills/brainstorming/SKILL.md"}}
{"type":"tool_result","tool_id":"a","status":"error","output":"Path not in workspace"}
{"type":"tool_use","tool_name":"run_shell_command","tool_id":"b",` +
		`"parameters":{"command":"cat /Users/steve/.agents/skills/brainstorming/SKILL.md"}}
{"type":"tool_result","tool_id":"b","status":"error","output":"Tool execution denied by policy. You are in Plan Mode."}
{"type":"tool_use","tool_name":"write_file","tool_id":"c","parameters":{"file_path":"main.go"}}
{"type":"tool_result","tool_id":"c","status":"success","output":"written"}
`
	p := filepath.Join(t.TempDir(), "transcript.json")
	if err := os.WriteFile(p, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestARefusedRunIsNotReportedAsASkillDefect is the operator-facing half of the
// unmeasurable verdict. Reporting "NOT TRIGGERED" here sends the author to rewrite a
// description that was never read, and it was the reported outcome for 19 of 27 runs.
func TestARefusedRunIsNotReportedAsASkillDefect(t *testing.T) {
	t.Parallel()
	out, err := run(t, "ordering", "--skill", "brainstorming", refusedTranscript(t))

	var exit root.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("a run that never loaded the skill passed: %v\n%s", err, out)
	}
	for _, want := range []string{
		"NOT MEASURABLE",
		"read_file", "run_shell_command", // which doors were shut
		"Nothing here is a fact about the skill",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
	for _, absent := range []string{"NOT TRIGGERED", "first —"} {
		if strings.Contains(out, absent) {
			t.Errorf("a refused run was reported as %q:\n%s", absent, out)
		}
	}
}

// TestANewVerdictDoesNotRenderAsAPass guards the shape of emit rather than any one verdict.
// The switch used to end in a default that printed "first", so adding an Order and
// forgetting this function showed every reader a pass. The check is that the two spellings
// of success are reachable only from OrderFirst, which is now an explicit case.
func TestANewVerdictDoesNotRenderAsAPass(t *testing.T) {
	t.Parallel()
	for name, path := range map[string]string{
		"a refused run":     refusedTranscript(t),
		"a run that worked": writeTranscript(t, "Bash", "Edit"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out, _ := run(t, "ordering", "--skill", "brainstorming", path)
			if strings.Contains(out, "loaded before any work") {
				t.Errorf("a non-first verdict rendered as a pass:\n%s", out)
			}
		})
	}
}

// TestAgreementNamesTheCause is L2210. Before has named the tools that ran first since it
// was written, and --agreement -- the view run.sh prints for every repeated run -- showed
// the tally alone, so it reached nobody. Three misreadings this week were settled by
// opening a transcript to find what this line can now say.
func TestAgreementNamesTheCause(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	copyTo := func(name, src string) {
		t.Helper()
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	copyTo("03-imperative.1.json", writeTranscript(t, "Bash", "Skill:brainstorming"))
	copyTo("03-imperative.2.json", writeTranscript(t, "Bash", "Skill:brainstorming"))
	copyTo("05-refused.1.json", refusedTranscript(t))
	copyTo("01-bare.1.json", writeTranscript(t, "Skill:brainstorming"))
	copyTo("01-bare.2.json", writeTranscript(t, "Skill:brainstorming"))

	out, _ := run(t, "ordering", "--skill", "brainstorming", "--agreement",
		filepath.Join(dir, "01-bare.1.json"), filepath.Join(dir, "01-bare.2.json"),
		filepath.Join(dir, "03-imperative.1.json"), filepath.Join(dir, "03-imperative.2.json"),
		filepath.Join(dir, "05-refused.1.json"))

	for _, want := range []string{
		"after-action in all 2 runs (Bash)",      // what ran first
		"unmeasurable (1 run — not repeated) (r", // which door was shut
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
	// A phrasing with nothing to explain must not grow an empty bracket.
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "01-bare") && strings.Contains(line, "(") {
			t.Errorf("a clean phrasing was given a cause: %q", line)
		}
	}
}

// TestAnUnreadableRunIsNamedNotBlank is the defect this closes. judge leaves Order at its
// zero value when the file cannot be read, and the agreement view tallies Order — so a run
// that could not be scored printed as "2 ", a count followed by nothing where a verdict
// goes. Two 17MB transcripts over the line cap produced exactly that, and it read as a
// rendering glitch rather than as two runs nobody scored.
func TestAnUnreadableRunIsNamedNotBlank(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	good := writeTranscript(t, "Skill:brainstorming")
	b, err := os.ReadFile(good)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "03-imperative.1.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}

	out, _ := run(t, "ordering", "--skill", "brainstorming", "--agreement",
		filepath.Join(dir, "03-imperative.1.json"),
		filepath.Join(dir, "03-imperative.2.json")) // never written: unreadable

	if !strings.Contains(out, "unreadable") {
		t.Errorf("an unscoreable run was not named:\n%s", out)
	}
	// The shape of the bug: a count with nothing after it, mid-tally or at line end.
	for _, blank := range []string{"1 ,", "1 \n"} {
		if strings.Contains(out, blank) {
			t.Errorf("a verdict rendered as blank (%q):\n%s", blank, out)
		}
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
