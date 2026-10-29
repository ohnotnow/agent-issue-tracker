# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.18.0] - 2026-10-10

The first release to come out of a full code review rather than a feature request. There are no new commands, but there is a run of fixes to IDs, claims, migrations and argument handling, which matter most when several agents share one tracker.

### Added
- An Installation section in the README. It shows how to download the prebuilt binary for your platform with `curl` from the `releases/latest` URL, which always points at the newest release, so you don't need Go or the current version number.

### Changed
- Commands now refuse stray words instead of silently ignoring every flag after them. Go's flag parser stops at the first positional word, and no command checked for leftovers, so `ait flush preview --dry-run` really flushed (the `--dry-run` was never seen), `ait self-update check` would have installed rather than checked, and `create`, `update`, `list`, `ready`, `log`, `log purge` and `init` quietly dropped filters or field changes. They now return a `usage` error (exit 64) naming the unexpected argument, before changing anything. This could break a script that relied on the old leniency, such as `ait list open`; use `--status open` instead.
- Release binaries are now built with the current stable Go rather than the `go 1.24.1` minimum in `go.mod`, so they pick up compiler and standard library security fixes. The release workflow also runs `go vet` and the test suite before it publishes anything.

### Fixed
- A deleted child's number could be handed out again. 1.16.1 stopped siblings being renumbered, but the next number was still worked out from the children (and moved-away aliases) that survived, so deleting the only child, the highest-numbered child, or a moved issue whose alias held the number left nothing to remember it. The next child got the same ID, and any note or commit that mentioned the deleted one silently pointed at the newcomer. Each parent now records the highest number it has ever handed out (schema migration 7). Numbers freed before upgrading can't be recovered, so one of those may be reused once.
- Two agents claiming the same issue at the same moment could both be told they had it. `claim` and `update --claim` read the issue, checked nobody held it, then wrote the claim, so racing agents could all pass the check; with eight processes racing, the old binary had more than one winner in 6 rounds out of 15. The claim is now a single conditional write: exactly one agent wins, the rest get the usual `conflict` error, and a losing `update --claim` changes none of its other fields either.
- A failed upgrade could leave the database unusable. The schema version was only recorded after every pending migration had run, so if a later one failed, the earlier ones were applied but not recorded, and every following command tried them again and failed (`duplicate column name`). Separately, several agents starting at once after an upgrade all raced to apply the same migration, and some failed on their first command (13 opens in 80 in a test). Each migration now records its own version as it commits, and rechecks after taking the write lock, so it is applied exactly once.
- SQLite's `foreign_keys` and `busy_timeout` settings only reached one database connection. Any other connection had cascading deletes switched off and gave up on a lock immediately instead of waiting. They are now applied to every connection.
- `update --human` deleted Markdown headings and other lines starting with `#` from the description, even when only the title was changed, because every `#` line was treated as help text. A title starting with `#` (`#42 regression`) disappeared entirely and the first line of the body took its place. The help text now sits below a git-style `# ---- >8 ----` line, and only that line and what follows are discarded. If the line is deleted, the old behaviour applies, so the help never ends up in the description.
- `list --human` and `list --tree` drew nothing for children whose parent had been filtered out: `list --parent <epic> --human` printed an empty list although the JSON had the tasks, and closing an epic hid its still-open children from the default human list. Such children are now drawn as roots under their full ID.
- Bash completion failed to load in the bash 3.2 that ships with macOS (`syntax error near unexpected token '&'`). It now loads and completes there.
- Correction to the 1.17.0 notes, which said moving an issue back to where it was restores its old ID. That holds for a move back to being a root; a move back under its former parent gets a new number.

## [1.17.0] - 2026-10-10

### Added
- `update <id> --parent <new-parent>` moves an issue, children and all, to a different parent, and `--parent ""` makes it a standalone root. Until now `--parent` was accepted and then refused, so an issue filed under the wrong epic had to be recreated and the original cancelled with a pointer note. A moved issue gets new IDs that match its new place, so the dotted ID never claims a parent it no longer has. The old IDs are kept as aliases and still work in every command, so references in notes, commit messages and other issues keep landing on the right issue: `ait show <old-id>` returns the issue as normal, plus a `moved_from` field set to the ID you asked for. A number freed by a move is never handed out again, and moving an issue back to where it was restores its old ID. Moves are refused under the issue itself or one of its own descendants, and wherever the type rules forbid (an epic under a task, say). Schema migration 6 adds the `issue_aliases` table; deleting or flushing an issue removes its aliases too. Suggested by an agent that had just done the recreate-and-cancel dance by hand.

## [1.16.1] - 2026-10-10

