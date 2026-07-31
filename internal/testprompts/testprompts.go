// Package testprompts is skillsaw's reader for the test-prompts.json contract it
// shares with exegesis. It accepts the canonical {"tests":[...]} shape, a bare
// top-level array, and the legacy {"test_cases":[...]} shape with
// "expected_behavior" (S2), normalizing all three into one form. A case carries
// an activation Type and optional Checks; ChecksFor bridges to judge (S1).
//
// Parsing is pure; only Load touches the filesystem.
package testprompts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/StevenACoffman/skillsaw/internal/judge"
)

// Case types (the activation composition).
const (
	TypeShouldTrigger    = "should_trigger"
	TypeShouldNotTrigger = "should_not_trigger"
	TypeEdgeCase         = "edge_case"
)

// Case is one normalized test prompt. Checks reuses judge.Check so a file's
// embedded checks feed judge directly.
type Case struct {
	ID       int
	Type     string
	Prompt   string
	Expected string
	Checks   []judge.Check
}

// File is a parsed, normalized test-prompts.json.
type File struct {
	Skill string
	Tests []Case
}

// rawCase tolerates every accepted on-disk shape: a numeric or string id, and
// either "expected" or the legacy "expected_behavior".
type rawCase struct {
	ID               json.RawMessage `json:"id"`
	Type             string          `json:"type"`
	Prompt           string          `json:"prompt"`
	Expected         string          `json:"expected"`
	ExpectedBehavior string          `json:"expected_behavior"`
	Checks           []judge.Check   `json:"checks"`
}

// Load reads and parses a test-prompts.json from path.
func Load(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("testprompts: read %s: %w", path, err)
	}
	f, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("testprompts: parse %s: %w", path, err)
	}
	return f, nil
}

// Parse normalizes any accepted shape into a File.
//
// Requires: b is JSON: an object with "tests" or "test_cases", or a bare array.
// Ensures:  every returned Case has a positive ID (position-derived when the
//
//	source id is non-numeric) and Expected populated from either "expected"
//	or "expected_behavior"; it is pure.
func Parse(b []byte) (*File, error) {
	if trimmed := bytes.TrimSpace(b); len(trimmed) > 0 && trimmed[0] == '[' {
		var arr []rawCase
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return nil, fmt.Errorf("bare array: %w", err)
		}
		return &File{Tests: normalize(arr)}, nil
	}
	var obj struct {
		Skill     string    `json:"skill"`
		Tests     []rawCase `json:"tests"`
		TestCases []rawCase `json:"test_cases"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return nil, fmt.Errorf("object: %w", err)
	}
	cases := obj.Tests
	if len(cases) == 0 {
		cases = obj.TestCases
	}
	return &File{Skill: obj.Skill, Tests: normalize(cases)}, nil
}

// Behavioral returns the cases whose output quality is worth judging:
// should_trigger and edge_case (a decoy has no good output to score).
func (f *File) Behavioral() []Case {
	out := make([]Case, 0, len(f.Tests))
	for _, c := range f.Tests {
		if c.Type == TypeShouldTrigger || c.Type == TypeEdgeCase {
			out = append(out, c)
		}
	}
	return out
}

// Decoys returns the should_not_trigger cases.
func (f *File) Decoys() []Case {
	out := make([]Case, 0, len(f.Tests))
	for _, c := range f.Tests {
		if c.Type == TypeShouldNotTrigger {
			out = append(out, c)
		}
	}
	return out
}

// Find returns the case with the given id.
func (f *File) Find(id int) (Case, bool) {
	for _, c := range f.Tests {
		if c.ID == id {
			return c, true
		}
	}
	return Case{}, false
}

// ChecksFor returns the checks to judge c against: its embedded Checks when
// present, otherwise checks derived from Expected. derived reports which source
// was used; an empty result means neither was available and the caller must
// treat the case as needing hand-written checks rather than silently passing.
func ChecksFor(c *Case) (checks []judge.Check, derived bool) {
	if len(c.Checks) > 0 {
		return c.Checks, false
	}
	return DeriveChecks(c.Expected), true
}

func normalize(raw []rawCase) []Case {
	cases := make([]Case, 0, len(raw))
	for i, r := range raw {
		expected := r.Expected
		if expected == "" {
			expected = r.ExpectedBehavior
		}
		cases = append(cases, Case{
			ID:       caseID(r.ID, i),
			Type:     r.Type,
			Prompt:   r.Prompt,
			Expected: expected,
			Checks:   r.Checks,
		})
	}
	return cases
}

// caseID reads a numeric id, falling back to position+1 for string ids like
// "should-trigger-01" so every case has a stable positive integer id.
func caseID(raw json.RawMessage, index int) int {
	if len(raw) > 0 {
		var n int
		if err := json.Unmarshal(raw, &n); err == nil {
			return n
		}
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			if n, err := strconv.Atoi(s); err == nil {
				return n
			}
		}
	}
	return index + 1
}
