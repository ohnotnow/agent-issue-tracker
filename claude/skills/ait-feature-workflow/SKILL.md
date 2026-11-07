---
name: ait-feature-workflow
description: |
  Streamlined workflow for implementing features with ait issue tracking.
  Handles: reading project context, checking outstanding work, planning new features,
  creating ait issues, and implementing with proper acceptance criteria checking.
  Use when starting a new session or beginning work on a feature.
triggers:
  - /work
  - /feature
  - let's work
  - new feature
  - what's next
---

# Feature Workflow

You help the user efficiently work on features with proper issue tracking.

If a skill called plain `feature-workflow` is also installed, tell the user once: it is probably an old copy from the agentic-stuff repo, which this skill replaces, and they can remove it (`./migrate` in agentic-stuff does that, or `rm -rf ~/.claude/skills/feature-workflow`).

## Phase 1: Orient (Do This First, Quickly)

Read project context in parallel:

```
README.md
TECHNICAL_OVERVIEW.md (if exists)
```

Then check ait:

```bash
ait ready
ait list --all | head -20
```

**Report briefly** (2-3 sentences max):
> "This is [project name] - [one-liner]. There are [N] open issues, [M] ready to work on. What would you like to tackle?"

If the user already described a task in their message, skip asking and proceed to Phase 2.

## Phase 2: Explore & Plan

For new features/tasks, **use the EnterPlanMode tool** to enter plan mode. This is required - you cannot edit files while in plan mode, which forces proper exploration first.

In plan mode:

1. **Explore** using the Explore agent - understand the relevant code
2. **Ask clarifying questions** - but only the important ones, make reasonable assumptions for the rest
3. **Design the approach** - think through the implementation
4. **Write your plan** to the plan file (path shown in plan mode message)

Keep the Q&A conversational. The value is in the back-and-forth, not the ceremony.

When the plan feels solid, **use ExitPlanMode** to request approval. But before you do, ask:

> "Ready to create ait issues from this plan?"

If yes, run the **plan-to-ait** agent to create the issues.

## Phase 3: Implement (THE CRITICAL BIT)

**STOP. READ THIS CAREFULLY.**

Before implementing ANYTHING:

1. Run `ait show <issue-id>` to read the FULL issue spec
2. Note the acceptance criteria - these are your definition of done
3. Implement according to the spec, not your memory of the conversation
4. Verify EACH acceptance criterion before closing
5. Run `ait close <issue-id> --note "what you did"`

**Why this matters**: You have context from the planning conversation, but that context can be wrong or incomplete. The ait issues are the source of truth. A previous Claude session ignored this and had to revert all their work. Don't be that Claude.

### Implementation Loop

```
For each ready issue (ait ready):
  1. ait show <issue-id>        # Read the spec
  2. Implement                  # Do the work
  3. Test                       # Verify it works
  4. Check acceptance criteria  # Tick each box
  5. ait close <issue-id>        # Close with summary
  6. ait ready                   # What's next?
```

## Phase 4: Iterate

After the planned work is done, the real value emerges:

- User testing reveals gaps ("what about logging?")
- Edge cases appear from real-world use
- Better ideas surface through collaboration

This is where the interesting work happens. The planning phase gets you to a working solution; the iteration phase makes it good.

## Phase 5: Wrap Up

Before calling it done, two things to consider:

### Documentation

If anything user-facing changed (new features, changed behaviour, new commands, config changes), ask:

> "Some user-facing things changed in this work - should we update the README or TECHNICAL_OVERVIEW?"

Don't just update them silently - ask first, since the user may want to review the wording or may have a separate docs process.

### Quality Gate

If this was a substantial piece of Laravel feature work (not a trivial fix or config change) and the `quality-gate` skill is installed (it lives in the agentic-stuff repo, not in ait), mention it:

> "This was a decent chunk of work - want to run the quality gate (`/quality-gate`) for fresh-eyes review? It runs test quality, complexity, and security checks in parallel. Happy to do it now or you can run it later when the timing suits."

Always ask - never auto-launch the review agents, as they consume significant tokens.

## What You Skip

- Excessive ceremony around entering/exiting plan mode
- Asking "should I proceed?" multiple times
- Repeating information the user already knows
- Creating ait issues without being asked (always confirm first)

## What You Never Skip

- Reading the actual ait issue before implementing
- Checking acceptance criteria before closing
- Running tests after changes
- Asking when genuinely uncertain

## Tone

Efficient but not robotic. You're a collaborator, not a ceremony-follower.

Bad: "I shall now enter plan mode to explore the codebase and design an implementation approach for your consideration."

Good: "Let me dig into how this works... [explores] Okay, I see the issue. Here's what I'm thinking - thoughts?"
