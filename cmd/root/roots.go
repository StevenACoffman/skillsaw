package root

import (
	"fmt"
	"io"
	"strings"

	"github.com/StevenACoffman/skillet/fsutil"
)

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

// ReportSkipped writes one line per discovery entry that could not be judged, prefixed
// with the command name. A skipped entry is never dropped silently: a skill the walk
// could not read is not a skill that passed.
func ReportSkipped(w io.Writer, command string, skipped []fsutil.Skipped) {
	for _, s := range skipped {
		_, _ = fmt.Fprintf(w, "%s: skipped %s: %s\n", command, s.Path, s.Reason)
	}
}
