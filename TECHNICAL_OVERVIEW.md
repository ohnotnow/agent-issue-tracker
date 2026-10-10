# Technical Overview

Last updated: 2026-10-10

## What This Is

`ait` is a local-first CLI issue tracker for coding agents. It stores ephemeral project work in a SQLite database inside the repo and is optimized for planning, dependency tracking, notes, claiming, and resuming work between sessions.

## Stack

- Go 1.24.1 minimum (`go.mod`); release binaries are built with the current stable Go
- SQLite via `modernc.org/sqlite` v1.46.1
- Public ID encoding via `github.com/sqids/sqids-go` v0.4.1
- Single-binary CLI app; no server, no web UI

## Directory Structure

```text
cmd/ait/main.go                 CLI entrypoint; parses global `--db`, looks up the command, opens the app only if it needs the DB
cmd/ait/main_test.go            End-to-end CLI behavior tests against in-memory/temp SQLite DBs
internal/ait/command.go         Command type and lookup
internal/ait/command_registry.go  Every command's name, aliases, flags, help text and handler
internal/ait/app.go             Command handlers, flag parsing (parseFlags), claim logic
internal/ait/store.go           Open(), DB access, query helpers, ID allocation, moves, ready/flush logic
internal/ait/migrate.go         Forward-only schema migration registry and schema_version tracking
internal/ait/types.go           Core types, validation, JSON/error helpers
internal/ait/config.go          Project root detection, DB path, prefix normalization/inference
internal/ait/keys.go            Root public ID generation using Sqids
internal/ait/format.go          Human/tree list rendering and Markdown export formatting
internal/ait/editor.go          $EDITOR round trip for `create`/`update --human`
internal/ait/guards.go          Read check, empty-body and shrink refusals for blind writes
internal/ait/gitignore.go       Adds `.ait/` to the project's .gitignore on `init`
internal/ait/completion.go      Shell completion script generation (bash, zsh)
internal/ait/version.go         `version` command and GitHub release check
internal/ait/cmd_self_update.go `self-update`: download, checksum verification, binary swap
internal/ait/exit_error.go      Non-error exit codes (e.g. `self-update --check`)
internal/ait/*_test.go          Unit tests (editor parsing, completion syntax, pooled-connection pragmas, version, updater, gitignore)
claude/                         Claude Code skill/agent docs for using `ait`
```

## Architecture

- `cmd/ait/main.go` is intentionally thin: it extracts `--db`, looks the command up with `LookupCommand`, opens `ait.App` only when `cmd.NeedsDB` is true, then delegates to `App.Run`.
- `App.Run` in [internal/ait/app.go](internal/ait/app.go) dispatches through the registry in [internal/ait/command_registry.go](internal/ait/command_registry.go), which is also the source of truth for help text and shell completion.
- `Open()` in [internal/ait/store.go](internal/ait/store.go) opens SQLite with its pragmas (`foreign_keys`, `busy_timeout`, WAL) in the DSN so every pooled connection gets them, applies migrations, infers/loads the project prefix, and re-synchronizes public IDs.
- Most behavior lives in a single package, `internal/ait`, split by concern rather than by large domain modules.

## Domain Model

```text
ProjectConfig (singleton)
  stores current public ID prefix

Issue
  parent_id -> Issue           hierarchical initiative/epic/task tree
  blocked by -> Issue          many-to-many via issue_dependencies
  has many -> Note             via issue_notes
  has many -> alias            old public IDs via issue_aliases

Issue
  types: initiative | epic | task
  statuses: open | in_progress | closed | cancelled
  priorities: P0 | P1 | P2 | P3 | P4
```

### Key Fields

- `issues.public_id`: user-facing ID such as `ait-abcde` or `ait-abcde.1`
- `issues.last_child_number`: highest child number this issue has ever handed out, so a deleted or moved child's number is never reused
- `issues.legacy_id`: preserved only for old TEXT-ID databases migrated in place
- `issues.type`: `initiative`, `epic`, or `task`
- `issues.status`: `open`, `in_progress`, `closed`, `cancelled`
- `issues.parent_id`: supports arbitrary depth hierarchy
- `issues.priority`: lexical priority ordering works because values are `P0`..`P4`
- `issues.claimed_by`, `issues.claimed_at`: cooperative multi-agent locking
- `issues.shown_by_session`, `issues.shown_at`: which Claude Code session last ran `show`, for the read check
- `issue_aliases.public_id`: an old ID of a moved issue, still resolvable everywhere
- `project_config.prefix`: repo-local prefix used in public IDs

