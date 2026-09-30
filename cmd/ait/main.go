package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/ohnotnow/agent-issue-tracker/internal/ait"
)

func main() {
	if err := dispatch(context.Background(), os.Args[1:]); err != nil {
		handleExit(err)
	}
}

// dispatch resolves and runs a single CLI invocation. It returns the command
// error instead of exiting so the same path can be driven from tests.
func dispatch(ctx context.Context, argv []string) error {
	dbPath, args, err := extractDBFlag(argv)
	if err != nil {
		return err
	}

	if len(args) == 0 {
		ait.PrintHelp()
		return nil
	}

	// Handle --help and --version as aliases for help/version commands.
	if args[0] == "--help" {
		args[0] = "help"
	} else if args[0] == "--version" {
		args[0] = "version"
	}

	cmd, ok := ait.LookupCommand(args[0])
	if !ok {
		return ait.UnknownCommandError(args[0])
	}

	// Help resolves before any database work: `ait init --help` must not
	// create .ait/ as a side effect, and `ait list --help` should print usage
	// rather than the "uninitialised" error.
	if topic, ok := ait.HelpRequest(cmd, args[1:]); ok {
		ait.PrintCommandHelp(topic)
		return nil
	}

	if !cmd.NeedsDB {
		return cmd.Run(nil, ctx, args[1:])
	}

	// Every DB-backed command except init refuses to run until the database
	// exists — only an explicit `ait init` creates it.
	if cmd.Name != "init" {
		if err := ait.RequireInitialised(dbPath); err != nil {
			return err
		}
	}

	app, err := ait.Open(ctx, dbPath)
	if err != nil {
		return err
	}
	defer app.Close()

	return app.Run(ctx, args)
}

// handleExit translates a command error into stderr output (when there is
// real failure detail) and a shell exit code. Errors carrying a specific
// exit code via ait.ExitWithCode skip the JSON envelope when the wrapped
// cause is nil — used by `self-update --check` to signal "newer version
// available" with exit 1 but no error payload.
func handleExit(err error) {
	if code, ok := ait.ExitCode(err); ok {
		if !ait.SilentExit(err) {
			cause := errors.Unwrap(err)
			if cause == nil {
				cause = err
			}
			ait.WriteError(ait.NormalizeError(cause))
		}
		os.Exit(code)
	}
	ait.ExitWithError(ait.NormalizeError(err))
}

func extractDBFlag(args []string) (string, []string, error) {
	for i := 0; i < len(args); i++ {
		if args[i] == "--db" {
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("--db requires a value")
			}
			dbPath := args[i+1]
			remaining := make([]string, 0, len(args)-2)
			remaining = append(remaining, args[:i]...)
			remaining = append(remaining, args[i+2:]...)
			return dbPath, remaining, nil
		}
	}
	return "", args, nil
}
