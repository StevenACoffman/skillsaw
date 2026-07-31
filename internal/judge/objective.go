package judge

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Objective answer-scorer operators, ported from cc-thinking-skills'
// evals/lib/objective.js. Unlike the text-matching ops they parse a terminal
// ANSWER out of the output and compare it to a gold value carried in Check.Arg.
const (
	OpBoolean        Op = "boolean"                    // arg = expected yes/no/true/false
	OpMultipleChoice Op = "multiple_choice"            // arg = expected letter A-E
	OpNumericOOM     Op = "numeric_order_of_magnitude" // arg = "gold[:tolerance]" (tol default 1)
)

var (
	reAnswerBool     = regexp.MustCompile(`(?i)ANSWER:\s*(yes|no|true|false)\b`)
	reAnswerJSONBool = regexp.MustCompile(`(?i)"answer"\s*:\s*(true|false)`)
	reAnswerMC       = regexp.MustCompile(`(?i)ANSWER:\s*([A-E])\b`)
	reTrailingLetter = regexp.MustCompile(`(?i)(?:^|\s)([A-E])\.?\s*$`)
	reAnswerNum      = regexp.MustCompile(
		`(?i)ANSWER:\s*\$?\s*(-?[\d,]*\.?\d+(?:\s*[eE]\s*[-+]?\d+)?(?:\s*[×xX*]\s*10\s*\^?-?\d+)?)`,
	)
	reAnyNum   = regexp.MustCompile(`-?\d[\d,]*\.?\d*(?:[eE][-+]?\d+)?`)
	reSciTimes = regexp.MustCompile(`(?i)[×xX*]\s*10\s*\^?`)
)

// evalObjective dispatches the answer-scorer ops. handled==false means op is not
// an objective op and the caller should fall through to the text-matching ops.
func evalObjective(op Op, output, arg string) (handled, pass bool, why string) {
	switch op {
	case OpBoolean:
		pass, why = evalBoolean(output, arg)
	case OpMultipleChoice:
		pass, why = evalMultipleChoice(output, arg)
	case OpNumericOOM:
		pass, why = evalNumericOOM(output, arg)
	default:
		return false, false, ""
	}
	return true, pass, why
}

// evalBoolean scores a boolean ANSWER against an expected yes/no/true/false arg.
func evalBoolean(output, arg string) (bool, string) {
	want, okArg := parseBoolArg(arg)
	if !okArg {
		return false, "boolean: invalid gold arg " + strconv.Quote(arg)
	}
	got, okParse := parseAnswerBool(output)
	if !okParse {
		return false, "boolean: no yes/no/true/false ANSWER found"
	}
	return boolWhy(got == want, "boolean",
		"got="+strconv.FormatBool(got)+" want="+strconv.FormatBool(want))
}

// evalMultipleChoice scores a letter ANSWER (A-E) against an expected letter.
func evalMultipleChoice(output, arg string) (bool, string) {
	want := strings.ToUpper(strings.TrimSpace(arg))
	if len(want) != 1 || want[0] < 'A' || want[0] > 'E' {
		return false, "multiple_choice: gold letter out of range " + strconv.Quote(arg)
	}
	got := ""
	if m := lastSubmatch(reAnswerMC, output); m != nil {
		got = strings.ToUpper(m[1])
	} else if m := lastSubmatch(reTrailingLetter, output); m != nil {
		got = strings.ToUpper(m[1])
	}
	if got == "" {
		return false, "multiple_choice: no A-E ANSWER found"
	}
	return boolWhy(got == want, "multiple_choice", "got="+got+" want="+want)
}

// evalNumericOOM passes when the parsed ANSWER is within `tolerance` orders of
// magnitude of gold: |log10(|est|/|gold|)| <= tolerance. Arg is "gold[:tol]".
func evalNumericOOM(output, arg string) (bool, string) {
	goldStr, tol := arg, 1.0
	if i := strings.IndexByte(arg, ':'); i >= 0 {
		goldStr = arg[:i]
		t, err := strconv.ParseFloat(strings.TrimSpace(arg[i+1:]), 64)
		if err != nil {
			return false, "numeric_order_of_magnitude: invalid tolerance " + strconv.Quote(
				arg[i+1:],
			)
		}
		tol = t
	}
	gold, okGold := parseNumber(goldStr)
	if !okGold || gold == 0 {
		return false, "numeric_order_of_magnitude: invalid gold " + strconv.Quote(goldStr)
	}
	est, okEst := parseAnswerNumber(output)
	if !okEst || est == 0 {
		return false, "numeric_order_of_magnitude: no finite non-zero numeric ANSWER found"
	}
	delta := math.Abs(math.Log10(math.Abs(est) / math.Abs(gold)))
	return boolWhy(delta <= tol, "numeric_order_of_magnitude",
		"log10_delta="+strconv.FormatFloat(delta, 'f', 3, 64)+
			" tol="+strconv.FormatFloat(tol, 'f', 3, 64))
}

func parseBoolArg(arg string) (want, ok bool) {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "yes", "true", "1":
		return true, true
	case "no", "false", "0":
		return false, true
	default:
		return false, false
	}
}

func parseAnswerBool(output string) (val, ok bool) {
	if m := lastSubmatch(reAnswerBool, output); m != nil {
		v := strings.ToLower(m[1])
		return v == "yes" || v == "true", true
	}
	if m := lastSubmatch(reAnswerJSONBool, output); m != nil {
		return strings.EqualFold(m[1], "true"), true
	}
	return false, false
}

func parseAnswerNumber(output string) (val float64, ok bool) {
	if m := lastSubmatch(reAnswerNum, output); m != nil {
		if f, good := parseNumber(m[1]); good {
			return f, true
		}
	}
	if all := reAnyNum.FindAllString(output, -1); len(all) > 0 {
		if f, good := parseNumber(all[len(all)-1]); good {
			return f, true
		}
	}
	return 0, false
}

// parseNumber accepts plain, comma-grouped, currency-prefixed, scientific
// (1.2e6), and times-power (1.2×10^6 / 1.2x10^6) forms.
func parseNumber(s string) (val float64, ok bool) {
	s = strings.NewReplacer("$", "", ",", "", " ", "", "\t", "").Replace(strings.TrimSpace(s))
	s = reSciTimes.ReplaceAllString(s, "e")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, false
	}
	return f, true
}

// lastSubmatch returns the submatches of the LAST occurrence of re in s (so a
// trailing ANSWER wins over any earlier restatement), or nil if none.
func lastSubmatch(re *regexp.Regexp, s string) []string {
	m := re.FindAllStringSubmatch(s, -1)
	if len(m) == 0 {
		return nil
	}
	return m[len(m)-1]
}
