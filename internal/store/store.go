// Package store reads and writes the darwin optimization log (results.tsv,
// spec §13): one baseline/keep/revert/error row per experiment, nine
// tab-separated columns. Read and Fields are pure (over an io.Reader / a value);
// the file open/append lives in the command shell. This is the write half of the
// ratchet's audit trail that skillsaw previously could only read.
package store

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Status values are the experiment outcomes recorded in the log.
const (
	StatusBaseline Status = "baseline"
	StatusKeep     Status = "keep"
	StatusRevert   Status = "revert"
	StatusError    Status = "error"
)

// Status is a results.tsv row outcome.
type Status string

// Row is one results.tsv record (spec §13). Scores are strings so a baseline's
// "-" old_score round-trips verbatim.
type Row struct {
	Timestamp string
	Commit    string
	Skill     string
	OldScore  string
	NewScore  string
	Status    Status
	Dimension string
	Note      string
	EvalMode  string
}

// Columns returns the canonical header, in order.
func Columns() []string {
	return []string{
		"timestamp", "commit", "skill", "old_score", "new_score",
		"status", "dimension", "note", "eval_mode",
	}
}

// Read parses rows from a TSV reader. A leading header row (first field
// "timestamp") is skipped; blank lines are ignored. A row with fewer than nine
// columns is a hard error rather than a silent skip (darwin E3: corruption is
// surfaced, never swallowed). Status values are stored verbatim — Read does not
// reject unknown outcomes a different tool may have written.
func Read(rd io.Reader) ([]Row, error) {
	sc := bufio.NewScanner(rd)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	want := len(Columns())
	var rows []Row
	for line := 0; sc.Scan(); line++ {
		text := sc.Text()
		if strings.TrimSpace(text) == "" {
			continue
		}
		f := strings.Split(text, "\t")
		if line == 0 && f[0] == "timestamp" {
			continue // header
		}
		if len(f) < want {
			return nil, fmt.Errorf("store: row %d has %d columns, want %d", line+1, len(f), want)
		}
		rows = append(rows, Row{
			Timestamp: f[0], Commit: f[1], Skill: f[2], OldScore: f[3], NewScore: f[4],
			Status: Status(f[5]), Dimension: f[6], Note: f[7], EvalMode: f[8],
		})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("store: read: %w", err)
	}
	return rows, nil
}

// Append writes rows as TSV lines to w. Each row's Status must be one of the
// known outcomes; an unknown status is rejected before anything is written for
// that row (validate on write, spec §13).
func Append(w io.Writer, rows ...Row) error {
	for i := range rows {
		r := &rows[i]
		if !r.Status.valid() {
			return fmt.Errorf("store: invalid status %q", r.Status)
		}
		if _, err := fmt.Fprintln(w, strings.Join(r.Fields(), "\t")); err != nil {
			return fmt.Errorf("store: write: %w", err)
		}
	}
	return nil
}

// Fields renders a row to its nine ordered column values.
func (r *Row) Fields() []string {
	return []string{
		r.Timestamp, r.Commit, r.Skill, r.OldScore, r.NewScore,
		string(r.Status), r.Dimension, r.Note, r.EvalMode,
	}
}

func (s Status) valid() bool {
	switch s {
	case StatusBaseline, StatusKeep, StatusRevert, StatusError:
		return true
	default:
		return false
	}
}
