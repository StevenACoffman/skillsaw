// Package transcript reads the event log an agent run produces, so a question about the
// order things happened in can be answered from what happened rather than from what the
// reply says happened.
//
// A prose answer cannot be trusted to report its own sequence: an agent that started work
// and then loaded the skill will describe itself as having used the skill, truthfully, and
// the ordering is gone. Only a structured log keeps it.
//
// Everything here is pure. Running the agent is a separate concern and lives outside this
// repository -- see superpowers/tests/explicit-skill-requests/run-test.sh, which writes the
// format read here.
package transcript

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
)

// skillFile is the marker a skill directory is identified by, and so the thing an agent
// that loads a skill by reading it must be seen reading.
const skillFile = "SKILL.md"

// maxLine bounds one transcript line. A single assistant message carrying a large tool
// result can be far past bufio's 64KiB default, and a truncated line would silently drop
// the events after it.
//
// 8MiB was sized for an agent that could not act. Once one could, a single tool_result
// reached 17.5MB -- an agent grepping a source tree returns the matches -- and two of nine
// phrasings came back unreadable. 64MiB is that observed maximum with room to spare rather
// than a principled bound; raise it again if a real transcript exceeds it.
//
// Exceeding it stays fatal for the whole transcript, which looks harsh and is the safe
// direction. The line that would be dropped is almost always a tool_result, and a dropped
// result silently merges two batches into one, which makes OrderOf *more* lenient -- a
// verdict softened by a parse failure nobody saw. Refusing the file is the loud version of
// the same fact, and it is why the reporting path has to name an unreadable run rather than
// leave a blank where its verdict goes.
const maxLine = 64 << 20

// ToolUse is one tool invocation, in the order it appeared.
type ToolUse struct {
	// Name is the tool, e.g. "Skill", "Bash", "read_file".
	Name string

	// Skill is the skill a dedicated skill-loading tool declared it was loading, possibly
	// namespaced ("superpowers:brainstorming"). Empty for every other tool, and empty on
	// runtimes that have no such tool -- see Args.
	Skill string

	// Args is every string argument the invocation carried, flattened.
	//
	// It exists because not every runtime has a skill-loading tool. Gemini CLI loads a
	// skill by reading its SKILL.md, so the only evidence is a path sitting in a
	// read_file argument -- or, when that read is refused, inside a shell command that
	// cats the same path. Keeping the argument strings lets LoadsSkill recognise both
	// without this package knowing which runtime wrote the log.
	Args []string

	// Batch groups the uses a runtime issued together, counting from 1. Two uses share a
	// number when no tool result returned between them.
	//
	// It exists because an agent that issues four calls at once has acted on none of them:
	// no result has come back to act on. Reading emission order as a sequence turns an
	// arbitrary ordering into a verdict. Measured on climax-cli-scaffold, where two runs of
	// one phrasing emitted the same four calls 275ms apart in different order and scored
	// first and after-action respectively.
	//
	// Zero means unknown, and a caller must treat an unknown batch as its own rather than
	// as shared -- see actedBefore. Every hand-built slice carries zeros, and reading those
	// as one batch would report each of them as clean.
	Batch int

	// Failed reports that the runtime returned an error for this call.
	//
	// A call that failed did nothing: it edited no file and loaded no skill. Recording it
	// as though it happened is how an errored activate_skill came to be reported as the
	// work an agent did before loading its skill.
	//
	// The zero value is "it worked", which is what a transcript whose results cannot be
	// correlated gets, and which is how this package behaved before the field existed. Note
	// that the default is not uniformly conservative, and this is the one place in the
	// package where it is not: assuming an action succeeded counts it as work and can only
	// harden a verdict, but assuming a load succeeded counts the skill as loaded and can
	// only soften one. There is no signal to do better with when correlation fails.
	Failed bool
}

// collector accumulates tool uses while walking a transcript, numbering the batches the
// runtime issued them in.
//
// It is a type rather than three out-parameters on a function because the walk is
// recursive and the batch state has to survive every level of it.
type collector struct {
	uses      []ToolUse
	ids       []string        // the call id of uses[i], for matching results; "" when absent
	failed    map[string]bool // call ids the runtime returned an error for
	batch     int
	open      bool // the current batch holds a use and no result has closed it yet
	sawResult bool // this transcript records results at all, so batching is knowable
}

