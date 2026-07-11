package skill_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/skill"
)

var hexOnly = regexp.MustCompile(`^[0-9a-f]{16}$`)

func TestHash(t *testing.T) {
	t.Parallel()
	// Parity with SkillOpt's test_scoring.py: 16 hex chars, deterministic,
	// distinct inputs differ, unicode/multiline/empty all produce valid hashes.
	tests := []struct {
		name string
		in   string
	}{
		{name: "empty", in: ""},
		{name: "ascii", in: "hello"},
		{name: "unicode", in: "café日本"},
		{name: "multiline", in: "line1\nline2\nline3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := skill.Hash(tt.in)
			if !hexOnly.MatchString(got) {
				t.Errorf("Hash(%q) = %q, want 16 lowercase hex chars", tt.in, got)
			}
			if got != skill.Hash(tt.in) {
				t.Errorf("Hash(%q) is not deterministic", tt.in)
			}
		})
	}
	if skill.Hash("hello") == skill.Hash("world") {
		t.Error("distinct inputs produced the same hash")
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		raw     string
		wantN   string
		wantD   string
		wantBod string
	}{
		{
			name:    "frontmatter split",
			raw:     "---\nname: my-skill\ndescription: does x, use when y\n---\n# Body\ntext here",
			wantN:   "my-skill",
			wantD:   "does x, use when y",
			wantBod: "# Body\ntext here",
		},
		{
			name:    "quoted description",
			raw:     "---\nname: q\ndescription: \"quoted value\"\n---\nbody",
			wantN:   "q",
			wantD:   "quoted value",
			wantBod: "body",
		},
		{
			name:    "no frontmatter",
			raw:     "# Just markdown\nno yaml",
			wantN:   "",
			wantD:   "",
			wantBod: "# Just markdown\nno yaml",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "SKILL.md"), tt.raw)
			s, err := skill.Load(dir)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			assertLoaded(t, s, tt.wantN, tt.wantD, tt.wantBod, len(tt.raw))
		})
	}
}

func TestLoadMissingFileErrors(t *testing.T) {
	t.Parallel()
	if _, err := skill.Load(t.TempDir()); err == nil {
		t.Fatal("Load of a dir without SKILL.md should return an error")
	}
}

func TestDiscover(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	writeFile(
		t,
		filepath.Join(base, ".claude", "skills", "alpha", "SKILL.md"),
		"---\nname: alpha\n---\n",
	)
	writeFile(
		t,
		filepath.Join(base, ".cursor", "skills", "beta", "SKILL.md"),
		"---\nname: beta\n---\n",
	)
	// A directory without a SKILL.md must be skipped.
	if err := os.MkdirAll(filepath.Join(base, ".claude", "skills", "empty"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// .codex/skills is absent — a missing root must be skipped, not error.
	roots := []string{".claude/skills", ".cursor/skills", ".codex/skills"}
	got, err := skill.Discover(base, roots)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Discover found %d dirs, want 2: %v", len(got), got)
	}
	for _, d := range got {
		if _, statErr := os.Stat(filepath.Join(d, "SKILL.md")); statErr != nil {
			t.Errorf("discovered dir %q has no SKILL.md", d)
		}
	}
}

func TestDiscoverRootsMissingRootsIsEmpty(t *testing.T) {
	t.Parallel()
	// Roots that do not exist under the cwd yield an empty result, not an error.
	got, err := skill.DiscoverRoots("does-not-exist-xyz, also-missing")
	if err != nil {
		t.Fatalf("DiscoverRoots: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no dirs for missing roots, got %v", got)
	}
}

func TestLoadBlockScalar(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		raw      string
		wantDesc string
	}{
		{
			name:     "folded scalar",
			raw:      "---\nname: s\ndescription: >\n  line one\n  line two\n---\nbody",
			wantDesc: "line one line two",
		},
		{
			name:     "literal scalar",
			raw:      "---\nname: s\ndescription: |\n  line one\n  line two\n---\nbody",
			wantDesc: "line one\nline two",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "SKILL.md"), tt.raw)
			s, err := skill.Load(dir)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if s.Description != tt.wantDesc {
				t.Errorf("Description = %q, want %q", s.Description, tt.wantDesc)
			}
		})
	}
}

func TestSlug(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "already kebab", in: "my-skill", want: "my-skill"},
		{name: "spaces and case", in: "My Cool Skill", want: "my-cool-skill"},
		{name: "punct collapses", in: "foo_bar.baz!", want: "foo-bar-baz"},
		{name: "trim and collapse", in: "--a  b--", want: "a-b"},
		{name: "empty becomes skill", in: "", want: "skill"},
		{name: "non-ascii separates", in: "café", want: "caf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := skill.Slug(tt.in)
			if got != tt.want {
				t.Errorf("Slug(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if again := skill.Slug(got); again != got {
				t.Errorf("Slug not idempotent: Slug(%q) = %q", got, again)
			}
		})
	}
	if got := skill.Slug(strings.Repeat("a", 100)); len([]rune(got)) != 64 {
		t.Errorf("Slug cap = %d runes, want 64", len([]rune(got)))
	}
}

func assertLoaded(
	t *testing.T,
	s *skill.Skill,
	wantName, wantDesc, wantBody string,
	wantBytes int,
) {
	t.Helper()
	if s.Name != wantName {
		t.Errorf("Name = %q, want %q", s.Name, wantName)
	}
	if s.Description != wantDesc {
		t.Errorf("Description = %q, want %q", s.Description, wantDesc)
	}
	if s.Body != wantBody {
		t.Errorf("Body = %q, want %q", s.Body, wantBody)
	}
	if s.Bytes != wantBytes {
		t.Errorf("Bytes = %d, want %d", s.Bytes, wantBytes)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
