package root_test

import (
	"reflect"
	"testing"

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
