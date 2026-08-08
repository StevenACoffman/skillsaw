package cmd_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fixtures for the two commands that read a skills-manifest.json. Both "verified" and
// "changed" consume the same document, so one set of fixtures serves both.

// tinySkill is enough for skill.Load to succeed and hash; these tests are about the
// manifest comparison, not about skill quality.
const tinySkill = "---\nname: %s\ndescription: Use when the demo thing is needed.\n---\n\nbody\n"

// entry and doc mirror the on-disk manifest shape, so these tests exercise the real
// reader rather than a Go value that skipped it.
type entry struct {
	Slug string `json:"slug"`
	Dir  string `json:"dir"`
	Hash string `json:"sha256,omitempty"`
}

type doc struct {
	Tool              string  `json:"tool"`
	Tree              string  `json:"tree"`
	StructureVerified bool    `json:"structure_verified"`
	Skills            []entry `json:"skills"`
}

// makeTree writes one directory per named skill and returns the tree path.
func makeTree(t *testing.T, names ...string) string {
	t.Helper()
	tree := t.TempDir()
	for _, n := range names {
		dir := filepath.Join(tree, n)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		body := strings.Replace(tinySkill, "%s", n, 1)
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return tree
}

// manifestDoc builds a manifest describing tree exactly as it stands, hashing each
// skill through the CLI so the fixture cannot drift from what skillsaw computes.
func manifestDoc(t *testing.T, tree string) doc {
	t.Helper()
	d := doc{Tool: "exegesis", Tree: tree, StructureVerified: true}
	ents, err := os.ReadDir(tree)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(tree, e.Name())
		out, err := run(t, "hash", dir)
		if err != nil {
			t.Fatalf("hash %s: %v", dir, err)
		}
		d.Skills = append(d.Skills, entry{
			Slug: e.Name(), Dir: dir, Hash: strings.Fields(out)[0],
		})
	}
	return d
}

// writeManifest marshals d to a fresh file and returns its path.
func writeManifest(t *testing.T, d doc) string {
	t.Helper()
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "skills-manifest.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// baseManifest writes a manifest describing tree exactly as it stands.
func baseManifest(t *testing.T, tree string, verified bool) string {
	t.Helper()
	d := manifestDoc(t, tree)
	d.StructureVerified = verified
	return writeManifest(t, d)
}

// makeOne writes a single extra skill into an existing tree.
func makeOne(t *testing.T, tree, name string) {
	t.Helper()
	dir := filepath.Join(tree, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := strings.Replace(tinySkill, "%s", name, 1)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// manifestSpelledRelative writes a manifest describing tree, but recording it as "."
// with bare skill names instead of by absolute path.
func manifestSpelledRelative(t *testing.T, tree string) string {
	t.Helper()
	doc := manifestDoc(t, tree)
	doc.Tree = "."
	for i := range doc.Skills {
		doc.Skills[i].Dir = doc.Skills[i].Slug
	}
	return writeManifest(t, doc)
}
