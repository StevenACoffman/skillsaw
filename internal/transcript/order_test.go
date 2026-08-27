package transcript_test

import (
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/transcript"
)

// use builds a plain tool invocation; loads builds a Skill invocation. Neither sets a
// batch, so both read as batch-unknown and get the stricter document-order treatment.
func use(name string) transcript.ToolUse { return transcript.ToolUse{Name: name} }
func loads(s string) transcript.ToolUse  { return transcript.ToolUse{Name: "Skill", Skill: s} }

// useIn and loadsIn are the same two, placed in a named batch. loadsIn takes no skill
// because every batched case below asks about the same one, and a parameter with a single
// value reads as a choice the cases make when they do not.
func useIn(batch int, name string) transcript.ToolUse {
	return transcript.ToolUse{Name: name, Batch: batch}
}

func loadsIn(batch int) transcript.ToolUse {
	return transcript.ToolUse{Name: "Skill", Skill: "brainstorming", Batch: batch}
}

// TestConcurrentIssuanceIsNotWorkDoneFirst is the defect this closes, and the first two
// rows are one experiment rather than two cases.
//
// climax-cli-scaffold was run three times on the 08-buried-in-a-list phrasing. Runs 1 and 2
// issued the same four calls in a single batch -- inside 742ms and 576ms, with every result
// arriving after the last of them -- and differed only in whether glob or the skill read was
// emitted first, 275ms apart. They scored after-action and first. Nothing was learned from
// the glob before the skill was read: its result had not come back, and when it did it said
// "No files found".
//
// Reading emission order as a sequence therefore turned an arbitrary ordering into a
// verdict, and made the phrasing look like the most discriminating one in the corpus.
func TestConcurrentIssuanceIsNotWorkDoneFirst(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		uses []transcript.ToolUse
		want transcript.Order
	}{
		"run 1: issued together, the load emitted second": {
			[]transcript.ToolUse{useIn(1, "glob"), loadsIn(1)},
			transcript.OrderFirst,
		},
		"run 2: issued together, the load emitted first": {
			[]transcript.ToolUse{loadsIn(1), useIn(1, "glob")},
			transcript.OrderFirst,
		},
		"a result came back, and then the skill loaded": {
			// The genuine defect: the agent saw what glob returned and only then read the
			// skill. Without this row the change could be "always return first".
			[]transcript.ToolUse{useIn(1, "glob"), loadsIn(2)},
			transcript.OrderAfterAction,
		},
		"two rounds of work, then the skill loaded": {
			[]transcript.ToolUse{
				useIn(1, "glob"), useIn(2, "Edit"), loadsIn(3),
			},
			transcript.OrderAfterAction,
		},
		"planning issued alongside the load": {
			[]transcript.ToolUse{useIn(1, "TodoWrite"), loadsIn(1)},
			transcript.OrderFirst,
		},
		"work in an earlier batch outweighs work alongside": {
			// The earliest action decides it, not the nearest one.
			[]transcript.ToolUse{
				useIn(1, "glob"), useIn(2, "Edit"), loadsIn(2),
			},
			transcript.OrderAfterAction,
		},
		"the skill never loaded at all": {
			[]transcript.ToolUse{useIn(1, "glob"), useIn(2, "Edit")},
			transcript.OrderNotTriggered,
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

// TestARefusedLoadIsNotATriggerFailure separates three things that were one verdict.
//
// An agent that never mentions the skill and an agent whose every attempt to read it was
// denied both used to report not-triggered, which is a claim about the description. It was
// the reported outcome for 19 of 27 captured runs, in every one of which the agent asked
// for the skill first and the sandbox refused: activate_skill absent under plan mode,
// read_file refusing a path outside the workspace, the cat fallback denied as script
// execution.
//
// The last two rows are the controls. Without "nothing referred to the skill" the change
// is "call everything unmeasurable"; without "refused, then loaded anyway" it is "any
// failure anywhere is unmeasurable".
func TestARefusedLoadIsNotATriggerFailure(t *testing.T) {
	t.Parallel()
	failedLoad := transcript.ToolUse{
		Name: "read_file", Batch: 1, Failed: true,
		Args: []string{"/Users/steve/.agents/skills/brainstorming/SKILL.md"},
	}
	cases := map[string]struct {
		uses []transcript.ToolUse
		want transcript.Order
	}{
		"every attempt to reach the skill was refused": {
			[]transcript.ToolUse{failedLoad}, transcript.OrderUnmeasurable,
		},
		"refused, and the agent worked anyway": {
			// Still unmeasurable, not after-action. The work happened without the skill in
			// context, so it says nothing about whether guidance was followed.
			[]transcript.ToolUse{failedLoad, useIn(2, "write_file")},
			transcript.OrderUnmeasurable,
		},
		"nothing referred to the skill at all": {
			[]transcript.ToolUse{useIn(1, "glob"), useIn(2, "write_file")},
			transcript.OrderNotTriggered,
		},
		"refused once, then loaded successfully": {
			[]transcript.ToolUse{failedLoad, loadsIn(2)}, transcript.OrderFirst,
		},
		"refused, then work, then loaded": {
			[]transcript.ToolUse{failedLoad, useIn(2, "write_file"), loadsIn(3)},
			transcript.OrderAfterAction,
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

// TestRefusedNamesTheToolsThatCouldNotReachTheSkill is the operator-facing half. A run
// reported unmeasurable without saying which door was shut sends the reader looking at the
// skill, which is the one place the answer is not.
func TestRefusedNamesTheToolsThatCouldNotReachTheSkill(t *testing.T) {
	t.Parallel()
	failedRead := transcript.ToolUse{
		Name: "read_file", Batch: 1, Failed: true,
		Args: []string{"/Users/steve/.agents/skills/brainstorming/SKILL.md"},
	}
	failedCat := transcript.ToolUse{
		Name: "run_shell_command", Batch: 2, Failed: true,
		Args: []string{"cat /Users/steve/.agents/skills/brainstorming/SKILL.md"},
	}
	cases := map[string]struct {
		uses []transcript.ToolUse
		want []string
	}{
		"both doors named, in order, once each": {
			[]transcript.ToolUse{failedRead, failedCat, failedRead},
			[]string{"read_file", "run_shell_command"},
		},
		"a run that loaded the skill names nothing": {
			[]transcript.ToolUse{loadsIn(1)}, nil,
		},
		"a failed tool unrelated to the skill is not a refused load": {
			[]transcript.ToolUse{
				{Name: "glob", Batch: 1, Failed: true}, loadsIn(2),
			}, nil,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := transcript.Refused(tc.uses, "brainstorming")
			if len(got) != len(tc.want) {
				t.Fatalf("Refused() = %v, want %v", got, tc.want)
			}
			for i, w := range tc.want {
				if got[i] != w {
					t.Errorf("Refused()[%d] = %q, want %q", i, got[i], w)
				}
			}
		})
	}
}

// TestNumberingEveryUseSeparatelyReproducesDocumentOrder is what makes the unknown-batch
// rule safe to rely on. A zero batch falls back to document order, and that fallback is
// only correct if one-use-per-batch and no-batch-at-all agree everywhere. Renumbering the
// table above proves it on the same cases rather than on cases chosen to agree.
//
// It also pins the comparison: loosening it from < to <= turns every row here into first.
func TestNumberingEveryUseSeparatelyReproducesDocumentOrder(t *testing.T) {
	t.Parallel()
	cases := map[string][]transcript.ToolUse{
		"loaded before anything else":                {loads("brainstorming"), use("Bash")},
		"work happened and the skill never loaded":   {use("Bash"), use("Edit")},
		"work happened first, then the skill loaded": {use("Bash"), loads("brainstorming")},
		"planning first does not count as work": {
			use("TodoWrite"), use("TaskCreate"), loads("brainstorming"),
		},
		"a different skill loaded first": {loads("other-skill"), loads("brainstorming")},
	}
	for name, flat := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			numbered := make([]transcript.ToolUse, len(flat))
			for i, u := range flat {
				u.Batch = i + 1
				numbered[i] = u
			}
			want := transcript.OrderOf(flat, "brainstorming")
			if got := transcript.OrderOf(numbered, "brainstorming"); got != want {
				t.Errorf("one use per batch = %q, but no batches at all = %q", got, want)
			}
		})
	}
}

// TestBeforeDoesNotNameToolsIssuedAlongsideTheLoad keeps the operator-facing half honest.
// Before exists so a reader knows which fix to reach for, and naming a glob that was issued
// in the same breath as the skill read -- and whose result had not returned -- points at a
// cause that is not one.
func TestBeforeDoesNotNameToolsIssuedAlongsideTheLoad(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		uses []transcript.ToolUse
		want []string
	}{
		"issued alongside, so nothing came first": {
			[]transcript.ToolUse{useIn(1, "glob"), loadsIn(1)}, nil,
		},
		"a genuine earlier round is still named": {
			[]transcript.ToolUse{useIn(1, "glob"), loadsIn(2)},
			[]string{"glob"},
		},
		"only the earlier round, not the concurrent one": {
			[]transcript.ToolUse{
				useIn(1, "glob"), useIn(2, "Edit"), loadsIn(2),
			},
			[]string{"glob"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := transcript.Before(tc.uses, "brainstorming")
			if len(got) != len(tc.want) {
				t.Fatalf("Before() = %v, want %v", got, tc.want)
			}
			for i, w := range tc.want {
				if got[i] != w {
					t.Errorf("Before()[%d] = %q, want %q", i, got[i], w)
				}
			}
		})
	}
}

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
		transcript.OrderUnmeasurable,
	} {
		if !o.Valid() {
			t.Errorf("Valid() = false for the defined order %q", o)
		}
	}
	if transcript.Order("triggered").Valid() {
		t.Error("Valid() = true for an undefined order")
	}
}
