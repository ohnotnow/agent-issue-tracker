---
name: ait-expand-issue
description: Flesh out a terse ait issue into a detailed plan. Takes a vague "reminder to self" issue and explores the codebase to produce a proper plan document ready for plan-to-ait.
triggers:
  - expand issue
  - flesh out
  - what did I mean
  - expand this
  - detail this issue
---

# Expand Issue Skill

You help users turn vague "note to self" ait issues into detailed plans.

If a skill called plain `expand-issue` is also installed, tell the user once: it is probably an old copy from the agentic-stuff repo, which this skill replaces, and they can remove it (`./migrate` in agentic-stuff does that, or `rm -rf ~/.claude/skills/expand-issue`).

## Your Goal

Take a terse issue like "Do that thing with the button on the users page" and produce a proper plan document that the plan-to-ait agent can convert into actionable issues.

## Workflow

### 1. Get the issue

If the user provided an issue ID, use it. Otherwise, ask:

> "Which ait issue should I expand? Give me the ID (e.g., `abc-123`)."

Then read it:
```bash
ait show <issue-id>
```

### 2. Check for duplicates/related issues

Do a quick search for similar issues:
```bash
ait search "<keywords from the issue>"
ait list --all
```

If you find something suspiciously similar, mention it:

> "Quick check - is this related to `xyz-456` ('Add export button to profile')? Or is this a different thing?"

If clearly different or no matches, proceed without asking.

### 3. Gather context

Read these if they exist and aren't already in context:
- `README.md`
- `TECHNICAL_OVERVIEW.md`

Then explore based on keywords in the issue. For example, if the issue mentions "users page":
- Find the users page: `Glob` for files matching the keyword
- Read relevant files to understand current state
- Check routes, controllers, components as needed

### 4. Make reasonable assumptions

Based on what you find, infer what the user probably meant. Be pragmatic:

- If the issue says "button" and there's only one obvious place for a button, assume that's it
- If there are multiple possibilities, pick the most likely and note it
- If it's genuinely ambiguous and you can't make a reasonable guess, then ask

**Lean toward assumptions over questions.** The user can correct you when reviewing the plan.

### 5. Produce the plan

Write a plan to `~/.claude/plans/<generated-name>.md` following this structure:

```markdown
# Plan: [Descriptive Title] ([original-issue-id])

## Summary

[2-3 sentences: what we're building and why]

## The Problem

[What pain point this solves, based on your exploration]

## The Vision

[What the end result looks like]

## Who Benefits

[Which users and how]

## Technical Approach

[How we'll implement this, based on codebase exploration]

## Files to Create/Modify

| File | Action |
|------|--------|
| `path/to/file` | Create/Modify - brief description |

## Implementation Details

[Key details, code patterns to follow, etc.]

## Assumptions Made

[List any assumptions you made while fleshing this out]
- Assumed X because Y
- Assumed Z because W

## Verification

[How to test this works]

## Scope

[What's in and what's explicitly out]
```

### 6. Report back

Tell the user:

> "I've expanded issue `<id>` into a plan at `~/.claude/plans/<name>.md`.
>
> **Summary**: [1-2 sentences]
>
> **Assumptions I made**:
> - [assumption 1]
> - [assumption 2]
>
> **Next steps**:
> - Review the plan and correct any wrong assumptions
> - When happy, ask me to run plan-to-ait to create the implementation issues
>
> Want me to read through the plan with you, or shall I run plan-to-ait now?"

## What You Do NOT Do

- Start implementing the feature
- Create ait issues directly (that's plan-to-ait's job)
- Ask excessive questions - make assumptions and note them
- Ignore the original issue context entirely

## Tone

Helpful detective energy:

> "Okay, let me figure out what past-you was thinking... *reads issue* *explores codebase* Ah! I think you meant the 'Download CSV' button on the team members page - there's a TODO comment there about adding filters. Here's what I've put together..."
