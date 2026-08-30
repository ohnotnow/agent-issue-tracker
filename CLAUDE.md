# Agent Issue Tracker (ait)

Local-first CLI issue tracker for coding agents. Go 1.24, SQLite, single binary.

## Quick Reference

- **Build**: `GOCACHE=$(pwd)/.gocache go build -o ait ./cmd/ait`
- **Test**: `GOCACHE=$(pwd)/.gocache go test ./...`
- **Run (dev)**: `go run . <command>`

## Project Structure

```
cmd/ait/main.go             Entrypoint: --db flag extraction, help/version shortcuts, Open + Run
internal/ait/
  app.go                    Command router, flag parsing, per-command handlers and help text
  store.go                  DB access, queries, ready/flush logic, schema bootstrap, Open()
  migrate.go                Forward-only numbered migrations with schema_version tracking
  types.go                  Core types (Issue, Note, CLIError), validation, JSON helpers
  config.go                 Project root detection, DB path, prefix normalisation/inference
  keys.go                   Sqids-based root public ID generation
  format.go                 Human/tree list rendering, Markdown export
  completion.go             Bash/zsh completion script generation
  version.go                Version command and GitHub release check
main_test.go                End-to-end CLI tests (in-memory and temp-file SQLite)
internal/ait/version_test.go  Version comparison unit tests
claude/                     Skills and agent docs for Claude Code integration
```

## Architecture

- `cmd/ait/main.go` is thin: extracts `--db`, looks up command via `LookupCommand`, opens `App` only if `cmd.NeedsDB` is true.
- `App.Run()` dispatches to command handlers in `app.go`.
- `Open()` in `store.go` sets up pragmas, creates schema, runs migrations, infers prefix, syncs public IDs.
- Single package `internal/ait` split by concern, not by domain.

## Domain Model

- **Issue types**: `initiative` > `epic` > `task` (hierarchy enforced)
- **Statuses**: `open`, `in_progress`, `closed`, `cancelled`
- **Priorities**: `P0`-`P4` (lexical ordering)
- **IDs**: `<prefix>-<sqid>` for roots, `.1`, `.1.1` etc for children
- Dependencies are many-to-many with cycle detection
- Claims use `claimed_by`/`claimed_at` fields for multi-agent coordination

## Key Invariants

- Initiatives cannot have parents; epics can only be children of initiatives; tasks can only be children of epics or other tasks.
- `ready` returns open/in_progress issues with no non-closed blockers, ordered by priority then creation time.
- `list` hides closed/cancelled unless `--all` or explicit `--status`.
- `flush` only removes root-level trees where the entire subtree is terminal.
- `dep add` rejects self-dependencies and transitive cycles.
- `close --cascade` recursively closes subtrees, skipping already-terminal issues.
- Notes and dependencies use `ON DELETE CASCADE`.

## Testing Conventions

- End-to-end tests in `main_test.go` using Go `testing`.
- Storage: `:memory:` SQLite for most tests, temp-file DBs for migration/reopen scenarios.
- Helper `runJSONCommand` drives `app.Run()` and unmarshals JSON output.

## Schema Migrations

Four numbered migrations (baseline, claim fields, initiative type, flush history tables). Append-only, one transaction per step. Also handles legacy TEXT-ID schema upgrade.

## Status

Feature-complete. Tight scope. No planned feature additions.

Cross-repo orchestration (one feature spanning a frontend and a backend repo, etc.) is handled by the `ait-fleet` Claude skill via the existing `--db` flag and a textual linking convention in notes. Do **not** add cross-DB dependency tracking, fleet manifests, or multi-DB queries to the binary itself — that's deliberately out of scope and the skill exists precisely so the binary can stay tightly scoped.

## Tweaks

If tweaks are asked for and work done on the project - offer to update the CHANGELOG.md.  Ask the user first what the new tag will be so you can use it when updating the file.

