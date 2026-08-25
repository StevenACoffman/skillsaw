package transcript_test

import (
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/transcript"
)

// use builds a plain tool invocation; loads builds a Skill invocation.
func use(name string) transcript.ToolUse { return transcript.ToolUse{Name: name} }
func loads(s string) transcript.ToolUse  { return transcript.ToolUse{Name: "Skill", Skill: s} }

// TestOrderOfSeparatesTriggeredFromTriggeredInTime is the distinction the item exists for.
// The third and fourth rows end identically — the skill loaded, the work got done — and a
// confusion matrix built from the reply records both as a success. Only the sequence
// separates them.
func TestOrderOfSeparatesTriggeredFromTriggeredInTime(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		uses []transcript.ToolUse
		want transcript.Order
	}{
		"loaded before anything else": {
			[]transcript.ToolUse{loads("brainstorming"), use("Bash")}, transcript.OrderFirst,
		},
		"nothing at all happened": {
			nil, transcript.OrderNotTriggered,
		},
		"work happened and the skill never loaded": {
			[]transcript.ToolUse{use("Bash"), use("Edit")}, transcript.OrderNotTriggered,
		},
		"work happened first, then the skill loaded": {
			[]transcript.ToolUse{use("Bash"), loads("brainstorming")}, transcript.OrderAfterAction,
		},
		"planning first does not count as work": {
			[]transcript.ToolUse{use("TodoWrite"), use("TaskCreate"), loads("brainstorming")},
			transcript.OrderFirst,
		},
		"a namespaced invocation still matches": {
			[]transcript.ToolUse{loads("superpowers:brainstorming")}, transcript.OrderFirst,
		},
		"a different skill loaded first": {
			// Loading the wrong skill and working from it is work under the wrong
			// guidance, not preparation for the right one.
			[]transcript.ToolUse{loads("other-skill"), loads("brainstorming")},
			transcript.OrderAfterAction,
		},
		"only a different skill loaded": {
			[]transcript.ToolUse{loads("other-skill")}, transcript.OrderNotTriggered,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := transcript.OrderOf(tc.uses, "brainstorming"); got != tc.want {
				t.Errorf("OrderOf() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestOrderOfWithoutASkillIsUnknown keeps the zero value honest: asking about no skill is
// not the same as asking about a skill and finding it absent.
func TestOrderOfWithoutASkillIsUnknown(t *testing.T) {
	t.Parallel()
	got := transcript.OrderOf([]transcript.ToolUse{loads("brainstorming")}, "")
	if got != transcript.OrderUnknown {
		t.Errorf("OrderOf(_, \"\") = %q, want the zero value", got)
	}
}

// TestOrderValidIsTotal keeps Valid honest about the set it guards.
func TestOrderValidIsTotal(t *testing.T) {
	t.Parallel()
	for _, o := range []transcript.Order{
		transcript.OrderUnknown, transcript.OrderNotTriggered,
		transcript.OrderAfterAction, transcript.OrderFirst,
	} {
		if !o.Valid() {
			t.Errorf("Valid() = false for the defined order %q", o)
		}
	}
	if transcript.Order("triggered").Valid() {
		t.Error("Valid() = true for an undefined order")
	}
}
