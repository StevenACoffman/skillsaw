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
		{"skill":"alpha","hash":"aaa1","bases":{"1":8}},
		{"skill":"beta","hash":"bbb2","bases":{"2":6}}]}`))
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
			`{"entries":[{"skill":"alpha","bases":{"1":8}}]}`, "no hash",
		},
		"entry base out of range": {
			`{"entries":[{"skill":"a","hash":"h","bases":{"1":99}}]}`, "out of range",
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
		`{"entries":[{"skill":"alpha","hash":"aaa1","bases":{"1":8}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	bases, stale := f.Bases("alpha", "aaa1")
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
		`{"entries":[{"skill":"alpha","hash":"aaa1","bases":{"1":8}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	t.Run("edited since it was judged", func(t *testing.T) {
		t.Parallel()
		bases, stale := f.Bases("alpha", "different")
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
		bases, stale := f.Bases("gamma", "ccc3")
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
	bases, stale := f.Bases("anything", "any-hash")
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
	f, err := scores.Parse([]byte(`{"entries":[{"hash":"aaa1","bases":{"1":8}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if bases, stale := f.Bases("", "aaa1"); stale || bases[1] != 8 {
		t.Errorf("unnamed entry not matched: bases=%v stale=%t", bases, stale)
	}
	if _, stale := f.Bases("", "other"); stale {
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
	if bases, stale := f.Bases("alpha", "aaa1"); stale || bases != nil {
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
		{Skill: "alpha", Hash: "aaa1", Bases: map[int]int{1: 8, 2: 7}},
		{Skill: "beta", Hash: "bbb2", Bases: map[int]int{5: 10}},
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
	if bases, stale := got.Bases("alpha", "aaa1"); stale || bases[1] != 8 {
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
