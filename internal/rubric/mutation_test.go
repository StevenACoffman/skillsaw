package rubric_test

import (
	"strings"
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/rubric"
)

// The rubric's checks are an eval suite, and an eval suite that passes has not shown that
// it checks anything. This file asks the mutation-testing question of it: inject a known
// defect into the artifact being graded and see whether any dimension notices. A defect
// nothing notices is a hole in the rubric -- a finding about the rubric, not a broken test.
//
// The injections are mined from what this corpus was measured to actually contain, not
// invented to match checks known to exist. Writing them from the checks would guarantee
// they all pass and prove nothing.

// cleanBody is a skill no dimension penalises. Every mutation below is a single departure
// from it, so a score that moves can only be the mutation's doing.
const cleanBody = "## Workflow\n\nStep 1: run the check\nStep 2: read the output\n\n" +
	"如果失败 → 回退 and retry\n\n" + markers + "\n" + markers + "\n" + markers + "\n\n" +
	"## 反例黑名单\n- do not A\n- do not B\n- do not C\n"

// cleanPrompts is one behavioral case carrying a real assertion.
const cleanPrompts = `{"tests":[{"id":1,"type":"should_trigger","prompt":"do the demo",` +
	`"expected":"it emits the report","checks":[{"kind":"contains","value":"report"}]}]}`

const cleanDesc = "Use when the reader needs the demo thing done."

// How the rubric responds to an injected defect. Docked and reported are both "noticed";
// separating them matters because a dimension that reports without docking has done its
// job -- it is NeedsJudge and the deterministic half is a lower bound, not a verdict.
// Collapsing them would file every judge-deferred check as a hole.
const (
	blind    notice = "blind"
	reported notice = "reported"
	docked   notice = "docked"
)

// notice is what the rubric did about an injected defect.
type notice string

// mutation is one known defect injected into the clean skill.
type mutation struct {
	desc, body, prompts string // empty means "leave the clean value"

	// want is declared rather than asserted non-blind, because a defect the rubric cannot
	// see is a legitimate and important result -- finding those is the point, and a table
	// that could only report success would hide them. Disagreement in either direction
	// fails: a hole that gets closed must be recorded as closed.
	want notice

	// why explains a declared blind spot, so the next reader knows it was measured rather
	// than overlooked.
	why string
}

