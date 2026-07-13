package markdown_test

import (
	"strings"
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/markdown"
)

func TestSectionsAndUnits(t *testing.T) {
	t.Parallel()
	body := "# Title\n\n## Boundary\n\n- do not A\n- avoid B\n- never C\n\n## Notes\n\nplain.\n"
	doc := markdown.Parse(body)
	if got := sectionUnits(doc, "boundary"); got != 3 {
		t.Errorf("Boundary units = %d, want 3 (three list items)", got)
	}
	// A short paragraph (<20 runes) is not a content unit.
	if got := sectionUnits(doc, "notes"); got != 0 {
		t.Errorf("Notes units = %d, want 0 (short paragraph)", got)
	}
	// An H1 spans its sub-sections: Boundary's 3 items + nothing countable in Notes.
	if got := sectionUnits(doc, "title"); got != 3 {
		t.Errorf("Title units = %d, want 3 (spans Boundary)", got)
	}
}

func TestFenceCommentIsNotAHeading(t *testing.T) {
	t.Parallel()
	// A "## Deploy step" line inside a fenced code block must NOT end the
	// "Common Mistakes" section — the three bullets after the block still count.
	body := "## Common Mistakes\n\n```bash\n# Configure\n## Deploy step\nexport X=1\n```\n\n" +
		"- do not skip validation\n- avoid hardcoding secrets\n- never commit the token\n"
	doc := markdown.Parse(body)
	for _, s := range doc.Sections {
		if strings.Contains(strings.ToLower(s.Title), "deploy step") {
			t.Fatalf("a code-fence comment became a section: %q", s.Title)
		}
	}
	if got := sectionUnits(doc, "common mistakes"); got != 3 {
		t.Errorf("Common Mistakes units = %d, want 3 (bullets after the code block)", got)
	}
}

func TestTableRowsCount(t *testing.T) {
	t.Parallel()
	body := "## Mistakes\n\n| A | B |\n| --- | --- |\n| one | two |\n| three | four |\n"
	if got := sectionUnits(markdown.Parse(body), "mistakes"); got < 3 {
		t.Errorf("table section units = %d, want >= 3 (header + 2 rows)", got)
	}
}

func TestProseBlanksCode(t *testing.T) {
	t.Parallel()
	body := "Please feel free to try. Remove hollow phrases like `it's worth noting`.\n\n" +
		"```\nit's worth noting this code\n```\n"
	prose := markdown.Parse(body).Prose
	if !strings.Contains(prose, "feel free") {
		t.Error("prose should keep normal text")
	}
	if strings.Contains(prose, "it's worth noting") {
		t.Errorf("prose should blank code-span/block text; got: %q", prose)
	}
}

func TestLinksAndOrderedList(t *testing.T) {
	t.Parallel()
	body := "1. first\n2. second\n\nSee [ref](references/x.md) and `methodology/y.md` and <https://ex.com>.\n"
	doc := markdown.Parse(body)
	if !doc.HasOrderedList {
		t.Error("expected HasOrderedList")
	}
	joined := strings.Join(doc.Links, "|")
	if !strings.Contains(joined, "references/x.md") ||
		!strings.Contains(joined, "methodology/y.md") {
		t.Errorf("links missing expected refs: %v", doc.Links)
	}
}

// sectionUnits returns the Units of the first section whose lowercased title
// contains want, or -1.
func sectionUnits(doc *markdown.Doc, want string) int {
	for _, s := range doc.Sections {
		if strings.Contains(strings.ToLower(s.Title), want) {
			return s.Units
		}
	}
	return -1
}
