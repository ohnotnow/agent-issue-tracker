# Agent Issue Tracker

`ait` is a small, local-first issue tracker built primarily for coding agents.  Heavily inspired by Steve Yegge's [beads](https://github.com/steveyegge/beads) project.  Just pruned down to the essentials I need.

It is intended to help an agent turn a plan into structured work, track dependencies, preserve notes between sessions, and quickly answer a practical question: what should I do next?

Repository: `https://github.com/ohnotnow/agent-issue-tracker`

## Status

This project is an early work in progress.

It is being actively built and dogfooded, but it should not be treated as stable, production-ready, or safe for real project tracking yet.

Current limitations include:

- command behaviour may change as the tool is dogfooded
- compatibility guarantees do not exist yet

Schema changes are now managed through a forward-only migration system, so existing databases are upgraded automatically on startup.

## Current Goals

The tool is optimized for agent workflow first:

- create epics and tasks
- model dependencies
- store progress notes
- claim issues to coordinate between multiple agents
- resume work after session loss or conversation compaction
- surface unblocked work via `ready`, ordered by priority

Human-friendly output is intentionally secondary for now. JSON is the default interface.

## Current Command Set

The current binary is `ait`.

Implemented commands:

- `init`
- `config`
- `create`
- `show`
- `list` (`--type`, `--status`, `--priority`, `--parent`, `--all`, `--long`, `--human`, `--tree`)
- `status`
- `search`
- `update`
- `close` (`--cascade`)
- `reopen`
- `cancel`
- `claim`
- `unclaim`
- `ready` (`--type`, `--long`)
- `dep add`
- `dep remove`
- `dep list`
- `dep tree`
- `note add`
- `note list`
- `export` (`--output`)

## Output Modes

By default, `list` and `ready` return a slim view with only the fields an agent typically needs: `id`, `title`, `status`, `type`, and `priority`. This keeps token usage low and makes it easier to reason about results quickly.

Pass `--long` to get the full issue record including `description`, `parent_id`, `claimed_by`, timestamps, and `closed_at`.

For human-friendly output, two display modes are available:

- `--human` — compact tabular view with epics and children grouped, child IDs shown as short suffixes (`.1`, `.2`)
- `--tree` — parent-child hierarchy using tree connectors (`├──`, `└──`), full IDs on every line

These are mutually exclusive and can be combined with the usual filters (`--type`, `--status`, `--priority`).

```bash
ait list                  # slim JSON (5 fields per issue)
ait list --long           # full JSON record
ait list --human          # compact tabular view
ait list --tree           # tree hierarchy view
ait list --human --priority P1  # filtered tabular view
ait ready --type task     # slim, tasks only (excludes epics)
ait ready --long          # full record, all types
```

## Issue Claiming

When multiple agents share one tracker, claiming prevents duplicate work:

```bash
ait claim <id> <agent-name>    # mark an issue as being worked on
ait unclaim <id>               # release the claim
```

If an issue is already claimed by another agent, `claim` returns a conflict error with the current holder's name. Claims are visible in `show` output via `claimed_by` and `claimed_at` fields.

## Cascade Close

By default, `close` only affects the specified issue. To close an epic and all of its descendants in one operation:

```bash
ait close <epic-id> --cascade
```

This recursively closes all open or in-progress children and grandchildren. Issues that are already closed or cancelled are skipped. The command returns the list of newly closed issues.

## Markdown Export

The `export` command produces a Markdown briefing for an issue and all its descendants. This is designed for delegating work to remote or cloud-based agents that don't have access to the `.ait/` database.

```bash
ait export <id>                     # print Markdown to stdout
ait export <id> --output briefing.md  # write to file
```

For an epic, the output includes:

- the epic title, ID, priority, and description
- a `## Tasks` section with checkbox items (`[ ]` open, `[x]` closed, `[-]` cancelled)
- dependencies and notes for each issue
- a `## Summary` with counts

Tasks are ordered by priority (P0 first), matching `ready` behaviour. For a single task with no children, only the header, description, notes, and dependencies are shown.

The resulting file travels with git or rsync, is readable by any agent or human, and requires no reconciliation.

## Ready Prioritisation

The `ready` command surfaces unblocked issues ordered by priority (P0 first, then P1, P2, etc.), with creation order as a tiebreaker within the same priority level. This means the most urgent actionable work appears first.

## Project Configuration

Use `config` to check the current project settings without inspecting the database directly:

```bash
ait config
```

Returns the current `prefix` and `schema_version` as JSON.

## Dependency Cycle Detection

When adding a dependency with `dep add`, the tool performs a transitive reachability check. If the new dependency would create a cycle (e.g. A depends on B, B depends on C, and you try to make C depend on A), the command is rejected with a validation error.

## Initialisation And IDs

Run `ait init --prefix <value>` to set the project prefix used for public issue IDs.

Examples:

- `ait init --prefix ait`
- `ait init --prefix deliveries`

If no prefix has been set yet, the tool will infer one automatically the first time you use it by normalizing the current project directory basename.

Examples:

- a repository directory named `ait` defaults to `ait`
- a repository directory named `dta` defaults to `dta`

The prefix is stored in local project configuration inside the SQLite database. Running `init --prefix ...` later will update the stored prefix and re-key existing public issue IDs to match.

Public issue IDs are hierarchical:

- root issue: `<prefix>-<sqid>`
- first child: `<prefix>-<sqid>.1`
- first grandchild: `<prefix>-<sqid>.1.1`

This makes parent-child structure visible directly in the identifier while keeping the root segment compact and readable.

## Custom Database Path

By default, the database is stored at `.ait/ait.db` in the current git repository root. You can override this with the `--db` flag:

```bash
ait --db /path/to/other.db list
ait --db /path/to/other.db create --title "Task in another DB"
```

This is useful for git worktrees (pointing back to the main repo's database), keeping separate databases for different subsystems, or using `:memory:` for testing.

## Schema Versioning

The database schema is managed through a forward-only migration system. Each migration is numbered and runs in its own transaction. On startup, the tool checks the current schema version and applies any pending migrations automatically.

This means you can update the `ait` binary and your existing database will be upgraded transparently — no manual steps required. The current schema version is visible via `ait config`.

## Local Storage

The tool uses SQLite and creates a local database at `.ait/ait.db` in the current git repository root (or the current directory if no git root is found).

That database stores:

- issues, dependencies, and notes
- project-level configuration such as the current public ID prefix

This keeps issue state close to the codebase it belongs to and makes it easy to inspect or back up.

## Claude Code Skill

The repository includes a `SKILL.md` file — a [Claude Code skill](https://docs.anthropic.com/en/docs/claude-code) that teaches Claude how to use `ait` effectively.

To use it, copy `SKILL.md` into your Claude skills directory:

```bash
# For a single project (from the project root):
mkdir -p .claude/skills && cp SKILL.md .claude/skills/ait/SKILL.md

# Or globally (available in all projects):
mkdir -p ~/.claude/skills/ait && cp SKILL.md ~/.claude/skills/ait/SKILL.md
```

Once installed, Claude Code will know the full command set, workflow patterns, and best practices for working with `ait`.

## Development

Current development priorities are tracked in the tool itself.

To run the test suite:

```bash
GOCACHE=$(pwd)/.gocache go test ./...
```

To build the binary:

```bash
GOCACHE=$(pwd)/.gocache go build -o ait .
```

## Warning

Do not rely on this for important or long-lived work yet.

The current focus is to validate the workflow by using the tool on itself, tighten the schema and command contract, and improve safety before calling it a usable v1.
