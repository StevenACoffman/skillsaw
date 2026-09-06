// Package root defines the root configuration for the CLI.
package root

import (
	"fmt"
	"io"

	"github.com/peterbourgon/ff/v4"
)

// ExitError is returned by commands that want a specific non-zero exit code
// without printing an additional error message. run() in main.go checks for
// ExitError with errors.As and calls os.Exit(int(e)) directly, bypassing the
// default "error: ..." printer.
type ExitError int

// UsageError marks an error as a misuse of the command line — a wrong argument
// count, a missing required flag, an invalid flag value — for which printing the
// command's usage is the helpful response. The dispatcher prints usage for these
// and only these: after a runtime failure (an unreadable file, a failed write, a
// data file whose contents are wrong) the invocation itself was correct, so a flag
// list is noise in front of the line that matters.
//
// Wrapping preserves the mark, so a command that already wraps an inner error needs
// no change: errors.As finds a UsageError through fmt.Errorf("cmd: %w", err). Mark
// the error where the knowledge is — the function that knows a flag value is invalid
// — rather than at the call site that wraps it.
type UsageError struct{ Err error }

// Config holds shared I/O writers and the root ff.Command.
// All subcommand configs embed *Config to inherit these.
type Config struct {
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Flags   *ff.FlagSet
	Command *ff.Command
}

func (e ExitError) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

// Error reports the wrapped message. The zero UsageError is constructible by any
// caller, so it describes itself rather than panicking on a nil Err — a panic while
// reporting a failure would bury the failure it was reporting.
func (e UsageError) Error() string {
	if e.Err == nil {
		return "usage error"
	}
	return e.Err.Error()
}

// Unwrap exposes the underlying error so errors.Is/As reach past the marker.
func (e UsageError) Unwrap() error { return e.Err }

// Usagef returns a UsageError whose message is formatted as fmt.Errorf does, so a
// %w verb still wraps an underlying error.
//
// It returns the concrete UsageError rather than error so that callers returning it
// are not reported by wrapcheck: there is no external error here to wrap, since this
// is the constructor of the error itself.
func Usagef(format string, args ...any) UsageError {
	return UsageError{Err: fmt.Errorf(format, args...)}
}

// MisplacedFlag reports a positional argument that looks like a flag (starts with
// "-"), as a UsageError naming the command.
//
// ff/v4 stops flag parsing at the first positional, so a flag placed after arguments
// (e.g. "eval <dir> --json") is silently swallowed as a positional; commands that
// take positional paths call this to fail loudly.
//
// It returns the finished error rather than the offending token. Fifteen callers
// spelled the same sentence fifteen times, which is one wording that can drift in
// fifteen places and — since the dispatcher now decides on the error's type — fifteen
// places that could each forget the mark. The command-specific prefix is the only
// thing that varied, so it is the only thing a caller passes.
//
// It returns the concrete UsageError with a comma-ok bool rather than a nil error,
// for the reason Usagef documents: a caller returning an error-typed value from
// another package is reported by wrapcheck, and there is nothing here to wrap.
//
// Requires: name is the command's name, as it appears in its messages.
// Ensures:  pure. ok is false and the UsageError is the zero value when no argument
// looks like a flag.
func MisplacedFlag(name string, args []string) (UsageError, bool) {
	for _, a := range args {
		if len(a) > 1 && a[0] == '-' {
			return Usagef(
				"%s: %q looks like a flag after arguments; put flags before positional arguments",
				name, a), true
		}
	}
	return UsageError{}, false
}

// New returns a new root Config with the given I/O writers.
func New(stdin io.Reader, stdout, stderr io.Writer) *Config {
	var cfg Config
	cfg.Stdin = stdin
	cfg.Stdout = stdout
	cfg.Stderr = stderr
	// No shared flags — cfg.Flags is nil; ff provides --help automatically.
	// Subcommands call SetParent(parent.Flags)
	// which is a no-op here; add shared flags (e.g. BoolVar) to activate.
	// To add shared flags, uncomment and bind before constructing the command:
	// cfg.Flags = ff.NewFlagSet("skillsaw")
	// cfg.Flags.BoolVar(&cfg.MyFlag, 0, "my-flag", "", "description")
	cfg.Command = &ff.Command{
		Name:      "skillsaw",
		Usage:     "skillsaw <SUBCOMMAND> ...",
		ShortHelp: "deterministically score, diagnose, and validate Agent Skills",
		// No hand-written command list here: ff renders SUBCOMMANDS from the registered
		// commands, and the copy that used to sit in this text had already drifted -- it
		// omitted preflight, calibrate, verified, changed, log and scores. One list that
		// cannot go stale beats two that can. (Same fix as exegesis PR #16.)
		LongHelp: `skillsaw is the deterministic core of the darwin-skill optimizer: it
scores, diagnoses, and validates Agent Skills, reserving a model only for the
irreducible judge-only rubric dimensions. See SUBCOMMANDS below, and
"skillsaw <SUBCOMMAND> -h" for what each one does.`,
	}
	return &cfg
}
