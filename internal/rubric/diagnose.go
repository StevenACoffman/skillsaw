package rubric

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
}

// Diagnose produces the next-target recommendation for an evaluated skill.
func Diagnose(ev *Evaluation) Diagnosis {
	d := Diagnosis{Skill: ev.Skill}

	// §9.3 gate consequence: a runtime hit forces the first round to P0.
	if ev.RuntimeWarn >= 1 {
		d.Target = "P0 runtime drift repair"
		d.Priority = "P0"
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

	d.Target = lowest.Name
	d.TargetNum = lowest.Num
	d.Findings = lowest.Flags
	d.Priority, d.Rationale = strategyFor(lowest.Num)

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
