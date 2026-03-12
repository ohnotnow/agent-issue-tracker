package ait

import "context"

func registerSubcommandHelp() map[string]string {
	return map[string]string{
		"dep add": `Usage: ait dep add <blocked-id> <blocker-id>

Add a dependency: <blocked-id> is blocked by <blocker-id>.

Examples:
  ait dep add PROJ-2 PROJ-1
`,
		"dep remove": `Usage: ait dep remove <blocked-id> <blocker-id>

Remove a dependency between two issues.

Examples:
  ait dep remove PROJ-2 PROJ-1
`,
		"dep list": `Usage: ait dep list <id>

List all blockers and blocks for an issue.

Examples:
  ait dep list PROJ-2
`,
		"dep tree": `Usage: ait dep tree <id>

Show the full dependency tree for an issue.

Examples:
  ait dep tree PROJ-2
`,
		"note add": `Usage: ait note add <id> <body>

Add a note to an issue.

Examples:
  ait note add PROJ-1 "Completed the first pass"
`,
		"note list": `Usage: ait note list <id>

List all notes for an issue.

Examples:
  ait note list PROJ-1
`,
	}
}

func registerCommands() []Command {
	return []Command{
		{
			Name:    "init",
			Summary: "Set project prefix for issue IDs",
			Help: `Usage: ait init [--prefix <value>]

Set or auto-generate the project prefix used for hierarchical issue IDs.

Flags:
  --prefix <value>   Set the prefix explicitly (e.g. "myproject")

Examples:
  ait init --prefix myapp
  ait init
`,
			Flags:   []string{"--prefix"},
			NeedsDB: true,
			Run: func(a *App, ctx context.Context, args []string) error {
				return a.runInit(ctx, args)
			},
		},
		{
			Name:    "config",
			Summary: "Show project configuration",
			Help: `Usage: ait config

Show the current project configuration (prefix, schema version).
`,
			NeedsDB: true,
			Run: func(a *App, ctx context.Context, args []string) error {
				return a.runConfig(ctx)
			},
		},
		{
			Name:    "create",
			Summary: "Create a new issue",
			Help: `Usage: ait create --title <t> [flags]

Create a new issue (task or epic).

Flags:
  --title <text>         Issue title (required)
  --description <text>   Issue description
  --type <task|epic>     Issue type (default: task)
  --parent <id>          Parent issue ID (tasks only)
  --priority <P0-P4>     Priority level (default: P2)

Examples:
  ait create --title "Add login page"
  ait create --title "Auth Epic" --type epic --priority P1
  ait create --title "OAuth flow" --parent PROJ-1
`,
			Flags:   []string{"--title", "--description", "--type", "--parent", "--priority"},
			NeedsDB: true,
			Run: func(a *App, ctx context.Context, args []string) error {
				return a.runCreate(ctx, args)
			},
		},
		{
			Name:    "show",
			Summary: "Show issue details and notes",
			Args:    "<id>",
			Help: `Usage: ait show <id>

Show full details for an issue, including children, dependencies, and notes.

Examples:
  ait show PROJ-1
  ait show PROJ-1.2
`,
			NeedsDB: true,
			Run: func(a *App, ctx context.Context, args []string) error {
				return a.runShow(ctx, args)
			},
		},
		{
			Name:    "list",
			Summary: "List issues",
			Help: `Usage: ait list [flags]

List issues with optional filters and output formats.

Flags:
  --all                Include closed and cancelled issues
  --long               Full JSON output (all fields)
  --human              Human-readable table output
  --tree               Tree view showing parent/child hierarchy
  --status <status>    Filter by status (open, in_progress, closed, cancelled)
  --type <type>        Filter by type (task, epic)
  --priority <P0-P4>   Filter by priority
  --parent <id>        Filter by parent issue

Examples:
  ait list
  ait list --status open --type task
  ait list --all --long
  ait list --tree
`,
			Flags:   []string{"--all", "--long", "--human", "--tree", "--status", "--type", "--priority", "--parent"},
			NeedsDB: true,
			Run: func(a *App, ctx context.Context, args []string) error {
				return a.runList(ctx, args)
			},
		},
		{
			Name:    "search",
			Summary: "Search issues by text",
			Args:    "<query>",
			Help: `Usage: ait search <query>

Search issues by title or description (case-insensitive).

Examples:
  ait search "auth"
  ait search "database migration"
`,
			NeedsDB: true,
			Run: func(a *App, ctx context.Context, args []string) error {
				return a.runSearch(ctx, args)
			},
		},
		{
			Name:    "status",
			Summary: "Show project summary counts",
			Help: `Usage: ait status

Show a summary of issue counts by status, plus the number of ready issues.
`,
			NeedsDB: true,
			Run: func(a *App, ctx context.Context, args []string) error {
				return a.runStatus(ctx)
			},
		},
		{
			Name:    "ready",
			Summary: "List unblocked issues",
			Help: `Usage: ait ready [flags]

List issues that are unblocked (all dependencies closed).

Flags:
  --long          Full JSON output
  --type <type>   Filter by type (task, epic)

Examples:
  ait ready
  ait ready --type task --long
`,
			Flags:   []string{"--long", "--type"},
			NeedsDB: true,
			Run: func(a *App, ctx context.Context, args []string) error {
				return a.runReady(ctx, args)
			},
		},
		{
			Name:    "update",
			Summary: "Update an issue",
			Args:    "<id>",
			Help: `Usage: ait update <id> [flags]

Update fields on an existing issue.

Flags:
  --title <text>         New title
  --description <text>   New description
  --status <status>      New status (open, in_progress, closed, cancelled)
  --priority <P0-P4>     New priority

Examples:
  ait update PROJ-1 --title "Renamed issue"
  ait update PROJ-1 --status in_progress --priority P0
`,
			Flags:   []string{"--title", "--description", "--status", "--priority", "--parent"},
			NeedsDB: true,
			Run: func(a *App, ctx context.Context, args []string) error {
				return a.runUpdate(ctx, args)
			},
		},
		{
			Name:    "close",
			Summary: "Close an issue (or subtree)",
			Args:    "<id>",
			Help: `Usage: ait close <id> [--cascade]

Close an issue. With --cascade, close the entire subtree.

Flags:
  --cascade   Also close all descendant issues

Examples:
  ait close PROJ-1
  ait close PROJ-1 --cascade
`,
			Flags:   []string{"--cascade"},
			NeedsDB: true,
			Run: func(a *App, ctx context.Context, args []string) error {
				return a.runClose(ctx, args)
			},
		},
		{
			Name:    "reopen",
			Summary: "Reopen a closed/cancelled issue",
			Args:    "<id>",
			Help: `Usage: ait reopen <id>

Reopen a closed or cancelled issue (sets status back to open).

Examples:
  ait reopen PROJ-1
`,
			NeedsDB: true,
			Run: func(a *App, ctx context.Context, args []string) error {
				return a.runReopen(ctx, args)
			},
		},
		{
			Name:    "cancel",
			Summary: "Cancel an issue",
			Args:    "<id>",
			Help: `Usage: ait cancel <id>

Cancel an issue.

Examples:
  ait cancel PROJ-1
`,
			NeedsDB: true,
			Run: func(a *App, ctx context.Context, args []string) error {
				return a.runStatusChange(ctx, args, StatusCancelled)
			},
		},
		{
			Name:    "claim",
			Summary: "Claim an issue for an agent",
			Args:    "<id> <agent-name>",
			Help: `Usage: ait claim <id> <agent-name>

Claim an issue for an agent. Fails if already claimed.

Examples:
  ait claim PROJ-1 builder-agent
`,
			NeedsDB: true,
			Run: func(a *App, ctx context.Context, args []string) error {
				return a.runClaim(ctx, args)
			},
		},
		{
			Name:    "unclaim",
			Summary: "Release a claim",
			Args:    "<id>",
			Help: `Usage: ait unclaim <id>

Release a claim on an issue.

Examples:
  ait unclaim PROJ-1
`,
			NeedsDB: true,
			Run: func(a *App, ctx context.Context, args []string) error {
				return a.runUnclaim(ctx, args)
			},
		},
		{
			Name:    "dep",
			Summary: "Manage dependencies",
			Args:    "add|remove|list|tree <id> [<id>]",
			Help: `Usage: ait dep <subcommand> ...

Manage issue dependencies.

Subcommands:
  add     <blocked-id> <blocker-id>   Add a dependency
  remove  <blocked-id> <blocker-id>   Remove a dependency
  list    <id>                         List dependencies for an issue
  tree    <id>                         Show dependency tree

Examples:
  ait dep add PROJ-2 PROJ-1
  ait dep list PROJ-2
  ait dep tree PROJ-2
`,
			NeedsDB: true,
			Run: func(a *App, ctx context.Context, args []string) error {
				return a.runDependency(ctx, args)
			},
		},
		{
			Name:    "note",
			Summary: "Manage notes",
			Args:    "add|list <id> [body]",
			Help: `Usage: ait note <subcommand> ...

Manage issue notes.

Subcommands:
  add   <id> <body>   Add a note to an issue
  list  <id>          List notes for an issue

Examples:
  ait note add PROJ-1 "Started implementation"
  ait note list PROJ-1
`,
			NeedsDB: true,
			Run: func(a *App, ctx context.Context, args []string) error {
				return a.runNote(ctx, args)
			},
		},
		{
			Name:    "export",
			Summary: "Export Markdown briefing",
			Args:    "<id>",
			Help: `Usage: ait export <id> [--output path.md]

Export an issue and its descendants as a Markdown briefing document.

Flags:
  --output <path>   Write to file instead of stdout

Examples:
  ait export PROJ-1
  ait export PROJ-1 --output briefing.md
`,
			Flags:   []string{"--output"},
			NeedsDB: true,
			Run: func(a *App, ctx context.Context, args []string) error {
				return a.runExport(ctx, args)
			},
		},
		{
			Name:    "flush",
			Summary: "Purge closed/cancelled issues",
			Help: `Usage: ait flush [--dry-run]

Purge closed/cancelled root issues whose entire subtree is also terminal.

Flags:
  --dry-run   Show what would be flushed without deleting

Examples:
  ait flush --dry-run
  ait flush
`,
			Flags:   []string{"--dry-run"},
			NeedsDB: true,
			Run: func(a *App, ctx context.Context, args []string) error {
				return a.runFlush(ctx, args)
			},
		},
		{
			Name:    "completion",
			Summary: "Print shell completion script",
			Args:    "bash|zsh",
			Help: `Usage: ait completion <bash|zsh>

Print a shell completion script to stdout. Source it in your shell profile.

Examples:
  eval "$(ait completion bash)"
  ait completion zsh > ~/.zsh/completions/_ait
`,
			NeedsDB: false,
			Run: func(_ *App, _ context.Context, args []string) error {
				if len(args) < 1 {
					PrintCommandHelp("completion")
					return nil
				}
				return RunCompletion(args[0])
			},
		},
		{
			Name:    "version",
			Summary: "Show version and check for updates",
			NeedsDB: false,
			Run: func(_ *App, _ context.Context, _ []string) error {
				return RunVersion()
			},
		},
		{
			Name:    "help",
			Summary: "Show this help",
			NeedsDB: false,
			Run: func(_ *App, _ context.Context, _ []string) error {
				PrintHelp()
				return nil
			},
		},
	}
}
