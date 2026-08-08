package calibrate_test

import (
	"math"
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/calibrate"
)

func TestConfidence(t *testing.T) {
	t.Parallel()
	// The endpoints are the contract: the rubric's floor maps to 0 and its ceiling to
	// 1, so a report cannot read a floor judgment as partial confidence.
	cases := map[string]struct {
		base int
		want float64
	}{
		"floor maps to zero":  {base: 1, want: 0},
		"ceiling maps to one": {base: 10, want: 1},
		"midpoint":            {base: 5, want: 4.0 / 9.0},
		"one below ceiling":   {base: 9, want: 8.0 / 9.0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := calibrate.Confidence(tc.base); math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("Confidence(%d) = %v, want %v", tc.base, got, tc.want)
			}
		})
	}
}

func TestSamples(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		in       []calibrate.Judgment
		wantLen  int
		wantConf []float64
	}{
		"empty input yields empty output": {},
		"passed carries through": {
			in:      []calibrate.Judgment{{Skill: "a", Dim: 8, Base: 10, Passed: true}},
			wantLen: 1, wantConf: []float64{1},
		},
		"a failing judgment is kept, not dropped": {
			in:      []calibrate.Judgment{{Skill: "a", Dim: 8, Base: 9, Passed: false}},
			wantLen: 1, wantConf: []float64{8.0 / 9.0},
		},
		// Clamping would invent a judgment the agent never made; dropping keeps the
		// report honest and shows up in the caller's count.
		"out-of-range bases are dropped, not clamped": {
			in: []calibrate.Judgment{
				{Skill: "a", Dim: 8, Base: 0},
				{Skill: "b", Dim: 8, Base: 11},
				{Skill: "c", Dim: 8, Base: -3},
				{Skill: "d", Dim: 8, Base: 7, Passed: true},
			},
			wantLen: 1, wantConf: []float64{6.0 / 9.0},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := calibrate.Samples(tc.in)
			if len(got) != tc.wantLen {
				t.Fatalf("Samples returned %d samples, want %d: %+v", len(got), tc.wantLen, got)
			}
			for i, want := range tc.wantConf {
				if math.Abs(got[i].Confidence-want) > 1e-9 {
					t.Errorf("sample %d Confidence = %v, want %v", i, got[i].Confidence, want)
				}
			}
		})
	}
}

func TestSamplesDoesNotMutateInput(t *testing.T) {
	t.Parallel()
	in := []calibrate.Judgment{{Skill: "a", Dim: 8, Base: 7, Passed: true}}
	calibrate.Samples(in)
	if in[0].Base != 7 || !in[0].Passed {
		t.Errorf("Samples mutated its input: %+v", in)
	}
}

func TestSamplesFeedsAnOverconfidentSetThrough(t *testing.T) {
	t.Parallel()
	// The signal the whole item exists to surface: high judgments that the
	// outcome did not bear out must land in the top bin with low
	// accuracy, so the report shows accuracy below confidence there.
	js := make([]calibrate.Judgment, 0, 10)
	for i := range 10 {
		js = append(js, calibrate.Judgment{Skill: "s", Dim: 8, Base: 10, Passed: i < 3})
	}
	got := calibrate.Samples(js)
	if len(got) != 10 {
		t.Fatalf("expected 10 samples, got %d", len(got))
	}
	correct := 0
	for _, s := range got {
		if s.Confidence != 1 {
			t.Fatalf("expected top-bin confidence, got %v", s.Confidence)
		}
		if s.Correct {
			correct++
		}
	}
	if correct != 3 {
		t.Errorf("expected 3 correct of 10 (a clearly overconfident set), got %d", correct)
	}
}
