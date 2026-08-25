package rubric_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/rubric"
)

// document returns the shipped rubric source, which every case here starts from. Reading
// the real file rather than a miniature keeps these tests honest about the document that
// actually loads: a hand-written fixture drifts and then validates a shape nobody ships.
func document(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("rubric.toml")
	if err != nil {
		t.Fatalf("read rubric.toml: %v", err)
	}
	return string(b)
}

// TestEmbeddedRubricLoads is the build-time check that lets mustLoadEmbedded panic. The
// bytes are compiled in, so a malformed document is a broken source file rather than
// anything a caller can recover from -- but only if something looks.
func TestEmbeddedRubricLoads(t *testing.T) {
	t.Parallel()
	dims := rubric.Dimensions()
	if len(dims) != 9 {
		t.Fatalf("%d dimensions, want 9", len(dims))
	}
	sum := 0
	for _, d := range dims {
		sum += d.Weight
		if d.Note == "" {
			t.Errorf("dimension %d ships without a note recording why its weight is %d",
				d.Num, d.Weight)
		}
	}
	if sum != 100 {
		t.Errorf("weights sum to %d, want 100", sum)
	}
}

// TestLoadRefusesRatherThanFallsBack is the property the whole item turns on. A loader that
// fell back to compiled defaults on a bad document would score against yesterday's rubric
// while reporting success, which is the silent downgrade this family refuses everywhere.
// There is no partial result: every case must yield an error and no rubric.
func TestLoadRefusesRatherThanFallsBack(t *testing.T) {
	t.Parallel()
	src := document(t)
	cases := map[string]struct{ doc, wantIn string }{
		"a misspelled key": {
			strings.Replace(src, "blacklist_thin_base", "blacklist_thin_bass", 1),
			"unknown key",
		},
		"a duplicated dimension": {
			strings.Replace(src, `num = 2`, `num = 1`, 1),
			"declared twice",
		},
		"weights that do not sum to 100": {
			strings.Replace(src, "weight = 7", "weight = 6", 1),
			"sum to 99",
		},
		"a dimension with no note": {
			// Matched by shape, not by the note's text: keying a fixture to prose means an
			// edit to that prose silently turns the case off, which is how it stopped
			// testing anything the first time.
			regexp.MustCompile(`note = "[^"]*"`).ReplaceAllString(src, `note = ""`),
			"no note recording why",
		},
		"an undefined response": {
			strings.Replace(src, `response = "additive"`, `response = "inverse"`, 1),
			"want neutral, additive",
		},
		"an emptied word list": {
			regexp.MustCompile(`checkpoint_markers = \[[^]]*]`).
				ReplaceAllString(src, "checkpoint_markers = []"),
			"turns its check off",
		},
		"not toml at all":   {`{"dimension": []}`, "decode"},
		"an empty document": {``, "0 dimensions"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := rubric.Load(strings.NewReader(tc.doc))
			if err == nil {
				// Not %+v: a Rubric carries the whole document, and printing it buries
				// the one line that says which case failed.
				t.Fatalf("Load() accepted it and returned a rubric with %d dimensions",
					len(got.Dimension))
			}
			if got != nil {
				t.Errorf("Load() returned a rubric alongside an error: %+v", got)
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Errorf("error %q does not say %q", err, tc.wantIn)
			}
		})
	}
}

// TestUnknownKeysAreNamed is why this is TOML. Decoding JSON into a struct cannot tell a
// typo from a key the tool was not built to read, and a threshold somebody believes they
// changed is the expensive failure.
func TestUnknownKeysAreNamed(t *testing.T) {
	t.Parallel()
	_, err := rubric.Load(strings.NewReader(
		strings.Replace(document(t), "checkpoint_min_markers", "checkpoint_min_marker", 1)))
	if err == nil {
		t.Fatal("Load() accepted a misspelled threshold key")
	}
	if !strings.Contains(err.Error(), "checkpoint_min_marker") {
		t.Errorf("error %q does not name the offending key", err)
	}
}

// TestEditionMovesWithTheDocument covers what the edition is for: any edit to the rules
// invalidates grades produced under the previous ones.
func TestEditionMovesWithTheDocument(t *testing.T) {
	t.Parallel()
	src := document(t)
	edited := strings.Replace(src, "checkpoint_min_markers = 3", "checkpoint_min_markers = 4", 1)
	if edited == src {
		t.Fatal("the fixture did not change; this test would pass vacuously")
	}
	same, err := rubric.Load(strings.NewReader(src))
	if err != nil {
		t.Fatalf("the shipped document does not load: %v", err)
	}
	if same.Edition() != rubric.Edition() {
		t.Error("loading the shipped document does not reproduce the shipped edition")
	}
	changed, err := rubric.Load(strings.NewReader(edited))
	if err != nil {
		t.Fatalf("the edited document does not load: %v", err)
	}
	if changed.Edition() == rubric.Edition() {
		t.Error("a changed threshold left the edition alone; grades from the old rules " +
			"would still read as current")
	}
}
