package transcript_test

import (
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/transcript"
)

// TestAgreeReportsTheSplitNotTheMajority is the property the metric turns on. Three firsts
// and two after-actions is not a pass with noise: it is a phrasing the skill does not
// reliably survive, and a majority verdict would report it as clean.
func TestAgreeReportsTheSplitNotTheMajority(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		orders        []transcript.Order
		wantUnanimous bool
		wantTop       transcript.Order
		wantKinds     int
	}{
		"every run agreed": {
			[]transcript.Order{transcript.OrderFirst, transcript.OrderFirst, transcript.OrderFirst},
			true, transcript.OrderFirst, 1,
		},
		"a majority, but not agreement": {
			[]transcript.Order{
				transcript.OrderFirst, transcript.OrderFirst, transcript.OrderFirst,
				transcript.OrderAfterAction, transcript.OrderAfterAction,
			},
			false, transcript.OrderFirst, 2,
		},
		"agreed on failing": {
			[]transcript.Order{transcript.OrderAfterAction, transcript.OrderAfterAction},
			true, transcript.OrderAfterAction, 1,
		},
		"three different outcomes": {
			[]transcript.Order{
				transcript.OrderFirst, transcript.OrderAfterAction, transcript.OrderNotTriggered,
			},
			false, transcript.OrderAfterAction, 3,
		},
		"a single run": {
			[]transcript.Order{transcript.OrderFirst}, true, transcript.OrderFirst, 1,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := transcript.Agree(tc.orders)
			if got.Unanimous != tc.wantUnanimous {
				t.Errorf(
					"Unanimous = %v, want %v (tally %+v)",
					got.Unanimous,
					tc.wantUnanimous,
					got.Tally,
				)
			}
			if len(got.Tally) != tc.wantKinds {
				t.Errorf(
					"got %d distinct verdicts, want %d: %+v",
					len(got.Tally),
					tc.wantKinds,
					got.Tally,
				)
			}
			if got.Tally[0].Order != tc.wantTop {
				t.Errorf("most common = %q, want %q", got.Tally[0].Order, tc.wantTop)
			}
			if got.Runs != len(tc.orders) {
				t.Errorf("Runs = %d, want %d", got.Runs, len(tc.orders))
			}
		})
	}
}

// TestAgreeOnNothingIsNotAgreement keeps the empty case from reading as convergence:
// nothing measured is not the same as everything agreeing.
func TestAgreeOnNothingIsNotAgreement(t *testing.T) {
	t.Parallel()
	got := transcript.Agree(nil)
	if got.Unanimous || got.Runs != 0 || len(got.Tally) != 0 {
		t.Errorf("Agree(nil) = %+v, want zero runs and no agreement", got)
	}
}

// TestTallyIsOrdered keeps a report from reshuffling between runs over one input, which
// would make a diff of two reports unreadable.
func TestTallyIsOrdered(t *testing.T) {
	t.Parallel()
	got := transcript.Agree([]transcript.Order{
		transcript.OrderNotTriggered, transcript.OrderFirst, transcript.OrderFirst,
	})
	if got.Tally[0].Order != transcript.OrderFirst || got.Tally[0].Count != 2 {
		t.Errorf("most common should lead: %+v", got.Tally)
	}
}
