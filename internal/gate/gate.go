// Package gate is the validation gate — the accept/reject decision at the heart
// of the darwin ratchet (spec §8.6). It is a direct port of SkillOpt's pure
// decision function (skillopt/evaluation/gate.py): a candidate is accepted only
// if it strictly beats the current score; it becomes the new best only if it
// strictly beats the best score. Ties reject and do not promote.
package gate

import "fmt"

// Metric value constants select how a (hard, soft) pair is projected onto one
// comparable score.
const (
	Hard  Metric = "hard"
	Soft  Metric = "soft"
	Mixed Metric = "mixed"
)

// Action value constants are the possible gate outcomes.
const (
	AcceptNewBest Action = "accept_new_best"
	Accept        Action = "accept"
	Reject        Action = "reject"
)

// Metric selects how a (hard, soft) pair is projected onto one comparable score.
type Metric string

// Action is the gate outcome.
type Action string

// Result is the immutable gate outcome plus the resulting state, mirroring
// SkillOpt's GateResult.
type Result struct {
	Action       Action  `json:"action"`
	CurrentScore float64 `json:"current_score"`
	BestScore    float64 `json:"best_score"`
	BestStep     int     `json:"best_step"`
}

// SelectScore projects (hard, soft) onto a single metric — a port of SkillOpt's
// select_gate_score. darwin uses one weighted rubric total, so callers usually
// pass that total as "hard" with the default metric.
func SelectScore(hard, soft float64, metric Metric, mixedWeight float64) (float64, error) {
	switch metric {
	case Hard:
		return hard, nil
	case Soft:
		return soft, nil
	case Mixed:
		w := mixedWeight
		if w < 0 {
			w = 0
		}
		if w > 1 {
			w = 1
		}
		return (1.0-w)*hard + w*soft, nil
	default:
		return 0, fmt.Errorf("unknown gate metric %q; expected hard, soft, or mixed", metric)
	}
}

// Evaluate compares an already-projected candidate score against current/best
// using strict ">" at both comparisons (spec §8.6, SkillOpt §21.4). globalStep
// is recorded as the new best step when a new best is accepted.
func Evaluate(cand, current, best float64, bestStep, globalStep int) Result {
	switch {
	case cand > current && cand > best:
		return Result{
			Action:       AcceptNewBest,
			CurrentScore: cand,
			BestScore:    cand,
			BestStep:     globalStep,
		}
	case cand > current:
		return Result{Action: Accept, CurrentScore: cand, BestScore: best, BestStep: bestStep}
	default:
		return Result{Action: Reject, CurrentScore: current, BestScore: best, BestStep: bestStep}
	}
}
