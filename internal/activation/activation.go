// Package activation surfaces the activation (trigger-accuracy) signal that the
// type-tagged test-prompts carry (S3). It is a deterministic, explainable proxy:
// does the skill description's trigger vocabulary plausibly match the
// should_trigger prompts and NOT match the should_not_trigger decoys?
//
// It is reported separately and never folded into the 9-dimension rubric total,
// whose weights are fixed at 100 by the darwin spec. Score is pure.
package activation

import (
	"strings"

	"github.com/StevenACoffman/skillsaw/internal/stats"
)

// minTermLen is the shortest token treated as salient; shorter words carry too
// little signal and inflate spurious overlaps.
const minTermLen = 5

// Report is the activation outcome, framed as a routing confusion matrix:
// targets are should_trigger prompts, distractors are should_not_trigger prompts,
// and "firing" means the description's vocabulary overlaps the prompt. Ported
// from cc-thinking-skills' scoreDistractor (evals/lib/stats.js).
type Report struct {
	Targets     int        `json:"targets"`     // should_trigger count
	Distractors int        `json:"distractors"` // should_not_trigger count
	TP          int        `json:"tp"`          // targets that fire (good)
	FN          int        `json:"fn"`          // targets that miss (bad)
	FP          int        `json:"fp"`          // distractors that fire (bad)
	TN          int        `json:"tn"`          // distractors excluded (good)
	TPR         float64    `json:"tpr"`         // TP / targets
	FPR         float64    `json:"fpr"`         // FP / distractors
	FNR         float64    `json:"fnr"`         // FN / targets
	NetUtility  float64    `json:"net_utility"` // (TP - FP) / total, in [-1, 1]
	TPRInterval [2]float64 `json:"tpr_ci95"`    // Wilson 95% interval on TPR
	FPRInterval [2]float64 `json:"fpr_ci95"`    // Wilson 95% interval on FPR
	Why         []string   `json:"why"`
}

// Score rates trigger accuracy for a description against its prompts.
//
// Requires: description is the skill's frontmatter description; triggerPrompts
//
//	are the should_trigger prompts and decoyPrompts the should_not_trigger ones.
//
// Ensures:  a target "fires" (TP) when it shares a salient term with the
//
//	description, else misses (FN); a distractor firing is a false positive
//	(FP), else a true negative (TN). NetUtility = (TP-FP)/total (0 when there
//	are no prompts). TPR/FPR carry Wilson 95% intervals. Pure.
func Score(description string, triggerPrompts, decoyPrompts []string) Report {
	terms := salientTerms(description)
	var r Report
	r.Targets = len(triggerPrompts)
	r.Distractors = len(decoyPrompts)

	for _, p := range triggerPrompts {
		if overlaps(terms, p) {
			r.TP++
			r.Why = append(r.Why, "target fired (good): "+snippet(p))
		} else {
			r.FN++
			r.Why = append(r.Why, "target MISSED (description vocabulary misses it): "+snippet(p))
		}
	}
	for _, p := range decoyPrompts {
		if overlaps(terms, p) {
			r.FP++
			r.Why = append(r.Why, "distractor FIRED (false positive): "+snippet(p))
		} else {
			r.TN++
			r.Why = append(r.Why, "distractor excluded (good): "+snippet(p))
		}
	}

	if r.Targets > 0 {
		r.TPR = float64(r.TP) / float64(r.Targets)
		r.FNR = float64(r.FN) / float64(r.Targets)
	}
	if r.Distractors > 0 {
		r.FPR = float64(r.FP) / float64(r.Distractors)
	}
	if total := r.Targets + r.Distractors; total > 0 {
		r.NetUtility = float64(r.TP-r.FP) / float64(total)
	}
	tprLo, tprHi := stats.Wilson(r.TP, r.Targets)
	r.TPRInterval = [2]float64{tprLo, tprHi}
	fprLo, fprHi := stats.Wilson(r.FP, r.Distractors)
	r.FPRInterval = [2]float64{fprLo, fprHi}
	return r
}

// salientTerms returns the lowercased content-word set of text.
func salientTerms(text string) map[string]bool {
	terms := map[string]bool{}
	for _, tok := range tokenize(text) {
		if len(tok) >= minTermLen && !isStopword(tok) {
			terms[tok] = true
		}
	}
	return terms
}

// isStopword reports whether tok is a high-frequency word that would create
// spurious overlaps between a description and unrelated prompts. Only words of
// at least minTermLen reach here (tokenize filters shorter ones first).
func isStopword(tok string) bool {
	switch tok {
	case "about", "after", "against", "because", "before",
		"could", "should", "would", "there", "their",
		"these", "those", "which", "while", "where",
		"needs", "users", "wants", "skill", "invoke",
		"using", "helps":
		return true
	default:
		return false
	}
}

// overlaps reports whether prompt shares at least one salient term with terms.
func overlaps(terms map[string]bool, prompt string) bool {
	for _, tok := range tokenize(prompt) {
		if terms[tok] {
			return true
		}
	}
	return false
}

// tokenize lowercases and splits text on any non-letter/digit run.
func tokenize(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
}

// snippet trims a prompt to a short, single-line preview for the Why log.
func snippet(p string) string {
	const maxLen = 60
	p = strings.Join(strings.Fields(p), " ")
	if len(p) > maxLen {
		return p[:maxLen] + "…"
	}
	return p
}
