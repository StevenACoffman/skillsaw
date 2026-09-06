package auditlog_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/auditlog"
)

func TestAppendReadRoundTrip(t *testing.T) {
	t.Parallel()
	rows := []auditlog.Row{
		{
			Timestamp: "t0",
			Commit:    "c0",
			Skill:     "s",
			OldScore:  "-",
			NewScore:  "8",
			Status:    auditlog.StatusBaseline,
		},
		{
			Timestamp: "t1",
			Commit:    "c1",
			Skill:     "s",
			OldScore:  "8",
			NewScore:  "9",
			Status:    auditlog.StatusKeep,
			Dimension: "d5",
		},
	}
	var buf bytes.Buffer
	if err := auditlog.Append(&buf, rows...); err != nil {
		t.Fatal(err)
	}
	got, err := auditlog.Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Status != auditlog.StatusBaseline || got[1].Dimension != "d5" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got[0].OldScore != "-" {
		t.Errorf("baseline old_score %q should round-trip verbatim", got[0].OldScore)
	}
}

func TestReadSkipsHeaderAndBlanks(t *testing.T) {
	t.Parallel()
	tsv := strings.Join(auditlog.Columns(), "\t") + "\n" +
		"\n" + // blank line ignored
		"t1\tc1\ts\t8\t9\tkeep\td\tnote\tmode\n"
	got, err := auditlog.Read(strings.NewReader(tsv))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Timestamp != "t1" {
		t.Fatalf("want 1 data row, got %+v", got)
	}
}

func TestReadShortRowIsError(t *testing.T) {
	t.Parallel()
	if _, err := auditlog.Read(strings.NewReader("t1\tc1\ts\n")); err == nil {
		t.Fatal("a row with fewer than 9 columns must be a hard error")
	}
}

func TestAppendRejectsUnknownStatus(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	err := auditlog.Append(&buf, auditlog.Row{Status: auditlog.Status("bogus")})
	if err == nil {
		t.Fatal("unknown status must be rejected")
	}
	if buf.Len() != 0 {
		t.Error("nothing should be written for a row with an invalid status")
	}
}