// LoadsSkill reports whether this invocation loaded the named skill.
//
// Two runtimes, two shapes, because loading a skill is not the same act in both.
//
// Claude Code has a Skill tool that names what it loaded, so the check is a name
// comparison, ignoring any namespace prefix: "superpowers:brainstorming" matches
// "brainstorming". A caller asks by the name the skill declares, and which plugin surfaced
// it is a fact about configuration rather than part of the skill's identity.
//
// Gemini CLI has activate_skill, which names it in "name" -- but observed runs reach the
// skill by *reading the file* first, and sometimes never call activate_skill at all. So the
// evidence may instead be a path ending "<name>/SKILL.md" among the arguments: a read_file
// file_path, or a shell command catting the same path when the read was refused for being
// outside the workspace. Matching the path suffix covers both without caring which tool
// carried it.
func (t ToolUse) LoadsSkill(name string) bool {
	if name == "" {
		return false
	}
	if t.Skill != "" {
		got := t.Skill
		if i := strings.LastIndex(got, ":"); i >= 0 {
			got = got[i+1:]
		}
		if got == name {
			return true
		}
	}
	want := "/" + name + "/" + skillFile
	for _, a := range t.Args {
		if strings.Contains(a, want) {
			return true
		}
	}
	return false
}

// skillTools are the tools whose whole job is loading a skill: Claude Code's Skill and
// Gemini CLI's activate_skill. Both name the skill in a parameter, so for these the name is
// read directly rather than inferred from a path.
//
// Recognised by tool name rather than by spotting a "name" parameter anywhere, because
// "name" is far too common a parameter to treat as a skill reference -- plenty of tools
// take a name that is a file, a variable, or a session.
func skillTools() map[string]bool {
	return map[string]bool{"Skill": true, "activate_skill": true}
}

// Read returns the tool uses in a transcript, in order.
//
// The format is the line-delimited JSON that `claude -p --output-format stream-json`
// writes. Where a tool_use object sits within a line is deliberately not assumed: it may be
// at the top level or nested under an assistant message's content array, so this walks the
// decoded value looking for objects that declare themselves a tool_use. Binding to one path
// would make the reader depend on a shape nobody here has captured.
//
// Requires: r yields the transcript.
// Ensures:  pure; tool uses in document order, each carrying the batch it was issued in.
// A line that is not JSON is skipped rather than fatal -- a transcript is a log, and a
// harness that also wrote a banner into it must not make the run unreadable.
func Read(r io.Reader) ([]ToolUse, error) {
	var c collector
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxLine)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var v any
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			continue
		}
		c.collect(v)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read transcript: %w", err)
	}
	return c.result(), nil
}

// result returns the collected uses, marking the ones the runtime rejected and withdrawing
// the batch numbers when the transcript recorded no tool results at all.
//
// Without results there is nothing to divide batches by, so every use would land in batch 1
// and read as one enormous concurrent burst -- which is the lenient reading, and would
// report every run of such a transcript as clean. A format that does not log results cannot
// answer the question, and must say so rather than answer it favourably. Zero is how a use
// says its batch is unknown, and unknown falls back to document order.
//
// Failures are matched by call id rather than by position, because a batch of four returns
// its results in whatever order they finish.
func (c *collector) result() []ToolUse {
	for i := range c.uses {
		if !c.sawResult {
			c.uses[i].Batch = 0
		}
		if id := c.ids[i]; id != "" && c.failed[id] {
			c.uses[i].Failed = true
		}
	}
	return c.uses
}

// addUse records one tool invocation, opening a batch if the previous one was closed.
//
// The batch is incremented here, lazily, rather than at the result that closed the previous
// one, so a run of consecutive results -- the ordinary shape when a batch of four returns --
// advances the count once rather than four times.
func (c *collector) addUse(t map[string]any) {
	if !c.open {
		c.batch++
		c.open = true
	}
	c.uses = append(c.uses, ToolUse{
		Name: toolName(t), Skill: skillIn(t), Args: argsIn(t), Batch: c.batch,
	})
	c.ids = append(c.ids, callID(t))
}

// addResult closes the open batch and remembers the call if the runtime rejected it.
//
// Recording the id rather than marking the use directly is what lets results arrive in any
// order, which they do: a batch of four returns as its calls finish.
func (c *collector) addResult(t map[string]any) {
	c.open = false
	c.sawResult = true
	id := callID(t)
	if id == "" || !resultFailed(t) {
		return
	}
	if c.failed == nil {
		c.failed = map[string]bool{}
	}
	c.failed[id] = true
}

