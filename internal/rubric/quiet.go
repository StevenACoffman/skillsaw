package rubric

// The three word lists, named so a self-test failure says which one is unsound.
const (
	listSlop        = "Slop"
	listMarkers     = "CheckpointMarkers"
	listFillerTails = "FillerTails"
)

// quietCase is prose one list must not fire on, plus the terms measured to fire on it
// anyway and the reason each is tolerated for now.
type quietCase struct {
	list string
	text string

	// allowed maps a term to why its false alarm is accepted. An entry that stops firing
	// fails the test: a tolerated defect that quietly fixes itself must be recorded as
	// fixed, or the exception outlives the problem and starts hiding a real regression.
	allowed map[string]string
}

// quietCases is the negative corpus: ordinary technical prose a good skill could contain,
// which the rules should ignore. Every allowed entry below was found by running this, not
// predicted -- the lists were written without anyone asking what else they match.
func quietCases() []quietCase {
	return []quietCase{
		{
			list: listSlop,
			text: "首先检查配置文件是否存在。其次运行迁移脚本。",
			allowed: map[string]string{
				// The sharpest finding here. In a workflow skill these are step ordering,
				// which dim 2 rewards, and dim 7 charges a point for each occurrence. Left
				// in place only because removing a term moves every score in the corpus
				// and no corpus is at hand to measure by how much; argued for removal in
				// TODO.md rather than done quietly.
				"首先": "ordinary step ordering, not slop; see TODO.md",
				"其次": "ordinary step ordering, not slop; see TODO.md",
			},
		},
		{
			list: listSlop,
			text: "The retry budget is three attempts. After that the run is abandoned " +
				"and the operator is paged.",
		},
		{
			list: listMarkers,
			text: "Do not STOP the deployment midway; let the connections drain. " +
				"The CHECKPOINT table stores replication state.",
			allowed: map[string]string{
				// Both are ordinary uppercase words in infrastructure prose. This is a
				// matcher question rather than a list question -- a bare substring cannot
				// tell a marker from a noun -- so it belongs with making the rubric data,
				// where the matcher becomes expressible. Over-matching here derives a
				// base of 9 where 10 was assumed, so the false alarm lowers a score.
				"STOP":       "needs a marker-shaped matcher, not a substring; see TODO.md",
				"CHECKPOINT": "needs a marker-shaped matcher, not a substring; see TODO.md",
			},
		},
		{
			list: listMarkers,
			text: "Stop the service, checkpoint the write-ahead log, then halt replication.",
		},
		{
			// Anchored at the end, so the same words mid-sentence must not reach it.
			list: listFillerTails,
			text: "Run it as needed by the operator's schedule and record the outcome.",
		},
		{
			list: listFillerTails,
			text: "Use when the reader needs the demo thing done in a particular way.",
		},
	}
}
