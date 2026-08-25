package transcript_test

import (
	"strings"
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/transcript"
)

// The two shapes a tool_use has been observed or expected in. The nested one is what an
// assistant message looks like; the flat one is what the reference harness's greps imply.
// Neither has been checked against a real capture, which is why the reader accepts both.
const (
	nested = `{"type":"assistant","message":{"content":[` +
		`{"type":"text","text":"working on it"},` +
		`{"type":"tool_use","name":"Bash","input":{"command":"ls"}},` +
		`{"type":"tool_use","name":"Skill","input":{"skill":"superpowers:brainstorming"}}]}}`
	flat = `{"type":"tool_use","name":"Skill","skill":"brainstorming"}`
)

// geminiRun is a verbatim excerpt from a real `gemini -p ... -o stream-json` run, kept as
// the fixture because the dialect differs from Claude Code's in three ways at once and a
// hand-written approximation would drift from all three: the tool name is under
// "tool_name", the arguments under "parameters", and the events are flat rather than nested
// in an assistant message.
const geminiRun = `{"type":"init","session_id":"dd58","model":"auto"}
{"type":"message","role":"user","content":"climax-cli-scaffold, please"}
{"type":"tool_use","tool_name":"update_topic","tool_id":"t1","parameters":{"title":"Activating climax-cli-scaffold Skill","summary":"reading the skill"}}
{"type":"tool_use","tool_name":"read_file","tool_id":"t2","parameters":{"file_path":"/Users/steve/.gemini/skills/climax-cli-scaffold/SKILL.md"}}
{"type":"result","status":"success","stats":{"tool_calls":2}}`

// TestReadFindsToolUsesInEitherShape is the reason the reader walks instead of indexing.
// The event shape here is inferred from a shell harness's grep patterns, so a reader bound
// to one JSON path would be betting on a schema nobody has seen.
func TestReadFindsToolUsesInEitherShape(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		doc   string
		want  []string // tool names, in order
		skill string   // skill of the last tool use, if any
	}{
		"nested under an assistant message": {
			nested,
			[]string{"Bash", "Skill"},
			"superpowers:brainstorming",
		},
		"flat at the top level": {flat, []string{"Skill"}, "brainstorming"},
		"both, in order": {
			nested + "\n" + flat,
			[]string{"Bash", "Skill", "Skill"},
			"brainstorming",
		},
		"empty transcript": {"", nil, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertToolUses(t, tc.doc, tc.want, tc.skill)
		})
	}
}

// TestANonJSONLineIsSkipped keeps a log readable. The harness writing these also echoes
// progress to the same stream, and one banner line must not discard every event after it.
func TestANonJSONLineIsSkipped(t *testing.T) {
	t.Parallel()
	doc := "=== Explicit Skill Request Test ===\n" + flat + "\nnot json either\n"
	got, err := transcript.Read(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("Read() = %v", err)
	}
	if len(got) != 1 || got[0].Name != "Skill" {
		t.Errorf("got %+v, want the one Skill invocation", got)
	}
}

// TestLoadsSkillIgnoresTheNamespace pins the matching rule. A caller asks by the name the
// skill declares; which plugin surfaced it is a fact about the runtime's configuration, not
// part of the skill's identity.
func TestLoadsSkillIgnoresTheNamespace(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		got, ask string
		want     bool
	}{
		"exact":                 {"brainstorming", "brainstorming", true},
		"namespaced":            {"superpowers:brainstorming", "brainstorming", true},
		"different skill":       {"superpowers:other", "brainstorming", false},
		"prefix is not a match": {"brainstorming-extra", "brainstorming", false},
		"no skill on this tool": {"", "brainstorming", false},
		"asking for nothing":    {"brainstorming", "", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := (transcript.ToolUse{Skill: tc.got}).LoadsSkill(tc.ask); got != tc.want {
				t.Errorf("ToolUse{Skill:%q}.LoadsSkill(%q) = %v, want %v",
					tc.got, tc.ask, got, tc.want)
			}
		})
	}
}

// assertToolUses reads a transcript and checks the tools it found, in order, plus the skill
// on the last one when the case names one.
func assertToolUses(t *testing.T, doc string, want []string, skill string) {
	t.Helper()
	got, err := transcript.Read(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("Read() = %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d tool uses %+v, want %v", len(got), got, want)
	}
	for i, w := range want {
		if got[i].Name != w {
			t.Errorf("tool %d = %q, want %q", i, got[i].Name, w)
		}
	}
	if skill != "" && got[len(got)-1].Skill != skill {
		t.Errorf("skill = %q, want %q", got[len(got)-1].Skill, skill)
	}
}

