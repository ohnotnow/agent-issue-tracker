package ait

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"
)

// Guards against blind writes by coding agents. Two independent checks:
//
//   - The read check: an agent running under Claude Code (which sets
//     CLAUDE_CODE_SESSION_ID in the environment of every command) may only
//     rewrite or close-with-note an issue it has `show`n in the same session
//     within ShownWindow. Humans in a plain terminal have no session id and
//     are never checked. Override with SkipReadCheckFlag.
//   - The shrink check: a new description less than half the length of the
//     existing one is refused unless --force is given, on the theory that a
//     diff or a summary was sent by mistake. Growing a description is never
//     questioned; that is the common case.

// ShownWindow is how long a `show` counts as having read the issue. Context
// compaction keeps the same session id, so a stale show must expire.
const ShownWindow = time.Hour

// SkipReadCheckFlag is deliberately long: typing it is an admission that the
// caller is editing something it has not read.
const SkipReadCheckFlag = "--dangerously-skip-read-check"

// ShrinkGuardMinLength is the existing-description length below which the
// shrink check does not apply; short bodies get rewritten wholesale all the time.
const ShrinkGuardMinLength = 200

const sessionEnvVar = "CLAUDE_CODE_SESSION_ID"

func currentSessionID() string {
	return strings.TrimSpace(os.Getenv(sessionEnvVar))
}

// markShown records that the current session has displayed the issue. A run
// with no session id records nothing.
func (a *App) markShown(ctx context.Context, internalID int64) error {
	session := currentSessionID()
	if session == "" {
		return nil
	}
	_, err := a.db.ExecContext(ctx,
		`UPDATE issues SET shown_by_session = ?, shown_at = ? WHERE id = ?`,
		session, NowUTC(), internalID)
	return err
}

// checkShown refuses the write unless the issue was shown in this session
// within ShownWindow. skip is the value of SkipReadCheckFlag.
func (a *App) checkShown(ctx context.Context, internalID int64, publicID string, skip bool) error {
	session := currentSessionID()
	if session == "" || skip {
		return nil
	}
	var shownBy, shownAt sql.NullString
	if err := a.db.QueryRowContext(ctx,
		`SELECT shown_by_session, shown_at FROM issues WHERE id = ?`, internalID,
	).Scan(&shownBy, &shownAt); err != nil {
		return err
	}

	var why string
	switch {
	case !shownBy.Valid || !shownAt.Valid:
		why = "it has never been shown in this session"
	case shownBy.String != session:
		why = "it was last shown by a different session"
	default:
		t, err := time.Parse(time.RFC3339, shownAt.String)
		if err != nil {
			return fmt.Errorf("parse shown_at %q: %w", shownAt.String, err)
		}
		age := time.Since(t)
		if age <= ShownWindow {
			return nil
		}
		why = fmt.Sprintf("this session last showed it %s ago, more than %s", age.Round(time.Minute), ShownWindow)
	}
	return &CLIError{
		Code: "unread",
		Message: fmt.Sprintf(
			"refusing to write to %s: %s. Run 'ait show %s' first and read it; if you genuinely know what you are overwriting, pass %s.",
			publicID, why, publicID, SkipReadCheckFlag),
		ExitCode: 65,
	}
}

// checkDescriptionShrink refuses a replacement description that is less than
// half the length of the existing one, unless force is set.
func checkDescriptionShrink(publicID, existing, replacement string, force bool) error {
	oldLen := len(strings.TrimSpace(existing))
	newLen := len(strings.TrimSpace(replacement))
	if force || oldLen < ShrinkGuardMinLength || newLen*2 >= oldLen {
		return nil
	}
	return &CLIError{
		Code: "shrink",
		Message: fmt.Sprintf(
			"refusing to replace the description of %s: the new text is %d characters, the existing one is %d. That looks like a diff or a summary rather than the whole body. If this is a deliberate rewrite, pass --force.",
			publicID, newLen, oldLen),
		ExitCode: 65,
	}
}
