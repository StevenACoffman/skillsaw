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
)

// Order is the outcome of asking when a skill loaded.
//
// OrderAfterAction is the state this exists to name, and it is worse than
// OrderNotTriggered rather than a milder version of it: the agent had the skill available,
// began work without it, and loaded it afterwards -- so part of the artifact was produced
// outside the guidance it now appears to have followed. A confusion matrix built from a
// prose reply records that as a plain success, because by the end the skill was used.
type Order string

// Valid reports whether o is one of the four defined outcomes.
func (o Order) Valid() bool {
	switch o {
	case OrderUnknown, OrderNotTriggered, OrderAfterAction, OrderFirst:
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

// OrderOf reports when the named skill loaded relative to the first real action.
//
// Requires: uses are in the order they occurred, as Read returns them; skill is the name
// the skill declares, without a namespace prefix.
// Ensures:  pure. OrderFirst only when no tool outside the planning allowlist ran before
// the skill loaded. A different skill loading first counts as action -- loading the wrong
// skill and working from it is work done under the wrong guidance, not preparation.
func OrderOf(uses []ToolUse, skill string) Order {
	if skill == "" {
		return OrderUnknown
	}
	allowed := planningTools()
	acted := false
	for _, u := range uses {
		if u.LoadsSkill(skill) {
			if acted {
				return OrderAfterAction
			}
			return OrderFirst
		}
		if !allowed[u.Name] {
			acted = true
		}
	}
	return OrderNotTriggered
}

// Before returns the names of the tools that ran before the skill loaded, excluding
// planning tools, in order and without repeats.
//
// A verdict of "work happened first" that does not say what happened is a verdict nobody
// can act on: the fix differs depending on whether the agent read a file or started editing
// one. Empty when the skill loaded first or never loaded at all.
func Before(uses []ToolUse, skill string) []string {
	if OrderOf(uses, skill) != OrderAfterAction {
		return nil
	}
	allowed := planningTools()
	var out []string
	seen := map[string]bool{}
	for _, u := range uses {
		if u.LoadsSkill(skill) {
			break
		}
		if allowed[u.Name] || seen[u.Name] {
			continue
		}
		seen[u.Name] = true
		out = append(out, u.Name)
	}
	return out
}
