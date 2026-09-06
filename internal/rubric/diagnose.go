package rubric

import (
	"fmt"

	"github.com/StevenACoffman/skillet/finding"
)

// healthyFinal is the per-dimension score at or above which a dimension is not a
// deterministic weakness worth flagging. Needs-judge dims floor at 10 and derived
// dims cap at 9, so without this bar the diagnosis would always point at whichever
// derived dim sits at 9 — even on a structurally excellent skill.
const healthyFinal = 8

// Diagnosis is the deterministic half of a darwin Phase 2 optimization round
// (spec §11.3 Step 1): identify the weakest dimension, warn about the
// dim2/3/4 correlated cluster, and route to a strategy-library priority (§12).
// The actual edit is the LLM's job; this tells a model precisely what to target.
type Diagnosis struct {
	Skill       string   `json:"skill"`
	Target      string   `json:"target"` // dimension name or "P0 runtime drift"
	TargetNum   int      `json:"target_num"`
	Priority    string   `json:"priority"` // P0..P3
	Rationale   string   `json:"rationale"`
	ClusterNote string   `json:"cluster_note,omitempty"`
	Findings    []string `json:"findings,omitempty"`
	// Response is how the target dimension's score moves when the artifact gains more
	// of what it counts (rubric.Dimensions). It travels with the diagnosis because the
	// loop that consumes this is an optimiser, and "the cheap move on this dimension is
	// to add three of something" is a fact it should have without parsing Rationale.
	Response Response `json:"response,omitempty"`
	// Action says who can close the diagnosed target: the loop picks one edit per round,
	// and without this it decides "is this safe to apply unattended" implicitly.
	//
	// Orthogonal to Priority, which answers how urgent rather than who acts, and must not be
	// derived from it: a P0 runtime hit needs a person, and a P3 frontmatter cap does not
	// become automatable by being unimportant.
	//
	// Unset when there is no target -- nothing scored, or every dimension healthy. Claiming a
	// human is required to fix nothing would be a false instruction, which is the same reason
	// finding has no ActionUnknown constant.
	Action finding.Action `json:"action,omitempty"`

	// Transition is how the target dimension moved since the evaluation this was
	// diagnosed against, and is unset when there was nothing to compare with.
	//
	// It changes what the next attempt should be, which is why it travels with the
	// diagnosis rather than being left for a reader to work out: a dimension that
	// regressed has a known-good previous version, so the cheap move is to read the diff
	// between them. One that has never been clean has no diff to read.
	Transition Transition `json:"transition,omitempty"`

	// PreviousFinal is the target's score in that earlier evaluation. Only meaningful
	// when Transition is Regressed or Persistent.
	PreviousFinal int `json:"previous_final,omitempty"`
}

// actionFor says who can close a defect in dimension num.
//
// A fixed table, not a measurement. Nothing here is ActionAutomatic, and that is a finding
// rather than an omission: every skillsaw defect is closed by editing prose whose correctness
// depends on what the skill means, so no tool here can close one unattended. canonizer
// reached the same conclusion independently over a different artifact.
func actionFor(num int) finding.Action {
	switch num {
	case 1, 4, 6:
		// A shorter description, a checkpoint marker, a link target: a tool can propose the
		// text, and only a person knows whether it still says what the skill does.
		return finding.ActionGuided
	default:
		// 2, 3, 5, 7, 8, 9 -- workflow, failure modes, specificity, architecture,
		// effectiveness and counter-examples each need domain knowledge the tool lacks.
		return finding.ActionHuman
	}
}

// Diagnose produces the next-target recommendation for an evaluated skill.
func Diagnose(ev *Evaluation) Diagnosis {
	return DiagnoseAgainst(ev, nil)
}

