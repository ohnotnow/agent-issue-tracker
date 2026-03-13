---
name: ait
description: >
  Local-first issue tracker for coding agents. Use when planning work, tracking
  multi-step tasks, modelling dependencies, coordinating between agents, or
  resuming after session loss or conversation compaction.
allowed-tools: "Read,Bash(ait:*)"
version: "0.1.0"
author: "ohnotnow <https://github.com/ohnotnow>"
license: "MIT"
---

# AIT (`ait`) - Agent Issue Tracker Quick Reference

AIT is a lightweight, SQLite-backed CLI issue tracker designed for coding agents.
Use it instead of TodoWrite for tracking work that spans sessions, has
dependencies, or involves multiple agents.

## Essential Commands

### View Issues
```bash
ait status                          # Overview: counts by status
ait list                            # List open issues (slim JSON, 5 fields)
ait list --long                     # Full JSON record (all fields)
ait list --human                    # Compact tabular view for humans
ait list --tree                     # Parent-child hierarchy with connectors
ait list --type epic                # Filter by type
ait list --status open              # Filter by status
ait list --priority P1              # Filter by priority
ait list --parent <id>              # Children of a specific epic
ait list --all                      # Include closed/cancelled issues
ait ready                           # Unblocked issues, ordered by priority
ait ready --type task               # Unblocked tasks only (excludes epics)
ait ready --long                    # Unblocked issues with full detail
ait show <id>                       # Full details, children, notes, deps
ait search "keyword"                # Search issues by text
ait config                          # Show project prefix and schema version
```

### Create Issues
```bash
ait create --title "Title"                              # Basic task
ait create --title "Title" --type initiative             # Initiative (strategic vision)
ait create --title "Title" --type epic                   # Epic (group of tasks)
ait create --title "Title" --type epic --parent <init-id>  # Epic under an initiative
ait create --title "Title" --parent <epic-id>            # Task under an epic
ait create --title "Title" --description "Details..."    # With description
ait create --title "Title" --priority P1                 # With priority
```

### Update Issues
```bash
ait update <id> --status in_progress   # Start working
ait update <id> --status open          # Back to open
ait update <id> --title "New title"    # Change title
ait update <id> --priority P0          # Change priority
```

### Close / Cancel / Reopen
```bash
ait close <id>                # Close a single issue
ait close <id> --cascade      # Close an epic and all its descendants
ait cancel <id>               # Cancel an issue
ait reopen <id>               # Reopen a closed or cancelled issue
```

### Dependencies
```bash
ait dep add <id> <blocker-id>      # <id> is blocked by <blocker-id>
ait dep remove <id> <blocker-id>   # Remove a dependency
ait dep list <id>                  # Show dependencies for an issue
ait dep tree <id>                  # Show dependency tree
```
Cycle detection is built in — adding a dependency that would create a cycle is
rejected automatically.

### Notes
```bash
ait note add <id> "Note body text"   # Attach a note to an issue
ait note list <id>                   # List notes for an issue
```

### Flush (Housekeeping)
```bash
ait flush              # permanently delete all closed/cancelled issues
ait flush --dry-run    # preview what would be deleted without changing anything
```
Flush removes root-level issues whose entire descendant tree is also closed or
cancelled. Notes and dependencies are cascade-deleted automatically.

**Important:** If the `skipped` list in the response is non-empty, it means a
closed epic has open or in-progress children — something that probably needs
human attention. Flag this to the user and suggest they review the skipped
issues before deciding what to do.

### Claiming (Multi-Agent)
```bash
ait claim <id> <agent-name>    # Claim an issue (prevents duplicate work)
ait unclaim <id>               # Release the claim
```
If another agent already holds the claim, `claim` returns a conflict error with
the current holder's name.

The agent-name parameter is for you to have a little creative fun if you want to.  You're free to use your real name, or pick a name that amuses and delights you or a project-specific name for your 'agentic persona'.  If the user seems like a terribly serious person - maybe steer away from 'plush-plush-tooshie-shake' though ;-)  It's important to pick one name and stick with it though!

## Issue Types
- `initiative` — the strategic "why": vision, goals, and key decisions behind a group of epics. Top-level only (no parent). When working through tasks, refer back to the initiative to resolve judgement calls.
- `epic` — container for related tasks. Can be top-level or a child of an initiative.
- `task` (default) — a unit of work. Child of an epic (or another task for subtasks).

## Priorities
- `P0` — critical / urgent
- `P1` — high priority
- `P2` — normal (default)
- `P3` — low priority
- `P4` — nice to have

## Hierarchical IDs

IDs are auto-generated with the project prefix:
- Root issue: `<prefix>-<sqid>` (e.g. `ait-AXs1i`)
- First child: `<prefix>-<sqid>.1` (e.g. `ait-AXs1i.1`)
- Grandchild: `<prefix>-<sqid>.1.1`

The parent-child structure is visible directly in the identifier. For a full
three-tier setup: `proj-abc` (initiative) -> `proj-abc.1` (epic) -> `proj-abc.1.1` (task).

## Workflow Pattern

1. **Start of session**: `ait ready` to see what is unblocked
2. **Pick work**: `ait claim <id> <your-name>` to claim an issue
3. **Check context**: `ait show <id>` for full details and notes. If the issue belongs to an initiative, read the initiative description to understand the strategic intent.
4. **Mark in progress**: `ait update <id> --status in_progress`
5. **Do the work**: implement, test, iterate
6. **Leave notes**: `ait note add <id> "what was done / what remains"`
7. **Close**: `ait close <id>` (or `--cascade` for an epic and its children)
8. **Next**: `ait ready` again to find the next unblocked item
9. **End of session**: `ait status` for an overall summary

## Output Modes

By default all commands return JSON — compact and token-efficient for agents.

- `--long` adds all fields (description, timestamps, claimed_by, etc.)
- `--human` gives a compact tabular view grouped by epic
- `--tree` shows parent-child hierarchy with tree connectors
- `--human` and `--tree` are mutually exclusive
- All display modes support the same filters (`--type`, `--status`, `--priority`)

## Initialisation

```bash
ait init --prefix myproject    # Set the project prefix for issue IDs
```
If no prefix is set, one is inferred from the directory name. The prefix can be
changed later with `init --prefix` — existing IDs are re-keyed automatically.

## Custom Database Path

```bash
ait --db /path/to/other.db list   # Use a different database file
```
Useful for git worktrees (pointing back to the main repo's database) or keeping
separate databases for different subsystems.

## Delegating Work

If you are supervising sub-agents or delegating work to agents that don't have
access to `ait`, see `DELEGATION.md` (in this skill directory) for the
export → delegate → reconcile workflow.

## Tips

- Prefer `ait ready` over `ait list` when deciding what to work on — it filters
  to unblocked issues and sorts by priority.
- Use notes liberally — they survive session loss and conversation compaction.
- Use `--cascade` on close to avoid closing children one by one.
- Run `ait flush` periodically to keep the database lean — the tracker is for
  ephemeral work, so there is no need to keep completed issues forever.
- `ait show <id>` returns children, blockers, and notes in one call — use it to
  get full context before starting work.
- The database lives at `.ait/ait.db` in the git root. It is a plain SQLite file
  and easy to inspect or back up.
