// Package skill loads and parses Agent Skills ("SKILL.md") files.
//
// A skill is a directory containing a SKILL.md in the Anthropic Agent Skills
// format: YAML frontmatter (name + description) delimited by "---" lines,
// followed by a Markdown body. This package does the deterministic parsing and
// identity hashing that the rest of skillsaw builds on; it never calls a model.
package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

// Skill is a parsed SKILL.md and its location.
type Skill struct {
	Dir         string // skill directory
	Path        string // <Dir>/SKILL.md
	Name        string // frontmatter name
	Description string // frontmatter description
	Frontmatter string // raw YAML frontmatter block (between the --- lines)
	Body        string // markdown body after the frontmatter
	Raw         string // full file contents
	Bytes       int    // byte size of Raw (for the 150% growth guard)
}

// Load reads and parses <dir>/SKILL.md.
func Load(dir string) (*Skill, error) {
	p := filepath.Join(dir, "SKILL.md")
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("load skill %s: %w", dir, err)
	}
	s := &Skill{Dir: dir, Path: p, Raw: string(b), Bytes: len(b)}
	s.parse()
	return s, nil
}

// DefaultRoots returns the runtime-neutral directories scanned by "--all". The
// darwin source hard-codes .claude/skills, but the skill's own neutrality
// mandate (spec §4.2, D7) requires covering every skills-compatible runtime.
func DefaultRoots() []string {
	return []string{
		".claude/skills",
		".cursor/skills",
		".codex/skills",
		".agents/skills",
	}
}

// Hash returns the first 16 hex chars of sha256(content) — byte-identical to
// SkillOpt's skill_hash (skillopt/utils/scoring.py). Used as a content identity
// for caching evaluations and detecting no-op edits (darwin spec §8.7).
func Hash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])[:16]
}

// Discover returns every directory under the given roots that contains a
// SKILL.md. Missing roots are skipped silently; the caller decides whether an
// empty result is an error.
func Discover(base string, roots []string) ([]string, error) {
	var dirs []string
	seen := map[string]bool{}
	for _, root := range roots {
		rootPath := filepath.Join(base, root)
		entries, err := os.ReadDir(rootPath)
		if err != nil {
			continue // root not present in this runtime; not an error
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dir := filepath.Join(rootPath, e.Name())
			if _, statErr := os.Stat(filepath.Join(dir, "SKILL.md")); statErr == nil && !seen[dir] {
				seen[dir] = true
				dirs = append(dirs, dir)
			}
		}
	}
	return dirs, nil
}

// DiscoverRoots splits a comma-separated roots string and discovers skill
// directories under the current working directory. Commands use this instead of
// os.Getwd + Discover so the "--all" logic lives in one place.
func DiscoverRoots(roots string) ([]string, error) {
	var rs []string
	for _, r := range strings.Split(roots, ",") {
		if r = strings.TrimSpace(r); r != "" {
			rs = append(rs, r)
		}
	}
	return Discover(".", rs)
}

// Slug normalizes a string to the Agent Skills slug form (SkillLens slugify,
// simplified): lowercase, every run of non-[a-z0-9] becomes one hyphen, leading
// and trailing hyphens are trimmed, and the result is capped at 64 runes; an
// empty result becomes "skill". It is idempotent: Slug(Slug(x)) == Slug(x).
//
// Unlike SkillLens it does not NFKD-fold accents (é stays a separator, not "e"),
// avoiding an x/text dependency; skill names are ASCII kebab-case in practice, so
// the results match for every real name.
func Slug(s string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevHyphen = false
			continue
		}
		if !prevHyphen {
			b.WriteByte('-')
			prevHyphen = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if r := []rune(out); len(r) > 64 {
		out = strings.TrimRight(string(r[:64]), "-")
	}
	if out == "" {
		return "skill"
	}
	return out
}

func (s *Skill) parse() {
	text := strings.ReplaceAll(s.Raw, "\r\n", "\n")
	s.Frontmatter, s.Body = splitFrontmatter(text)
	var fm struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	// A malformed frontmatter leaves Name/Description empty; dim1 flags it.
	if err := yaml.Unmarshal([]byte(s.Frontmatter), &fm); err != nil {
		return
	}
	s.Name = strings.TrimSpace(fm.Name)
	s.Description = strings.TrimSpace(fm.Description)
}

// splitFrontmatter separates a leading "---"-delimited YAML block from the body.
// It returns ("", text) when there is no frontmatter and ("", rest) when the
// opening delimiter has no matching close.
func splitFrontmatter(text string) (frontmatter, body string) {
	if !strings.HasPrefix(text, "---\n") {
		return "", text
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", rest
	}
	after := rest[end+len("\n---"):]
	if nl := strings.IndexByte(after, '\n'); nl >= 0 {
		body = after[nl+1:]
	}
	return rest[:end], body
}