// DiagnoseAgainst is Diagnose with a previous evaluation to compare the target against.
//
// prev may be nil, which is the ordinary case and leaves Transition unset. It is a separate
// entry point rather than a variadic or an option because the comparison changes what the
// diagnosis recommends, and a caller passing nothing should read as having no baseline
// rather than as having found no change.
//
// Requires: prev, when non-nil, evaluates the same skill.
// Ensures:  pure; identical to Diagnose when prev is nil.
func DiagnoseAgainst(ev, prev *Evaluation) Diagnosis {
	d := Diagnosis{Skill: ev.Skill}

	// §9.3 gate consequence: a runtime hit forces the first round to P0.
	if ev.RuntimeWarn >= 1 {
		d.Target = "P0 runtime drift repair"
		d.Priority = "P0"
		// Deciding what a runtime-bound phrase should say instead is a rewrite.
		d.Action = finding.ActionHuman
		d.Rationale = "runtime-neutrality gate hit; must be fixed before any dimension (spec §9.3, §12 P0)"
		return d
	}

	// Lowest Final dimension wins; ties break to the lowest dimension number.
	var lowest *DimScore
	for i := range ev.Dims {
		ds := &ev.Dims[i]
		if lowest == nil || ds.Final < lowest.Final {
			lowest = ds
		}
	}
	if lowest == nil {
		d.Target = "none"
		d.Rationale = "no dimensions scored"
		return d
	}

	// When even the weakest deterministic dimension is healthy, there is nothing
	// specific to fix by machine: differentiation now depends on the judge dims.
	if lowest.Final >= healthyFinal {
		d.Target = "no deterministic weakness — score the judge dimensions"
		d.Priority = "judge"
		d.Rationale = "every deterministic dimension is healthy; further gains require judging dims 2/3/5/7/8"
		return d
	}

	d.Target = lowest.Name
	d.TargetNum = lowest.Num
	d.Action = actionFor(lowest.Num)
	d.Findings = lowest.Flags
	d.Priority, d.Rationale = strategyFor(lowest.Num)
	d.Response = responseFor(lowest.Num)
	if caveat := responseCaveat(d.Response); caveat != "" {
		d.Rationale += caveat
	}

	d.Transition = TransitionOf(prev, ev, lowest.Num)
	if before, ok := priorFinal(prev, lowest.Num, d.Transition); ok {
		d.PreviousFinal = before
	}
	d.Rationale += transitionNote(d.Transition, d.PreviousFinal)

	if inCluster(lowest.Num) {
		d.ClusterNote = "dims 2/3/4 are a correlated cluster — inspect all three before editing; " +
			"fixing the lowest often lifts the others (spec §11.3 HL-3)."
	}
	return d
}

// inCluster reports whether a dimension is in the correlated dim2/3/4 cluster
// (spec §11.3 HL-3): fixing one often lifts the others.
func inCluster(num int) bool {
	return num == 2 || num == 3 || num == 4
}

// responseFor is the target dimension's declared score response, or unclassified
// when the number names no dimension.
func responseFor(num int) Response {
	for _, dim := range Dimensions() {
		if dim.Num == num {
			return dim.Response
		}
	}
	return ResponseUnclassified
}

// responseCaveat qualifies a strategy whose dimension an optimiser can feed.
//
// The strategies below name the cheap move on their own dimension -- "insert explicit
// checkpoints", "add 'if X fails then Y' fallbacks", "add counter-examples" -- and the
// loop reading them is a hill climber. Unqualified, they are instructions for raising a
// number without changing the artifact, which is the failure this repo watched a
// surveyed optimiser commit and log as a win. So say what does not count.
//
// Subtractive gets a caveat of its own because the advice and the score disagree there,
// and a reader who only sees the strategy would not know it.
func responseCaveat(r Response) string {
	switch r {
	case ResponseAdditive:
		return " — this dimension rises when you add what it counts, so add things that " +
			"describe real behaviour: filler scores the same and is the defect the " +
			"dimension exists to catch"
	case ResponseSubtractive:
		return " — note the deterministic score falls when this is derived rather than " +
			"judged, so a genuine improvement here can show as a lower number until a " +
			"judge scores it"
	case ResponseNeutral, ResponseUnclassified:
		return ""
	default:
		return ""
	}
}

// strategyFor maps a target dimension to its strategy-library priority and a
// one-line rationale (spec §12).
func strategyFor(num int) (priority, rationale string) {
	switch num {
	case 8:
		return "P0", "effectiveness gap: check for misleading/over-constraining instructions or missing output template (§12 P0 effectiveness)"
	case 1, 2, 4:
		return "P1", "structural gap: add trigger words / linearize workflow / insert explicit checkpoints (§12 P1)"
	case 3, 5:
		return "P2", "specificity gap: replace vague steps with concrete params and add 'if X fails then Y' fallbacks (§12 P2)"
	case 6, 7, 9:
		return "P3", "readability/structure gap: split long sections, dedupe, add counter-examples / quick-reference (§12 P3)"
	default:
		return "P2", "target the lowest-scoring dimension"
	}
}

// priorFinal returns the target's earlier score, and whether reporting it means anything.
// A not-compared or new transition has no comparable prior, and printing one would invite
// a reader to subtract two numbers that are not on the same scale.
func priorFinal(prev *Evaluation, num int, tr Transition) (int, bool) {
	if prev == nil || (tr != TransitionRegressed && tr != TransitionPersistent) {
		return 0, false
	}
	return findDim(prev, num)
}

// transitionNote appends what the transition changes about the next attempt. Only a
// regression gets one: it is the case with a known-good previous version, and the whole
// value of noticing it is that the next move is reading a diff rather than rewriting.
func transitionNote(tr Transition, before int) string {
	switch tr {
	case TransitionRegressed:
		return fmt.Sprintf(" — this dimension scored %d in the evaluation compared against "+
			"and is lower now; read the diff since then rather than reworking it from scratch",
			before)
	case TransitionNotCompared:
		return " — not compared against the earlier evaluation, which was produced under a " +
			"different rubric edition; scores from different rules are not a before and after"
	case TransitionNew, TransitionPersistent, TransitionUnknown:
		return ""
	default:
		return ""
	}
}