### Fixed
- Child IDs no longer shift when a sibling is deleted. A child's `.N` suffix was never stored for good: every time the database was opened, each parent's children were renumbered `.1`, `.2`, `.3` in creation order. That was harmless while children only ever closed or got cancelled, but after `ait delete <epic>.1 --force`, the old `.2` quietly became `.1` and `.3` became `.2`. Any note, commit message or other issue that mentioned `.2` then pointed at a different issue, with no warning. A child now keeps the number it was given at creation, and deleting a sibling just leaves a gap (`.2`, `.3`, `.4`). Databases that have already had a child deleted were renumbered at the time, and that can't be undone; IDs are stable from this release onwards.
- Creating a child in the same process as a delete could fail with `UNIQUE constraint failed: issues.public_id`, because the new child's number came from counting its siblings, and after a gap that count matched a number already in use. New children now take one past the highest number in use. Ordinary CLI use never hit this, because the renumbering on open (above) closed the gap first.

## [1.16.0] - 2026-09-19

### Added
- Guards against blind writes, mirrored in `ant` (same flags, same error codes). `update` refuses an empty `--description` (a literal empty string, or an `@file` with nothing in it) rather than silently ignoring it. A replacement description under half the length of the existing one is refused with a new `shrink` error unless `--force` is passed, on the theory that a diff or a summary was sent where the whole text was expected; growing a description is never questioned and descriptions under 200 characters are exempt. And when `CLAUDE_CODE_SESSION_ID` is set (Claude Code sets it for every command an agent runs), `show` now records the session and time against the issue, and `update --title/--description/--human` and `close`/`cancel --note` refuse with a new `unread` error unless that same session showed the issue within the last hour. The message says which it was: never shown, shown by a different session, or shown too long ago. Runs with no session id, i.e. a person in a terminal, are never checked. The override flag is deliberately long: `--dangerously-skip-read-check`. Schema migration 5 adds the two `shown_*` columns. This came out of a session where an agent overwrote a hand-written issue it had never read; the hour-long window exists because context compaction keeps the session id but not the memory of what was read.
- `--description -` on `create` and `update` reads the description from stdin, so a heredoc works, matching `ant --body -`. Until now a bare `-` was taken literally, which is how the agent above replaced an issue body with a single dash. With the empty-body refusal, that same command is now an error.

## [1.15.0] - 2026-08-29

### Fixed
- The self-update redirect for go-install users printed `go install github.com/ohnotnow/agent-issue-tracker@latest`, a command that could never work: `go.mod` declared the bare module name `agent-issue-tracker`, and Go refuses to install a module whose declared path doesn't match the path you asked for. The module is now declared under its full GitHub path and the hint points at `github.com/ohnotnow/agent-issue-tracker/cmd/ait@latest`, which installs a binary actually named `ait` rather than `agent-issue-tracker`. (Spotted by a passing agent reading the source; nobody had ever hit it in practice, because nobody could go-install the tool in the first place.) `go install` works from this release's tag onwards; older tags still carry the bare module name.

### Changed
- The main package moved from the repository root to `cmd/ait/` in support of the above (with `go install`, the binary is named after the last element of the package path). Local builds are now `go build -o ait ./cmd/ait`; release builds and docs updated to match. No change to the CLI itself.

## [1.14.0] - 2026-07-12

