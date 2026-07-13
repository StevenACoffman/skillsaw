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

// MisplacedFlag returns the first positional argument that looks like a flag
// (starts with "-"), or "" if there is none. ff/v4 stops flag parsing at the
// first positional, so a flag placed after arguments (e.g. "eval <dir> --json")
// is silently swallowed as a positional; commands that take positional paths use
// this to fail loudly. It returns the offending token so the caller builds the
// error in its own package with a command-specific prefix.
func MisplacedFlag(args []string) string {
	for _, a := range args {
		if len(a) > 1 && a[0] == '-' {
			return a
		}
	}
	return ""
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
		ShortHelp: "TODO: describe skillsaw here",
	}
	return &cfg
}