// TestKnownDefectsAreNoticed injects each defect and compares every dimension's final
// score against the clean baseline.
func TestKnownDefectsAreNoticed(t *testing.T) {
	t.Parallel()
	cases := map[string]mutation{
		// Measured over the corpus: frontmatter written as an unquoted scalar followed by
		// prose parses to nothing, and every field below reads as missing.
		"frontmatter parses to nothing": {
			desc: `"Site Reliability Engineering" by Betsy Beyer`, want: docked,
		},
		"description over the 1024 cap": {
			desc: strings.Repeat("a", 1100), want: docked,
		},
		// The dim-3 defect: something is run and nothing says what to do when it fails.
		"runs commands with no failure branch": {
			body: "## Workflow\n\nStep 1: do it\n\n```sh\nrun --it\n```\n\n## 反例黑名单\n- do not A\n- do not B\n- do not C\n",
			want: docked,
		},
		// 154 of 233 corpus skills carry a failure-handling heading with nothing under it.
		// 154 of 233 corpus skills carry a failure-handling heading with nothing under
		// it. Measured: reported, not docked -- this body executes nothing, so there is no
		// runtime failure to encode and docking would be a category error. The dock arrives
		// as soon as it runs something; the case below proves the reassuring heading does
		// not suppress it.
		"a failure-handling heading with no branch beneath it": {
			body: strings.Replace(cleanBody, "如果失败 → 回退 and retry", "## 失败处理\n", 1),
			want: reported,
		},
		"a failure-handling heading over commands that can fail": {
			body: strings.Replace(cleanBody, "如果失败 → 回退 and retry",
				"## 失败处理\n\n```sh\nrun --it\n```\n", 1),
			want: docked,
		},
		"boundary section with no items": {
			body: strings.Replace(cleanBody, "- do not A\n- do not B\n- do not C\n", "", 1),
			want: docked,
		},
		"hedged steps": {
			body: cleanBody + "\n建议这样 可以考虑那样 视情况而定\n", want: docked,
		},
		"a resource it points at is not there": {
			body: cleanBody + "\nsee [ref](references/missing.md)\n", want: docked,
		},
		// Reported, not docked, and correctly so: dim 8 is NeedsJudge and its
		// deterministic half is a lower bound. It says the dimension cannot be scored
		// until checks exist, which is the whole finding.
		"cases specify nothing to assert": {
			prompts: `{"tests":[{"id":1,"type":"should_trigger","prompt":"do the demo",` +
				`"expected":"it emits the report"}]}`,
			want: reported,
		},

		// Below: defects the rubric is measured NOT to see. Each is real and each is
		// recorded rather than quietly tolerated.
		"assertions that assert nothing about behaviour": {
			// The corpus shape: `expected` holds activation prose -- "Invokes
			// 10x9-cost-reliability" -- and the check merely repeats a word from it. The
			// case is fully scorable and asserts nothing about what the skill did.
			prompts: `{"tests":[{"id":1,"type":"should_trigger","prompt":"do the demo",` +
				`"expected":"Invokes the demo skill","checks":[{"kind":"contains","value":"demo"}]}]}`,
			want: blind,
			why: "dim 8 counts whether a case is scorable, not whether its assertion says " +
				"anything; judging that needs a reader, which is why dim 8 is NeedsJudge",
		},
		"boundary items that are filler": {
			body: strings.Replace(cleanBody, "- do not A\n- do not B\n- do not C\n",
				"- do not do bad things\n- avoid problems\n- never fail\n", 1),
			want: blind,
			why: "dim 9 counts boundary units; three rows of filler score as three rows. " +
				"This is the defect Response=additive on dim 9 records, and a deterministic " +
				"filler detector would just be a new feedable dimension",
		},
	}

	base := evaluateMutant(t, &mutation{})
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, where := respondsTo(base, evaluateMutant(t, &m))
			switch {
			case got == m.want:
			case m.want == blind:
				t.Errorf("declared a blind spot (%s), but dim %d %s it; update the "+
					"declaration -- a hole that closes must be recorded as closed",
					m.why, where, got)
			default:
				t.Errorf("the rubric's response was %q; the table declares %q. If %q is "+
					"right, change the declaration and say why -- do not weaken the "+
					"injection to make this pass", got, m.want, got)
			}
		})
	}
}

// respondsTo reports the strongest response any dimension made to the mutation, and which
// dimension made it. A changed flag counts: a dimension that names the defect and defers
// the price to a judge has noticed it, and filing that as a blind spot would mistake this
// rubric's deliberate lower-bound design for a gap in it.
func respondsTo(base, got *rubric.Evaluation) (notice, int) {
	out, where := blind, 0
	for i := range got.Dims {
		d := &got.Dims[i]
		before := dimOf(base, d.Num)
		switch {
		case d.Final < before.Final:
			return docked, d.Num
		case strings.Join(d.Flags, "|") != strings.Join(before.Flags, "|"):
			out, where = reported, d.Num
		}
	}
	return out, where
}

// dimOf returns one dimension of an evaluation, or a zero DimScore when absent.
func dimOf(ev *rubric.Evaluation, num int) rubric.DimScore {
	for i := range ev.Dims {
		if ev.Dims[i].Num == num {
			return ev.Dims[i]
		}
	}
	return rubric.DimScore{}
}

// evaluateMutant scores the clean skill with the mutation's non-empty fields substituted.
func evaluateMutant(t *testing.T, m *mutation) *rubric.Evaluation {
	t.Helper()
	desc, body, prompts := cleanDesc, cleanBody, cleanPrompts
	if m.desc != "" {
		desc = m.desc
	}
	if m.body != "" {
		body = m.body
	}
	if m.prompts != "" {
		prompts = m.prompts
	}
	s := mkSkill(t, "demo-skill", desc, body, map[string]string{"test-prompts.json": prompts})
	return rubric.Evaluate(s, rubric.DefaultConfig())
}
