package neutrality_test

import (
	"testing"

	"github.com/StevenACoffman/skillsaw/internal/neutrality"
)

func TestScanHitsPerAlternation(t *testing.T) {
	t.Parallel()
	// One case per red-light alternation in the source regex (spec §9.1): each
	// must produce at least one hit.
	tests := []struct {
		name    string
		content string
	}{
		{name: "在 Claude Code", content: "本 skill 在 Claude Code 里使用"},
		{name: "Claude Code skill", content: "this is a Claude Code skill"},
		{name: "Claude Code 用户", content: "Claude Code 用户请注意"},
		{name: "Cursor only", content: "Cursor only feature"},
		{name: "Codex 中", content: "在 Codex 中运行"},
		{name: "badge at line start", content: "[![Claude Code](x)](y)"},
		{name: "claude skills path", content: "put it in ~/.claude/skills/foo/"},
		{name: "plugin install", content: "run /plugin install foo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hits := neutrality.Scan([]neutrality.NamedFile{{Name: "SKILL.md", Content: tt.content}})
			if len(hits) == 0 {
				t.Errorf("expected a red-light hit for %q, got none", tt.content)
			}
		})
	}
}

func TestScanClean(t *testing.T) {
	t.Parallel()
	clean := "# My Skill\nWorks in any skills-compatible agent runtime.\nNo red flags here."
	hits := neutrality.Scan([]neutrality.NamedFile{{Name: "SKILL.md", Content: clean}})
	if len(hits) != 0 {
		t.Errorf("expected no hits on clean content, got %v", hits)
	}
}

func TestScanReportsLineAndFile(t *testing.T) {
	t.Parallel()
	content := "line one is fine\n本 skill 在 Claude Code 里\nline three is fine"
	hits := neutrality.Scan([]neutrality.NamedFile{{Name: "README.md", Content: content}})
	if len(hits) != 1 {
		t.Fatalf("expected exactly 1 hit, got %d: %v", len(hits), hits)
	}
	if hits[0].Line != 2 {
		t.Errorf("hit line = %d, want 2", hits[0].Line)
	}
	if hits[0].File != "README.md" {
		t.Errorf("hit file = %q, want README.md", hits[0].File)
	}
}
