package auditlog_test

import (
	"bytes"
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
