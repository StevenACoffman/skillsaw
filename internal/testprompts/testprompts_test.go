package testprompts_test

import (
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/judge"
	"github.com/StevenACoffman/skillsaw/internal/testprompts"
)

func TestParseCanonical(t *testing.T) {
	t.Parallel()
	f, err := testprompts.Parse([]byte(`{"skill":"s","tests":[
		{"id":1,"type":"should_trigger","prompt":"p","expected":"e",
		 "checks":[{"op":"contains","arg":"x"}]}]}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Skill != "s" || len(f.Tests) != 1 {
		t.Fatalf("unexpected parse: %+v", f)
	}
	if f.Tests[0].ID != 1 || len(f.Tests[0].Checks) != 1 {
		t.Errorf("case not parsed: %+v", f.Tests[0])
	}
}

func TestParseBareArray(t *testing.T) {
	t.Parallel()
	f, err := testprompts.Parse([]byte(`[{"id":2,"type":"edge_case","prompt":"p","expected":"e"}]`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(f.Tests) != 1 || f.Tests[0].ID != 2 {
		t.Errorf("bare array not parsed: %+v", f.Tests)
	}
}

func TestParseLegacyTestCases(t *testing.T) {
	t.Parallel()
	// Legacy shape: test_cases, string id, expected_behavior.
	f, err := testprompts.Parse([]byte(`{"test_cases":[
		{"id":"should-trigger-01","type":"should_trigger","prompt":"p",
		 "expected_behavior":"invokes the skill"}]}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(f.Tests) != 1 {
		t.Fatalf("legacy not parsed: %+v", f)
	}
	c := f.Tests[0]
	if c.ID != 1 { // non-numeric id falls back to position+1
		t.Errorf("legacy string id should fall back to 1, got %d", c.ID)
	}
	if c.Expected != "invokes the skill" {
		t.Errorf("expected_behavior not mapped: %q", c.Expected)
	}
}

func TestBehavioralAndDecoys(t *testing.T) {
	t.Parallel()
	f, err := testprompts.Parse([]byte(`{"tests":[
		{"id":1,"type":"should_trigger","prompt":"p","expected":"e"},
		{"id":2,"type":"should_not_trigger","prompt":"p","expected":"e"},
		{"id":3,"type":"edge_case","prompt":"p","expected":"e"}]}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := len(f.Behavioral()); got != 2 {
		t.Errorf("Behavioral = %d, want 2 (trigger+edge)", got)
	}
	if got := len(f.Decoys()); got != 1 {
		t.Errorf("Decoys = %d, want 1", got)
	}
}

func TestChecksForPrefersEmbedded(t *testing.T) {
	t.Parallel()
	embedded := testprompts.Case{
		Checks:   []judge.Check{{Op: judge.OpContains, Arg: "x"}},
		Expected: `a "Risks" section`,
	}
	got, derived := testprompts.ChecksFor(&embedded)
	if derived {
		t.Error("embedded checks should not be marked derived")
	}
	if len(got) != 1 || got[0].Arg != "x" {
		t.Errorf("expected embedded checks, got %v", got)
	}
}

func TestChecksForDerivesWhenAbsent(t *testing.T) {
	t.Parallel()
	c := testprompts.Case{Expected: `output contains a "Risks" section`}
	got, derived := testprompts.ChecksFor(&c)
	if !derived {
		t.Error("expected derived=true when no embedded checks")
	}
	if len(got) != 1 || got[0].Op != judge.OpSectionPresent || got[0].Arg != "Risks" {
		t.Errorf("derive mismatch: %v", got)
	}
}

func TestChecksForEmptyWhenNothingInferable(t *testing.T) {
	t.Parallel()
	c := testprompts.Case{Expected: "a generally reasonable response"}
	got, _ := testprompts.ChecksFor(&c)
	if len(got) != 0 {
		t.Errorf("expected no checks, got %v", got)
	}
}
