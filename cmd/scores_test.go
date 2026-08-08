package cmd_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScoresBindsBasesToTheSkillItRead(t *testing.T) {
	t.Parallel()
	// The hash comes from the skill, not from a flag: passing it in would reintroduce
	// the mistake the binding exists to catch.
	tree := makeTree(t, "alpha")
	dir := filepath.Join(tree, "alpha")
	out, err := run(t, "scores", "--skill", dir, "--bases", "1=8,2=7,5=9")
	if err != nil {
		t.Fatalf("scores failed: %v\n%s", err, out)
	}
	var got struct {
		Entries []struct {
			Skill string         `json:"skill"`
			Hash  string         `json:"hash"`
			Bases map[string]int `json:"bases"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if len(got.Entries) != 1 {
		t.Fatalf("want one entry, got %d", len(got.Entries))
	}
	e := got.Entries[0]
	if e.Skill != "alpha" || e.Bases["1"] != 8 || e.Bases["5"] != 9 {
		t.Errorf("entry wrong: %+v", e)
	}
	if e.Hash != hashOf(t, dir) {
		t.Errorf("hash = %q, want the skill's own %q", e.Hash, hashOf(t, dir))
	}
}

func TestScoresOutputIsAcceptedByEval(t *testing.T) {
	t.Parallel()
	// The round trip that matters: what this writes is what eval reads.
	tree := makeTree(t, "alpha")
	dir := filepath.Join(tree, "alpha")
	out, err := run(t, "scores", "--skill", dir, "--bases", "1=9,2=9,3=9,4=9,5=9,6=9,7=9,8=9,9=9")
	if err != nil {
		t.Fatalf("scores failed: %v\n%s", err, out)
	}
	f := filepath.Join(t.TempDir(), "scores.json")
	if err := os.WriteFile(f, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := run(t, "eval", "--scores", f, dir)
	if err != nil {
		t.Fatalf("eval rejected what scores wrote: %v\n%s", err, got)
	}
	if full := fullOf(got, "alpha"); full == "" || full == "-" {
		t.Errorf("no FULL total from the written bases:\n%s", got)
	}
}

func TestScoresRefusesWhatEvalWouldReject(t *testing.T) {
	t.Parallel()
	tree := makeTree(t, "alpha")
	dir := filepath.Join(tree, "alpha")
	cases := map[string]string{
		"base above the ceiling": "1=11",
		"base below the floor":   "1=0",
		"dimension out of range": "12=8",
		"not a pair":             "1",
		// The leak a lone bad pair hides: dropped silently, the valid ones still
		// produce a document, and the missing dimension only shows up as no FULL total.
		"a bad pair beside good ones": "1=8,2,3=7",
		"non-numeric base":            "1=high",
		"non-numeric dimension":       "one=8",
		"same dimension twice":        "1=8,1=9",
		"nothing at all":              "",
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if out, err := run(t, "scores", "--skill", dir, "--bases", spec); err == nil {
				t.Errorf("accepted %q:\n%s", spec, out)
			}
		})
	}
}

func TestScoresRequiresASkill(t *testing.T) {
	t.Parallel()
	if out, err := run(t, "scores", "--bases", "1=8"); err == nil {
		t.Errorf("ran without --skill:\n%s", out)
	}
}

func TestScoresBindingIsRefusedAfterAnEdit(t *testing.T) {
	t.Parallel()
	// End to end: emit, edit, and the same file is no longer applied.
	tree := makeTree(t, "alpha")
	dir := filepath.Join(tree, "alpha")
	doc, err := run(t, "scores", "--skill", dir, "--bases", "1=9,2=9,3=9,4=9,5=9,6=9,7=9,8=9,9=9")
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "scores.json")
	if err := os.WriteFile(f, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	md := filepath.Join(dir, "SKILL.md")
	b, err := os.ReadFile(md)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(md, append(b, []byte("\nedited\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "eval", "--scores", f, dir)
	if err == nil {
		t.Fatalf("stale bases were applied after an edit:\n%s", out)
	}
	if !strings.Contains(out, "re-judge it") {
		t.Errorf("expected the stale report, got:\n%s", out)
	}
}