## ID And Prefix Conventions

- Root issues use Sqids-based IDs derived from the internal ID: `<prefix>-<sqid>`
- Child issues are hierarchical: `<parent>.1`, `<parent>.2`, etc.
- A child keeps its number for life. New children get one past the highest number the parent has ever handed out (`last_child_number`, plus surviving children and aliases for numbers handed out before that column existed), so deletes and moves leave gaps that are never refilled.
- `update --parent <id>` moves an issue and its subtree to new IDs; `--parent ""` makes it a root. Old IDs are recorded in `issue_aliases`, and `show <old-id>` sets `moved_from`. Moving back to root restores the original root ID; moving back under a former parent allocates a new number.
- Prefix defaults to the git repo basename, normalized to lowercase hyphenated text
- `init --prefix ...` updates the stored prefix and re-keys all public IDs
- On startup, `syncPublicIDs()` repairs missing or stale public IDs
- Legacy issue IDs remain resolvable through `legacy_id` after migration

## Command Surface

Commands are registered in [internal/ait/command_registry.go](internal/ait/command_registry.go):

- Lifecycle: `init`, `config`, `create` (alias `add`), `show`, `update` (alias `edit`), `close`, `reopen`, `cancel`, `delete`
- Discovery: `list`, `ready`, `status`, `search`
- Coordination: `claim`, `unclaim`
- Dependencies: `dep add`, `dep remove`, `dep list`, `dep tree`
- Notes: `note add`, `note list`
- Reporting/cleanup: `export`, `flush`, `log`, `log purge`
- Tooling: `completion`, `version`, `self-update`, `help`

### Output Modes

- Default `list`/`ready`/`log` output is slim JSON using `IssueRef` or `FlushHistoryEntrySummary`
- `--long` returns full `Issue` or `FlushHistoryEntry` records
- `list --human` prints grouped tabular output; `list --tree` prints an ASCII hierarchy. An issue whose parent is filtered out is drawn as a root under its full ID, so both show the same issues as the JSON.
- `export` is the only command that emits Markdown instead of JSON
- Every subcommand supports `--help`/`-h` for command-specific usage text
- `completion`, `version`, `self-update` and `help` have `NeedsDB: false` and run without opening a database

## Business Logic And Conventions

| Location | Purpose |
| --- | --- |
| [internal/ait/app.go](internal/ait/app.go) | CLI parsing, validation, command orchestration |
| [internal/ait/command_registry.go](internal/ait/command_registry.go) | Command metadata, per-command help text, flag lists for completion |
| [internal/ait/store.go](internal/ait/store.go) | Query helpers, ID allocation and moves, dependency traversal, ready selection, flush logic |
| [internal/ait/migrate.go](internal/ait/migrate.go) | Ordered forward-only migrations with `schema_version` |
| [internal/ait/guards.go](internal/ait/guards.go) | Read check and description guards |
| [internal/ait/format.go](internal/ait/format.go) | Human-readable list rendering and Markdown briefing generation |
| [internal/ait/completion.go](internal/ait/completion.go) | Bash and zsh completion script generation |
| [internal/ait/config.go](internal/ait/config.go) | Prefix normalization/inference and project-root-based DB placement |

### Notable Behavior

