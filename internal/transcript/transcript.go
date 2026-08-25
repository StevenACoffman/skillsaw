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
const maxLine = 8 << 20

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
// Ensures:  pure; tool uses in document order. A line that is not JSON is skipped rather
// than fatal -- a transcript is a log, and a harness that also wrote a banner into it must
// not make the run unreadable.
func Read(r io.Reader) ([]ToolUse, error) {
	var out []ToolUse
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
		collect(v, &out)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read transcript: %w", err)
	}
	return out, nil
}

// collect walks a decoded JSON value, appending every tool_use it finds in order.
func collect(v any, out *[]ToolUse) {
	switch t := v.(type) {
	case map[string]any:
		if s, _ := t["type"].(string); s == "tool_use" {
			*out = append(*out, ToolUse{Name: toolName(t), Skill: skillIn(t), Args: argsIn(t)})
			return
		}
		for _, k := range sortedKeys(t) {
			collect(t[k], out)
		}
	case []any:
		for _, e := range t {
			collect(e, out)
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

// skillIn finds the skill a dedicated skill-loading tool declared, wherever the field sits.
//
// Claude Code's Skill tool carries it; the reference harness greps a top-level "skill", and
// an invocation's arguments are conventionally nested under "input", so both are checked.
// Gemini CLI's activate_skill names it in "name", which is
// read only for tools known to load skills. When an agent instead loads a skill by reading
// the file -- which Gemini did in every captured run before reaching activate_skill -- this
// yields "" and LoadsSkill falls back to the argument paths.
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
