package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/ohnotnow/agent-issue-tracker/internal/ait"
)

func main() {
	ctx := context.Background()

	dbPath, args, err := extractDBFlag(os.Args[1:])
	if err != nil {
		ait.ExitWithError(ait.NormalizeError(err))
	}

	if len(args) == 0 {
		ait.PrintHelp()
		return
	}

	// Handle --help and --version as aliases for help/version commands.
	if args[0] == "--help" {
		args[0] = "help"
	} else if args[0] == "--version" {
		args[0] = "version"
	}

	cmd, ok := ait.LookupCommand(args[0])
	if !ok {
		handleExit(ait.UnknownCommandError(args[0]))
	}
	if !cmd.NeedsDB {
		if err := cmd.Run(nil, ctx, args[1:]); err != nil {
			handleExit(err)
		}
		return
	}

	// Every DB-backed command except init refuses to run until the database
	// exists — only an explicit `ait init` creates it.
	if cmd.Name != "init" {
		if err := ait.RequireInitialised(dbPath); err != nil {
			handleExit(err)
		}
	}

	app, err := ait.Open(ctx, dbPath)
	if err != nil {
		handleExit(err)
	}
	defer app.Close()

	if err := app.Run(ctx, args); err != nil {
		handleExit(err)
	}
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
