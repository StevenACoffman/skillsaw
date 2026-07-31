// Package cmd is the dispatcher for the skillsaw CLI.
// It registers all commands and routes incoming arguments
// to the matching command implementation.
package cmd

// climax:name skillsaw
// climax:root-pkg root
// climax:env-prefix SKILLSAW

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"

	"github.com/StevenACoffman/skillsaw/cmd/activation"
	"github.com/StevenACoffman/skillsaw/cmd/diagnose"
	"github.com/StevenACoffman/skillsaw/cmd/eval"
	"github.com/StevenACoffman/skillsaw/cmd/gate"
	"github.com/StevenACoffman/skillsaw/cmd/hash"
	"github.com/StevenACoffman/skillsaw/cmd/history"
	"github.com/StevenACoffman/skillsaw/cmd/judge"
	"github.com/StevenACoffman/skillsaw/cmd/root"
	"github.com/StevenACoffman/skillsaw/cmd/scan"
	"github.com/StevenACoffman/skillsaw/cmd/version"
)

// Run parses args and dispatches to the matching command.
// args must not include the executable name (pass os.Args[1:]).
//
// Every flag can be set via a SKILLSAW_-prefixed environment variable.
// The mapping rule is: prepend SKILLSAW_, uppercase, replace dashes with
// underscores.
//
// Flags supplied on the command line always take precedence over env vars.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	r := root.New(stdin, stdout, stderr)
	version.New(r)
	activation.New(r)
	eval.New(r)
	scan.New(r)
	diagnose.New(r)
	hash.New(r)
	gate.New(r)
	history.New(r)
	judge.New(r)
	// register new commands here

	if err := r.Command.Parse(args, ff.WithEnvVarPrefix("SKILLSAW")); err != nil {
		_, _ = fmt.Fprintf(stderr, "\n%s\n", ffhelp.Command(r.Command))
		return fmt.Errorf("parse: %w", err)
	}

	// An unmatched token leaves the selected command a group parent (Exec == nil)
	// with a leftover positional. Without this it would fall through to Run,
	// return ff.ErrNoExec, and exit 0 — indistinguishable from a bare invocation.
	// A bare invocation has no leftover arg and is left to the ErrNoExec path.
	if sel := r.Command.GetSelected(); sel.Exec == nil {
		if rest := sel.Flags.GetArgs(); len(rest) > 0 {
			_, _ = fmt.Fprintf(stderr, "\n%s\n", ffhelp.Command(sel))
			return fmt.Errorf("%s: unknown subcommand %q", sel.Name, rest[0])
		}
	}

	if err := r.Command.Run(ctx); err != nil {
		// Don't print usage help for ErrNoExec (no subcommand given) or
		// ExitError (command already reported its own outcome).
		var exitErr root.ExitError
		if !errors.Is(err, ff.ErrNoExec) && !errors.As(err, &exitErr) {
			_, _ = fmt.Fprintf(stderr, "\n%s\n", ffhelp.Command(r.Command.GetSelected()))
		}
		return err
	}

	return nil
}
