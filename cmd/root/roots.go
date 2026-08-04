package root

import "strings"

// SplitRoots splits a comma-separated --roots flag value into trimmed, non-empty
// directory names, for skill.DiscoverRoots. It is the CLI-side counterpart to
// skillet/skill's DiscoverRoots(base, roots), which takes an explicit slice.
func SplitRoots(csv string) []string {
	var roots []string
	for _, r := range strings.Split(csv, ",") {
		if r = strings.TrimSpace(r); r != "" {
			roots = append(roots, r)
		}
	}
	return roots
}
