---
name: ait-recap
description: >
  Generate a friendly markdown recap of recent `ait` activity for the current project, or scan a
  directory of projects. Use when the user asks "what have I been doing this week?", "what
  shipped recently?", wants an aide-mémoire before a status update with a manager, or asks for a
  summary of recent activity across their projects.
allowed-tools: "Read,Bash(ait:*),Glob"
version: "0.1.0"
---

# AIT Recap

Build a friendly, human-readable markdown summary of recent activity from the `ait` issue tracker.
The report **is** the deliverable — present the markdown directly to the user.

## Resolve the request

Three things to pin down from the user's prompt:

### 1. Period (default: this week)
- "today" / "since this morning" → today's date
- "this week" / "last week" / no period given → 7 days ago
- "this month" / "last month" → 30 days ago
- Explicit date ("since 2026-04-01", "last fortnight") → use as-is

Convert to a `YYYY-MM-DD` for `ait log --since` and client-side timestamp filtering.

### 2. Mode (default: detailed)
- Prompt contains "summary", "summarise", "tl;dr", "high level", "quick", "brief" → **summary mode**
- Otherwise → **detailed mode**

### 3. Scope (default: current project)
- Prompt mentions a directory ("scan `~/Documents/code/`", "all my projects in X", "across everything") → **multi-project mode**
- Otherwise → **single project mode**

---

## Single-project mode

Run these queries:

```bash
ait config                            # project prefix
ait log --since YYYY-MM-DD --long     # flushed history in the period
ait list --all --long                 # all live issues (filter by date client-side)
```

For live issues, keep any with `created_at`, `updated_at` or `closed_at` >= the period start.

### Throwaway filter

Drop flushed items where **both**:
- `close_reason` is null or empty, **and**
- the item has no children (no other items in the same flush list have it as `parent_id`).

This silently culls test detritus while keeping deliberate-but-unnoted closes visible.

### Output template

```markdown
# This <Period> in `<prefix>` (<start-date> → <today>)

<one-line vibe — "A productive week.", "A quieter one.", "Mostly maintenance.">

## Highlights
- **<initiative or top-level epic title>** — <one-sentence "what shipped" distilled from close_reason>
- **<...>**
- **<...>**

## Currently in flight
- **<title>** (`<id>`, P<n>) — <status; latest note if useful>

## Detail
<!-- detailed mode only -->

### <Initiative title> (P<n>)
<n epics, n tasks>.

- **<epic title>** — <distilled close_reason>
- **<epic title>** — <distilled close_reason>

---
*Source: <n> flush events between <date> and <date>. <If excluded:> <n> throwaway issue(s) excluded.*
```

### Mode rules

- **Detailed mode**: include the "Detail" section with per-initiative breakdown.
- **Summary mode**: skip "Detail" entirely. Just Highlights + Currently in flight.
- **Currently in flight**: omit the section if no open/in-progress issues. Show priorities and IDs in detailed mode; collapse to a one-line count in summary mode if there are many (>5).
- **Empty period**: say so cheerfully — "Quiet week — nothing flushed, nothing in flight."

### Tone

- Friendly, conversational. Aide-mémoire vibe — "oh yeah, *that's* what I was doing."
- Distill close_reasons; don't quote them verbatim.
- No emoji.
- Test counts **only** if they signal a problem ("3 failing tests", "still need to write tests for X"). Don't trumpet "457 tests passing".
- Dates rough ("all on 2026-04-16", "shipped Wednesday") — not minute-precise.
- If a flush event has its own `summary` field, that's the user's curated editorial — lean on it for the headline.

---

## Multi-project mode

Find candidate databases with `Glob`:

```
<scan-dir>/**/.ait/ait.db
```

For each, run the queries with `--db <path>`:

```bash
ait --db <path> config
ait --db <path> log --since YYYY-MM-DD --long
ait --db <path> list --all --long
```

Skip projects with no activity in the period (no flushed items, no live issues touched in range).

### Output template

```markdown
# Recent Activity Across Projects (<start-date> → <today>)

<n> projects with activity in the period.

## Overall highlights
- **<prefix>**: <one-line of what shipped>
- **<prefix>**: <one-line of what shipped>

## Per-project

### `<prefix>` — `<path>`
<single-project body, without its own top-level heading>

### `<prefix>` — `<path>`
<...>
```

### Mode rules

- **Detailed mode**: include "Per-project" with each project's full single-project body (Highlights + Currently in flight + Detail).
- **Summary mode**: drop "Per-project" entirely — overall highlights only.
- **Empty scan**: "No activity across <n> projects in the period — must've been on holiday."

---

## Tips

- The user's `ait` agent name in this repo (and probably others) may already be set in memory — check before claiming any issues, but this skill is **read-only** so you shouldn't need to claim anything.
- If `ait config` returns no prefix, the project hasn't been initialised — flag it and move on.
- For multi-project mode against ~10+ projects, the raw JSON could grow context. If it gets unwieldy, summarise each project's data into the markdown line by line as you go, rather than holding it all at once.
