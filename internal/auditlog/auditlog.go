// Package auditlog reads and writes the optimization log (results.tsv): one
// baseline/keep/revert/error row per experiment, nine tab-separated columns.
// Read and Fields are pure (over an io.Reader / a value); the file open/append
// lives in the command shell.
//
// Moved here from skillet on 2026-08-22, unchanged. It was extracted to the kernel
// speculatively and never earned the second consumer that would have justified it:
// skillsaw was the only importer for the whole of its life there, and gnosis — the
// one plausible candidate — examined it for the mutation-row job and declined in
// writing, because this shape is nine columns describing an optimization experiment
// and not a general audit row. A single-consumer package in a shared kernel is
// surface every other consumer pays for and none of them uses, so it came home.
//
// If a second tool ever wants an experiment log, promote it back rather than copying
// it — that is the family's promote-on-second-consumer rule, and the reason this
// package is a clean stdlib-only unit is to keep that move cheap.
package auditlog

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// requiredColumns is how many fields a row must have to be read. It stays at nine while
// Columns has ten: promoting it would fail every log written before the rubric column
// existed, and a format change that invalidates the history is a poor way to start
// recording history properly.
const requiredColumns = 9

// Status values are the experiment outcomes recorded in the log.
const (
	StatusBaseline Status = "baseline"
	StatusKeep     Status = "keep"
	StatusRevert   Status = "revert"
	StatusError    Status = "error"
)

// Status is a results.tsv row outcome.
type Status string

// Streak is the tail run of experiments that did not improve the skill: how many in a row,
// and how many of those were the harness failing rather than the skill regressing.
type Streak struct {
	Length int
	Errors int
}

// Row is one results.tsv record. Scores are strings so a baseline's "-"
// old_score round-trips verbatim.
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

	// Rubric is the rubric edition this score was produced under.
	//
	// Without it the log is a series of numbers that may not be comparable: an edit to
	// the rubric that raises every score is indistinguishable, in this file, from skills
	// that got better. Recording it lets a reader see the change rather than infer it,
	// and lets "regression" refuse to average across one.
	//
	// Empty means the row predates editions, which is not the same as matching. It is the
	// tenth column and rows are read with nine or ten, so no existing log is invalidated
	// by its arrival.
	Rubric string
}

// Columns returns the canonical header, in order.
func Columns() []string {
	return []string{
		"timestamp", "commit", "skill", "old_score", "new_score",
		"status", "dimension", "note", "eval_mode", "rubric",
	}
}

// Read parses rows from a TSV reader. A leading header row (first field
// "timestamp") is skipped; blank lines are ignored. A row with fewer than nine
// columns is a hard error rather than a silent skip (corruption is surfaced,
// never swallowed). Status values are stored verbatim — Read does not reject
// unknown outcomes a different tool may have written.
func Read(rd io.Reader) ([]Row, error) {
	sc := bufio.NewScanner(rd)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	want := requiredColumns
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
			return nil, fmt.Errorf("auditlog: row %d has %d columns, want %d", line+1, len(f), want)
		}
		row := Row{
			Timestamp: f[0], Commit: f[1], Skill: f[2], OldScore: f[3], NewScore: f[4],
			Status: Status(f[5]), Dimension: f[6], Note: f[7], EvalMode: f[8],
		}
		if len(f) > requiredColumns {
			row.Rubric = f[requiredColumns]
		}
		rows = append(rows, row)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("auditlog: read: %w", err)
	}
	return rows, nil
}

// Append writes rows as TSV lines to w. Each row's Status must be one of the
// known outcomes; an unknown status is rejected before anything is written for
// that row (validate on write).
func Append(w io.Writer, rows ...Row) error {
	for i := range rows {
		r := &rows[i]
		if !r.Status.valid() {
			return fmt.Errorf("auditlog: invalid status %q", r.Status)
		}
		if _, err := fmt.Fprintln(w, strings.Join(r.Fields(), "\t")); err != nil {
			return fmt.Errorf("auditlog: write: %w", err)
		}
	}
	return nil
}

// Fields renders a row to its nine ordered column values.
func (r *Row) Fields() []string {
	return []string{
		r.Timestamp, r.Commit, r.Skill, r.OldScore, r.NewScore,
		string(r.Status), r.Dimension, r.Note, r.EvalMode, r.Rubric,
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

// CurrentStreak folds a log into its tail run of experiments that did not improve the
// skill.
//
// It exists so that a consumer counting failures counts consecutive ones. A total that
// never resets turns a merely noisy subject into a terminal verdict on elapsed time, which
// is a statement about how long the loop has been running rather than about the thing it
// is measuring.
//
// Errors is separated from the total for the same reason. A revert is a measured
// regression -- evidence about the skill. An error is an experiment that failed to run --
// evidence about the harness. Fusing them lets a broken check masquerade as a skill that
// keeps getting worse, and the two want opposite responses.
//
// Requires: rows are in log order, oldest last written; already filtered to one subject,
// since deciding what counts as the same subject is the caller's question.
// Ensures:  Length counts back from the final row and stops at the first keep or baseline;
// Errors <= Length; both are zero for an empty log or one ending in success.
func CurrentStreak(rows []Row) Streak {
	var s Streak
	for i := len(rows) - 1; i >= 0; i-- {
		switch rows[i].Status {
		case StatusKeep, StatusBaseline:
			return s
		case StatusError:
			s.Length++
			s.Errors++
		case StatusRevert:
			s.Length++
		default:
			// An unrecognised status is not a success, and treating it as one would end
			// the streak early and under-report. Count it and attribute it to nothing.
			s.Length++
		}
	}
	return s
}

// Series splits one subject's log into the history and the measurement being judged: every
// scorable row in order, with the last one separated out.
//
// Rows whose new_score is not a number are dropped rather than treated as zero. A baseline
// row records "-" and an error row records whatever the harness managed, and folding either
// in as zero would manufacture a regression out of a run that measured nothing -- the same
// mistake as reading an absent value as a passing one.
//
// Requires: rows are in log order, oldest first, already filtered to one subject.
// Ensures:  pure; ok is false when no row carries a score, in which case there is nothing
// to judge and history is empty.
func Series(rows []Row) (history []float64, current float64, ok bool) {
	scores := make([]float64, 0, len(rows))
	for i := range rows {
		v, err := strconv.ParseFloat(strings.TrimSpace(rows[i].NewScore), 64)
		if err != nil {
			continue
		}
		scores = append(scores, v)
	}
	if len(scores) == 0 {
		return nil, 0, false
	}
	return scores[:len(scores)-1], scores[len(scores)-1], true
}

// Editions returns the distinct rubric editions the rows were scored under, in first-seen
// order, ignoring rows that record none.
//
// More than one means the scores are not a series. An edit to the rubric that raised every
// score looks, in a column of numbers, exactly like skills that got better; comparing
// across it is comparing answers to two different questions. Rows predating the column are
// skipped rather than counted as a distinct edition -- an unknown edition is not evidence
// of a second one, and treating it as such would make every historical log unusable.
//
// Requires: rows are already filtered to one subject.
// Ensures:  pure; empty when no row records an edition.
func Editions(rows []Row) []string {
	var out []string
	seen := make(map[string]bool, 2)
	for i := range rows {
		e := rows[i].Rubric
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out
}
