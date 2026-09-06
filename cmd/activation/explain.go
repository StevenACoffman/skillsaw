package activation

import (
	"fmt"

	"github.com/StevenACoffman/skillsaw/internal/noise"
)

// explain renders a gate outcome as the one line a reader needs in order to act on it.
// An unresolved result gets told what would change it, because the fix is to write more
// test-prompts rather than to edit the skill, and a bare non-zero exit sends the reader to
// the wrong one.
func explain(r *report, floor float64) string {
	span := fmt.Sprintf("bound [%+.2f,%+.2f] against min %+.2f", r.Bound[0], r.Bound[1], floor)
	switch r.Gate {
	case noise.VerdictClears:
		return "clears — " + span
	case noise.VerdictBelow:
		return "BELOW — " + span
	case noise.VerdictUnresolved:
		return fmt.Sprintf(
			"UNRESOLVED — %s; the bound spans it, so %d targets and %d distractors cannot "+
				"tell an improvement from noise. Add test-prompts.",
			span, r.Activation.Targets, r.Activation.Distractors)
	case noise.VerdictUnknown:
		return "not evaluated — " + span
	default:
		return string(r.Gate) + " — " + span
	}
}
