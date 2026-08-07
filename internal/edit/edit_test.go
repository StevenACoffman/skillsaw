package edit_test

import (
	"strings"
	"testing"

	"github.com/StevenACoffman/skillet/skill"
	"github.com/StevenACoffman/skillsaw/internal/edit"
)

// riaBody carries all six RIA-TV++ segments, so a case can omit exactly one.
const riaBody = "## R\n\n## I\n\n## A1\n\n## A2\n\n## E\n\n## B\n"

func TestWithinSizeBudget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		orig  int
		newB  int
		ratio float64
		want  bool
	}{
		{name: "exactly at 1.5x", orig: 100, newB: 150, ratio: 1.5, want: true},
		{name: "just over 1.5x", orig: 100, newB: 151, ratio: 1.5, want: false},
		{name: "under budget", orig: 100, newB: 120, ratio: 1.5, want: true},
		{name: "unchanged", orig: 100, newB: 100, ratio: 1.5, want: true},
		{name: "shrunk", orig: 100, newB: 40, ratio: 1.5, want: true},
		{name: "zero orig allows empty", orig: 0, newB: 0, ratio: 1.5, want: true},
		{name: "zero orig rejects growth", orig: 0, newB: 1, ratio: 1.5, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := edit.WithinSizeBudget(tt.orig, tt.newB, tt.ratio); got != tt.want {
				t.Errorf("WithinSizeBudget(%d,%d,%v) = %v, want %v",
					tt.orig, tt.newB, tt.ratio, got, tt.want)
			}
		})
	}
}

func TestIsNoOp(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		before, after string
		want          bool
	}{
		{name: "identical", before: "same text", after: "same text", want: true},
		{name: "both empty", before: "", after: "", want: true},
		{name: "different", before: "a", after: "b", want: false},
		{name: "whitespace differs", before: "x", after: "x ", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := edit.IsNoOp(tt.before, tt.after); got != tt.want {
				t.Errorf("IsNoOp(%q,%q) = %v, want %v", tt.before, tt.after, got, tt.want)
			}
			// Property: a value is always a no-op against itself (idempotent identity).
			if !edit.IsNoOp(tt.before, tt.before) {
				t.Errorf("IsNoOp(%q,%q) should be true", tt.before, tt.before)
			}
		})
	}
}

// TestStructuralDefects checks that both families of rule are wired in, not what
// each rule says. The rules themselves are skillet's to define and to test; asserting
// their wording here would break skillsaw every time skillet rephrases a message.
func TestStructuralDefects(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		name        string
		description string
		body        string
		wantDefects bool
		wantFamily  string // a substring identifying which family must fire
	}{
		"a well-formed skill has no structural defect": {
			name:        "demo-skill",
			description: "Use when the reader needs the demo thing done.",
			body:        riaBody,
		},
		"a missing RIA segment is a defect": {
			name:        "demo-skill",
			description: "Use when the reader needs the demo thing done.",
			body:        "## R\n\n## I\n\n## A1\n\n## A2\n\n## E\n",
			wantDefects: true,
			wantFamily:  "redline",
		},
		"an over-long description is a defect": {
			name:        "demo-skill",
			description: "Use when " + strings.Repeat("x", 1100),
			body:        riaBody,
			wantDefects: true,
			wantFamily:  "frontmatter",
		},
		"an empty description is a defect": {
			name:        "demo-skill",
			description: "",
			body:        riaBody,
			wantDefects: true,
			wantFamily:  "frontmatter",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := &skill.Skill{
				Name:            tc.name,
				Description:     tc.description,
				Body:            tc.body,
				FrontmatterKeys: []string{"description", "name"},
			}
			got := edit.StructuralDefects(s, true)
			if tc.wantDefects != (len(got) > 0) {
				t.Fatalf("StructuralDefects = %+v, want any defect: %v", got, tc.wantDefects)
			}
			if tc.wantFamily == "" {
				return
			}
			messages := make([]string, 0, len(got))
			for _, d := range got {
				messages = append(messages, d.Message)
			}
			if !strings.Contains(strings.Join(messages, "\n"), tc.wantFamily) {
				t.Errorf("expected a %q defect, got:\n%s",
					tc.wantFamily, strings.Join(messages, "\n"))
			}
		})
	}
}

// TestStructuralDefectsRejectsWhatTheRubricOnlyPenalises pins the distinction the gate
// exists for: the rubric scores an over-long description as a penalty that a gain
// elsewhere can outweigh, whereas this rejects it outright.
func TestStructuralDefectsRejectsWhatTheRubricOnlyPenalises(t *testing.T) {
	t.Parallel()
	s := &skill.Skill{
		Name:            "demo-skill",
		Description:     "Use when " + strings.Repeat("x", 1100),
		Body:            riaBody,
		FrontmatterKeys: []string{"description", "name"},
	}
	if len(edit.StructuralDefects(s, false)) == 0 {
		t.Fatal("an over-long description must block adoption, not merely cost points")
	}
}

// TestStructuralDefectsRedlinesAreOptIn pins the reason they are opt-in: a
// hand-written skill legitimately carries no RIA-TV++ segments, and enforcing them
// by default would reject nearly everything skillsaw optimizes.
func TestStructuralDefectsRedlinesAreOptIn(t *testing.T) {
	t.Parallel()
	notRIA := &skill.Skill{
		Name:            "demo-skill",
		Description:     "Use when the reader needs the demo thing done.",
		Body:            "# Body\n\nA perfectly good skill with no RIA segments.\n",
		FrontmatterKeys: []string{"description", "name"},
	}
	if got := edit.StructuralDefects(notRIA, false); len(got) != 0 {
		t.Errorf("without --redlines a non-RIA skill must pass, got %+v", got)
	}
	if got := edit.StructuralDefects(notRIA, true); len(got) == 0 {
		t.Error("with --redlines the missing RIA segments must be reported")
	}
}