// collect walks a decoded JSON value, appending every tool use it finds in document order
// and closing the open batch at every tool result.
//
// A tool result is not descended into. Nothing nests a use inside a result, and stopping
// there keeps a result that quotes a transcript from being read as one.
func (c *collector) collect(v any) {
	switch t := v.(type) {
	case map[string]any:
		switch s, _ := t["type"].(string); s {
		case "tool_use":
			c.addUse(t)
			return
		case "tool_result":
			c.addResult(t)
			return
		}
		for _, k := range sortedKeys(t) {
			c.collect(t[k])
		}
	case []any:
		for _, e := range t {
			c.collect(e)
		}
	}
}

// sortedKeys orders a map's keys so the walk is deterministic.
//
// Go randomises map iteration, and document order within one JSON object is lost at decode
// anyway. Ordering between tool uses comes from the arrays they sit in, which slices
// preserve; this only ensures the same transcript always reads the same way, which a
// caller comparing two runs depends on.
func sortedKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}

// toolName reads the invoked tool's name. Claude Code writes "name"; Gemini CLI writes
// "tool_name", captured from a real run.
func toolName(t map[string]any) string {
	if s, ok := t["name"].(string); ok {
		return s
	}
	if s, ok := t["tool_name"].(string); ok {
		return s
	}
	return ""
}

// callID reads the identifier tying a tool result back to the call it answers.
//
// Gemini CLI writes "tool_id" on both sides. Claude Code writes "id" on the tool_use block
// and "tool_use_id" on the tool_result, so all three are checked and whichever is present
// wins -- the alternative is knowing which runtime wrote the log, which this package
// deliberately does not.
//
// Empty when the record carries no id, which leaves the use unmatched and so assumed to
// have worked.
func callID(t map[string]any) string {
	for _, k := range []string{"tool_id", "tool_use_id", "id"} {
		if s, ok := t[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// resultFailed reports whether a tool result records an error.
//
// Gemini CLI writes "status":"error"; Claude Code writes "is_error":true. Anything else,
// including a result that says nothing about how it went, reads as success -- see the note
// on ToolUse.Failed for why the unknown case defaults that way.
func resultFailed(t map[string]any) bool {
	if s, ok := t["status"].(string); ok && s == "error" {
		return true
	}
	b, ok := t["is_error"].(bool)
	return ok && b
}

// skillIn finds the skill a dedicated skill-loading tool declared, wherever the field sits.
//
// Claude Code's Skill tool carries it; the reference harness greps a top-level "skill", and
// an invocation's arguments are conventionally nested under "input", so both are checked.
// Gemini CLI's activate_skill names it in "name", read only for tools known to load skills.
// When an agent instead loads a skill by reading the file -- which Gemini often does before
// reaching activate_skill -- this yields "" and LoadsSkill falls back to the argument paths.
//
// "skill_name" is not a second spelling, and reading it here would be wrong. Gemini CLI
// 0.46.0 documents exactly one argument, `name`, and both captured calls using "skill_name"
// came back "Tool \"activate_skill\" not found" -- under --approval-mode plan the tool is
// not in the set, and the model invented the call along with the parameter. Two of three
// observed calls look like evidence for "skill_name"; they are evidence of a hallucinated
// call to an absent tool. Honouring it would credit a failed call as a loaded skill.
func skillIn(t map[string]any) string {
	if s, ok := t["skill"].(string); ok {
		return s
	}
	dedicated := skillTools()[toolName(t)]
	for _, key := range []string{"input", "parameters"} {
		in, ok := t[key].(map[string]any)
		if !ok {
			continue
		}
		if s, ok := in["skill"].(string); ok {
			return s
		}
		// Only for a tool that exists to load skills: activate_skill puts it in "name".
		if dedicated {
			if s, ok := in["name"].(string); ok {
				return s
			}
		}
	}
	return ""
}

// argsIn flattens an invocation's string arguments, so a caller can look for evidence the
// tool itself does not label -- a skill path inside a file read or a shell command.
func argsIn(t map[string]any) []string {
	var out []string
	for _, key := range []string{"input", "parameters"} {
		in, ok := t[key].(map[string]any)
		if !ok {
			continue
		}
		for _, k := range sortedKeys(in) {
			if s, ok := in[k].(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}
