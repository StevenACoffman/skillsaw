package cmd_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenACoffman/skillsaw/cmd/root"
)

// writeRepo builds a repository whose skills sit at the given relative paths, each
// declaring the name in its frontmatter and carrying the given body.
func writeRepo(t *testing.T, name string, skills map[string]string) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), name)
	for rel, body := range skills {
		dir := filepath.Join(repo, rel)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		declared := filepath.Base(rel)
		doc := fmt.Sprintf(
			"---\nname: %s\ndescription: Use when the demo thing is needed.\n---\n\n%s\n",
			declared,
			body,
		)
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return repo
}

// TestPortableFindsWhatStoppedIdentifyingOneThing is the command end to end, over the two
// shapes that matter and the several that must stay quiet.
func TestPortableFindsWhatStoppedIdentifyingOneThing(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		a, b     map[string]string
		wantExit bool
		wantIn   string
		wantOut  string
	}{
		"a portable skill that drifted": {
			map[string]string{"curated/shared": "one"},
			map[string]string{"skills/shared": "two"},
			true, "DIVERGED", "",
		},
		"a portable skill that agrees": {
			map[string]string{"curated/shared": "same"},
			map[string]string{"skills/shared": "same"},
			false, "every name identifies one artifact", "DIVERGED",
		},
		"skills found at different depths": {
			map[string]string{"a/b/c/deep": "same"},
			map[string]string{"deep": "same"},
			false, "2 skill(s)", "DIVERGED",
		},
		"an embedded copy of the repository's own skill": {
			map[string]string{"skills/thing": "same", "internal/assets/skills/thing": "same"},
			map[string]string{"other": "x"},
			false, "every name identifies one artifact", "COLLISION",
		},
		"two things in one repository claiming one name": {
			map[string]string{"skills/thing": "one", "extra/thing": "two"},
			map[string]string{"other": "x"},
			true, "COLLISION", "",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			a := writeRepo(t, "alpha", tc.a)
			b := writeRepo(t, "beta", tc.b)
			out, err := run(t, "portable", a, b)
			var exit root.ExitError
			switch {
			case tc.wantExit && !errors.As(err, &exit):
				t.Fatalf("a finding did not gate: %v\n%s", err, out)
			case !tc.wantExit && err != nil:
				t.Fatalf("a clean pair was rejected: %v\n%s", err, out)
			}
			if !strings.Contains(out, tc.wantIn) {
				t.Errorf("output does not say %q:\n%s", tc.wantIn, out)
			}
			if tc.wantOut != "" && strings.Contains(out, tc.wantOut) {
				t.Errorf("output says %q and should not:\n%s", tc.wantOut, out)
			}
		})
	}
}

// TestARepoSpecificNameIsExempt covers the convention the check exists to enforce: a name
// carrying its own repository's prefix is allowed to differ, because it never claimed to
// be portable.
func TestARepoSpecificNameIsExempt(t *testing.T) {
	t.Parallel()
	a := writeRepo(t, "alpha", map[string]string{"skills/alpha-local": "one"})
	b := writeRepo(t, "beta", map[string]string{"skills/alpha-local": "two"})
	out, err := run(t, "portable", a, b)
	if err != nil {
		t.Fatalf("a prefixed name gated: %v\n%s", err, out)
	}
}

// TestOneRepositoryIsNotAPass is the third state. Checking portability against nothing is
// not evidence that a name is portable, and a run that printed "ok" for it would be making
// a claim about repositories nobody looked at.
func TestOneRepositoryIsNotAPass(t *testing.T) {
	t.Parallel()
	a := writeRepo(t, "alpha", map[string]string{"skills/solo": "one"})
	out, err := run(t, "portable", a)
	if err != nil {
		t.Fatalf("a single repository gated: %v\n%s", err, out)
	}
	if !strings.Contains(out, "nothing to compare against") {
		t.Errorf("a one-repository run did not say it compared nothing:\n%s", out)
	}
}

// TestPortableNeedsAnArgument pins the no-defaults rule: which checkouts constitute
// "everywhere" is a fact about one machine.
func TestPortableNeedsAnArgument(t *testing.T) {
	t.Parallel()
	out, err := run(t, "portable")
	if err == nil {
		t.Fatalf("ran with no repositories:\n%s", out)
	}
	if !strings.Contains(err.Error(), "no defaults") {
		t.Errorf("error does not say why: %v", err)
	}
}
