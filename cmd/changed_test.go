package cmd_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChangedReportsNothingForAnUntouchedTree(t *testing.T) {
	t.Parallel()
	tree := makeTree(t, "alpha", "beta")
	m := baseManifest(t, tree, true)
	out, err := run(t, "changed", "--manifest", m, "--tree", tree)
	if err != nil {
		t.Fatalf("changed failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "0 to reprocess") || !strings.Contains(out, "2 unchanged") {
		t.Errorf("want nothing stale and 2 unchanged, got:\n%s", out)
	}
}

func TestChangedNamesTheEditedAndTheNew(t *testing.T) {
	t.Parallel()
	tree := makeTree(t, "alpha", "beta")
	m := baseManifest(t, tree, true)
	// Edit one, add one, delete one.
	f := filepath.Join(tree, "alpha", "SKILL.md")
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, append(b, []byte("\nedited\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	makeOne(t, tree, "gamma")
	if err := os.RemoveAll(filepath.Join(tree, "beta")); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "changed", "--manifest", m, "--tree", tree)
	if err != nil {
		t.Fatalf("changed failed: %v\n%s", err, out)
	}
	for _, want := range []string{"alpha", "gamma"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q not reported as stale:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "2 to reprocess (1 added, 1 changed)") {
		t.Errorf("counts wrong:\n%s", out)
	}
	if !strings.Contains(out, "1 removed") {
		t.Errorf("the deleted skill was not reported:\n%s", out)
	}
}

func TestChangedDoesNotCareHowTheTreeIsSpelled(t *testing.T) {
	t.Parallel()
	// The base manifest records the tree as "." with bare skill names; the scan records
	// it by absolute path. Diff takes each side's locations relative to its own tree, so
	// the two must agree -- otherwise every skill would look both added and removed.
	tree := makeTree(t, "alpha", "beta")
	m := manifestSpelledRelative(t, tree)
	out, err := run(t, "changed", "--manifest", m, "--tree", tree)
	if err != nil {
		t.Fatalf("changed failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "0 to reprocess") || !strings.Contains(out, "2 unchanged") {
		t.Errorf("two spellings of the same tree disagreed:\n%s", out)
	}
}

func TestChangedReportsADeletedSkillAsRemoved(t *testing.T) {
	t.Parallel()
	// Deleting SKILL.md takes the directory out of discovery entirely, so the skill is
	// gone rather than unreadable. Removed is the honest answer.
	tree := makeTree(t, "alpha", "beta")
	m := baseManifest(t, tree, true)
	if err := os.Remove(filepath.Join(tree, "alpha", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "changed", "--manifest", m, "--tree", tree)
	if err != nil {
		t.Fatalf("changed failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "1 removed") || !strings.Contains(out, "0 to reprocess") {
		t.Errorf("want alpha removed and nothing to reprocess:\n%s", out)
	}
}

func TestChangedReportsAnUnreadableSkillRatherThanDroppingIt(t *testing.T) {
	t.Parallel()
	// SKILL.md is present, so the skill is still in the tree, but its content cannot be
	// read and therefore cannot be shown to be unchanged. Silently treating it as
	// unchanged would drop it from every future campaign.
	if os.Geteuid() == 0 {
		t.Skip("running as root; an unreadable file cannot be simulated with permissions")
	}
	tree := makeTree(t, "alpha", "beta")
	m := baseManifest(t, tree, true)
	unreadable := filepath.Join(tree, "alpha", "SKILL.md")
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o600) })
	out, err := run(t, "changed", "--manifest", m, "--tree", tree)
	if err != nil {
		t.Fatalf("one bad skill aborted the whole scan: %v\n%s", err, out)
	}
	if !strings.Contains(out, "alpha") {
		t.Errorf("the unreadable skill was dropped:\n%s", out)
	}
	if !strings.Contains(out, "1 changed") {
		t.Errorf("an unknown hash must count as changed:\n%s", out)
	}
	if !strings.Contains(out, "1 unchanged") {
		t.Errorf("the readable skill was not still triaged:\n%s", out)
	}
}

func TestChangedJSON(t *testing.T) {
	t.Parallel()
	tree := makeTree(t, "alpha")
	m := baseManifest(t, tree, true)
	makeOne(t, tree, "gamma")
	out, err := run(t, "changed", "--manifest", m, "--tree", tree, "--json")
	if err != nil {
		t.Fatalf("changed failed: %v\n%s", err, out)
	}
	var got struct {
		Stale     []string `json:"stale"`
		Added     []string `json:"added"`
		Unchanged int      `json:"unchanged"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if len(got.Stale) != 1 || got.Stale[0] != "gamma" {
		t.Errorf("stale = %v, want [gamma]", got.Stale)
	}
	if got.Unchanged != 1 {
		t.Errorf("unchanged = %d, want 1", got.Unchanged)
	}
}

func TestChangedRejectsAFileThatIsNotAManifest(t *testing.T) {
	t.Parallel()
	// Aiming --manifest at the wrong JSON decodes into an empty manifest, which would
	// report every skill as added -- a plausible-looking answer to the wrong question.
	tree := makeTree(t, "alpha")
	bad := filepath.Join(t.TempDir(), "other.json")
	if err := os.WriteFile(bad, []byte(`{"name":"something-else"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := run(t, "changed", "--manifest", bad, "--tree", tree); err == nil {
		t.Errorf("accepted a document that is not a manifest:\n%s", out)
	}
}

func TestChangedRequiresAManifest(t *testing.T) {
	t.Parallel()
	if out, err := run(t, "changed", "--tree", t.TempDir()); err == nil {
		t.Errorf("ran without --manifest:\n%s", out)
	}
}
