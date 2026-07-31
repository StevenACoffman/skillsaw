package testprompts

import (
	"regexp"
	"strings"

	"github.com/StevenACoffman/skillsaw/internal/judge"
)

// Derivation patterns mirror exegesis's DeriveChecks byte-for-byte (the two
// tools share the contract across module boundaries). Each cue is conservative:
// a check is emitted only when unambiguous, so a derived set never fails judge
// on a guess.
var (
	reSectionQuoted = regexp.MustCompile(`"([^"]+)"\s+section`)
	reHeading       = regexp.MustCompile(`(?m)^#{1,6}\s+(.+?)\s*$`)
	reToolQuoted    = regexp.MustCompile("[\"`]([^\"`]+)[\"`]\\s+tool")
	reContains      = regexp.MustCompile(`(?i)(?:contains|includes|mentions|outputs)\s+"([^"]+)"`)
	reMaxChars      = regexp.MustCompile(`(?i)(?:<=|≤|under|at most|no more than)\s+(\d+)\s+char`)
	reMinChars      = regexp.MustCompile(`(?i)(?:>=|≥|at least|no fewer than)\s+(\d+)\s+char`)
)

// DeriveChecks converts an Expected description into deterministic judge checks.
//
// Requires: expected is the human description of a good output.
// Ensures:  every returned Check is backed by an unambiguous cue in expected;
//
//	returns nil (not a wrong guess) when nothing is inferable; the result
//	is de-duplicated and stably ordered for identical input.
func DeriveChecks(expected string) []judge.Check {
	var checks []judge.Check
	seen := map[judge.Check]bool{}
	add := func(op judge.Op, arg string) {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			return
		}
		c := judge.Check{Op: op, Arg: arg}
		if !seen[c] {
			seen[c] = true
			checks = append(checks, c)
		}
	}
	for _, m := range reSectionQuoted.FindAllStringSubmatch(expected, -1) {
		add(judge.OpSectionPresent, m[1])
	}
	for _, m := range reHeading.FindAllStringSubmatch(expected, -1) {
		add(judge.OpSectionPresent, m[1])
	}
	for _, m := range reToolQuoted.FindAllStringSubmatch(expected, -1) {
		add(judge.OpToolCalled, m[1])
	}
	for _, m := range reContains.FindAllStringSubmatch(expected, -1) {
		add(judge.OpContains, m[1])
	}
	for _, m := range reMaxChars.FindAllStringSubmatch(expected, -1) {
		add(judge.OpMaxChars, m[1])
	}
	for _, m := range reMinChars.FindAllStringSubmatch(expected, -1) {
		add(judge.OpMinChars, m[1])
	}
	return checks
}
