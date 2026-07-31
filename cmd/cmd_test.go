package cmd_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/skillsaw/cmd"
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
