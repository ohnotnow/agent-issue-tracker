package ait

import (
	"context"
	"fmt"
	"strings"
)

// Command describes a single CLI subcommand.
type Command struct {
	Name    string
	Summary string   // one-liner for both global and per-command help
	Args    string   // positional args pattern, e.g. "<id>", "<query>", "bash|zsh"
	Help    string   // full per-command help text (hand-written, with examples)
	Flags   []string // flag names — source of truth for completion AND global help
	NeedsDB bool     // false for help, version, completion
	Run     func(a *App, ctx context.Context, args []string) error
}

var commands []Command
var commandsByName map[string]*Command
var subcommandHelp map[string]string

func init() {
	commands = registerCommands()
	commandsByName = make(map[string]*Command, len(commands))
	for i := range commands {
		commandsByName[commands[i].Name] = &commands[i]
	}
	subcommandHelp = registerSubcommandHelp()
}

// LookupCommand finds a command by name.
func LookupCommand(name string) (*Command, bool) {
	cmd, ok := commandsByName[name]
	return cmd, ok
}

// CommandNames returns all command names in registration order.
func CommandNames() []string {
	names := make([]string, len(commands))
	for i, cmd := range commands {
		names[i] = cmd.Name
	}
	return names
}

// CommandFlags returns the flag names for a command, or nil if not found.
func CommandFlags(name string) []string {
	cmd, ok := commandsByName[name]
	if !ok {
		return nil
	}
	return cmd.Flags
}

func usageSummary(cmd Command) string {
	var parts []string
	if cmd.Args != "" {
		parts = append(parts, cmd.Args)
	}
	if len(cmd.Flags) > 0 {
		parts = append(parts, strings.Join(cmd.Flags, " "))
	}
	summary := strings.Join(parts, " ")
	// If the usage column is too wide, collapse flags to [flags]
	if len(summary) > 35 && len(cmd.Flags) > 0 {
		if cmd.Args != "" {
			summary = cmd.Args + " [flags]"
		} else {
			summary = "[flags]"
		}
	}
	return summary
}

func generateHelpText() string {
	var b strings.Builder
	b.WriteString("Usage: ait [--db <path>] <command> [options]\n\nCommands:\n")

	for _, cmd := range commands {
		usage := usageSummary(cmd)
		fmt.Fprintf(&b, "  %-10s %-35s %s\n", cmd.Name, usage, cmd.Summary)
	}

	b.WriteString(`
Global options:
  --db <path>     Use a specific database file (default: .ait/ait.db)
  --version       Show version and check for updates
`)
	return b.String()
}

// PrintHelp prints the global help text generated from the command registry.
func PrintHelp() {
	fmt.Print(generateHelpText())
}

// PrintCommandHelp prints help for a specific command, falling back to global help.
func PrintCommandHelp(cmd string) {
	c, ok := commandsByName[cmd]
	if ok && c.Help != "" {
		fmt.Print(c.Help)
		return
	}
	if text, ok := subcommandHelp[cmd]; ok {
		fmt.Print(text)
		return
	}
	PrintHelp()
}
