package rubric

import (
	"fmt"
	"strings"
)

// Hit is one term of a word list matching a text, and how often.
type Hit struct {
	Term  string
	Count int
}

// SlopHits reports every dim-7 slop term occurring in prose.
//
// Extracted so the soundness check and the scoring check cannot disagree. A self-test that
// matched by its own rules would report on a rule nobody applies, which is the failure it
// exists to catch in the first place.
func (c *Config) SlopHits(prose string) []Hit {
	return hitsIn(prose, c.Slop)
}

// MarkerHits reports every dim-4 checkpoint marker occurring in prose.
func (c *Config) MarkerHits(prose string) []Hit {
	return hitsIn(prose, c.CheckpointMarkers)
}

// HasFillerTail reports the dim-1 filler phrase a description ends on, if any.
//
// Anchored at the end rather than counted anywhere, because the defect is a description
// that trails off into vagueness -- "use your judgment" as the last thing said. The same
// words mid-sentence are ordinary prose and this must not see them.
func (c *Config) HasFillerTail(desc string) (string, bool) {
	trimmed := strings.TrimRight(desc, "。.\" '")
	for _, tail := range c.FillerTails {
		if strings.HasSuffix(trimmed, tail) {
			return tail, true
		}
	}
	return "", false
}

// SelfTest runs each word list against prose it must stay quiet on, and reports every term
// that fires rather than the first -- an author fixing one wants to see the rest.
//
// Soundness before completeness, and only soundness: a positive example for a substring
// rule is a string containing the substring, which asserts nothing. The negative case is
// the one that carries information here, and it is the one nobody writes unprompted.
//
// This is a method rather than a test so that the same check can run at load time once the
// rubric is data. It is deliberately not run at load today: the lists are a compiled-in Go
// literal, so a test already *is* the load-time check -- it runs before the binary ships,
// which is the property a file-backed ruleset has to buy at runtime.
//
// Ensures: pure; nil when every list stays quiet on prose it should ignore.
func (c *Config) SelfTest() error {
	var fired []string
	for _, q := range quietCases() {
		for _, h := range c.hitsFor(q.list, q.text) {
			if q.allowed[h.Term] != "" {
				continue
			}
			fired = append(fired, fmt.Sprintf("%s: %q fires on ordinary prose %q",
				q.list, h.Term, q.text))
		}
	}
	if len(fired) > 0 {
		return fmt.Errorf("rubric rules are unsound:\n  %s", strings.Join(fired, "\n  "))
	}
	return nil
}

// hitsFor dispatches to the matcher that governs one list.
func (c *Config) hitsFor(list string, text string) []Hit {
	switch list {
	case listSlop:
		return c.SlopHits(text)
	case listMarkers:
		return c.MarkerHits(text)
	case listFillerTails:
		if tail, ok := c.HasFillerTail(text); ok {
			return []Hit{{Term: tail, Count: 1}}
		}
		return nil
	default:
		return nil
	}
}

// hitsIn counts each term's occurrences in text, skipping terms that do not occur.
func hitsIn(text string, terms []string) []Hit {
	var out []Hit
	for _, t := range terms {
		if n := strings.Count(text, t); n > 0 {
			out = append(out, Hit{Term: t, Count: n})
		}
	}
	return out
}