- `search` is case-insensitive (uses `LIKE ... COLLATE NOCASE`).
- `ready` includes only `open` and `in_progress` issues with no non-closed blockers.
- Ready items are ordered by priority first, then creation time.
- `list` hides `closed` and `cancelled` items unless `--all` or an explicit `--status` is provided.
- Initiatives cannot have parents; epics can only be children of initiatives (or standalone); tasks can only be children of epics or other tasks.
- Commands parsed with Go's `flag` package go through `parseFlags`, which refuses leftover positional arguments with a usage error. The flag package stops at the first positional word, so without this a stray word silently dropped every later flag.
- `dep add` prevents self-dependencies and transitive cycles.
- `close --cascade` recursively closes a subtree but skips already terminal descendants.
- `flush` records all flushed issues to `flush_history`/`flush_history_items` before deleting.
- `flush` only deletes root-level terminal trees; mixed-status trees are reported in `skipped`.
- `delete` is permanent and unrecorded; it needs `--force`, and `--cascade` for an issue with children.
- `log` default output shows root-level items only with item counts; `--long` shows all items.
- `log purge` defaults to compact mode (keeps summary rows, drops items); `--full` deletes entirely.
- Notes, dependencies and aliases rely on `ON DELETE CASCADE`.
- `update --human` opens `$EDITOR` with the help text below a git-style scissors line (`# ---- >8 ----`); everything above it, `#` lines included, is kept. If the scissors line is deleted, every `#` line is dropped instead.

## Schema And Migration Notes

- On a fresh database, `runMigrations` creates `schema_version`, migration 1 creates `issues`, `issue_dependencies`, `issue_notes` and `project_config`, and the rest apply in order.
- Migrations are append-only. Each runs in its own transaction, which also records its version; before applying, it takes the write lock and rechecks the version, so concurrent opens apply each step once and a failed step leaves the database at the last one that succeeded.
- Current numbered migrations:
  - `1`: baseline schema
  - `2`: add claim fields to `issues`
  - `3`: add `initiative` issue type (table rebuild to update CHECK constraint)
  - `4`: add `flush_history` and `flush_history_items` tables for flush history
  - `5`: add `shown_by_session`/`shown_at` to `issues` for the read check
  - `6`: add `issue_aliases` for moved issues
  - `7`: add `issues.last_child_number`
- The app also detects a pre-migration legacy schema where `issues.id` was TEXT and upgrades it in place to integer primary keys plus `legacy_id`/`public_id`.

## Authorization / Coordination Model

There is no user authentication or role system. Coordination is lightweight and local:

- `claim <id> <agent-name>` sets `claimed_by`/`claimed_at` with one conditional write (`WHERE claimed_by IS NULL`), so when several agents race for an issue exactly one wins
- a claim on an already-claimed issue returns a `conflict` error; `update --claim` changes nothing at all if its claim loses
- `unclaim` clears the claim
- claim state is visible in `show` and full issue payloads
- `show` stamps the issue with `CLAUDE_CODE_SESSION_ID`; `update` of title/description and `close`/`cancel --note` refuse unless the same session showed it within the last hour (override with `--dangerously-skip-read-check`)

## Testing

- Framework: Go `testing`
- Main pattern: end-to-end command tests in [cmd/ait/main_test.go](cmd/ait/main_test.go), driving `App.Run` directly
- Storage strategy: `:memory:` SQLite for most tests; temp-file DBs for reopen, migration and concurrency scenarios
- Coverage emphasis:
  - creation/show/update/status transitions
  - hierarchical ID generation, moves, aliases and number reuse
  - default vs long output contracts
  - dependency add/remove/tree and cycle detection
  - claim/unclaim, including several agents racing for one claim
  - export and flush semantics
  - migration idempotency, resuming after a failed step, concurrent upgrades, legacy schema upgrade
- Run: `go test ./...`; the release workflow runs `go vet ./...` and `go test ./...` before building

## Local Development

- Default DB path: `.ait/ait.db` at the git repo root
- Alternate DB: `ait --db /path/to/file.db ...`
- In-memory DB is supported for tests via `:memory:`
- Useful commands:
  - `go test ./...`
  - `go run ./cmd/ait help`
  - `go run ./cmd/ait init --prefix ait`
  - `go run ./cmd/ait list --human`

## External Integrations

- `version` checks GitHub Releases for the configured repo URL when the binary version is not `dev`
- `self-update` downloads the matching release asset and verifies it against the release's `SHA256SUMS` before replacing the binary
- `claude/` contains agent/skill docs that teach Claude Code how to work against this CLI

## Things To Read First

- [README.md](README.md) for intended workflow and command examples
- [internal/ait/command_registry.go](internal/ait/command_registry.go) for the CLI contract
- [internal/ait/store.go](internal/ait/store.go) for the real data model and most important invariants
- [cmd/ait/main_test.go](cmd/ait/main_test.go) for expected behavior and edge cases
