# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `--note <text>` flag on `close` — the clearer name for what was previously `--reason`. Attaches a closing note to the issue before closing it.

### Changed
- `--reason <text>` on `close` is now documented as an alias for `--note`. Existing usage continues to work unchanged.

## [1.3.0] - 2026-04-03

### Added
- **Flush history** — `flush` now records all flushed issues into the database before deleting them, preserving a searchable record of completed work.
- `--summary` flag on `flush` — attach an editorial note describing what was accomplished (e.g. `ait flush --summary "Fixed pg compatibility"`).
- `log` command — view flush history with slim/long output modes, `--last`, `--since`, and `--search` flags.
- `log purge` subcommand — compact old history by removing per-issue items while keeping summary rows (`--keep`, `--before`), or fully delete old entries with `--full`.
- Schema migration (v4) to add `flush_history` and `flush_history_items` tables.

### Changed
- `flush` help text updated to document `--summary` and history recording.

## [1.2.2] - 2026-03-20

### Added
- `edit` command alias for `update` — works identically, including shell completion and help text.

### Fixed
- `ready` now respects parent epic dependencies — tasks inside a blocked epic no longer appear as ready.

## [1.2.1] - 2026-03-19

### Added
- `--reason <text>` flag on `close` — automatically adds a note with the reason before closing the issue.
- `@file` syntax for `--description` on `create` and `update` — reads description content from a file (e.g. `--description @spec.md`).

### Changed
- Clearer validation message when attempting to add a task directly under an initiative — now suggests creating an epic first.

## [1.2.0] - 2026-03-13

### Added
- New `initiative` issue type — the strategic "why" above epics. Initiatives are always top-level and can contain epics as children.
- Three-tier hierarchy: initiative > epic > task, with parent-type validation enforced at creation time.
- Schema migration (v3) to add `initiative` to the issue type constraint.

### Changed
- Markdown export uses "Epics" heading instead of "Tasks" when exporting an initiative.
- Human and tree list views sort initiatives first, then epics, then tasks.
- Shell completion now includes `initiative` in type values.

## [1.1.2] - 2026-03-12

### Fixed
- Human (`--human`) and tree (`--tree`) list views now render deeply nested hierarchies correctly — previously only one level of children was shown.

## [1.1.1] - 2026-03-12

### Changed
- Refactored command routing into a registry pattern (`command.go`, `command_registry.go`), replacing the switch statement in `app.go`. This also makes per-command help and shell completion data-driven from a single source.
- Simplified shell completion generation to derive flag lists from the command registry.

### Fixed
- `update --help` now shows command-specific help instead of failing.

## [1.1.0] - 2026-03-11

### Added
- Shell tab completion for bash and zsh (`ait completion bash`, `ait completion zsh`). Completes subcommands, flags, flag values, and issue IDs.
- Per-command `--help` / `-h` support for every subcommand with usage text, flags, and examples.

### Fixed
- Search is now properly case-insensitive (added `COLLATE NOCASE` to query).

## [1.0.0] - 2026-03-05

First stable release. Core feature set:

- Hierarchical issue tracking (epics and tasks) with Sqids-based public IDs
- Dependencies with transitive cycle detection
- Notes for preserving context between sessions
- Issue claiming for multi-agent coordination
- `ready` command for surfacing unblocked work by priority
- Markdown export for delegating work to sub-agents
- Cascade close for entire subtrees
- `flush` for cleaning up completed work
- Human-readable (`--human`) and tree (`--tree`) list views
- Forward-only schema migration system
- Custom database path via `--db`

[Unreleased]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.3.0...HEAD
[1.3.0]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.2.2...v1.3.0
[1.2.2]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.2.1...v1.2.2
[1.2.1]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.2.0...v1.2.1
[1.2.0]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.1.2...v1.2.0
[1.1.2]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.1.1...v1.1.2
[1.1.1]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.1.0...v1.1.1
[1.1.0]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/ohnotnow/agent-issue-tracker/releases/tag/v1.0.0
