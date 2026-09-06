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

// geminiBatch is 08-buried-in-a-list run 1 against climax-cli-scaffold, trimmed to the tool
// events and their timestamps. It is kept verbatim because the timestamps are the evidence:
// four calls inside 742ms, and every result arriving after the last of them. The run scored
// after-action only because glob was emitted before the skill read, and a second run of the
// same phrasing emitted the same four calls in the other order and scored first.
const geminiBatch = `{"type":"init","session_id":"21b8a657","model":"auto"}
{"type":"tool_use","timestamp":"2026-08-27T06:28:14.401Z","tool_name":"update_topic","parameters":{"title":"Context Gathering for User Requests"}}
{"type":"tool_use","timestamp":"2026-08-27T06:28:14.404Z","tool_name":"glob","parameters":{"pattern":"**/*config*"}}
{"type":"tool_use","timestamp":"2026-08-27T06:28:14.679Z","tool_name":"read_file","parameters":{"file_path":"/Users/steve/.agents/skills/climax-cli-scaffold/SKILL.md"}}
{"type":"tool_use","timestamp":"2026-08-27T06:28:15.143Z","tool_name":"read_file","parameters":{"file_path":"/Users/steve/.gemini/tmp/tmp-vkoaz2id4c/memory/MEMORY.md"}}
{"type":"tool_result","timestamp":"2026-08-27T06:28:15.200Z","tool_id":"update_topic__call_939209","status":"success","output":"topic set"}
{"type":"tool_result","timestamp":"2026-08-27T06:28:15.200Z","tool_id":"glob__call_939210","status":"success","output":"No files found"}
{"type":"tool_use","timestamp":"2026-08-27T06:28:24.561Z","tool_name":"run_shell_command","parameters":{"command":"cat /Users/steve/.agents/skills/climax-cli-scaffold/SKILL.md"}}`

// claudeBatch is the same situation in Claude Code's shape: one assistant message carrying
// two tool_use blocks, the user message returning their results, then a second assistant
// message. Both runtimes have to number batches the same way, and the events sit at
// different depths in each, so one fixture cannot stand for both.
const claudeBatch = `{"type":"assistant","message":{"content":[` +
	`{"type":"tool_use","name":"Glob","input":{"pattern":"**/*config*"}},` +
	`{"type":"tool_use","name":"Skill","input":{"skill":"brainstorming"}}]}}
{"type":"user","message":{"content":[` +
	`{"type":"tool_result","tool_use_id":"a","content":"No files found"},` +
	`{"type":"tool_result","tool_use_id":"b","content":"loaded"}]}}
{"type":"assistant","message":{"content":[` +
	`{"type":"tool_use","name":"Edit","input":{"file_path":"main.go"}}]}}`

// TestReadNumbersTheBatchesToolsWereIssuedIn is what makes an ordering verdict mean
// anything. A runtime issues several calls at once and no result returns between them, so
// the agent has acted on none of them -- but a reader that only recorded document order
// would let the arbitrary ordering within one batch decide the verdict.
func TestReadNumbersTheBatchesToolsWereIssuedIn(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		doc  string
		want []int // the Batch of each tool use, in order
	}{
		"a real four-call batch, then one after the results": {
			geminiBatch, []int{1, 1, 1, 1, 2},
		},
		"claude code's nesting is numbered the same way": {
			claudeBatch, []int{1, 1, 2},
		},
		"consecutive results advance the count once": {
			// Two results close one batch, not two. Counting at the result rather than at
			// the next use would put the final call in batch 3 and make it look as though
			// a round trip had happened that did not.
			`{"type":"tool_use","name":"Glob"}
{"type":"tool_result","tool_use_id":"a"}
{"type":"tool_result","tool_use_id":"b"}
{"type":"tool_use","name":"Edit"}`,
			[]int{1, 2},
		},
		"a result before any use does not start a batch": {
			`{"type":"tool_result","tool_use_id":"a"}
{"type":"tool_use","name":"Glob"}`,
			[]int{1},
		},
		"every use alone in its batch": {
			`{"type":"tool_use","name":"Glob"}
{"type":"tool_result","tool_use_id":"a"}
{"type":"tool_use","name":"Edit"}
{"type":"tool_result","tool_use_id":"b"}
{"type":"tool_use","name":"Skill","skill":"brainstorming"}`,
			[]int{1, 2, 3},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := transcript.Read(strings.NewReader(tc.doc))
			if err != nil {
				t.Fatalf("Read() = %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d tool uses %+v, want %d", len(got), got, len(tc.want))
			}
			for i, w := range tc.want {
				if got[i].Batch != w {
					t.Errorf("%s (use %d) is in batch %d, want %d",
						got[i].Name, i, got[i].Batch, w)
				}
			}
		})
	}
}