// TestReadUnderstandsGeminisDialect pins the second runtime. Claude Code names the tool in
// "name" and nests events under an assistant message; Gemini CLI names it in "tool_name",
// puts arguments under "parameters", and writes events flat.
func TestReadUnderstandsGeminisDialect(t *testing.T) {
	t.Parallel()
	got, err := transcript.Read(strings.NewReader(geminiRun))
	if err != nil {
		t.Fatalf("Read() = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d tool uses %+v, want 2", len(got), got)
	}
	if got[0].Name != "update_topic" || got[1].Name != "read_file" {
		t.Errorf("tool names = %q, %q; want update_topic, read_file", got[0].Name, got[1].Name)
	}
}

// TestASkillReadCountsAsLoadingIt is the substantive difference between the two runtimes.
// Gemini CLI has no Skill tool: it loads a skill by reading SKILL.md, so the only evidence
// is a path among the arguments. A reader that only knew Claude Code's shape would report
// every Gemini run as never having triggered — a parse failure wearing a verdict's clothes.
func TestASkillReadCountsAsLoadingIt(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		use  transcript.ToolUse
		ask  string
		want bool
	}{
		"read_file on the skill": {
			transcript.ToolUse{
				Name: "read_file",
				Args: []string{"/Users/steve/.gemini/skills/brainstorming/SKILL.md"},
			}, "brainstorming", true,
		},
		"a shell cat of the same path": {
			// Observed: when read_file was refused for being outside the workspace, the
			// agent fell back to catting the file. Same act, different tool.
			transcript.ToolUse{
				Name: "run_shell_command",
				Args: []string{
					"read the skill",
					"cat /Users/steve/.gemini/skills/brainstorming/SKILL.md",
				},
			},
			"brainstorming",
			true,
		},
		"a different skill's file": {
			transcript.ToolUse{
				Name: "read_file",
				Args: []string{"/x/skills/other/SKILL.md"},
			}, "brainstorming", false,
		},
		"a file that merely mentions the name": {
			transcript.ToolUse{
				Name: "read_file",
				Args: []string{"/x/notes/brainstorming.md"},
			}, "brainstorming", false,
		},
		"no arguments at all": {
			transcript.ToolUse{Name: "read_file"}, "brainstorming", false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := tc.use.LoadsSkill(tc.ask); got != tc.want {
				t.Errorf("LoadsSkill(%q) = %v, want %v", tc.ask, got, tc.want)
			}
		})
	}
}

// TestGeminiBookkeepingIsNotAction pins update_topic as planning. It fired immediately
// before the skill read in the captured run, carrying a title and a summary of what the
// agent was about to do; counting it as work would make every Gemini run report
// after-action.
func TestGeminiBookkeepingIsNotAction(t *testing.T) {
	t.Parallel()
	uses, err := transcript.Read(strings.NewReader(geminiRun))
	if err != nil {
		t.Fatal(err)
	}
	if got := transcript.OrderOf(uses, "climax-cli-scaffold"); got != transcript.OrderFirst {
		t.Errorf("OrderOf() = %q, want %q", got, transcript.OrderFirst)
	}
}

// TestActivateSkillIsRecognised covers the gap the first real corpus run exposed. Gemini
// CLI does have a dedicated skill tool — activate_skill — which none of my probes reached,
// because in every captured run the agent got to the skill by reading the file first. A run
// that went straight to activate_skill would have scored not-triggered: a parse failure
// wearing a verdict's clothes, which is the failure this package exists to prevent.
//
// The event is verbatim from that run.
func TestActivateSkillIsRecognised(t *testing.T) {
	t.Parallel()
	const doc = `{"type":"tool_use","timestamp":"2026-08-24T15:44:32.549Z",` +
		`"tool_name":"activate_skill","tool_id":"activate_skill__call_2813803",` +
		`"parameters":{"name":"climax-cli-scaffold"}}`
	got, err := transcript.Read(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("Read() = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d tool uses, want 1", len(got))
	}
	if !got[0].LoadsSkill("climax-cli-scaffold") {
		t.Errorf("activate_skill(name=climax-cli-scaffold) was not read as loading it: %+v", got[0])
	}
	if got[0].LoadsSkill("something-else") {
		t.Error("it matched a skill it did not name")
	}
	if o := transcript.OrderOf(got, "climax-cli-scaffold"); o != transcript.OrderFirst {
		t.Errorf("OrderOf() = %q, want %q", o, transcript.OrderFirst)
	}
}

// TestAGenericNameParameterIsNotASkill is why the "name" parameter is read only for tools
// whose job is loading skills. Plenty of tools take a name that is a file, a session, or a
// variable, and treating any of them as a skill reference would manufacture triggers.
func TestAGenericNameParameterIsNotASkill(t *testing.T) {
	t.Parallel()
	const doc = `{"type":"tool_use","tool_name":"write_file","parameters":{"name":"brainstorming"}}`
	got, err := transcript.Read(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].LoadsSkill("brainstorming") {
		t.Error("a write_file with name=brainstorming was read as loading that skill")
	}
}
