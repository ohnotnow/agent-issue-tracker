---
name: tag-release
description: >
  Get an ait release ready to tag. Checks the working tree, the latest tag and CHANGELOG.md,
  agrees the new version with the maintainer, checks it is a sensible next step (and not a
  burned tag name), moves [Unreleased] into a dated release entry, then prints the exact
  tag-and-push command for the maintainer to paste. Never runs git or gh writes itself. Use when
  the user says "tag a release", "ship a release", "cut a release", "pre-flight check",
  "tag-release", or asks to look over the repo before they cut a tag.
allowed-tools: "Read,Edit,Bash"
version: "0.3.0"
---

# tag-release

Pushing a `v*` tag runs `.github/workflows/release.yml`, which builds and publishes a GitHub
release. The repo has **immutable releases** switched on, so a published release can't be
changed, and once a tag name has been used for one, GitHub refuses to reuse it even after the
release and tag are deleted. A wrong tag permanently burns a version number. That is why this
skill prepares everything but leaves the tag itself to the maintainer.

**Never run `git commit`, `git tag`, `git push`, or `gh release ...`.** The only file this skill
edits is `CHANGELOG.md`, and only after the maintainer has agreed the version.

## 1. Gather the state

Run these (read-only):

- `git status -sb`: current branch, uncommitted or untracked files, and whether the branch is
  ahead of `origin` (the tag must point at a pushed commit).
- `git tag --sort=-v:refname | head -3`: most recent tags.
- `git log <latest-tag>..HEAD --oneline`: commits since the latest tag.
- `git ls-remote --tags origin`: tags on GitHub, in case one exists remotely but not locally.
- The top of `CHANGELOG.md`: the `[Unreleased]` block, the latest `## [X.Y.Z]` heading, and the
  compare links at the bottom of the file.

If there are untracked directories, peek inside (`git ls-files --others --exclude-standard
<path>`) so you can say what's there rather than just "something untracked".

## 2. Report and agree the version

A short report, skipping any section with nothing in it:

- **State**: branch, latest tag, commits since it (list them if 5 or fewer), and the
  `[Unreleased]` content (verbatim if short, summarised if long).
- **Flags**: one line each. For example: uncommitted changes that look like part of the release;
  `[Unreleased]` empty although there are commits since the last tag; not on `master`; branch
  ahead of `origin`; the latest tag has no changelog entry.
- **Proposed version**: suggest the next version from what changed (patch for fixes only, minor
  for new behaviour, major for breaking changes) and say why. Then **ask the maintainer to
  confirm it**. Don't go on until they have.

## 3. Check the version

Before touching the changelog, check the agreed version:

- It must be exactly one step on from the latest tag: `X.Y.(Z+1)`, `X.(Y+1).0` or `(X+1).0.0`.
  Anything else (say 1.17.0 to 1.80.0) is almost certainly a typo, so stop and ask.
- It must not already exist locally or on `origin`.
- It must not be in the burned list below.

### Burned tag names

These were used for immutable releases that were then deleted. GitHub refuses to publish them
again (the API error is `tag_name was used by an immutable release`).

| Tag | What happened | Use instead |
|---|---|---|
| `v1.80.0` | Typo for v1.18.0, tagged 2026-10-10. Deleted 2026-10-29 and re-released as `v1.18.0`. | `v1.80.1` or `v1.81.0` |

If a release is ever deleted again, add it to this table.

## 4. Update the changelog

Once the version is agreed and checked, edit `CHANGELOG.md`:

- Move the `[Unreleased]` content under a new `## [X.Y.Z] - YYYY-MM-DD` heading (today's date),
  leaving an empty `## [Unreleased]` above it.
- Update the compare links at the bottom: `[Unreleased]` compares `vX.Y.Z...HEAD`, and a new
  `[X.Y.Z]` line compares the previous tag with `vX.Y.Z`.

Show the maintainer the new entry.

## 5. Hand over the command

The changelog edit still needs committing and pushing before the tag. Say so in plain English,
then print the tag command with the version copied from the changelog heading, so nothing is
typed by hand:

```
git tag vX.Y.Z && git push origin vX.Y.Z
```

Recent tags are lightweight (no annotation). Release notes come from the changelog and GitHub's
generated notes, not from the tag.

## Notes

- Voice: matter-of-fact and brief. The maintainer knows git and `gh`; don't write out long
  command sequences beyond the one tag command.
- If the maintainer asks for something beyond this (say, editing release notes with
  `gh release edit`), that's fine, but only after they've explicitly asked in this session.
