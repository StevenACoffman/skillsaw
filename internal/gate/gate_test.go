package gate_test

import (
	"testing"

	"github.com/StevenACoffman/skillet/manifest"
	"github.com/StevenACoffman/skillsaw/internal/gate"
)

func TestOf(t *testing.T) {
	t.Parallel()
	skills := []manifest.Skill{{Slug: "a", Dir: "/t/a", Hash: "h1"}}
	cases := map[string]struct {
		m    manifest.Manifest
		want gate.Coverage
	}{
		"examined some": {
			m:    manifest.Manifest{Tool: "exegesis", Examined: 3, Skills: skills},
			want: gate.CoverageExamined,
		},
		// The defect this exists for: no skills, nothing examined, and every gate passed
		// vacuously.
		"examined nothing": {
			m:    manifest.Manifest{Tool: "exegesis", StructureVerified: true},
			want: gate.CoverageNone,
		},
		// Every manifest written before skillet v0.30.0. Refusing these would fail the
		// whole stored corpus, which is why they are a third answer and not the second.
		"written before the field existed": {
			m:    manifest.Manifest{Tool: "exegesis", Skills: skills},
			want: gate.CoverageUnknown,
		},
		// Impossible from a real producer, which lists what it examined. Recorded so the
		// reading is decided here rather than discovered later: the count is the producer's
		// own claim, so it wins.
		"a count with no listed skills is still examined": {
			m:    manifest.Manifest{Tool: "exegesis", Examined: 2},
			want: gate.CoverageExamined,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := gate.Of(&tc.m); got != tc.want {
				t.Errorf("Of = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestUnknownIsTheZeroValue pins the direction: a coverage nobody computed must read as "we
// do not know", never as "examined". The comment saying so is what a refactor deletes.
func TestUnknownIsTheZeroValue(t *testing.T) {
	t.Parallel()
	var zero gate.Coverage
	if zero != gate.CoverageUnknown {
		t.Errorf("zero Coverage = %v, want CoverageUnknown so it fails closed", zero)
	}
	if zero == gate.CoverageExamined {
		t.Error("the zero value reads as examined; an uncomputed coverage would approve")
	}
}
