package main

import (
	"context"
	"fmt"
	"os"

	"agent-issue-tracker/internal/ait"
)

func main() {
	ctx := context.Background()

	dbPath, args, err := extractDBFlag(os.Args[1:])
	if err != nil {
		ait.ExitWithError(ait.NormalizeError(err))
	}

	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		ait.PrintHelp()
		return
	}

	if args[0] == "version" || args[0] == "--version" {
		if err := ait.RunVersion(); err != nil {
			ait.ExitWithError(ait.NormalizeError(err))
		}
		return
	}

	app, err := ait.Open(ctx, dbPath)
	if err != nil {
		ait.ExitWithError(ait.NormalizeError(err))
	}
	defer app.Close()

	if err := app.Run(ctx, args); err != nil {
		ait.ExitWithError(ait.NormalizeError(err))
	}
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
