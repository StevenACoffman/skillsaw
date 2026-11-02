package root_test

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/StevenACoffman/skillet/fsutil"
	"github.com/StevenACoffman/skillsaw/cmd/root"
)

func TestSplitRoots(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want []string
	}{
		{".claude/skills,.cursor/skills", []string{".claude/skills", ".cursor/skills"}},
		{" a , , b ", []string{"a", "b"}}, // trims and drops empties
		{"", nil},
		{",,", nil},
	}
	for _, tt := range tests {
		if got := root.SplitRoots(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("SplitRoots(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestReportSkipped(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	root.ReportSkipped(&buf, "scan", []fsutil.Skipped{
		{Path: "a/SKILL.md", Reason: "path escapes from parent"},
	})
	want := "scan: skipped a/SKILL.md: path escapes from parent\n"
	if got := buf.String(); got != want {
		t.Errorf("ReportSkipped wrote %q, want %q", got, want)
	}
}
