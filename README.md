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
- allow delegation of work to sub-agents

Human-friendly output is intentionally secondary for now. JSON is the default interface.

## What it is not 

The tool is not a replacement for a real issue tracker.  The workflow is envisioned as 'developer has a plan/issues/feature - gets the coding agent to plan them out (or does it themselves), then the actual coding agent manages the sub-epics/issues for that work alone.

It's not designed to handle cross-team shared issues, work, projects.  The internal database should be added to .gitignore.

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
- `flush` (`--dry-run`)
- `dep add`
- `dep remove`
- `dep list`
- `dep tree`
- `note add`
- `note list`
- `version`
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

## Flush

The `flush` command permanently deletes all closed and cancelled issues to keep the database lean. Since `ait` tracks ephemeral work, there is no need to keep completed issues around indefinitely.

```bash
ait flush              # delete all terminal issues
ait flush --dry-run    # preview what would be deleted
```

Flush only removes **root-level** issues whose entire descendant tree is also closed or cancelled. If a closed epic still has open or in-progress children, it is skipped and reported in the `skipped` list. Notes and dependencies belonging to flushed issues are removed automatically via cascade delete.

## Markdown Export and Delegation

The `export` command produces a self-contained Markdown briefing for an issue and all its descendants. This supports a lightweight delegation workflow for handing work to sub-agents that don't have access to the `ait` binary, or don't need to know about it at all.

```bash
ait export <id>                       # print Markdown to stdout
ait export <id> --output briefing.md  # write to file
```

For an epic, the output includes the title, ID, priority, description, a task checklist ordered by priority, dependencies, notes, and a summary with counts. The resulting file is also useful as a human-readable report of an epic's current state.

The delegation workflow is straightforward:

1. **Export** an epic as a Markdown briefing
2. **Delegate** the file to a sub-agent — in a worktree, background process, or remote context
3. The sub-agent **works through the checklist** — no tracker needed
4. The supervisor agent **reconciles** the results back into the tracker by closing completed tasks

This keeps the contract in plain Markdown, so it works across context boundaries, and doesn't couple the receiving agent to any tooling. For the full workflow with worked examples, see [claude/skills/ait/DELEGATION.md](claude/skills/ait/DELEGATION.md).

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

## Claude Code Skills and Agents

The `claude/` directory contains pre-written [Claude Code](https://docs.anthropic.com/en/docs/claude-code) skills and agents that teach Claude how to use `ait` effectively:

- **`claude/skills/ait/SKILL.md`** — core command reference, workflow patterns, and best practices
- **`claude/skills/ait/DELEGATION.md`** — guide for delegating work to sub-agents via Markdown export
- **`claude/agents/plan-to-ait.md`** — agent that converts plan-mode plans into structured ait epics and issues

To install, copy the skill and agent directories into your Claude configuration:

```bash
# For a single project (from the project root):
cp -r claude/skills/ait .claude/skills/ait
cp -r claude/agents/plan-to-ait.md .claude/agents/plan-to-ait.md

# Or globally (available in all projects):
cp -r claude/skills/ait ~/.claude/skills/ait
cp -r claude/agents/plan-to-ait.md ~/.claude/agents/plan-to-ait.md
```

Once installed, Claude Code will know the full command set and can follow the delegation workflow when supervising sub-agents. The plan-to-ait agent can be used to convert approved plans into consultant-ready epics and issues.

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

### Version Embedding

Release builds inject the version and repository URL at compile time via ldflags. For development builds, the version defaults to `dev` and the update check is skipped.

If you fork this repository and want the update check to point at your own releases, either update the `RepoURL` default in `internal/ait/version.go` or pass it via ldflags:

```bash
go build -ldflags "-X agent-issue-tracker/internal/ait.Version=v0.1.0 -X agent-issue-tracker/internal/ait.RepoURL=https://github.com/youruser/yourfork" -o ait .
```

## Warning

Do not rely on this for important or long-lived work yet.

The current focus is to validate the workflow by using the tool on itself, tighten the schema and command contract, and improve safety before calling it a usable v1.
