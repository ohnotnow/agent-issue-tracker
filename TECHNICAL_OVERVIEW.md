# Technical Overview

Last updated: 2026-04-03

## What This Is

`ait` is a local-first CLI issue tracker for coding agents. It stores ephemeral project work in a SQLite database inside the repo and is optimized for planning, dependency tracking, notes, claiming, and resuming work between sessions.

## Stack

- Go 1.24.1
- SQLite via `modernc.org/sqlite` v1.46.1
- Public ID encoding via `github.com/sqids/sqids-go` v0.4.1
- Single-binary CLI app; no server, no web UI

## Directory Structure

```text
main.go                  CLI entrypoint; parses global `--db`, opens app, dispatches commands
internal/ait/app.go      Command handlers and CLI help text
internal/ait/store.go    DB access, query helpers, legacy migration, ready/flush logic
internal/ait/migrate.go  Forward-only schema migration registry and schema_version tracking
internal/ait/types.go    Core types, validation, JSON/error helpers
internal/ait/config.go   Project root detection, DB path, prefix normalization/inference
internal/ait/keys.go     Root public ID generation using Sqids
internal/ait/format.go   Human/tree list rendering and Markdown export formatting
internal/ait/completion.go  Shell completion script generation (bash, zsh)
internal/ait/version.go  `version` command and GitHub release check
main_test.go             End-to-end CLI behavior tests against in-memory/temp SQLite DBs
internal/ait/version_test.go  Unit tests for version comparison and release URL logic
claude/                  Claude Code skill/agent docs for using `ait`
```

## Architecture

- `main.go` is intentionally thin: it extracts `--db`, handles help/version shortcuts, opens `ait.App`, then delegates to `App.Run`.
- `App.Run` in [internal/ait/app.go](/Users/billy/Documents/code/agent-issue-tracker/internal/ait/app.go) is the command router for all subcommands.
- `Open()` in [internal/ait/store.go](/Users/billy/Documents/code/agent-issue-tracker/internal/ait/store.go) configures SQLite pragmas, ensures the schema exists, applies migrations, infers/loads the project prefix, and re-synchronizes public IDs.
- Most behavior lives in a single package, `internal/ait`, split by concern rather than by large domain modules.

## Domain Model

```text
ProjectConfig (singleton)
  stores current public ID prefix

Issue
  parent_id -> Issue           hierarchical initiative/epic/task tree
  blocked by -> Issue          many-to-many via issue_dependencies
  has many -> Note             via issue_notes

Issue
  types: initiative | epic | task
  statuses: open | in_progress | closed | cancelled
  priorities: P0 | P1 | P2 | P3 | P4
```

```text
Issue(parent) -> Issue(child)
Issue(blocker) -> Issue(blocked)   via issue_dependencies
Issue -> Note
ProjectConfig -> prefix used to generate Issue.public_id
```

### Key Fields

- `issues.public_id`: user-facing stable ID such as `ait-abcde` or `ait-abcde.1`
- `issues.legacy_id`: preserved only for old TEXT-ID databases migrated in place
- `issues.type`: `initiative`, `epic`, or `task`
- `issues.status`: `open`, `in_progress`, `closed`, `cancelled`
- `issues.parent_id`: supports arbitrary depth hierarchy
- `issues.priority`: lexical priority ordering works because values are `P0`..`P4`
- `issues.claimed_by`, `issues.claimed_at`: cooperative multi-agent locking
- `project_config.prefix`: repo-local prefix used in public IDs

## ID And Prefix Conventions

- Root issues use Sqids-based IDs: `<prefix>-<sqid>`
- Child issues are hierarchical: `<parent>.1`, `<parent>.2`, etc.
- Prefix defaults to the git repo basename, normalized to lowercase hyphenated text
- `init --prefix ...` updates the stored prefix and re-keys all public IDs
- On startup, `syncPublicIDs()` repairs missing or stale public IDs
- Legacy issue IDs remain resolvable through `legacy_id` after migration

## Command Surface

Primary commands implemented in [internal/ait/app.go](/Users/billy/Documents/code/agent-issue-tracker/internal/ait/app.go):

- Lifecycle: `init`, `config`, `create`, `show`, `update`, `close`, `reopen`, `cancel`
- Discovery: `list`, `ready`, `status`, `search`
- Coordination: `claim`, `unclaim`
- Dependencies: `dep add`, `dep remove`, `dep list`, `dep tree`
- Notes: `note add`, `note list`
- Reporting/cleanup: `export`, `flush`, `version`, `log`, `log purge`
- Shell integration: `completion`

### Output Modes

