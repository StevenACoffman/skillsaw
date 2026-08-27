package transcript

// Where a skill's invocation sits relative to the work done around it.
//
// The zero value is the unevaluated one and never reads as a pass, so a transcript nobody
// checked cannot be mistaken for one that came out clean.
const (
	OrderUnknown      Order = ""
	OrderNotTriggered Order = "not-triggered"
	OrderAfterAction  Order = "after-action"
	OrderFirst        Order = "first"
	OrderUnmeasurable Order = "unmeasurable"
)

// Order is the outcome of asking when a skill loaded.
//
// OrderAfterAction is the state this exists to name, and it is worse than
// OrderNotTriggered rather than a milder version of it: the agent had the skill available,
// began work without it, and loaded it afterwards -- so part of the artifact was produced
// outside the guidance it now appears to have followed. A confusion matrix built from a
// prose reply records that as a plain success, because by the end the skill was used.
//
// "Began work" means a tool result had come back before the skill loaded. Issuing calls is
// not working from them: a runtime that fires four tools at once has learned nothing from
// any of them yet, and which one it happened to emit first is not a fact about the skill.
// See actedBefore.
//
// OrderUnmeasurable is the odd one out, and is deliberately not ranked among the others: it
// is the absence of a verdict rather than a bad one. The agent asked for the skill and the
// runtime refused every attempt, so nothing that happened afterwards bears on whether the
// wording works. It exists because the alternative was OrderNotTriggered, which is a claim
// about the description -- and it was the reported outcome for 19 of 27 captured runs in
// which the description was never read. "activation" already calls this shape UNMEASURED.
type Order string

// Valid reports whether o is one of the defined outcomes.
func (o Order) Valid() bool {
	switch o {
	case OrderUnknown, OrderNotTriggered, OrderAfterAction, OrderFirst, OrderUnmeasurable:
		return true
	default:
		return false
	}
}

// planningTools are the tools that do not count as having started work.
//
// Writing down an intention is not acting on one: an agent that lists its steps, or records
// what it is about to do, has not yet changed anything the skill would have changed.
//
// Two runtimes' vocabularies, because a transcript does not say which wrote it. The first
// five are Claude Code's, from the reference harness. update_topic is Gemini CLI's, captured
// from a real run where it fired immediately before the skill was read, carrying only a
// title and a summary of what the agent was about to do -- bookkeeping by any reading.
//
// The list stays here rather than in configuration: one consumer, and no evidence any of it
// needs tuning. rubric.toml is the precedent if that changes.
func planningTools() map[string]bool {
	return map[string]bool{
		"TodoWrite":    true,
		"TaskCreate":   true,
		"TaskUpdate":   true,
		"TaskList":     true,
		"TaskGet":      true,
		"update_topic": true,
	}
}

// actedBefore reports whether an earlier use is work the later one was done on top of.
//
// Uses issued in the same batch are concurrent: neither result had returned when the other
// was issued, so neither could have informed the other. Only a use from a strictly earlier
// batch is work the agent had something to show for.
//
// Requires: a appeared before b in the transcript.
// Ensures:  pure. An unknown batch on either side falls back to document order, which is
// the stricter reading. Two things produce one: a slice built by hand, and a transcript
// whose format logs no tool results, which Read reports as unknown rather than guess. Both
// have to read strictly, because the lenient reading of "unknown" passes everything.
func actedBefore(a, b ToolUse) bool {
	if a.Batch == 0 || b.Batch == 0 {
		return true
	}
	return a.Batch < b.Batch
}

// loadIndex returns the position of the first use that loaded the skill, or -1 if none did.
//
// A rejected call is not a load: the runtime refused it, so the skill was never activated
// and anything the agent did afterwards was done without it.
func loadIndex(uses []ToolUse, skill string) int {
	for i, u := range uses {
		if !u.Failed && u.LoadsSkill(skill) {
			return i
		}
	}
	return -1
}

