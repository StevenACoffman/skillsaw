// Package calibrate turns recorded judge-dimension scores into calibration
// samples, so a run can ask whether the agent scoring the judge-only dimensions is
// systematically over- or under-confident.
//
// The subject is a prediction the outcome cannot see. In the optimize loop that is
// the agent's stated confidence that an edit will pass the validation gate: the
// confidence is fixed when the edit is proposed, and `skillsaw gate` decides after
// the independent re-score, so neither derives from the other.
//
// Two nearby quantities are deliberately NOT the subject. A deterministic rubric
// total has no variance to calibrate — it is a function of the file. And the dim-8
// base only looks independent: it is computed from the judge output
// (round(10 × mean soft)), so scoring it against judge's hard verdict would measure
// how the checks were written rather than how well the agent judges.
//
// Every function is pure; the command owns the file I/O.
package calibrate

import "github.com/StevenACoffman/skillet/calibration"

// MinBase and MaxBase bound a stated confidence. The scale is the rubric's 1-10, so
// a value outside it was never a rating the loop could have produced.
const (
	MinBase = 1
	MaxBase = 10
)

// Judgment is one recorded prediction: the confidence stated before an outcome was
// known, the dimension it concerned, and what the outcome turned out to be.
type Judgment struct {
	Skill  string `json:"skill"`
	Dim    int    `json:"dim"`
	Base   int    `json:"base"`   // the confidence stated beforehand, MinBase..MaxBase
	Passed bool   `json:"passed"` // what the outcome turned out to be
}

// Confidence maps a stated 1-10 rating onto the [0,1] scale calibration works in.
//
// This is a stated convention, not a probability: a base of 8 does not assert "80%
// chance of passing". MinBase is the rubric's floor, so it maps to 0 and MaxBase to
// 1. What the resulting report legitimately shows is the relative signal — accuracy
// sitting consistently below confidence across bins means the judge is
// systematically overconfident, whatever the absolute scale is taken to mean. Read
// the Brier score with that caveat.
//
// Ensures: the result is in [0,1] for a base in [MinBase, MaxBase]; it is pure.
func Confidence(base int) float64 {
	return float64(base-MinBase) / float64(MaxBase-MinBase)
}

// Samples converts judgments into calibration samples, dropping any whose Base falls
// outside [MinBase, MaxBase].
//
// A base out of range is dropped rather than clamped: clamping would invent a
// judgment the agent never made and quietly skew the report, which is the same
// reason calibration.Compute skips a confidence outside [0,1]. The caller reports the
// difference between the input and output lengths so the drop is visible.
//
// Ensures: len(result) is the number of in-range judgments; it is pure and does not
//
//	mutate js.
func Samples(js []Judgment) []calibration.Sample {
	out := make([]calibration.Sample, 0, len(js))
	for _, j := range js {
		if j.Base < MinBase || j.Base > MaxBase {
			continue
		}
		out = append(out, calibration.Sample{
			Confidence: Confidence(j.Base),
			Correct:    j.Passed,
		})
	}
	return out
}