### Changed
- Commands no longer silently create the database — a breaking change. Every DB-backed command except `init` now refuses to run until `ait init` has been done, returning `{"error": {"code": "uninitialised", "message": "no ait database at <path> — run 'ait init' first"}}` with exit code 1. Previously a stray `ait list` in the wrong directory quietly bootstrapped `.ait/ait.db` (and, in a git repository, edited that repo's `.gitignore`) — and its empty-looking response couldn't be told apart from "this project uses ait and has nothing open", so an agent could happily start filing issues in a project that never used ait at all. The error code, exit code, and message shape all match `ant`, so an agent hopping between the two tools sees one contract. `--db :memory:` remains exempt.
- Errors now go to stderr — also breaking. The JSON `{"error": …}` envelope used to share stdout with data, so a pipeline like `ait list | jq '.issues[]'` or an `$(ait export …)` capture would ingest an error as if it were data. stdout now only ever carries data; failures are the envelope on stderr plus a non-zero exit code, matching `ant` and every other JSON-speaking CLI (gh, kubectl, aws). Scripts that parsed errors from stdout need `2>&1`.
- `init` now tops up the `.gitignore` entry on every run rather than only when first creating `.ait/`, so an entry that's been lost (or a project that gained a `.git` since init) is repaired by re-running `ait init`. An unwritable `.gitignore` warns on stderr instead of failing the whole init — the database is already set up by that point.

### Added
- `init` reports what the `.gitignore` step did: `gitignore_updated: true` when the entry was added this run, and a `note` field (`"no .git directory — not adding .ait/ to .gitignore"`) when there was no repository to add it to. A quick `ait init` in a brand-new project now tells you about the gap up front, instead of leaving it to be discovered at the first `git status` after `git init`. Both fields mirror `ant init`.

### Fixed
- An unknown command (`ait lst`, say) no longer creates the database as a side effect. main opened the database before the command name was checked, so even a typo bootstrapped `.ait/`; unknown commands are now rejected (usage error, exit 64) before the database is touched.

## [1.13.0] - 2026-06-07

### Added
- `delete <id> --force` (with `--cascade` for subtrees) — a narrow, guarded way to permanently remove a genuine mistake: a fat-fingered duplicate, a throwaway, an issue created against the wrong parent. Until now the only options were `cancel` (which leaves a visible stub forever) and `flush` (which won't touch an issue while its root tree is still open), so an honest slip lingered in every `--all` listing for the life of the project. `delete` is deliberately guarded — nothing happens without `--force`, and an issue that has children is refused unless you also pass `--cascade` — and, unlike `flush`, it records nothing: it's the "this should never have existed" escape hatch, not housekeeping of completed work. Notes and dependency links are cascade-removed automatically, and the response lists exactly what was deleted.
- `init` (and re-keying) now returns more than just the prefix: `db` (the resolved database path), `schema_version`, `created` (whether this call opened a fresh database), and `rekeyed` (how many existing issue IDs were rewritten). Re-keying rewrites every ID in the project — a consequential, once-in-a-while operation — and the previous single-word `{"prefix": "…"}` reply gave no way to tell a fresh database from a re-key, how many IDs it touched, or where the database lives. The new fields are purely additive, so anything reading `.prefix` keeps working.

## [1.12.0] - 2026-05-30

### Added
- `ait` now adds `.ait/` to your project's `.gitignore` the first time it creates its data directory inside a git repository, so the local issue database isn't committed. It only does this for the default `.ait/ait.db` location, leaves custom `--db` paths and non-git directories alone, and never overwrites a `.ait/` entry you've already added or removed yourself. Mirrors the behaviour of the companion `ant` tool.

## [1.11.0] - 2026-05-28

### Added
- `update --claim <agent-name>` (also `edit --claim`) — fold a claim into an update so you can claim an issue and mark it `in_progress` in a single call, instead of running `claim` then `update`. Agents (this one included) regularly assume `update` can claim, or trip over the two-step dance; this removes the round-trip. Uses the same rules as the `claim` command — it fails with a conflict if the issue is already held — and `--claim` on its own counts as a valid update.
- `cancel --note "<text>"` (with `--reason` as an alias) — attach a note while cancelling, matching the surface `close` already had. Stored as a `Cancelled: <text>` note. A small paper-cut for agents that learned `close --note` and reached for the same on `cancel`.

### Fixed
- `close --note` and `cancel --note` no longer emit two JSON documents (the note acknowledgement followed by the issue ref). They now return a single issue ref, so a caller parsing stdout gets one object rather than tripping over trailing JSON. Plain `note add` still prints its acknowledgement as before.

## [1.10.0] - 2026-05-20

### Added
- `add` as an alias for `create`. Coding agents (this one included) regularly try `ait add ...` on muscle memory, hit an error, read help, then retry with `create`. The alias removes that round-trip — both spellings now do the same thing, and shell completion treats them identically.

## [1.9.0] - 2026-05-15

### Added
- `update --human` (also `edit --human`) — mirrors `create --human` for editing. Opens `$EDITOR` pre-filled with the issue's current title and description so you can fix a typo (or rewrite the lot) without retyping it on the command line.
  - Only fields you actually change in the editor are written, so saving with no edits surfaces the existing "no fields were provided to update" error rather than silently no-op'ing.
  - Composes with other flags, e.g. `ait update PROJ-1 --human --priority P0`.
  - `--human` is mutually exclusive with `--title` / `--description` — the editor is the source of truth when it's open.

## [1.8.2] - 2026-05-04

### Added
- Internal `tag-release` maintainer skill (lives in `.claude/skills/`) — just looks through the state of the repo and suggests what needs doing before tagging a release.

## [1.8.1] - 2026-05-04

Test tag to exercise `self-update`. No functional changes.

## [1.8.0] - 2026-05-04

### Added
- `self-update` command — download, verify, and atomically swap the running binary against the latest GitHub release. Mirrors the surface of the sibling `ant` tool so the two stay muscle-memory compatible.
  - `ait self-update` — interactive flow showing release notes and a y/N prompt.
  - `ait self-update --yes` (or `-y`) — skip the prompt.
  - `ait self-update --check` — report only; exits 0 if up to date, 1 if a newer release is available, 2 if the lookup failed (mirrors `composer outdated`).
  - Verifies the downloaded binary against a published `SHA256SUMS` before swapping. Refuses to run on dev builds, redirects Homebrew and `go install` users to the right tool, warns (but proceeds) for `/usr/bin` and `/usr/local/bin` on Linux, and bails out before any download if the install directory is unwriteable.
- Release workflow now publishes a `SHA256SUMS` asset alongside the platform binaries so users can verify downloads independently of self-update.

### Changed
- `version` upgrade hint now mentions `ait self-update` alongside the releases page link.
- Internal: error plumbing gained an `ExitWithCode` / `SilentExit` pair so commands can return a specific shell exit code (with or without the JSON error envelope). `WriteError` was split out of `ExitWithError` to support the new path.

## [1.7.0] - 2026-05-03

### Added
- `ait-fleet` Claude skill — multi-repo orchestration for projects that span more than one git repository (e.g. separate frontend and backend repos). Walks the user through creating a manifest at `~/.config/ait-fleet/<name>.json`, validates each repo path and prefix, and documents the textual linking convention (`Linked epic: <alias>:<id>`, `Blocked by <alias>:<id>`) used to coordinate work across DBs via the existing `--db` flag. No binary changes — cross-DB linking is deliberately kept out of the tool itself and noted as such in `CLAUDE.md`.

## [1.6.0] - 2026-05-03

### Changed
**Breaking-ish:** if you have custom scripts or tooling that relied on the old 'full fat' output JSON format, you'll need to update your code. Mutating commands now return slim output by default, dropping the verbose full-record echo that wasted a lot of tokens. Pass `--long` on any of these commands to get the full record back.
  - `create`, `update`, `close` (single), `cancel`, `reopen`, `claim`, `unclaim` now return a slim issue ref (`{id, title, status, type, priority}`) by default. `--long` returns the full `Issue`.
  - `close --cascade` returns `{closed: [refs]}` by default. `close --cascade --long` returns `{closed: [Issues]}` with full records.
  - `dep add` and `dep remove` now return a slim ack (`{ok, blocked_id, blocker_id}`) by default. `--long` returns the full blocker list (the previous behaviour, equivalent to `dep list`).
  - `note add` returns a slim ack (`{ok, issue_id, note_id}`) by default. `--long` returns the full `Note` record.
- `list` now includes a `hidden_count` field in the JSON response when the default filter is active (no `--all`, no explicit `--status`). It tells you how many closed/cancelled issues are being filtered out so an empty-looking response is never a surprise. Omitted when `--all` or `--status` is passed.

### Added
- Help text for `list` and `ready` now calls out the default filter and the slim-vs-`--long` distinction respectively, addressing user feedback that both behaviours can catch new callers off-guard.

## [1.5.0] - 2026-04-20

### Added
- `--note <text>` flag on `close` — the clearer name for what was previously `--reason`. Attaches a closing note to the issue before closing it.
- `ait-recap` Claude skill — generates a friendly markdown summary of recent `ait` activity for the current project or across a directory of projects. Useful as an aide-mémoire before a status update.

### Changed
- `--reason <text>` on `close` is now documented as an alias for `--note`. Existing scripts, agents and skills continue to work unchanged.
- `plan-to-ait` Claude agent reworked around a TDD-oriented flow.

### Fixed
- zsh shell completion for `close` now offers `--note` and `--reason` (previously only `--cascade` was completed).

## [1.4.0] - 2026-04-12

### Added
- `--human` flag on `create` — opens `$EDITOR` (falling back to `vi`) with a git-commit-style template so the title and description can be authored interactively. Useful when writing out a longer description is easier than passing it on the command line.

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

[Unreleased]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.18.0...HEAD
[1.18.0]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.17.0...v1.18.0
[1.17.0]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.16.1...v1.17.0
[1.16.1]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.16.0...v1.16.1
[1.16.0]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.15.0...v1.16.0
[1.15.0]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.14.0...v1.15.0
[1.14.0]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.13.0...v1.14.0
[1.13.0]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.12.0...v1.13.0
[1.12.0]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.11.0...v1.12.0
[1.11.0]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.10.0...v1.11.0
[1.10.0]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.9.0...v1.10.0
[1.9.0]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.8.2...v1.9.0
[1.8.2]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.8.1...v1.8.2
[1.8.1]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.8.0...v1.8.1
[1.8.0]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.7.0...v1.8.0
[1.7.0]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.6.0...v1.7.0
[1.6.0]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.5.0...v1.6.0
[1.5.0]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.4.0...v1.5.0
[1.4.0]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.3.0...v1.4.0
[1.3.0]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.2.2...v1.3.0
[1.2.2]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.2.1...v1.2.2
[1.2.1]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.2.0...v1.2.1
[1.2.0]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.1.2...v1.2.0
[1.1.2]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.1.1...v1.1.2
[1.1.1]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.1.0...v1.1.1
[1.1.0]: https://github.com/ohnotnow/agent-issue-tracker/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/ohnotnow/agent-issue-tracker/releases/tag/v1.0.0