// TestATranscriptWithoutResultsReportsItsBatchesUnknown is the fail-closed half, and the
// case that nearly went the other way. A transcript that logs no tool results gives nothing
// to divide batches by, so every use lands in batch 1 -- and a reader that trusted those
// numbers would see one enormous concurrent burst and call every such run clean.
//
// Found by the cmd-level tests, whose fixtures log uses without results. The tempting fix
// was to add results to the fixtures. That would have left every real transcript in a
// format that does not log them silently passing.
func TestATranscriptWithoutResultsReportsItsBatchesUnknown(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		doc       string
		wantKnown bool
	}{
		"gemini, results logged":         {geminiBatch, true},
		"claude code, results logged":    {claudeBatch, true},
		"gemini, no results logged":      {geminiRun, false},
		"claude code, no results logged": {nested, false},
		"a single flat use":              {flat, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := transcript.Read(strings.NewReader(tc.doc))
			if err != nil {
				t.Fatalf("Read() = %v", err)
			}
			if len(got) == 0 {
				t.Fatal("no tool uses read, so the case proves nothing")
			}
			for i, u := range got {
				if known := u.Batch > 0; known != tc.wantKnown {
					t.Errorf("%s (use %d) is in batch %d; batch known = %v, want %v",
						u.Name, i, u.Batch, known, tc.wantKnown)
				}
			}
		})
	}
}

// TestReadMarksTheCallsTheRuntimeRejected pins the correlation. Results come back in
// whatever order they finish, so a failure has to be matched by call id and not by
// position -- matching by position would mark the wrong call in any batch of more than one.
func TestReadMarksTheCallsTheRuntimeRejected(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		doc  string
		want []bool // Failed, per use in order
	}{
		"gemini: status error, matched out of order": {
			`{"type":"tool_use","tool_name":"activate_skill","tool_id":"a","parameters":{"name":"s"}}
{"type":"tool_use","tool_name":"glob","tool_id":"b","parameters":{"pattern":"*"}}
{"type":"tool_result","tool_id":"b","status":"success","output":"ok"}
{"type":"tool_result","tool_id":"a","status":"error","output":"Tool \"activate_skill\" not found."}`,
			[]bool{true, false},
		},
		"claude code: is_error on the result block": {
			`{"type":"assistant","message":{"content":[` +
				`{"type":"tool_use","id":"a","name":"Edit","input":{"file_path":"main.go"}}]}}
{"type":"user","message":{"content":[` +
				`{"type":"tool_result","tool_use_id":"a","is_error":true,"content":"no such file"}]}}`,
			[]bool{true},
		},
		"a use whose result never arrives is assumed to have worked": {
			`{"type":"tool_use","tool_name":"glob","tool_id":"a"}
{"type":"tool_result","tool_id":"zzz","status":"error"}`,
			[]bool{false},
		},
		"a use with no id cannot be matched, so it worked": {
			`{"type":"tool_use","tool_name":"glob"}
{"type":"tool_result","status":"error"}`,
			[]bool{false},
		},
		"a result that says nothing about how it went": {
			`{"type":"tool_use","tool_name":"glob","tool_id":"a"}
{"type":"tool_result","tool_id":"a","output":"ok"}`,
			[]bool{false},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := transcript.Read(strings.NewReader(tc.doc))
			if err != nil {
				t.Fatalf("Read() = %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d tool uses %+v, want %d", len(got), got, len(tc.want))
			}
			for i, w := range tc.want {
				if got[i].Failed != w {
					t.Errorf("%s (use %d) Failed = %v, want %v", got[i].Name, i, got[i].Failed, w)
				}
			}
		})
	}
}

