package rubric_test

import (
	"strings"
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/rubric"
)

// diagnoseWeakest asks for a diagnosis of one dimension by making it the weakest.
func diagnoseWeakest(dim *rubric.Dimension) rubric.Diagnosis {
	return rubric.Diagnose(&rubric.Evaluation{
		Skill: "s",
		Dims:  []rubric.DimScore{{Num: dim.Num, Name: dim.Name, Weight: dim.Weight, Final: 1}},
	})
}

// TestFeedableDiagnosesSayWhatDoesNotCount pins the property Response exists for. The
// strategy text names the cheap move on its own dimension -- "insert explicit
// checkpoints", "add counter-examples" -- and the loop reading it is a hill climber, so
// a dimension an optimiser can feed must not be advised without saying what does not
// count. A neutral dimension needs no such caution and must not carry one, or the
// caveat stops meaning anything.
func TestFeedableDiagnosesSayWhatDoesNotCount(t *testing.T) {
	t.Parallel()
	dims := rubric.Dimensions()
	for i := range dims {
		dim := &dims[i]
		t.Run(dim.Key, func(t *testing.T) {
			t.Parallel()
			rationale := diagnoseWeakest(dim).Rationale
			cautioned := strings.Contains(rationale, "filler scores the same") ||
				strings.Contains(rationale, "until a judge scores it")
			switch dim.Response {
			case rubric.ResponseAdditive, rubric.ResponseSubtractive:
				if !cautioned {
					t.Errorf("dim %d is %q but its rationale carries no caveat: %q",
						dim.Num, dim.Response, rationale)
				}
			case rubric.ResponseNeutral, rubric.ResponseUnclassified:
				if cautioned {
					t.Errorf("dim %d is %q yet carries a feedability caveat: %q",
						dim.Num, dim.Response, rationale)
				}
			}
		})
	}
}

// TestDiagnosisCarriesTheResponse checks the field a consuming loop reads instead of the
// prose, since parsing a rationale to decide anything is what the field exists to avoid.
func TestDiagnosisCarriesTheResponse(t *testing.T) {
	t.Parallel()
	dims := rubric.Dimensions()
	for i := range dims {
		dim := &dims[i]
		if got := diagnoseWeakest(dim).Response; got != dim.Response {
			t.Errorf("dim %d: diagnosis says %q, the table says %q", dim.Num, got, dim.Response)
		}
	}
}

// TestRuntimeDriftDiagnosisCarriesNoResponse is the case the field must stay quiet for: a
// runtime hit returns before any dimension is chosen, so there is no target whose score
// direction could be reported, and the zero value says exactly that.
func TestRuntimeDriftDiagnosisCarriesNoResponse(t *testing.T) {
	t.Parallel()
	d := rubric.Diagnose(&rubric.Evaluation{Skill: "s", RuntimeWarn: 1})
	if d.Response != rubric.ResponseUnclassified {
		t.Errorf("Response = %q, want the zero value; no dimension was targeted", d.Response)
	}
}
