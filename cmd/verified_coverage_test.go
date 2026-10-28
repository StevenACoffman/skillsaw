package cmd_test

import (
	"strings"
	"testing"
)

// TestVerifiedRefusesAManifestCoveringNothing is the measured defect: a tree holding no
// skills passed every gate vacuously, so structure_verified was true and this command --
// the one that exists to stop a campaign starting on a bad tree -- said go.
func TestVerifiedRefusesAManifestCoveringNothing(t *testing.T) {
	t.Parallel()
	path := writeManifest(t, doc{Tool: "exegesis", Tree: "/t", StructureVerified: true})

	out, err := run(t, "verified", path)
	if err == nil {
		t.Fatalf("a manifest covering no skills should not pass, got:\n%s", out)
	}
	if !strings.Contains(out, "examined=0") {
		t.Errorf("want the coverage reported, got:\n%s", out)
	}
}

// TestVerifiedDoesNotCallAnEmptyTreeDefective guards the property, not the wording. This
// command's help already argues the neighbouring case: reporting an unexamined tree as a
// failed gate "would send the reader looking for structural defects in a tree that was
// never examined". An empty tree is that sentence with the boolean the other way up.
func TestVerifiedDoesNotCallAnEmptyTreeDefective(t *testing.T) {
	t.Parallel()
	path := writeManifest(t, doc{Tool: "exegesis", Tree: "/t", StructureVerified: true})

	out, _ := run(t, "verified", path)
	if !strings.Contains(out, "not examined rather than found defective") {
		t.Errorf("the refusal must say the tree was not examined, got:\n%s", out)
	}
	if strings.Contains(out, "structural defect") || strings.Contains(out, "failed its") {
		t.Errorf("the refusal reads as a structural failure, which it is not:\n%s", out)
	}
}

// TestVerifiedHonoursAManifestWrittenBeforeTheCount is the migration property. Every
// manifest stored before skillet v0.30.0 lists skills and carries no count; refusing those
// would fail the whole corpus on the day this shipped.
func TestVerifiedHonoursAManifestWrittenBeforeTheCount(t *testing.T) {
	t.Parallel()
	tree := makeTree(t, "alpha")
	path := baseManifest(t, tree, true)

	out, err := run(t, "verified", path)
	if err != nil {
		t.Fatalf("a pre-count manifest was rejected: %v\n%s", err, out)
	}
	if !strings.Contains(out, "examined=unknown") {
		t.Errorf("want the missing count reported as unknown, got:\n%s", out)
	}
}

// TestVerifiedStillFailsAnUnverifiedTreeWithACount keeps the new check from swallowing the
// old one: coverage is asked first, and a tree that was examined and failed still fails.
func TestVerifiedStillFailsAnUnverifiedTreeWithACount(t *testing.T) {
	t.Parallel()
	d := manifestDoc(t, makeTree(t, "alpha"))
	d.StructureVerified = false
	d.Examined = 1
	path := writeManifest(t, d)

	out, err := run(t, "verified", path)
	if err == nil {
		t.Fatalf("an unverified tree should still fail, got:\n%s", out)
	}
	if !strings.Contains(out, "examined=1") {
		t.Errorf("want the count reported, got:\n%s", out)
	}
}

// TestVerifiedPassesAnExaminedVerifiedTree is the ordinary case, and the one that must not
// have become harder: a real exegesis manifest now carries a count and still passes.
func TestVerifiedPassesAnExaminedVerifiedTree(t *testing.T) {
	t.Parallel()
	d := manifestDoc(t, makeTree(t, "alpha", "beta"))
	d.Examined = 2
	path := writeManifest(t, d)

	out, err := run(t, "verified", path)
	if err != nil {
		t.Fatalf("a verified tree with a count was rejected: %v\n%s", err, out)
	}
	if !strings.Contains(out, "examined=2") {
		t.Errorf("want the count itself reported, got:\n%s", out)
	}
}
