// Package neutrality implements the runtime-neutrality gate (darwin spec §9):
// a deterministic red-light scan that flags wording or paths binding a skill to
// a single agent runtime, which causes other agents to refuse to install it.
package neutrality

import (
	"regexp"
	"strings"
)

// redLight is the exact pattern from the darwin source (SKILL.md §"Runtime
// 适配性审查" and references/runtime-neutrality.md). It is applied per line, so
// the "^" anchor matches the start of each line.
var redLight = regexp.MustCompile(
	`(在 Claude Code|Claude Code skill|Claude Code 用户|Cursor only|Codex 中|^\[!\[Claude Code|~/\.claude/skills/[a-z]|/plugin install\b)`,
)

// Hit is one red-light match.
type Hit struct {
	File string `json:"file"`
	Line int    `json:"line"` // 1-indexed
	Text string `json:"text"` // the matching line, trimmed
}

// NamedFile pairs a display name with file contents for Scan.
type NamedFile struct {
	Name    string
	Content string
}

// Scan runs the red-light regex line-by-line over each file. The map key is a
// display name (e.g. "SKILL.md"); the value is the file contents. Order of
// returned hits follows files in the given order, then line number.
//
// This reproduces the source grep verbatim. Exception handling for legitimate
// occurrences (frontmatter trigger words, labeled runtime-specific sections,
// commit messages — spec §9.2) is a downstream classification concern; this
// function reports raw hits so a baseline "runtime_warn=N" is faithful.
func Scan(files []NamedFile) []Hit {
	var hits []Hit
	for _, f := range files {
		lines := strings.Split(strings.ReplaceAll(f.Content, "\r\n", "\n"), "\n")
		for i, line := range lines {
			if redLight.MatchString(line) {
				hits = append(hits, Hit{File: f.Name, Line: i + 1, Text: strings.TrimSpace(line)})
			}
		}
	}
	return hits
}
