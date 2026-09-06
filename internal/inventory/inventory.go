// Package inventory reads a skill tree into the manifest shape that manifest.Diff
// compares. It exists because two commands need the same reading -- "changed" to triage a
// tree against a baseline, "preflight" to judge one edit against it -- and a manifest
// entry built two ways is a manifest entry that can disagree with itself.
package inventory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/StevenACoffman/skillet/identity"
	"github.com/StevenACoffman/skillet/manifest"
	"github.com/StevenACoffman/skillet/related"
	"github.com/StevenACoffman/skillet/skill"
)

// promptsFile is the test-prompts filename this family writes, everywhere.
const promptsFile = "test-prompts.json"

// skillFile is the document every skill directory is identified by.
const skillFile = "SKILL.md"

// ErrEdgesUnrecorded says a manifest's producer never read the related-skills graph, so
// the document says nothing about what the graph looked like.
//
// It is distinct from a parse failure on purpose: the two want different advice. This one
// says re-run the producer with a version that records edges; a parse failure says check
// the path. Collapsing them would send a reader to the wrong repair.
//
// A sentinel rather than a message, so a caller matches it with errors.Is instead of
// reading the prose -- and so the manifest's own fail-closed rule has something to fail
// closed *to*. Reporting "nothing was disconnected" against a graph nobody recorded is
// the green-build-over-red-report shape this check exists to avoid.
var ErrEdgesUnrecorded = errors.New("the manifest's producer did not record skill edges")

// Entry reads one skill directory into its manifest record.
//
// Both hashes are recorded, and the two absences mean different things. An unreadable
// SKILL.md leaves Hash empty, which Diff reads as unknown-therefore-changed -- the skill
// stays in the campaign rather than dropping out of the inventory. A missing test-prompts
// file leaves TestPrompts and TestPromptsHash both empty, which Diff reads as absent
// rather than unknown, so a skill that has never had prompts is not permanently
// interesting.
//
// The edges come off the same load as the hash rather than a second read, which is what
// keeps an unloadable skill under one rule: Hash and Edges are both empty because nothing
// was read, not because the document said nothing.
//
// Recording them cannot change what Diff reports. Edges live inside SKILL.md, so an edge
// change already moves Hash, and skillet excludes them from the comparison for that
// reason -- feeding them to the axes as well would report one change on two and make
// every graph edit look like two.
//
// Requires: dir names a directory.
// Ensures:  never fails -- a directory that cannot be read yields an entry with empty
// hashes, because refusing here would remove a skill from the inventory entirely and it
// would then never be looked at again.
func Entry(dir string) manifest.Skill {
	entry := manifest.Skill{Slug: filepath.Base(dir), Dir: dir}
	if s, err := skill.Load(dir); err == nil {
		entry.Hash = s.Hash()
		entry.Edges = related.EdgeMap(related.ParseSection(s.Body))
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
//
// EdgesRecorded is set because Entry reads the graph on every skill. It is not a claim
// that any edges were found: a tree declaring none and a tree nobody asked are the same
// bytes on the wire, and this flag is the only thing that tells them apart.
func Tree(root string) (manifest.Manifest, error) {
	dirs, err := skill.Discover(root)
	if err != nil {
		return manifest.Manifest{}, fmt.Errorf("discover skills under %s: %w", root, err)
	}
	skills := make([]manifest.Skill, 0, len(dirs))
	for _, dir := range dirs {
		skills = append(skills, Entry(dir))
	}
	return manifest.Manifest{Tree: root, Skills: skills, EdgesRecorded: true}, nil
}

// RecordedGraph reconstitutes the skill graph a manifest recorded.
//
// It is the path for the case the recorded edges exist for: the baseline is a published
// artifact and the checkout is gone. When both trees are on disk, read both -- one parser
// at one version cannot drift from itself, and a recorded edge is a snapshot that can.
//
// Nodes carry no Body, so nothing downstream may ask a reconstituted graph how much of
// itself the reader could see. That is why Coverage describes the current tree alone.
//
// The Slug is the one recorded, not filepath.Base of the recorded Dir. Edges name slugs,
// so the nodes must be slug-keyed, and re-deriving one would produce a second answer that
// can disagree with the field beside it in the same document.
//
// Requires: m came from manifest.Parse.
// Ensures:  pure. One Node per skill in m, in the manifest's order, with an edge for every
// recorded kind and target. Returns ErrEdgesUnrecorded when the producer did not read the
// graph, because a manifest that recorded nothing and a tree that declares nothing are the
// same bytes and must not be the same answer.
func RecordedGraph(m manifest.Manifest) ([]related.Node, error) {
	if !m.EdgesRecorded {
		return nil, fmt.Errorf("%s: %w", m.Tool, ErrEdgesUnrecorded)
	}
	nodes := make([]related.Node, 0, len(m.Skills))
	for _, s := range m.Skills {
		nodes = append(nodes, related.Node{
			Slug:  s.Slug,
			Edges: related.EdgesFrom(s.Edges),
		})
	}
	return nodes, nil
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

// Graph walks root and reads every skill's related-skills edges.
//
// The parse is skillet's. related.ParseSection handles six bullet dialects, fenced sections
// and wrapped rationales, and a second reading of "what is an edge" would disagree with it
// at exactly those margins -- which is the reason the orphan check compares two trees rather
// than a tree against recorded edges: both sides go through this function, at one version.
//
// A skill whose SKILL.md cannot be read is skipped rather than fatal, matching Tree: one
// unreadable directory in a corpus of hundreds must not discard the rest, and a skill that
// is not there points at nothing and is pointed at by nothing.
//
// Requires: root contains skill directories.
// Ensures:  one Node per readable skill, in discovery order, Slug being the directory name.
func Graph(root string) ([]related.Node, error) {
	dirs, err := skill.Discover(root)
	if err != nil {
		return nil, fmt.Errorf("discover skills under %s: %w", root, err)
	}
	nodes := make([]related.Node, 0, len(dirs))
	for _, dir := range dirs {
		b, err := os.ReadFile(filepath.Join(dir, skillFile))
		if err != nil {
			continue
		}
		body := string(b)
		nodes = append(nodes, related.Node{
			Slug:  filepath.Base(dir),
			Body:  body,
			Edges: related.ParseSection(body),
		})
	}
	return nodes, nil
}
