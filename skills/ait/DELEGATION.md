# Delegating Work with AIT Export

This guide covers how a supervisor agent can delegate structured work to another
agent (or human) that doesn't have access to `ait`. The Markdown export is the
contract — the receiver doesn't need the `ait` binary or any knowledge of the
tracker.

## When to Use This

Use the export-delegate-reconcile pattern any time work crosses a context
boundary:

- Sub-agents in git worktrees
- Background agents in the same repo but a separate context
- Remote or cloud-based agents
- Human collaborators

The common thread is that the receiver shouldn't couple to your tracker.

## The Workflow

### 1. Export

Generate a Markdown briefing for the epic (or task) you want to delegate:

```bash
ait export <id> --output briefing.md
```

This produces a self-contained Markdown file with:
- The epic title, ID, priority, and description
- A checklist of tasks (`[ ]` open, `[x]` closed, `[-]` cancelled)
- Dependencies and notes for each issue
- A summary with counts

### 2. Delegate

Hand the Markdown file to the sub-agent. How you do this depends on the
context:

- **Worktree agent**: place `briefing.md` in the worktree root and tell the
  agent to work through it
- **Background agent**: pass the file path as part of the agent's prompt
- **Remote agent**: include the Markdown content in the prompt or upload it
- **Human**: share the file via git, email, or any other channel

Tell the receiver: *"Work through the tasks in `briefing.md`. Tick each
checkbox as you complete it."*

### 3. Sub-Agent Works

The sub-agent reads the Markdown, does the work, and ticks checkboxes:

```markdown
- [x] **First task** (`ait-abc12.1`) — P1
- [ ] **Second task** (`ait-abc12.2`) — P2
```

No `ait` commands needed. The sub-agent can also add notes or comments inline
if useful.

### 4. Reconcile

Once the sub-agent finishes (or you review its work), read the updated
Markdown and update the central tracker:

- **All tasks completed**: use cascade close on the epic
  ```bash
  ait close <epic-id> --cascade
  ```
- **Partial completion**: close individual tasks that were ticked off
  ```bash
  ait close <task-id>
  ```
- **Notes worth preserving**: add them to the tracker
  ```bash
  ait note add <id> "Completed by sub-agent — see PR #123"
  ```

## Worked Example

A supervisor has an epic `ait-vEmzD` with two tasks. The full cycle:

```bash
# 1. Export the epic
ait export ait-vEmzD --output briefing.md

# 2. Delegate — launch a sub-agent in a worktree with the briefing
#    (the mechanism depends on your agent framework)

# 3. Sub-agent works through briefing.md, ticking boxes...

# 4. Reconcile — sub-agent finished, all tasks ticked
ait close ait-vEmzD --cascade

# Or if only some tasks were completed:
ait close ait-vEmzD.1
ait note add ait-vEmzD.2 "Partially done — needs error handling added"
```

## Tips

- Export just before delegating so the Markdown reflects current state.
- For large epics, the sub-agent can focus on a subset — just tell it which
  tasks to prioritise.
- The issue IDs in the Markdown make reconciliation straightforward — you can
  match ticked items back to tracker IDs directly.
- If the sub-agent creates new work (discovered tasks, follow-ups), capture
  those as new issues in the tracker during reconciliation.
