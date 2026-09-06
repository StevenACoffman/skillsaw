package inventory_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenACoffman/skillet/manifest"
	"github.com/StevenACoffman/skillet/related"
	"github.com/StevenACoffman/skillsaw/internal/inventory"
)

// skillMD keeps the fixture writer honest about which document it is writing.
const skillMD = "SKILL.md"

// writeSkill writes one skill with the given Related Skills bullets. The bullet dialect is
// the one skillet renders, so these tests exercise the real parser rather than a shape
// hand-built to suit them.
func writeSkill(t *testing.T, tree, slug string, bullets ...string) {
	t.Helper()
	dir := filepath.Join(tree, slug)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	body.WriteString("---\nname: " + slug + "\ndescription: Use when demoing " + slug +
		".\n---\n\n# " + slug + "\n\nbody\n")
	if len(bullets) > 0 {
		body.WriteString("\n## Related Skills\n\n")
		for _, b := range bullets {
			body.WriteString(b + "\n")
		}
	}
	if err := os.WriteFile(filepath.Join(dir, skillMD), []byte(body.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

// render writes a graph as one line per skill: "slug: kind→target(rationale), ...".
//
// The rationale is rendered even though a manifest never carries one, which is the point:
// if the round trip ever started preserving it the expectation below would show it rather
// than a separate branch having to go looking.
func render(nodes []related.Node) string {
	var out strings.Builder
	for _, n := range nodes {
		out.WriteString(n.Slug + ":")
		for _, e := range n.Edges {
			out.WriteString(" " + string(e.Kind) + "→" + e.Target + "(" + e.Rationale + ")")
		}
		out.WriteString("\n")
	}
	return out.String()
}

// TestRecordedGraphRoundTripsKindAndTargetAndDropsTheRationale pins the contract in the
// direction it is actually true. The manifest deliberately keeps no rationale, so a test
// asserting that a node survives a manifest unchanged would be asserting something the
// design rejected — and would fail for the right reason in the wrong words.
func TestRecordedGraphRoundTripsKindAndTargetAndDropsTheRationale(t *testing.T) {
	t.Parallel()
	tree := t.TempDir()
	writeSkill(t, tree, "a",
		"- **depends-on** → `b`: because b comes first.",
		"- **composes-with** → `c`: because they are used together.")
	writeSkill(t, tree, "b")
	writeSkill(t, tree, "c")

	m, err := inventory.Tree(tree)
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if !m.EdgesRecorded {
		t.Fatal("Tree walked the graph but did not say so; a consumer will read it as unavailable")
	}
	nodes, err := inventory.RecordedGraph(m)
	if err != nil {
		t.Fatalf("RecordedGraph: %v", err)
	}

	// The empty parentheses are the assertion: both bullets carried a rationale on disk.
	want := "a: composes-with→c() depends-on→b()\nb:\nc:\n"
	if got := render(nodes); got != want {
		t.Errorf("round trip mismatch\ngot:\n%swant:\n%s", got, want)
	}
}

// TestRecordingEdgesDoesNotMoveTheDiff is the control on the producer change. Edges live
// inside SKILL.md, so an edge change already moves Hash; if Diff read them as well, every
// graph edit would report as a change on two axes and the campaign triage this manifest
// exists for would double-count it.
//
// skillet pins the rule from its own side. This asserts it from the side that would break
// it — recording a field is what makes reading it possible.
func TestRecordingEdgesDoesNotMoveTheDiff(t *testing.T) {
	t.Parallel()
	tree := t.TempDir()
	writeSkill(t, tree, "a", "- **depends-on** → `b`: because b comes first.")
	writeSkill(t, tree, "b")

	withEdges, err := inventory.Tree(tree)
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	// The same reading with the edges struck out: identical hashes, no recorded graph.
	stripped := manifest.Manifest{Tree: withEdges.Tree}
	for _, s := range withEdges.Skills {
		s.Edges = nil
		stripped.Skills = append(stripped.Skills, s)
	}
	if len(withEdges.Skills) == 0 || withEdges.Skills[0].Edges == nil {
		t.Fatal("the fixture recorded no edges, so this control proves nothing")
	}

	d := manifest.Diff(stripped, withEdges)
	if len(d.Changed) != 0 || len(d.Added) != 0 || len(d.Removed) != 0 {
		t.Errorf("recording edges moved the diff: %+v", d)
	}
	if len(d.Unchanged) != len(withEdges.Skills) {
		t.Errorf("got %d unchanged, want %d", len(d.Unchanged), len(withEdges.Skills))
	}
}

// TestRecordedGraphRefusesAManifestThatNeverReadTheGraph is the fail-closed rule, and it
// is the whole reason skillet put EdgesRecorded on the manifest rather than inferring it
// per skill. A tree that genuinely declares no edges and a producer that never looked are
// the same bytes; returning an empty graph for the second would report "nothing was
// disconnected" against a baseline nobody ever had.
func TestRecordedGraphRefusesAManifestThatNeverReadTheGraph(t *testing.T) {
	t.Parallel()
	cases := map[string]manifest.Manifest{
		"a manifest predating the field": {
			Tool:   "exegesis",
			Tree:   ".",
			Skills: []manifest.Skill{{Slug: "a", Dir: "a", Hash: "h"}},
		},
		"a producer that recorded edges but forgot the flag": {
			Tool: "exegesis",
			Tree: ".",
			Skills: []manifest.Skill{
				{Slug: "a", Dir: "a", Hash: "h", Edges: map[string][]string{
					"depends-on": {"b"},
				}},
			},
		},
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			nodes, err := inventory.RecordedGraph(m)
			if !errors.Is(err, inventory.ErrEdgesUnrecorded) {
				t.Fatalf("got (%v, %v), want ErrEdgesUnrecorded", nodes, err)
			}
			if nodes != nil {
				t.Errorf("a refusal still returned %d nodes", len(nodes))
			}
		})
	}
}

// TestRecordedGraphIsOrderedIndependentlyOfMapIteration guards the one nondeterminism a
// recorded edge map introduces. Go randomises map iteration, so without an explicit order
// two runs over one manifest would produce differently ordered edges and any comparison
// against them would be unstable for no reason in the data.
func TestRecordedGraphIsOrderedIndependentlyOfMapIteration(t *testing.T) {
	t.Parallel()
	m := manifest.Manifest{
		Tool: "skillsaw", Tree: ".", EdgesRecorded: true,
		Skills: []manifest.Skill{{Slug: "a", Dir: "a", Edges: map[string][]string{
			"informs":       {"z", "y"},
			"depends-on":    {"c", "b"},
			"composes-with": {"d"},
		}}},
	}
	want := []related.Edge{
		{Kind: related.ComposesWith, Target: "d"},
		{Kind: related.DependsOn, Target: "c"},
		{Kind: related.DependsOn, Target: "b"},
		{Kind: related.Informs, Target: "z"},
		{Kind: related.Informs, Target: "y"},
	}
	for range 20 {
		nodes, err := inventory.RecordedGraph(m)
		if err != nil {
			t.Fatalf("RecordedGraph: %v", err)
		}
		if len(nodes) != 1 || len(nodes[0].Edges) != len(want) {
			t.Fatalf("got %v, want one node with %d edges", nodes, len(want))
		}
		for i, e := range nodes[0].Edges {
			if e != want[i] {
				t.Fatalf("edge %d = %v, want %v", i, e, want[i])
			}
		}
	}
}
