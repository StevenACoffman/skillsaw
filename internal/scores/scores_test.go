package scores_test

import (
	"math"
	"strings"
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/scores"
)

func TestParseTheLegacyShape(t *testing.T) {
	t.Parallel()
	// What skillsaw-skill's Phase 1 writes today. It must keep working.
	f, err := scores.Parse([]byte(`{"1": 8, "2": 7, "5": 10}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Entries) != 0 {
		t.Errorf("a legacy document has no hash-bound entries: %+v", f.Entries)
	}
	want := map[int]int{1: 8, 2: 7, 5: 10}
	for dim, base := range want {
		if f.Unbound[dim] != base {
			t.Errorf("dimension %d = %d, want %d", dim, f.Unbound[dim], base)
		}
	}
}

func TestParseTheHashBoundShape(t *testing.T) {
	t.Parallel()
	f, err := scores.Parse([]byte(`{"entries":[
		{"skill":"alpha","hash":"aaa1","bases":{"1":8},"rubric":"ed1"},
		{"skill":"beta","hash":"bbb2","bases":{"2":6},"rubric":"ed1"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(f.Entries))
	}
	if len(f.Unbound) != 0 {
		t.Errorf("a hash-bound document has no unbound bases: %+v", f.Unbound)
	}
	if f.Entries[0].Skill != "alpha" || f.Entries[0].Hash != "aaa1" {
		t.Errorf("entry 1 wrong: %+v", f.Entries[0])
	}
}

func TestParseRejectsWhatCannotBeScoredHonestly(t *testing.T) {
	t.Parallel()
	// A partially understood scores file yields a total that looks just as comparable
	// as a correct one, so every one of these is an error rather than a default.
	cases := map[string]struct{ in, wantSub string }{
		"dimension key is not a number": {`{"first": 8}`, "invalid dimension key"},
		"dimension out of range":        {`{"12": 8}`, "invalid dimension key"},
		"dimension zero":                {`{"0": 8}`, "invalid dimension key"},
		"base above the ceiling":        {`{"1": 11}`, "out of range"},
		"base below the floor":          {`{"1": 0}`, "out of range"},
		"malformed json":                {`{"1":`, "parse scores"},
		"entry with no hash": {
			`{"entries":[{"skill":"alpha","bases":{"1":8},"rubric":"ed1"}]}`, "no hash",
		},
		"entry base out of range": {
			`{"entries":[{"skill":"a","hash":"h","bases":{"1":99},"rubric":"ed1"}]}`,
			"out of range",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := scores.Parse([]byte(tc.in))
			if err == nil {
				t.Fatalf("accepted %s", tc.in)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q does not mention %q", err, tc.wantSub)
			}
		})
	}
}

func TestBasesAppliesOnlyToTheVersionJudged(t *testing.T) {
	t.Parallel()
	f, err := scores.Parse([]byte(
		`{"entries":[{"skill":"alpha","hash":"aaa1","bases":{"1":8},"rubric":"ed1"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	bases, stale := f.Bases("alpha", "aaa1", "ed1")
	if stale || bases[1] != 8 {
		t.Errorf("the judged version was not matched: bases=%v stale=%t", bases, stale)
	}
}

func TestBasesTellsNeverJudgedApartFromJudgedAtAnotherVersion(t *testing.T) {
	t.Parallel()
	// The distinction the whole item exists for. Both yield no total, but one means
	// "judge it" and the other means "re-judge it"; a caller that could not tell them
	// apart would see an unexplained missing score in both.
	f, err := scores.Parse([]byte(
		`{"entries":[{"skill":"alpha","hash":"aaa1","bases":{"1":8},"rubric":"ed1"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	t.Run("edited since it was judged", func(t *testing.T) {
		t.Parallel()
		bases, stale := f.Bases("alpha", "different", "ed1")
		if !stale {
			t.Error("an edited skill reused bases judged against the old text")
		}
		if bases != nil {
			t.Errorf("stale bases must not be handed back: %v", bases)
		}
		if got := f.JudgedAt("alpha"); got != "aaa1" {
			t.Errorf("JudgedAt = %q, want the version it was judged at", got)
		}
	})
	t.Run("never judged at all", func(t *testing.T) {
		t.Parallel()
		bases, stale := f.Bases("gamma", "ccc3", "ed1")
		if stale {
			t.Error("a skill absent from the file was reported as stale")
		}
		if bases != nil {
			t.Errorf("want no bases, got %v", bases)
		}
	})
}

func TestBasesFromALegacyFileApplyToWhateverTheyAreGiven(t *testing.T) {
	t.Parallel()
	// A legacy file records no version, so it cannot be shown to belong to this skill
	// or to any other. It is unverifiable rather than stale, and keeps today's meaning.
	f, err := scores.Parse([]byte(`{"1": 8}`))
	if err != nil {
		t.Fatal(err)
	}
	bases, stale := f.Bases("anything", "any-hash", "ed1")
	if stale {
		t.Error("a file with no hash cannot be stale")
	}
	if bases[1] != 8 {
		t.Errorf("legacy bases were not applied: %v", bases)
	}
}

func TestAnEntryWithNoSkillNameIsStillMatchedByHash(t *testing.T) {
	t.Parallel()
	// The hash is the key; the name is a label for reports. An unnamed entry must
	// still bind to its version, and must never be reported stale for another skill.
	f, err := scores.Parse([]byte(`{"entries":[{"hash":"aaa1","bases":{"1":8},"rubric":"ed1"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if bases, stale := f.Bases("", "aaa1", "ed1"); stale || bases[1] != 8 {
		t.Errorf("unnamed entry not matched: bases=%v stale=%t", bases, stale)
	}
	if _, stale := f.Bases("", "other", "ed1"); stale {
		t.Error("an unnamed entry was claimed as another skill's stale judgment")
	}
}

func TestParseEmptyEntries(t *testing.T) {
	t.Parallel()
	f, err := scores.Parse([]byte(`{"entries":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Entries) != 0 || len(f.Unbound) != 0 {
		t.Errorf("want an empty document, got %+v", f)
	}
	if bases, stale := f.Bases("alpha", "aaa1", "ed1"); stale || bases != nil {
		t.Errorf("empty file gave bases=%v stale=%t", bases, stale)
	}
}

func TestAggregated(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		softs    []float64
		wantBase int
		wantMean float64
	}{
		"every check passed":    {[]float64{1, 1, 1}, 10, 1},
		"half passed":           {[]float64{1, 0}, 5, 0.5},
		"rounds to the nearest": {[]float64{0.74}, 7, 0.74},
		"rounds up at the half": {[]float64{0.75}, 8, 0.75},
		"one case":              {[]float64{0.6}, 6, 0.6},
		// 10 x 0 rounds to 0, which is not a point on the scale: failing everything is
		// the floor, and the floor is 1.
		"everything failed lands on the floor": {[]float64{0, 0}, 1, 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := scores.Aggregated(tc.softs)
			if got.Base != tc.wantBase {
				t.Errorf("Base = %d, want %d", got.Base, tc.wantBase)
			}
			if math.Abs(got.MeanSoft-tc.wantMean) > 1e-9 {
				t.Errorf("MeanSoft = %v, want %v", got.MeanSoft, tc.wantMean)
			}
			if got.Cases != len(tc.softs) {
				t.Errorf("Cases = %d, want %d", got.Cases, len(tc.softs))
			}
		})
	}
}

func TestAggregatedAlwaysLandsOnTheScale(t *testing.T) {
	t.Parallel()
	// Whatever the inputs, the result must be a base Parse would accept — otherwise a
	// scores file built from it is rejected by the tool that just produced it.
	for _, softs := range [][]float64{
		{0}, {1}, {0, 0, 0}, {1, 1}, {0.04}, {0.96}, {0.5, 0.5, 0.5},
	} {
		got := scores.Aggregated(softs)
		if got.Base < scores.MinBase || got.Base > scores.MaxBase {
			t.Errorf("Aggregated(%v).Base = %d, outside %d..%d",
				softs, got.Base, scores.MinBase, scores.MaxBase)
		}
	}
}

func TestAggregatedWithNoCases(t *testing.T) {
	t.Parallel()
	// There is no base for zero cases; the caller must refuse before asking rather
	// than be handed a fabricated one.
	if got := scores.Aggregated(nil); got.Cases != 0 || got.Base != 0 {
		t.Errorf("Aggregated(nil) = %+v, want a zero Aggregate the caller can detect", got)
	}
}

func TestMarshalRoundTripsThroughParse(t *testing.T) {
	t.Parallel()
	// The property that keeps the writer and the reader one definition.
	want := []scores.Entry{
		{Skill: "alpha", Hash: "aaa1", Bases: map[int]int{1: 8, 2: 7}, Rubric: "ed1"},
		{Skill: "beta", Hash: "bbb2", Bases: map[int]int{5: 10}, Rubric: "ed1"},
	}
	b, err := scores.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := scores.Parse(b)
	if err != nil {
		t.Fatalf("Parse rejected our own Marshal output: %v\n%s", err, b)
	}
	if len(got.Entries) != len(want) {
		t.Fatalf("round trip lost entries: %+v", got.Entries)
	}
	for i := range want {
		if got.Entries[i].Skill != want[i].Skill || got.Entries[i].Hash != want[i].Hash {
			t.Errorf("entry %d = %+v, want %+v", i, got.Entries[i], want[i])
		}
		for dim, base := range want[i].Bases {
			if got.Entries[i].Bases[dim] != base {
				t.Errorf("entry %d dim %d = %d, want %d", i, dim, got.Entries[i].Bases[dim], base)
			}
		}
	}
	// And the written entry is usable: it matches by hash.
	if bases, stale := got.Bases("alpha", "aaa1", "ed1"); stale || bases[1] != 8 {
		t.Errorf("the written entry does not match its own hash: %v %t", bases, stale)
	}
}

func TestMarshalRefusesWhatParseWouldReject(t *testing.T) {
	t.Parallel()
	// An out-of-range base must be unwritable, not merely rejected later by the reader.
	cases := map[string][]scores.Entry{
		"no hash":            {{Skill: "a", Bases: map[int]int{1: 8}}},
		"base above ceiling": {{Skill: "a", Hash: "h", Bases: map[int]int{1: 11}}},
		"base below floor":   {{Skill: "a", Hash: "h", Bases: map[int]int{1: 0}}},
		"dimension too high": {{Skill: "a", Hash: "h", Bases: map[int]int{12: 8}}},
		"dimension zero":     {{Skill: "a", Hash: "h", Bases: map[int]int{0: 8}}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if b, err := scores.Marshal(entries); err == nil {
				t.Errorf("wrote a document Parse would reject:\n%s", b)
			}
		})
	}
}

// TestAggregatedReportsWhatTheSampleSupports covers the reason Base is paired with a range.
// Three cases agreeing exactly still leave most of the scale open, and a Base read on its
// own looks like a measurement; forty cases agreeing pin it, and must be allowed to.
func TestAggregatedReportsWhatTheSampleSupports(t *testing.T) {
	t.Parallel()
	three := make([]float64, 3)
	forty := make([]float64, 40)
	for i := range three {
		three[i] = 0.8
	}
	for i := range forty {
		forty[i] = 0.8
	}
	cases := map[string]struct {
		softs        []float64
		wantResolved bool
	}{
		"three identical cases cannot pin a base": {three, false},
		"forty identical cases can":               {forty, true},
		"no cases at all":                         {nil, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			agg := scores.Aggregated(tc.softs)
			if got := agg.Resolved(); got != tc.wantResolved {
				t.Errorf("Resolved() = %v, want %v (base %d, supported %d-%d)",
					got, tc.wantResolved, agg.Base, agg.BaseLow, agg.BaseHigh)
			}
			if agg.Cases > 0 && (agg.Base < agg.BaseLow || agg.Base > agg.BaseHigh) {
				t.Errorf("base %d lies outside the range %d-%d it is supposed to sit in",
					agg.Base, agg.BaseLow, agg.BaseHigh)
			}
		})
	}
}

// TestMeasuredSeparatesUnmeasuredFromScored is the third state L1118 asks for. A skill can
// be well scored and unmeasured, and that combination is the one worth naming: no rubric
// dimension can detect a skill written against a failure the model does not have, because
// such a skill is well-formed by construction.
func TestMeasuredSeparatesUnmeasuredFromScored(t *testing.T) {
	t.Parallel()
	doc := []byte(`{"entries":[
		{"skill":"measured","hash":"aaaa","bases":{"2":8},
		 "baseline":"without the skill, 4 of 5 runs skipped the checkpoint"},
		{"skill":"unmeasured","hash":"bbbb","bases":{"2":9},"rubric":"ed1"}
	]}`)
	f, err := scores.Parse(doc)
	if err != nil {
		t.Fatalf("Parse() = %v", err)
	}
	if !f.Measured("aaaa") {
		t.Error("an entry recording a control reads as unmeasured")
	}
	if f.Measured("bbbb") {
		t.Error("an entry with bases and no control reads as measured; a high score is not " +
			"evidence that there was a problem to solve")
	}
	if f.Measured("cccc") {
		t.Error("a hash the evidence has never seen reads as measured")
	}
	// Bases still work for both: the two questions are independent, and an unmeasured
	// skill is not an unscored one.
	if b, _ := f.Bases("unmeasured", "bbbb", "ed1"); len(b) == 0 {
		t.Error("an unmeasured entry lost its bases")
	}
}

// TestLegacyEvidenceCannotClaimAControl pins the fail-closed direction. An unbound document
// records no hash, so nothing in it can be attributed to a particular version -- including
// a baseline observation.
func TestLegacyEvidenceCannotClaimAControl(t *testing.T) {
	t.Parallel()
	f, err := scores.Parse([]byte(`{"2":8,"3":7}`))
	if err != nil {
		t.Fatalf("Parse() = %v", err)
	}
	if f.Measured("aaaa") {
		t.Error("a legacy document claimed a control for a hash it does not record")
	}
}

// TestBaselineSurvivesARoundTrip guards the asymmetry that just bit: Marshal reflects over
// Entry's tags while parseEntries names its fields one by one, so a field added to the
// struct is written and silently dropped on the way back in. Marshal's contract says
// Parse(Marshal(e)) yields e, and only a test makes that true of a new field.
func TestBaselineSurvivesARoundTrip(t *testing.T) {
	t.Parallel()
	want := "without the skill, 4 of 5 runs skipped the checkpoint"
	b, err := scores.Marshal([]scores.Entry{
		{Skill: "alpha", Hash: "aaaa", Bases: map[int]int{2: 8}, Baseline: want},
	})
	if err != nil {
		t.Fatalf("Marshal() = %v", err)
	}
	f, err := scores.Parse(b)
	if err != nil {
		t.Fatalf("Parse() = %v", err)
	}
	if len(f.Entries) != 1 || f.Entries[0].Baseline != want {
		t.Errorf("baseline did not survive: %+v", f.Entries)
	}
	if !f.Measured("aaaa") {
		t.Error("a round-tripped control reads as unmeasured")
	}
}

// TestBasesRefuseAMismatchedRubric is the point of the edition. Right text, wrong rules is
// as stale as wrong text: a base answers the question the rubric asked, and changing the
// question changes what the answer means however unchanged the skill is.
//
// The legacy case is the migration cost and it is deliberate. Every scores file written
// before editions existed goes stale on upgrade and has to be re-judged, because an entry
// that records no edition cannot claim to match today's rules and reading it as a match is
// the silent failure the field exists to stop.
func TestBasesRefuseAMismatchedRubric(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		recorded  string
		asked     string
		wantStale bool
	}{
		"same edition":             {`,"rubric":"ed1"`, "ed1", false},
		"the rubric changed":       {`,"rubric":"ed1"`, "ed2", true},
		"legacy entry, no edition": {``, "ed1", true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := `{"entries":[{"skill":"alpha","hash":"aaa1","bases":{"1":8}` + tc.recorded + `}]}`
			f, err := scores.Parse([]byte(doc))
			if err != nil {
				t.Fatal(err)
			}
			bases, stale := f.Bases("alpha", "aaa1", tc.asked)
			if stale != tc.wantStale {
				t.Errorf("stale = %t, want %t", stale, tc.wantStale)
			}
			if stale && bases != nil {
				t.Errorf("stale bases were returned anyway: %v", bases)
			}
		})
	}
}

// TestRubricAtNamesTheOtherSide covers the reporting half: a stale verdict that does not
// say which rules produced the old number leaves the reader nothing to act on.
func TestRubricAtNamesTheOtherSide(t *testing.T) {
	t.Parallel()
	f, err := scores.Parse([]byte(
		`{"entries":[{"skill":"alpha","hash":"aaa1","bases":{"1":8},"rubric":"ed1"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := f.RubricAt("alpha"); got != "ed1" {
		t.Errorf("RubricAt = %q, want ed1", got)
	}
	if got := f.RubricAt("nobody"); got != "" {
		t.Errorf("RubricAt = %q for an unknown skill, want empty", got)
	}
}
