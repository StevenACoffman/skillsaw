package store_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/store"
)

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	rows := []store.Row{
		{
			Timestamp: "2026-07-09T10:00", Commit: "baseline", Skill: "huashu",
			OldScore: "-", NewScore: "78", Status: store.StatusBaseline,
			Dimension: "-", Note: "初始评估", EvalMode: "full_test",
		},
		{
			Timestamp: "2026-07-09T10:05", Commit: "a1b2c3d", Skill: "huashu",
			OldScore: "78", NewScore: "84", Status: store.StatusKeep,
			Dimension: "边界条件", Note: "补充fallback", EvalMode: "full_test",
		},
	}

	var buf bytes.Buffer
	// A caller writes the header once, then appends rows.
	if _, err := buf.WriteString(strings.Join(store.Columns(), "\t") + "\n"); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if err := store.Append(&buf, rows...); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got, err := store.Read(&buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != len(rows) {
		t.Fatalf("Read returned %d rows, want %d", len(got), len(rows))
	}
	for i := range got {
		if strings.Join(got[i].Fields(), "\t") != strings.Join(rows[i].Fields(), "\t") {
			t.Errorf("row %d = %v, want %v", i, got[i].Fields(), rows[i].Fields())
		}
	}
}

func TestReadSkipsHeaderAndBlankLines(t *testing.T) {
	t.Parallel()
	in := strings.Join(store.Columns(), "\t") + "\n" +
		"\n" + // blank line ignored
		"2026\tc\ts\t-\t80\tbaseline\t-\tnote\tdry_run\n"
	got, err := store.Read(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Read returned %d rows, want 1", len(got))
	}
	if got[0].Skill != "s" || got[0].Status != store.StatusBaseline {
		t.Errorf("unexpected row: %+v", got[0])
	}
}

func TestReadRejectsMalformedRow(t *testing.T) {
	t.Parallel()
	in := "2026\tc\ts\t-\t80\tbaseline\n" // only 6 columns
	if _, err := store.Read(strings.NewReader(in)); err == nil {
		t.Fatal("Read should reject a row with too few columns")
	}
}

func TestAppendRejectsInvalidStatus(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	err := store.Append(&buf, store.Row{Skill: "s", Status: store.Status("bogus")})
	if err == nil {
		t.Fatal("Append should reject an unknown status")
	}
	if buf.Len() != 0 {
		t.Errorf("Append wrote %d bytes for an invalid row, want 0", buf.Len())
	}
}