- Default `list`/`ready`/`log` output is slim JSON using `IssueRef` or `FlushHistoryEntrySummary`
- `--long` returns full `Issue` or `FlushHistoryEntry` records
- `list --human` prints grouped tabular output
- `list --tree` prints ASCII hierarchy
- `export` is the only command that emits Markdown instead of JSON
- Every subcommand supports `--help`/`-h` for command-specific usage text
- `completion` is handled in `main.go` before `Open()` (no database needed)

## Business Logic And Conventions

| Location | Purpose |
| --- | --- |
| [internal/ait/app.go](/Users/billy/Documents/code/agent-issue-tracker/internal/ait/app.go) | CLI parsing, validation, command orchestration, per-command help text |
| [internal/ait/store.go](/Users/billy/Documents/code/agent-issue-tracker/internal/ait/store.go) | Query helpers, dependency traversal, ready selection, flush logic, schema bootstrap |
| [internal/ait/migrate.go](/Users/billy/Documents/code/agent-issue-tracker/internal/ait/migrate.go) | Ordered forward-only migrations with `schema_version` |
| [internal/ait/format.go](/Users/billy/Documents/code/agent-issue-tracker/internal/ait/format.go) | Human-readable list rendering and Markdown briefing generation |
| [internal/ait/completion.go](/Users/billy/Documents/code/agent-issue-tracker/internal/ait/completion.go) | Bash and zsh completion script generation |
| [internal/ait/config.go](/Users/billy/Documents/code/agent-issue-tracker/internal/ait/config.go) | Prefix normalization/inference and project-root-based DB placement |

### Notable Behavior

- `search` is case-insensitive (uses `LIKE ... COLLATE NOCASE`).
- `ready` includes only `open` and `in_progress` issues with no non-closed blockers.
- Ready items are ordered by priority first, then creation time.
- `list` hides `closed` and `cancelled` items unless `--all` or an explicit `--status` is provided.
- Initiatives cannot have parents; epics can only be children of initiatives (or standalone); tasks can only be children of epics or other tasks.
- Reparenting is intentionally blocked once hierarchical IDs exist.
- `dep add` prevents self-dependencies and transitive cycles.
- `close --cascade` recursively closes a subtree but skips already terminal descendants.
- `flush` records all flushed issues to `flush_history`/`flush_history_items` before deleting.
- `flush` only deletes root-level terminal trees; mixed-status trees are reported in `skipped`.
- `log` default output shows root-level items only with item counts; `--long` shows all items.
- `log purge` defaults to compact mode (keeps summary rows, drops items); `--full` deletes entirely.
- Notes and dependencies rely on `ON DELETE CASCADE`.

## Schema And Migration Notes

- Fresh startup creates `issues`, `issue_dependencies`, `issue_notes`, `project_config`, and `schema_version`.
- Migrations are append-only and run one version step per transaction.
- Current numbered migrations:
  - `1`: baseline schema
  - `2`: add claim fields to `issues`
  - `3`: add `initiative` issue type (table rebuild to update CHECK constraint)
  - `4`: add `flush_history` and `flush_history_items` tables for flush history
- The app also detects a pre-migration legacy schema where `issues.id` was TEXT and upgrades it in place to integer primary keys plus `legacy_id`/`public_id`.

## Authorization / Coordination Model

There is no user authentication or role system. Coordination is lightweight and local:

- `claim <id> <agent-name>` sets `claimed_by`/`claimed_at`
- a second claim on an already-claimed issue returns a conflict error
- `unclaim` clears the claim
- claim state is visible in `show` and full issue payloads

## Testing

- Framework: Go `testing`
- Main pattern: end-to-end command tests in [main_test.go](/Users/billy/Documents/code/agent-issue-tracker/main_test.go)
- Storage strategy: `:memory:` SQLite for most tests; temp-file DBs for reopen/migration scenarios
- Coverage emphasis:
  - creation/show/update/status transitions
  - hierarchical ID generation and re-keying
  - default vs long output contracts
  - dependency add/remove/tree and cycle detection
  - claim/unclaim behavior
  - export and flush semantics
  - migration idempotency and legacy schema upgrade
- Run: `go test ./...`

## Local Development

- Default DB path: `.ait/ait.db` at the git repo root
- Alternate DB: `ait --db /path/to/file.db ...`
- In-memory DB is supported for tests via `:memory:`
- Useful commands:
  - `go test ./...`
  - `go run . help`
  - `go run . init --prefix ait`
  - `go run . list --human`

## External Integrations

- `version` checks GitHub Releases for the configured repo URL when the binary version is not `dev`
- `claude/` contains agent/skill docs that teach Claude Code how to work against this CLI

## Things To Read First

- [README.md](README.md) for intended workflow and command examples
- [internal/ait/app.go](internal/ait/app.go) for the CLI contract
- [internal/ait/store.go](internal/ait/store.go) for the real data model and most important invariants
- [main_test.go](main_test.go) for expected behavior and edge cases
