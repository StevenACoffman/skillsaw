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
	"github.com/StevenACoffman/skillsaw/cmd/calibrate"
	"github.com/StevenACoffman/skillsaw/cmd/changed"
	"github.com/StevenACoffman/skillsaw/cmd/checks"
	"github.com/StevenACoffman/skillsaw/cmd/diagnose"
	"github.com/StevenACoffman/skillsaw/cmd/eval"
	"github.com/StevenACoffman/skillsaw/cmd/gate"
	"github.com/StevenACoffman/skillsaw/cmd/hash"
	"github.com/StevenACoffman/skillsaw/cmd/history"
	"github.com/StevenACoffman/skillsaw/cmd/judge"
	skillsawlog "github.com/StevenACoffman/skillsaw/cmd/log"
	"github.com/StevenACoffman/skillsaw/cmd/ordering"
	"github.com/StevenACoffman/skillsaw/cmd/orphans"
	"github.com/StevenACoffman/skillsaw/cmd/portable"
	"github.com/StevenACoffman/skillsaw/cmd/preflight"
	"github.com/StevenACoffman/skillsaw/cmd/regression"
	"github.com/StevenACoffman/skillsaw/cmd/root"
	"github.com/StevenACoffman/skillsaw/cmd/scan"
	"github.com/StevenACoffman/skillsaw/cmd/scores"
	"github.com/StevenACoffman/skillsaw/cmd/verified"
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
	ordering.New(r)
	orphans.New(r)
	portable.New(r)
	regression.New(r)
	judge.New(r)
	preflight.New(r)
	calibrate.New(r)
	verified.New(r)
	changed.New(r)
	checks.New(r)
	skillsawlog.New(r)
	scores.New(r)
	// register new commands here

	if err := r.Command.Parse(args, ff.WithEnvVarPrefix("SKILLSAW")); err != nil {
		return usage(stderr, r.Command, root.Usagef("parse: %w", err))
	}

	// An unmatched token leaves the selected command a group parent (Exec == nil)
	// with a leftover positional. Without this it would fall through to Run,
	// return ff.ErrNoExec, and exit 0 — indistinguishable from a bare invocation.
	// A bare invocation has no leftover arg and is left to the ErrNoExec path.
	if sel := r.Command.GetSelected(); sel.Exec == nil {
		if rest := sel.Flags.GetArgs(); len(rest) > 0 {
			return usage(stderr, sel, root.Usagef("%s: unknown subcommand %q", sel.Name, rest[0]))
		}
	}

	if err := r.Command.Run(ctx); err != nil {
		// Usage is printed for a misuse of the command line and nothing else. A runtime
		// failure -- an unreadable file, a failed write, a data file whose contents are
		// wrong -- was invoked correctly, so a flag list is thirty-five lines of noise
		// scrolling the one line that matters off the top. That mattered most where a
		// message named a repair: the reader had to page back up to find it.
		//
		// The predicate is the error's type rather than a list of exceptions. The list it
		// replaces named ErrNoExec and ExitError, so every error added since defaulted to
		// printing usage -- the wrong default, silently applied.
		var usageErr root.UsageError
		if errors.As(err, &usageErr) {
			return usage(stderr, r.Command.GetSelected(), err)
		}
		return err
	}

	return nil
}

// usage prints cmd's help ahead of err and returns err unchanged.
//
// One function knows how usage is rendered and to which writer. The three call sites
// each know only that they are reporting a misuse, which is the half that differs.
func usage(w io.Writer, cmd *ff.Command, err error) error {
	_, _ = fmt.Fprintf(w, "\n%s\n", ffhelp.Command(cmd))
	return err
}
