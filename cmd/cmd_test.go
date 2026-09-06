package cmd_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/skillsaw/cmd"
	"github.com/StevenACoffman/skillsaw/cmd/root"
)

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := cmd.Run(context.Background(), args, strings.NewReader(""), &out, &out)
	return out.String(), err
}

func TestUnknownSubcommandIsAnError(t *testing.T) {
	t.Parallel()
	out, err := run(t, "definitely-not-a-command")
	if err == nil {
		t.Fatal("expected an error for an unknown subcommand")
	}
	if errors.Is(err, ff.ErrNoExec) {
		t.Errorf("unknown subcommand must not be ErrNoExec (that path exits 0): %v", err)
	}
	if !strings.Contains(err.Error(), "unknown subcommand") {
		t.Errorf("error should name the problem, got %v", err)
	}
	if !strings.Contains(out, "unknown subcommand") && !strings.Contains(out, "SUBCOMMANDS") {
		t.Errorf("expected usage/help on stderr, got:\n%s", out)
	}
}

func TestBareInvocationStaysErrNoExec(t *testing.T) {
	t.Parallel()
	// No args is a genuine bare invocation: ErrNoExec, which main.go maps to
	// exit 0. It must not be treated as an unknown subcommand.
	_, err := run(t)
	if !errors.Is(err, ff.ErrNoExec) {
		t.Fatalf("bare invocation should return ff.ErrNoExec, got %v", err)
	}
}

// TestUsageIsPrintedForMisuseAndNothingElse pins the invariant rather than an example of
// it: usage appears if and only if the error carries root.UsageError.
//
// The rule this replaces was a list of exceptions — not ErrNoExec, not ExitError — so
// every error added after it was written defaulted to printing usage. That is how a
// producer gap came to be reported behind thirty-five lines of flag list, with the line
// naming the repair scrolled off the top. A test over one case would not have caught it;
// what has to hold is the correspondence.
//
// The runtime rows are the load-bearing half. Each is a correct invocation that fails on
// what it found, and each was printing usage before this change.
func TestUsageIsPrintedForMisuseAndNothingElse(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "absent.json")
	cases := map[string]struct {
		args  []string
		usage bool
	}{
		"a missing required flag":    {[]string{"changed", "--tree", "."}, true},
		"an unknown flag":            {[]string{"orphans", "--nope"}, true},
		"an unknown subcommand":      {[]string{"definitely-not-a-command"}, true},
		"a flag after the arguments": {[]string{"hash", ".", "--json"}, true},
		"an invalid flag value": {
			[]string{"gate", "--candidate", "1", "--current", "x"},
			true,
		},
		"an unreadable manifest":       {[]string{"changed", "--manifest", missing}, false},
		"an unreadable judgments file": {[]string{"calibrate", missing}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out, err := run(t, tc.args...)
			if err == nil {
				t.Fatalf("expected an error:\n%s", out)
			}
			var usageErr root.UsageError
			if got := errors.As(err, &usageErr); got != tc.usage {
				t.Errorf("errors.As(UsageError) = %v, want %v: %v", got, tc.usage, err)
			}
			// "USAGE" heads every rendered help block and appears in no error message.
			// Not "FLAGS": the root command declares none, so its help omits that
			// section — which is why the unknown-subcommand row failed a probe that
			// looked plausible and described only the subcommands.
			if got := strings.Contains(out, "USAGE"); got != tc.usage {
				t.Errorf("usage printed = %v, want %v:\n%s", got, tc.usage, out)
			}
		})
	}
}

func TestKnownSubcommandRuns(t *testing.T) {
	t.Parallel()
	out, err := run(t, "version")
	if err != nil {
		t.Fatalf("version should succeed, got %v", err)
	}
	if !strings.Contains(out, "GitVersion") {
		t.Errorf("version output unexpected:\n%s", out)
	}
}
