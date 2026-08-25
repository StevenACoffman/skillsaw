package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unauthorizedClaims are the phrases EVIDENCE.md rules out. Each asserts something about
// the world that nothing measured here supports: this tool scores the form of an artifact,
// and no number it produces says a skill works.
//
// "validated" is deliberately not on the list as a bare word. It has an ordinary and
// correct meaning in this codebase -- a validated constructor, a status validated on write
// -- and matching it produced three false positives on the first run, all of them input
// validation. A check that cries wolf is a check that gets deleted, so the claim sense is
// matched by phrase and the engineering sense is left alone.
func unauthorizedClaims() []string {
	return []string{
		"proven",
		"eval-informed",
		"auto-invoke",
		"validated skill",
		"skill is validated",
		"empirically validated",
	}
}

// TestNoUnauthorizedClaims is the enforcing half of EVIDENCE.md. A list of claims nobody
// may make is a list nobody re-reads; this is what makes it survive.
//
// It reads the repository's own shipped documents, so the rule is checked against what
// would actually be published rather than against an inventory that can fall behind.
func TestNoUnauthorizedClaims(t *testing.T) {
	t.Parallel()
	docs, err := filepath.Glob("../*.md")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("no documents found; this test would pass vacuously")
	}
	for _, doc := range docs {
		name := filepath.Base(doc)
		// EVIDENCE.md names the forbidden words in order to forbid them, and TODO.md
		// quotes the sources these rules came from. Both are about the rule rather than
		// instances of breaking it.
		if name == "EVIDENCE.md" || name == "TODO.md" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertMakesNoUnauthorizedClaim(t, doc)
		})
	}
}

// assertMakesNoUnauthorizedClaim reads one document and reports every forbidden phrase in
// it, rather than stopping at the first: a writer fixing one wants to see the rest.
func assertMakesNoUnauthorizedClaim(t *testing.T, doc string) {
	t.Helper()
	b, err := os.ReadFile(doc)
	if err != nil {
		t.Fatalf("read %s: %v", doc, err)
	}
	body := strings.ToLower(string(b))
	for _, claim := range unauthorizedClaims() {
		if strings.Contains(body, claim) {
			t.Errorf("%s claims %q; nothing measured here supports it. Say what was measured "+
				"instead, or add the exemption to EVIDENCE.md and this list",
				filepath.Base(doc), claim)
		}
	}
}
