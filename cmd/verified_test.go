package cmd_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenACoffman/skillsaw/cmd/root"
)

// The manifest fixtures these use (makeTree, baseManifest) live in changed_test.go;
// both commands read the same document, so one set of fixtures serves both.

func TestVerifiedGate(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{"verified tree passes": true, "unverified tree fails": false}
	for name, verified := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := baseManifest(t, makeTree(t, "alpha"), verified)
			out, err := run(t, "verified", m)
			assertGate(t, verified, out, err)
			// Either way it must name what it gated, not just the verdict.
			for _, want := range []string{"tool=exegesis", "skills=1", "structure_verified"} {
				if !strings.Contains(out, want) {
					t.Errorf("output does not report %q:\n%s", want, out)
				}
			}
		})
	}
}

// assertGate checks the exit behaviour for a verified or unverified manifest.
func assertGate(t *testing.T, verified bool, out string, err error) {
	t.Helper()
	if verified {
		if err != nil {
			t.Fatalf("a verified tree was rejected: %v\n%s", err, out)
		}
		return
	}
	if err == nil {
		t.Fatalf("an unverified tree passed the gate:\n%s", out)
	}
	var exit root.ExitError
	if !errors.As(err, &exit) {
		t.Errorf("want an ExitError, got %T", err)
	}
}

func TestVerifiedRejectsAFileThatIsNotAManifest(t *testing.T) {
	t.Parallel()
	// Reporting this as "unverified" would send the reader hunting for structural
	// defects in a tree that was never examined.
	bad := filepath.Join(t.TempDir(), "other.json")
	if err := os.WriteFile(bad, []byte(`{"name":"something-else"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "verified", bad)
	if err == nil {
		t.Fatalf("accepted a document that is not a manifest:\n%s", out)
	}
	var exit root.ExitError
	if errors.As(err, &exit) {
		t.Error("a malformed input should be an error, not a failed gate")
	}
}