// TestCurrentStreakCountsOnlyTheTail is the property the entry L970 asks for: a counter
// that does not reset turns elapsed time into a verdict. The reset case is the one that
// matters -- a subject that failed, recovered, and failed again has a streak of one, not
// of three.
func TestCurrentStreakCountsOnlyTheTail(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		statuses []auditlog.Status
		want     auditlog.Streak
	}{
		"empty log": {nil, auditlog.Streak{}},
		"ends in success": {
			[]auditlog.Status{auditlog.StatusRevert, auditlog.StatusRevert, auditlog.StatusKeep},
			auditlog.Streak{},
		},
		"failures after a recovery count from the recovery": {
			[]auditlog.Status{
				auditlog.StatusRevert, auditlog.StatusRevert,
				auditlog.StatusKeep,
				auditlog.StatusRevert,
			},
			auditlog.Streak{Length: 1},
		},
		"a baseline resets it too": {
			[]auditlog.Status{auditlog.StatusRevert, auditlog.StatusBaseline, auditlog.StatusError},
			auditlog.Streak{Length: 1, Errors: 1},
		},
		"reverts and errors are counted together and apart": {
			[]auditlog.Status{
				auditlog.StatusKeep,
				auditlog.StatusRevert, auditlog.StatusError, auditlog.StatusRevert,
			},
			auditlog.Streak{Length: 3, Errors: 1},
		},
		"an all-error streak is about the harness": {
			[]auditlog.Status{auditlog.StatusError, auditlog.StatusError, auditlog.StatusError},
			auditlog.Streak{Length: 3, Errors: 3},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rows := make([]auditlog.Row, 0, len(tc.statuses))
			for _, st := range tc.statuses {
				rows = append(rows, auditlog.Row{Skill: "alpha", Status: st})
			}
			if got := auditlog.CurrentStreak(rows); got != tc.want {
				t.Errorf("CurrentStreak() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestUnrecognisedStatusIsNotSuccess pins the fail-closed direction. A status this package
// does not know is not evidence that the experiment went well, and ending the streak on one
// would under-report exactly when the log is already suspect.
func TestUnrecognisedStatusIsNotSuccess(t *testing.T) {
	t.Parallel()
	rows := []auditlog.Row{
		{Skill: "alpha", Status: auditlog.StatusRevert},
		{Skill: "alpha", Status: auditlog.Status("weird")},
	}
	if got := auditlog.CurrentStreak(rows); got.Length != 2 {
		t.Errorf("CurrentStreak() = %+v, want Length 2; an unknown status ended the streak", got)
	}
}

// TestSeriesSkipsUnscorableRows is the property that keeps a baseline row from becoming a
// regression. A baseline records "-" for new_score and an error row may record nothing at
// all; reading either as zero would invent a collapse out of a run that measured nothing --
// the same mistake as reading an absent value as a passing one.
func TestSeriesSkipsUnscorableRows(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		scores      []string
		wantHistory []float64
		wantCurrent float64
		wantOK      bool
	}{
		"all scored":        {[]string{"90", "92", "88"}, []float64{90, 92}, 88, true},
		"a baseline dash":   {[]string{"-", "90", "92"}, []float64{90}, 92, true},
		"an empty score":    {[]string{"90", "", "92"}, []float64{90}, 92, true},
		"nothing scorable":  {[]string{"-", ""}, nil, 0, false},
		"no rows at all":    {nil, nil, 0, false},
		"one scored row":    {[]string{"90"}, []float64{}, 90, true},
		"whitespace padded": {[]string{" 90 ", "88"}, []float64{90}, 88, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rows := make([]auditlog.Row, 0, len(tc.scores))
			for _, s := range tc.scores {
				rows = append(rows, auditlog.Row{Skill: "alpha", NewScore: s})
			}
			history, current, ok := auditlog.Series(rows)
			if ok != tc.wantOK {
				t.Fatalf("ok = %t, want %t", ok, tc.wantOK)
			}
			if current != tc.wantCurrent {
				t.Errorf("current = %v, want %v", current, tc.wantCurrent)
			}
			if !slices.Equal(history, tc.wantHistory) {
				t.Errorf("history = %v, want %v", history, tc.wantHistory)
			}
		})
	}
}

// TestEditionsSpotsAMixedHistory is the guard that stops a rubric change from being
// laundered through the log. A column of numbers cannot distinguish "skills got better"
// from "the rules got easier", and averaging across the two produces a figure about
// neither.
func TestEditionsSpotsAMixedHistory(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		recorded []string
		want     []string
	}{
		"one edition throughout": {[]string{"ed1", "ed1", "ed1"}, []string{"ed1"}},
		"the rubric changed":     {[]string{"ed1", "ed1", "ed2"}, []string{"ed1", "ed2"}},
		"and changed back":       {[]string{"ed1", "ed2", "ed1"}, []string{"ed1", "ed2"}},
		"nothing recorded":       {[]string{"", "", ""}, nil},
		"older rows predate it":  {[]string{"", "", "ed1"}, []string{"ed1"}},
		"no rows":                {nil, nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rows := make([]auditlog.Row, 0, len(tc.recorded))
			for _, e := range tc.recorded {
				rows = append(rows, auditlog.Row{Skill: "alpha", Rubric: e})
			}
			if got := auditlog.Editions(rows); !slices.Equal(got, tc.want) {
				t.Errorf("Editions() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestNineColumnLogsStillRead is the migration property. The rubric column is tenth, and
// promoting the required count to ten would fail every log written before it existed --
// a format change that invalidates the history is a poor way to start recording history.
func TestNineColumnLogsStillRead(t *testing.T) {
	t.Parallel()
	nine := "2026-01-01T00:00:00Z\tabc\talpha\t80\t84\tkeep\tdim3\tnote\tfull_test\n"
	rows, err := auditlog.Read(strings.NewReader(nine))
	if err != nil {
		t.Fatalf("a nine-column log no longer reads: %v", err)
	}
	if len(rows) != 1 || rows[0].NewScore != "84" {
		t.Fatalf("row did not parse: %+v", rows)
	}
	if rows[0].Rubric != "" {
		t.Errorf("Rubric = %q for a row that predates the column; empty means unknown",
			rows[0].Rubric)
	}
}
