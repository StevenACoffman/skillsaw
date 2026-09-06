package noise_test

import (
	"strings"
	"testing"

	"github.com/StevenACoffman/skillet/ratchet"
	"github.com/StevenACoffman/skillsaw/internal/noise"
)

// TestEvidenceInSeparatesTheTwoHalves is the specification. Recall and precision are
// evidenced independently, and the quiet case matters most: a real decoy set that a good
// skill correctly excludes must not be told to write harder decoys just for being clean.
func TestEvidenceInSeparatesTheTwoHalves(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		targets, distractors, hit, falseHit int
		wantCaveat                          string // "" means the sample supports both halves
	}{
		"nothing at all": {0, 0, 0, 0, "nothing here was measured"},
		"targets only":   {3, 0, 3, 0, "precision is unevidenced"},
		"decoys only":    {0, 3, 0, 1, "TPR is a ratio over nothing"},
		"decoys that never fire": {
			3, 3, 3, 0, "either perfect precision or decoys too easy",
		},
		"a decoy fired, so the set discriminates": {3, 3, 3, 1, ""},
		"a large clean sample where decoys did fire elsewhere": {
			20, 20, 20, 1, "",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := noise.EvidenceIn(&ratchet.Report{
				Targets: tc.targets, Distractors: tc.distractors,
				TP: tc.hit, FP: tc.falseHit,
			})
			caveat := got.Caveat()
			if tc.wantCaveat == "" {
				if caveat != "" {
					t.Errorf("a sample supporting both halves was caveated: %q", caveat)
				}
				return
			}
			if !strings.Contains(caveat, tc.wantCaveat) {
				t.Errorf("Caveat() = %q, want it to say %q", caveat, tc.wantCaveat)
			}
		})
	}
}

// TestDiscriminatingImpliesPrecision pins the dependency between the two flags. A decoy set
// that does not exist cannot discriminate, and a caller checking only Discriminating must
// not be told a nonexistent set failed to separate anything.
func TestDiscriminatingImpliesPrecision(t *testing.T) {
	t.Parallel()
	for _, r := range []ratchet.Report{
		{Targets: 3, Distractors: 0, TP: 3},
		{Targets: 0, Distractors: 0},
		{Targets: 3, Distractors: 3, TP: 3, FP: 2},
	} {
		got := noise.EvidenceIn(&r)
		if got.Discriminating && !got.Precision {
			t.Errorf("%+v: Discriminating without Precision", r)
		}
	}
}

// TestTheCaveatNamesARepair is the operator-facing half. "Unevidenced" with no next step
// reads as a scolding; the two precision cases want different work and must say which.
func TestTheCaveatNamesARepair(t *testing.T) {
	t.Parallel()
	noDecoys := noise.EvidenceIn(&ratchet.Report{Targets: 3, TP: 3}).Caveat()
	easyDecoys := noise.EvidenceIn(&ratchet.Report{
		Targets: 3, Distractors: 3, TP: 3,
	}).Caveat()
	if noDecoys == easyDecoys {
		t.Fatal("having no decoys and having easy ones read identically; they want " +
			"different repairs")
	}
	if !strings.Contains(easyDecoys, "harder") {
		t.Errorf("the easy-decoy caveat does not say what to do: %q", easyDecoys)
	}
}
