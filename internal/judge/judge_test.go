package judge_test

import (
	"math"
	"strings"
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/judge"
)

const eps = 1e-9

func TestScoreSingleCheck(t *testing.T) {
	t.Parallel()
	const output = "# Brief\n## Key Risks\nsome body text\nConfidence: High\ncall search(\"q\")"
	tests := []struct {
		name string
		op   judge.Op
		arg  string
		want bool
	}{
		{name: "section_present found", op: judge.OpSectionPresent, arg: "Key Risks", want: true},
		{name: "section_present missing", op: judge.OpSectionPresent, arg: "Appendix", want: false},
		{name: "regex match", op: judge.OpRegex, arg: `[Cc]onfidence\s*:`, want: true},
		{name: "regex no match", op: judge.OpRegex, arg: `^NOPE`, want: false},
		{name: "regex invalid fails", op: judge.OpRegex, arg: `[unclosed`, want: false},
		{name: "contains hit", op: judge.OpContains, arg: "body text", want: true},
		{name: "contains miss", op: judge.OpContains, arg: "absent", want: false},
		{name: "tool_called hit", op: judge.OpToolCalled, arg: "search(", want: true},
		{name: "tool_called miss", op: judge.OpToolCalled, arg: "browse(", want: false},
		{name: "max_chars under", op: judge.OpMaxChars, arg: "1000", want: true},
		{name: "max_chars over", op: judge.OpMaxChars, arg: "5", want: false},
		{name: "max_chars invalid arg fails", op: judge.OpMaxChars, arg: "big", want: false},
		{name: "min_chars over", op: judge.OpMinChars, arg: "5", want: true},
		{name: "min_chars under", op: judge.OpMinChars, arg: "10000", want: false},
		{name: "unknown op fails", op: judge.Op("bogus"), arg: "x", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := judge.Score(output, []judge.Check{{Op: tt.op, Arg: tt.arg}})
			if err != nil {
				t.Fatalf("Score returned error: %v", err)
			}
			wantHard := 0.0
			if tt.want {
				wantHard = 1.0
			}
			if math.Abs(res.Hard-wantHard) > eps {
				t.Errorf("Hard = %v, want %v (why: %v)", res.Hard, wantHard, res.Why)
			}
			if len(res.Why) != 1 {
				t.Errorf("expected 1 why line, got %d", len(res.Why))
			}
		})
	}
}

func TestScoreAggregate(t *testing.T) {
	t.Parallel()
	const output = "only alpha here"
	tests := []struct {
		name     string
		checks   []judge.Check
		wantHard float64
		wantSoft float64
	}{
		{
			name: "all pass -> hard 1",
			checks: []judge.Check{
				{Op: judge.OpContains, Arg: "alpha"},
				{Op: judge.OpContains, Arg: "here"},
			},
			wantHard: 1.0, wantSoft: 1.0,
		},
		{
			name: "partial -> soft fraction, hard 0",
			checks: []judge.Check{
				{Op: judge.OpContains, Arg: "alpha"},
				{Op: judge.OpContains, Arg: "beta"},
			},
			wantHard: 0.0, wantSoft: 0.5,
		},
		{
			name: "none pass -> soft 0",
			checks: []judge.Check{
				{Op: judge.OpContains, Arg: "beta"},
				{Op: judge.OpContains, Arg: "gamma"},
			},
			wantHard: 0.0, wantSoft: 0.0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := judge.Score(output, tt.checks)
			if err != nil {
				t.Fatalf("Score returned error: %v", err)
			}
			if math.Abs(res.Hard-tt.wantHard) > eps {
				t.Errorf("Hard = %v, want %v", res.Hard, tt.wantHard)
			}
			if math.Abs(res.Soft-tt.wantSoft) > eps {
				t.Errorf("Soft = %v, want %v", res.Soft, tt.wantSoft)
			}
			if len(res.Why) != len(tt.checks) {
				t.Errorf("why lines = %d, want %d", len(res.Why), len(tt.checks))
			}
		})
	}
}

func TestScoreNoChecksErrors(t *testing.T) {
	t.Parallel()
	if _, err := judge.Score("anything", nil); err == nil {
		t.Fatal("Score with no checks should return an error")
	}
}

func TestScoreUnicodeCharCount(t *testing.T) {
	t.Parallel()
	// "café日本" is 6 runes but more than 6 bytes; max_chars counts runes.
	res, err := judge.Score("café日本", []judge.Check{{Op: judge.OpMaxChars, Arg: "6"}})
	if err != nil {
		t.Fatalf("Score returned error: %v", err)
	}
	if res.Hard != 1.0 {
		t.Errorf("expected rune-count <= 6 to pass, got why: %v", strings.Join(res.Why, "; "))
	}
}
