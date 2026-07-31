package judge_test

import (
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/judge"
)

func score1(t *testing.T, op judge.Op, arg, output string) bool {
	t.Helper()
	res, err := judge.Score(output, []judge.Check{{Op: op, Arg: arg}})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	return res.Hard == 1.0
}

func TestEvalBoolean(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, arg, output string
		want              bool
	}{
		{"answer yes matches true", "true", "blah\nANSWER: yes", true},
		{"answer no vs true fails", "true", "ANSWER: no", false},
		{"json answer field", "true", `some text {"answer": true} trailing`, true},
		{"last answer wins", "false", "ANSWER: yes\n...revised...\nANSWER: no", true},
		{"no answer parse-fails", "true", "there is no verdict here", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := score1(t, judge.OpBoolean, tc.arg, tc.output); got != tc.want {
				t.Errorf("boolean arg=%q output=%q = %v, want %v", tc.arg, tc.output, got, tc.want)
			}
		})
	}
}

func TestEvalMultipleChoice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, arg, output string
		want              bool
	}{
		{"answer C matches", "C", "reasoning...\nANSWER: C", true},
		{"answer C vs D fails", "D", "ANSWER: C", false},
		{"trailing bare letter", "B", "I would pick\nB", true},
		{"no letter parse-fails", "A", "no clear choice", false},
		{"gold out of range", "Z", "ANSWER: A", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := score1(t, judge.OpMultipleChoice, tc.arg, tc.output); got != tc.want {
				t.Errorf("mcq arg=%q output=%q = %v, want %v", tc.arg, tc.output, got, tc.want)
			}
		})
	}
}

func TestEvalNumericOOM(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, arg, output string
		want              bool
	}{
		{"within default 1 OOM", "1000000", "ANSWER: 1,200,000", true},
		{"tight tolerance fails", "1000000:0.01", "ANSWER: 1,200,000", false},
		{"times-power notation", "300000000", "ANSWER: 3 × 10^8", true},
		{"scientific notation", "300000000", "ANSWER: 3e8", true},
		{"two OOM off fails", "1000000", "ANSWER: 100", false},
		{"no number parse-fails", "1000000", "I cannot estimate", false},
		{"invalid tolerance", "1000000:abc", "ANSWER: 1000000", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := score1(t, judge.OpNumericOOM, tc.arg, tc.output); got != tc.want {
				t.Errorf("numeric arg=%q output=%q = %v, want %v", tc.arg, tc.output, got, tc.want)
			}
		})
	}
}
