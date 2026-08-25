// Package inventory reads a skill tree into the manifest shape that manifest.Diff
// compares. It exists because two commands need the same reading -- "changed" to triage a
// tree against a baseline, "preflight" to judge one edit against it -- and a manifest
// entry built two ways is a manifest entry that can disagree with itself.
package inventory

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/StevenACoffman/skillet/identity"
	"github.com/StevenACoffman/skillet/manifest"
	"github.com/StevenACoffman/skillet/skill"
)

// promptsFile is the test-prompts filename this family writes, everywhere.
const promptsFile = "test-prompts.json"

// Entry reads one skill directory into its manifest record.
//
// Both hashes are recorded, and the two absences mean different things. An unreadable
// SKILL.md leaves Hash empty, which Diff reads as unknown-therefore-changed -- the skill
// stays in the campaign rather than dropping out of the inventory. A missing test-prompts
// file leaves TestPrompts and TestPromptsHash both empty, which Diff reads as absent
// rather than unknown, so a skill that has never had prompts is not permanently
// interesting.
//
// Requires: dir names a directory.
// Ensures:  never fails -- a directory that cannot be read yields an entry with empty
// hashes, because refusing here would remove a skill from the inventory entirely and it
// would then never be looked at again.
func Entry(dir string) manifest.Skill {
	entry := manifest.Skill{Slug: filepath.Base(dir), Dir: dir}
	if s, err := skill.Load(dir); err == nil {
		entry.Hash = s.Hash()
	}
	path := filepath.Join(dir, promptsFile)
	if b, err := os.ReadFile(path); err == nil {
		entry.TestPrompts = path
		entry.TestPromptsHash = identity.Hash(string(b))
	}
	return entry
}

// Tree walks root and reads every skill under it.
//
// The result is a manifest literal rather than manifest.Build's output: Build also records
// the emitting tool and whether every gate passed, and neither has a meaning for a tree
// that has just been walked. Diff reads only Tree and Skills.
func Tree(root string) (manifest.Manifest, error) {
	dirs, err := skill.Discover(root)
	if err != nil {
		return manifest.Manifest{}, fmt.Errorf("discover skills under %s: %w", root, err)
	}
	skills := make([]manifest.Skill, 0, len(dirs))
	for _, dir := range dirs {
		skills = append(skills, Entry(dir))
	}
	return manifest.Manifest{Tree: root, Skills: skills}, nil
}

// Location identifies a skill the way manifest.Diff keys it: by directory relative to the
// tree it was recorded under, so the same skill matches across manifests that spelled the
// tree differently. A caller matching a Delta entry back to the directory that produced it
// needs the identical rule, and two copies of it that must agree is the class of defect
// this package exists to prevent.
func Location(tree, dir string) string {
	if rel, err := filepath.Rel(tree, dir); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(dir)
}