// TestARejectedCallIsNotWorkDoneFirst is the defect this closes, transcribed from
// 06-user-pre-explains run 3.
//
// The agent asked for activate_skill as its first call. Under --approval-mode plan the tool
// does not exist, so the runtime answered "Tool activate_skill not found" and nothing
// happened -- then the agent read SKILL.md and got on with it. skillsaw called that
// after-action and named the failed call as the work that came first, which is a verdict
// about a call that did nothing.
//
// The second row is the guard against "always return first": a call that succeeded before
// the load still counts.
func TestARejectedCallIsNotWorkDoneFirst(t *testing.T) {
	t.Parallel()
	const rejected = `{"type":"tool_use","tool_name":"activate_skill","tool_id":"a","parameters":{"skill_name":"climax-cli-scaffold"}}
{"type":"tool_result","tool_id":"a","status":"error","output":"Tool \"activate_skill\" not found. Did you mean one of: \"write_file\", \"update_topic\", \"read_file\"?"}
{"type":"tool_use","tool_name":"update_topic","tool_id":"b","parameters":{"title":"Reviewing climax-cli-scaffold"}}
{"type":"tool_use","tool_name":"read_file","tool_id":"c","parameters":{"file_path":"/Users/steve/.agents/skills/climax-cli-scaffold/SKILL.md"}}`
	const succeeded = `{"type":"tool_use","tool_name":"glob","tool_id":"a","parameters":{"pattern":"*"}}
{"type":"tool_result","tool_id":"a","status":"success","output":"main.go"}
{"type":"tool_use","tool_name":"read_file","tool_id":"c","parameters":{"file_path":"/Users/steve/.agents/skills/climax-cli-scaffold/SKILL.md"}}`
	cases := map[string]struct {
		doc  string
		want transcript.Order
	}{
		"the call before the load was rejected": {rejected, transcript.OrderFirst},
		"the call before the load succeeded":    {succeeded, transcript.OrderAfterAction},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			uses, err := transcript.Read(strings.NewReader(tc.doc))
			if err != nil {
				t.Fatalf("Read() = %v", err)
			}
			got := transcript.OrderOf(uses, "climax-cli-scaffold")
			if got != tc.want {
				t.Errorf("OrderOf() = %q, want %q; Before() blames %v",
					got, tc.want, transcript.Before(uses, "climax-cli-scaffold"))
			}
		})
	}
}

// TestARejectedLoadIsNotALoad is the other direction, and the more flattering mistake. An
// activate_skill that the runtime refused loaded nothing, so a run that tried it and then
// did real work without the skill must not be credited with having loaded it first.
func TestARejectedLoadIsNotALoad(t *testing.T) {
	t.Parallel()
	const doc = `{"type":"tool_use","tool_name":"activate_skill","tool_id":"a","parameters":{"name":"climax-cli-scaffold"}}
{"type":"tool_result","tool_id":"a","status":"error","output":"Tool \"activate_skill\" not found."}
{"type":"tool_use","tool_name":"write_file","tool_id":"b","parameters":{"file_path":"main.go"}}
{"type":"tool_result","tool_id":"b","status":"success","output":"written"}
{"type":"tool_use","tool_name":"read_file","tool_id":"c","parameters":{"file_path":"/Users/steve/.agents/skills/climax-cli-scaffold/SKILL.md"}}`
	uses, err := transcript.Read(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("Read() = %v", err)
	}
	if got := transcript.OrderOf(uses, "climax-cli-scaffold"); got != transcript.OrderAfterAction {
		t.Errorf("OrderOf() = %q, want %q; a refused activation was credited as a load",
			got, transcript.OrderAfterAction)
	}
}

// TestOnlyASkillLoadingToolNamesASkill is the distinction that keeps a load from being
// invented. Plenty of tools take a "name", and reading one as a skill would report guidance
// the agent never received -- the flattering mistake, and so the one worth pinning.
//
// The rows also record what "skill_name" is, because two of the three activate_skill calls
// in a 27-run capture used it and it reads like a second spelling. Gemini CLI 0.46.0
// documents one argument, `name`; both "skill_name" calls came back "Tool activate_skill
// not found", because under --approval-mode plan the tool is absent and the model invented
// the call along with the parameter. Honouring it would credit a failed call as a load.
func TestOnlyASkillLoadingToolNamesASkill(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		doc  string
		want bool
	}{
		"activate_skill with the documented argument": {
			`{"type":"tool_use","tool_name":"activate_skill",` +
				`"parameters":{"name":"climax-cli-scaffold"}}`, true,
		},
		"activate_skill with a hallucinated argument": {
			`{"type":"tool_use","tool_name":"activate_skill",` +
				`"parameters":{"skill_name":"climax-cli-scaffold"}}`, false,
		},
		"activate_skill naming a different skill": {
			`{"type":"tool_use","tool_name":"activate_skill",` +
				`"parameters":{"name":"other-skill"}}`, false,
		},
		"an ordinary tool with a name parameter": {
			`{"type":"tool_use","tool_name":"write_file",` +
				`"parameters":{"name":"climax-cli-scaffold"}}`, false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := transcript.Read(strings.NewReader(tc.doc))
			if err != nil {
				t.Fatalf("Read() = %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("got %d tool uses, want 1", len(got))
			}
			if loads := got[0].LoadsSkill("climax-cli-scaffold"); loads != tc.want {
				t.Errorf("LoadsSkill() = %v, want %v", loads, tc.want)
			}
		})
	}
}

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
