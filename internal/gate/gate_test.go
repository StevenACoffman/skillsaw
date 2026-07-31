package gate_test

import (
	"math"
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/gate"
)

const eps = 1e-9

func TestSelectScore(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		hard    float64
		soft    float64
		metric  gate.Metric
		weight  float64
		want    float64
		wantErr bool
	}{
		{name: "hard returns hard", hard: 0.8, soft: 0.2, metric: gate.Hard, want: 0.8},
		{name: "soft returns soft", hard: 0.8, soft: 0.2, metric: gate.Soft, want: 0.2},
		{
			name:   "mixed half is average",
			hard:   80,
			soft:   60,
			metric: gate.Mixed,
			weight: 0.5,
			want:   70,
		},
		{
			name:   "mixed weight 0 is hard",
			hard:   80,
			soft:   60,
			metric: gate.Mixed,
			weight: 0,
			want:   80,
		},
		{
			name:   "mixed weight 1 is soft",
			hard:   80,
			soft:   60,
			metric: gate.Mixed,
			weight: 1,
			want:   60,
		},
		{
			name:   "mixed weight clamped high",
			hard:   80,
			soft:   60,
			metric: gate.Mixed,
			weight: 5,
			want:   60,
		},
		{
			name:   "mixed weight clamped low",
			hard:   80,
			soft:   60,
			metric: gate.Mixed,
			weight: -5,
			want:   80,
		},
		{name: "unknown metric errors", metric: gate.Metric("bogus"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := gate.SelectScore(tt.hard, tt.soft, tt.metric, tt.weight)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("SelectScore(%v) = %v, want error", tt.metric, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("SelectScore returned unexpected error: %v", err)
			}
			if math.Abs(got-tt.want) > eps {
				t.Errorf("SelectScore = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvaluateStatusAndDelta(t *testing.T) {
	t.Parallel()
	// Status/Delta are the measured axis, independent of the accept/reject action.
	tests := []struct {
		name       string
		cand       float64
		current    float64
		wantStatus gate.Status
		wantDelta  float64
	}{
		{name: "improved", cand: 88, current: 84, wantStatus: gate.Improved, wantDelta: 4},
		{name: "tie", cand: 84, current: 84, wantStatus: gate.Tie, wantDelta: 0},
		{name: "regressed", cand: 80, current: 84, wantStatus: gate.Regressed, wantDelta: -4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := gate.Evaluate(tt.cand, tt.current, tt.current, 0, 0)
			if got.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", got.Status, tt.wantStatus)
			}
			if math.Abs(got.Delta-tt.wantDelta) > eps {
				t.Errorf("Delta = %v, want %v", got.Delta, tt.wantDelta)
			}
		})
	}
}

func TestEvaluate(t *testing.T) {
	t.Parallel()
	// Mirrors SkillOpt's evaluate_gate: strict ">" at both comparisons.
	// A candidate is accepted only if it beats current, and becomes the new
	// best only if it also beats best. Ties reject and do not promote.
	tests := []struct {
		name         string
		cand         float64
		current      float64
		best         float64
		bestStep     int
		globalStep   int
		wantAction   gate.Action
		wantCurrent  float64
		wantBest     float64
		wantBestStep int
	}{
		{
			name: "new best when beating both", cand: 88, current: 84, best: 84,
			bestStep: 1, globalStep: 5,
			wantAction: gate.AcceptNewBest, wantCurrent: 88, wantBest: 88, wantBestStep: 5,
		},
		{
			name: "accept but keep best when below best", cand: 85, current: 84, best: 90,
			bestStep: 2, globalStep: 6,
			wantAction: gate.Accept, wantCurrent: 85, wantBest: 90, wantBestStep: 2,
		},
		{
			name: "accept without promote on tie with best", cand: 90, current: 84, best: 90,
			bestStep: 3, globalStep: 7,
			wantAction: gate.Accept, wantCurrent: 90, wantBest: 90, wantBestStep: 3,
		},
		{
			name: "reject on tie with current", cand: 84, current: 84, best: 90,
			bestStep: 4, globalStep: 8,
			wantAction: gate.Reject, wantCurrent: 84, wantBest: 90, wantBestStep: 4,
		},
		{
			name: "reject when below current", cand: 80, current: 84, best: 90,
			bestStep: 5, globalStep: 9,
			wantAction: gate.Reject, wantCurrent: 84, wantBest: 90, wantBestStep: 5,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := gate.Evaluate(tt.cand, tt.current, tt.best, tt.bestStep, tt.globalStep)
			if got.Action != tt.wantAction {
				t.Errorf("Action = %q, want %q", got.Action, tt.wantAction)
			}
			if math.Abs(got.CurrentScore-tt.wantCurrent) > eps {
				t.Errorf("CurrentScore = %v, want %v", got.CurrentScore, tt.wantCurrent)
			}
			if math.Abs(got.BestScore-tt.wantBest) > eps {
				t.Errorf("BestScore = %v, want %v", got.BestScore, tt.wantBest)
			}
			if got.BestStep != tt.wantBestStep {
				t.Errorf("BestStep = %d, want %d", got.BestStep, tt.wantBestStep)
			}
		})
	}
}
