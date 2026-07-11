package edit_test

import (
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/edit"
)

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