// OrderOf reports when the named skill loaded relative to the first real action.
//
// Requires: uses are in the order they occurred, as Read returns them; skill is the name
// the skill declares, without a namespace prefix.
// Ensures:  pure. OrderFirst unless some tool outside the planning allowlist ran in an
// earlier batch than the load -- see actedBefore for why the batch and not the position. A
// different skill loading first counts as action: loading the wrong skill and working from
// it is work done under the wrong guidance, not preparation.
//
// The earliest action decides it rather than the nearest one: any action in an earlier
// batch than the load is enough, and the earliest sits in the lowest-numbered batch of all
// of them, so if none qualifies neither does any other.
//
// A call the runtime rejected is skipped entirely -- neither a load nor an action. It
// edited no file, so no artifact was produced outside the guidance; and it loaded no skill,
// so counting it would report guidance the agent never received. When every attempt to
// reach the skill was rejected the answer is OrderUnmeasurable rather than
// OrderNotTriggered: the agent asked, and the runtime is what said no.
func OrderOf(uses []ToolUse, skill string) Order {
	if skill == "" {
		return OrderUnknown
	}
	load := loadIndex(uses, skill)
	if load < 0 {
		if attemptedLoad(uses, skill) {
			return OrderUnmeasurable
		}
		return OrderNotTriggered
	}
	allowed := planningTools()
	for i, u := range uses[:load] {
		if u.Failed || allowed[u.Name] {
			continue
		}
		if actedBefore(uses[i], uses[load]) {
			return OrderAfterAction
		}
		break // the earliest action decides it; a later one is no earlier than this
	}
	return OrderFirst
}

// attemptedLoad reports whether any use referred to the skill, successfully or not.
//
// The difference from loadIndex is the whole of it: a run where every reference failed is
// one the agent tried to reach the skill in, so its outcome is a fact about the runtime and
// not about the wording. Both go through LoadsSkill, so what counts as referring to a skill
// is still defined in exactly one place.
func attemptedLoad(uses []ToolUse, skill string) bool {
	for _, u := range uses {
		if u.LoadsSkill(skill) {
			return true
		}
	}
	return false
}

// Before returns the names of the tools whose results were in before the skill loaded,
// excluding planning tools, in order and without repeats.
//
// A verdict of "work happened first" that does not say what happened is a verdict nobody
// can act on: the fix differs depending on whether the agent read a file or started editing
// one. Empty when the skill loaded first or never loaded at all.
//
// Tools issued in the load's own batch are left out, and so are calls the runtime rejected,
// for the same reasons they do not make the verdict after-action. Naming either would point
// a reader at a cause that is not one -- the worse failure here, since this exists to be
// acted on. An errored activate_skill reported as the work done first is what prompted it.
func Before(uses []ToolUse, skill string) []string {
	load := loadIndex(uses, skill)
	if load < 0 || OrderOf(uses, skill) != OrderAfterAction {
		return nil
	}
	allowed := planningTools()
	return distinctNames(uses[:load], func(u ToolUse) bool {
		return !u.Failed && !allowed[u.Name] && actedBefore(u, uses[load])
	})
}

// Refused returns the names of the tools that tried to reach the skill and were rejected,
// in order and without repeats. Empty unless the verdict is OrderUnmeasurable.
//
// A run reported unmeasurable without saying which door was shut sends the reader to the
// skill, which is the one place the answer is not: the captured corpus shut three of them
// -- activate_skill absent under plan mode, read_file refusing a path outside the
// workspace, and the shell fallback denied as script execution -- and each wants a
// different change to the harness.
func Refused(uses []ToolUse, skill string) []string {
	if OrderOf(uses, skill) != OrderUnmeasurable {
		return nil
	}
	return distinctNames(uses, func(u ToolUse) bool {
		return u.Failed && u.LoadsSkill(skill)
	})
}

// distinctNames returns the names of the uses keep selects, in order and without repeats.
//
// Shared by Before and Refused because both answer "which tools" and neither should be the
// one that gets de-duplication wrong -- a difference a reader would not notice, since both
// outputs look plausible either way.
func distinctNames(uses []ToolUse, keep func(ToolUse) bool) []string {
	var out []string
	seen := map[string]bool{}
	for _, u := range uses {
		if seen[u.Name] || !keep(u) {
			continue
		}
		seen[u.Name] = true
		out = append(out, u.Name)
	}
	return out
}
