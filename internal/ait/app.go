package ait

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
)

func (a *App) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return &CLIError{
			Code:     "usage",
			Message:  "missing command",
			ExitCode: 64,
		}
	}

	switch args[0] {
	case "create":
		return a.runCreate(ctx, args[1:])
	case "show":
		return a.runShow(ctx, args[1:])
	case "list":
		return a.runList(ctx, args[1:])
	case "status":
		return a.runStatus(ctx)
	case "search":
		return a.runSearch(ctx, args[1:])
	case "update":
		return a.runUpdate(ctx, args[1:])
	case "close":
		return a.runStatusChange(ctx, args[1:], StatusClosed)
	case "reopen":
		return a.runReopen(ctx, args[1:])
	case "cancel":
		return a.runStatusChange(ctx, args[1:], StatusCancelled)
	case "ready":
		return a.runReady(ctx)
	case "dep":
		return a.runDependency(ctx, args[1:])
	case "note":
		return a.runNote(ctx, args[1:])
	default:
		return &CLIError{
			Code:     "usage",
			Message:  fmt.Sprintf("unknown command %q", args[0]),
			ExitCode: 64,
		}
	}
}

func (a *App) runCreate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	title := fs.String("title", "", "")
	description := fs.String("description", "", "")
	issueType := fs.String("type", "task", "")
	parentID := fs.String("parent", "", "")
	priority := fs.String("priority", "P2", "")
	fs.SetOutput(io.Discard)

	if err := fs.Parse(args); err != nil {
		return &CLIError{Code: "usage", Message: err.Error(), ExitCode: 64}
	}

	if strings.TrimSpace(*title) == "" {
		return &CLIError{Code: "validation", Message: "title is required", ExitCode: 65}
	}
	if err := ValidateIssueType(*issueType); err != nil {
		return err
	}
	if err := ValidatePriority(*priority); err != nil {
		return err
	}
	if *issueType == "epic" && strings.TrimSpace(*parentID) != "" {
		return &CLIError{Code: "validation", Message: "epics cannot have a parent", ExitCode: 65}
	}

	var parent *string
	var parentInternalID any
	if strings.TrimSpace(*parentID) != "" {
		parent = parentID
		if err := a.validateParent(ctx, *parent); err != nil {
			return err
		}
		resolvedParentID, err := a.resolveIssueID(ctx, *parent)
		if err != nil {
			return err
		}
		parentInternalID = resolvedParentID
	}

	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := NowUTC()
	result, err := tx.ExecContext(
		ctx,
		`INSERT INTO issues (public_id, type, title, description, status, parent_id, priority, created_at, updated_at, closed_at)
		 VALUES (NULL, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
		*issueType,
		strings.TrimSpace(*title),
		strings.TrimSpace(*description),
		StatusOpen,
		parentInternalID,
		*priority,
		now,
		now,
	)
	if err != nil {
		return err
	}

	internalID, err := result.LastInsertId()
	if err != nil {
		return err
	}

	publicID, err := PublicIDFromInternalID(internalID)
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `UPDATE issues SET public_id = ? WHERE id = ?`, publicID, internalID); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	created, err := a.fetchIssueByInternalID(ctx, internalID)
	if err != nil {
		return err
	}

	return PrintJSON(created)
}

func (a *App) runShow(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return &CLIError{Code: "usage", Message: "usage: ait show <id>", ExitCode: 64}
	}

	internalID, err := a.resolveIssueID(ctx, args[0])
	if err != nil {
		return err
	}

	iss, err := a.fetchIssueByInternalID(ctx, internalID)
	if err != nil {
		return err
	}
	children, err := a.fetchChildren(ctx, internalID)
	if err != nil {
		return err
	}
	blockers, err := a.fetchBlockers(ctx, internalID)
	if err != nil {
		return err
	}
	blocks, err := a.fetchBlocks(ctx, internalID)
	if err != nil {
		return err
	}
	notes, err := a.fetchNotes(ctx, internalID)
	if err != nil {
		return err
	}

	return PrintJSON(ShowResponse{
		Issue:    iss,
		Children: children,
		Blockers: blockers,
		Blocks:   blocks,
		Notes:    notes,
	})
}

func (a *App) runList(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	includeAll := fs.Bool("all", false, "")
	parentID := fs.String("parent", "", "")
	status := fs.String("status", "", "")
	issueType := fs.String("type", "", "")
	priority := fs.String("priority", "", "")
	fs.SetOutput(io.Discard)

	if err := fs.Parse(args); err != nil {
		return &CLIError{Code: "usage", Message: err.Error(), ExitCode: 64}
	}
	if *status != "" {
		if err := ValidateStatus(*status); err != nil {
			return err
		}
	}
	if *issueType != "" {
		if err := ValidateIssueType(*issueType); err != nil {
			return err
		}
	}
	if *priority != "" {
		if err := ValidatePriority(*priority); err != nil {
			return err
		}
	}

	query := fmt.Sprintf(
		`SELECT %s
		 FROM issues i
		 LEFT JOIN issues parent ON parent.id = i.parent_id`,
		issueSelectColumns("i"),
	)
	var clauses []string
	var params []any

	if !*includeAll && *status == "" {
		clauses = append(clauses, "i.status != ? AND i.status != ?")
		params = append(params, StatusClosed, StatusCancelled)
	}
	if *parentID != "" {
		resolvedParentID, err := a.resolveIssueID(ctx, *parentID)
		if err != nil {
			return err
		}
		clauses = append(clauses, "i.parent_id = ?")
		params = append(params, resolvedParentID)
	}
	if *status != "" {
		clauses = append(clauses, "i.status = ?")
		params = append(params, *status)
	}
	if *issueType != "" {
		clauses = append(clauses, "i.type = ?")
		params = append(params, *issueType)
	}
	if *priority != "" {
		clauses = append(clauses, "i.priority = ?")
		params = append(params, *priority)
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY i.created_at ASC"

	items, err := a.queryIssues(ctx, query, params...)
	if err != nil {
		return err
	}

	return PrintJSON(map[string]any{"issues": items})
}

func (a *App) runStatus(ctx context.Context) error {
	row := a.db.QueryRowContext(
		ctx,
		`SELECT
		 COUNT(*) AS total,
		 COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS open_count,
		 COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS in_progress_count,
		 COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS closed_count,
		 COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS cancelled_count
		 FROM issues`,
		StatusOpen,
		StatusInProgress,
		StatusClosed,
		StatusCancelled,
	)

	var total, openCount, inProgressCount, closedCount, cancelledCount int
	if err := row.Scan(&total, &openCount, &inProgressCount, &closedCount, &cancelledCount); err != nil {
		return err
	}

	readyItems, err := a.readyIssues(ctx)
	if err != nil {
		return err
	}

	return PrintJSON(map[string]any{
		"counts": map[string]int{
			"total":       total,
			"open":        openCount,
			"in_progress": inProgressCount,
			"closed":      closedCount,
			"cancelled":   cancelledCount,
			"ready":       len(readyItems),
		},
	})
}

func (a *App) runSearch(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return &CLIError{Code: "usage", Message: "usage: ait search <keyword>", ExitCode: 64}
	}
	needle := "%" + args[0] + "%"

	items, err := a.queryIssues(
		ctx,
		fmt.Sprintf(
			`SELECT %s
			 FROM issues i
			 LEFT JOIN issues parent ON parent.id = i.parent_id
			 WHERE i.title LIKE ? OR i.description LIKE ?
			 ORDER BY i.created_at ASC`,
			issueSelectColumns("i"),
		),
		needle,
		needle,
	)
	if err != nil {
		return err
	}

	return PrintJSON(map[string]any{"issues": items})
}

func (a *App) runUpdate(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return &CLIError{Code: "usage", Message: "usage: ait update <id> [flags]", ExitCode: 64}
	}
	id := args[0]
	internalID, err := a.resolveIssueID(ctx, id)
	if err != nil {
		return err
	}

	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	title := fs.String("title", "", "")
	description := fs.String("description", "", "")
	status := fs.String("status", "", "")
	parentID := fs.String("parent", "", "")
	priority := fs.String("priority", "", "")
	fs.SetOutput(io.Discard)

	if err := fs.Parse(args[1:]); err != nil {
		return &CLIError{Code: "usage", Message: err.Error(), ExitCode: 64}
	}

	current, err := a.fetchIssueByInternalID(ctx, internalID)
	if err != nil {
		return err
	}
	if *status != "" {
		if err := ValidateStatus(*status); err != nil {
			return err
		}
		if err := ValidateTransition(current.Status, *status); err != nil {
			return err
		}
	}
	if *priority != "" {
		if err := ValidatePriority(*priority); err != nil {
			return err
		}
	}
	if *parentID != "" {
		if current.Type == "epic" {
			return &CLIError{Code: "validation", Message: "epics cannot have a parent", ExitCode: 65}
		}
		if err := a.validateParent(ctx, *parentID); err != nil {
			return err
		}
	}

	var sets []string
	var params []any

	if *title != "" {
		sets = append(sets, "title = ?")
		params = append(params, strings.TrimSpace(*title))
	}
	if *description != "" {
		sets = append(sets, "description = ?")
		params = append(params, strings.TrimSpace(*description))
	}
	if *status != "" {
		sets = append(sets, "status = ?")
		params = append(params, *status)
		if *status == StatusClosed {
			sets = append(sets, "closed_at = ?")
			params = append(params, NowUTC())
		} else {
			sets = append(sets, "closed_at = NULL")
		}
	}
	if *parentID != "" {
		sets = append(sets, "parent_id = ?")
		parentInternalID, err := a.resolveIssueID(ctx, *parentID)
		if err != nil {
			return err
		}
		params = append(params, parentInternalID)
	}
	if *priority != "" {
		sets = append(sets, "priority = ?")
		params = append(params, *priority)
	}
	if len(sets) == 0 {
		return &CLIError{Code: "validation", Message: "no fields were provided to update", ExitCode: 65}
	}

	sets = append(sets, "updated_at = ?")
	params = append(params, NowUTC(), internalID)

	query := "UPDATE issues SET " + strings.Join(sets, ", ") + " WHERE id = ?"
	result, err := a.db.ExecContext(ctx, query, params...)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return &CLIError{Code: "not_found", Message: fmt.Sprintf("issue %s not found", id), ExitCode: 66}
	}

	updated, err := a.fetchIssueByInternalID(ctx, internalID)
	if err != nil {
		return err
	}

	return PrintJSON(updated)
}

func (a *App) runStatusChange(ctx context.Context, args []string, nextStatus string) error {
	if len(args) != 1 {
		return &CLIError{Code: "usage", Message: fmt.Sprintf("usage: ait %s <id>", CommandNameForStatus(nextStatus)), ExitCode: 64}
	}
	internalID, err := a.resolveIssueID(ctx, args[0])
	if err != nil {
		return err
	}
	current, err := a.fetchIssueByInternalID(ctx, internalID)
	if err != nil {
		return err
	}
	if err := ValidateTransition(current.Status, nextStatus); err != nil {
		return err
	}

	closedAt := any(nil)
	if nextStatus == StatusClosed {
		closedAt = NowUTC()
	}

	_, err = a.db.ExecContext(
		ctx,
		`UPDATE issues SET status = ?, updated_at = ?, closed_at = ? WHERE id = ?`,
		nextStatus,
		NowUTC(),
		closedAt,
		internalID,
	)
	if err != nil {
		return err
	}

	updated, err := a.fetchIssueByInternalID(ctx, internalID)
	if err != nil {
		return err
	}
	return PrintJSON(updated)
}

func (a *App) runReopen(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return &CLIError{Code: "usage", Message: "usage: ait reopen <id>", ExitCode: 64}
	}
	internalID, err := a.resolveIssueID(ctx, args[0])
	if err != nil {
		return err
	}
	current, err := a.fetchIssueByInternalID(ctx, internalID)
	if err != nil {
		return err
	}
	if err := ValidateTransition(current.Status, StatusOpen); err != nil {
		return err
	}

	_, err = a.db.ExecContext(
		ctx,
		`UPDATE issues SET status = ?, updated_at = ?, closed_at = NULL WHERE id = ?`,
		StatusOpen,
		NowUTC(),
		internalID,
	)
	if err != nil {
		return err
	}

	updated, err := a.fetchIssueByInternalID(ctx, internalID)
	if err != nil {
		return err
	}
	return PrintJSON(updated)
}

func (a *App) runReady(ctx context.Context) error {
	items, err := a.readyIssues(ctx)
	if err != nil {
		return err
	}
	return PrintJSON(map[string]any{"issues": items})
}

func (a *App) runDependency(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return &CLIError{Code: "usage", Message: "usage: ait dep <add|remove|list|tree> ...", ExitCode: 64}
	}

	switch args[0] {
	case "add":
		return a.runDepAdd(ctx, args[1:])
	case "remove":
		return a.runDepRemove(ctx, args[1:])
	case "list":
		return a.runDepList(ctx, args[1:])
	case "tree":
		return a.runDepTree(ctx, args[1:])
	default:
		return &CLIError{Code: "usage", Message: fmt.Sprintf("unknown dep subcommand %q", args[0]), ExitCode: 64}
	}
}

func (a *App) runDepAdd(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return &CLIError{Code: "usage", Message: "usage: ait dep add <blocked-id> <blocker-id>", ExitCode: 64}
	}
	blockedID := args[0]
	blockerID := args[1]

	blockedInternalID, err := a.resolveIssueID(ctx, blockedID)
	if err != nil {
		return err
	}
	blockerInternalID, err := a.resolveIssueID(ctx, blockerID)
	if err != nil {
		return err
	}

	if blockedID == blockerID {
		return &CLIError{Code: "validation", Message: "an issue cannot depend on itself", ExitCode: 65}
	}
	if blockedInternalID == blockerInternalID {
		return &CLIError{Code: "validation", Message: "an issue cannot depend on itself", ExitCode: 65}
	}
	if a.hasDirectDependency(ctx, blockerInternalID, blockedInternalID) {
		return &CLIError{Code: "validation", Message: "direct reciprocal dependencies are not allowed", ExitCode: 65}
	}

	_, err = a.db.ExecContext(
		ctx,
		`INSERT INTO issue_dependencies (blocked_id, blocker_id, created_at) VALUES (?, ?, ?)`,
		blockedInternalID,
		blockerInternalID,
		NowUTC(),
	)
	if err != nil {
		if dependencyAlreadyExists(err) {
			return &CLIError{Code: "validation", Message: "dependency already exists", ExitCode: 65}
		}
		return err
	}

	return a.runDepList(ctx, []string{blockedID})
}

func (a *App) runDepRemove(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return &CLIError{Code: "usage", Message: "usage: ait dep remove <blocked-id> <blocker-id>", ExitCode: 64}
	}

	blockedInternalID, err := a.resolveIssueID(ctx, args[0])
	if err != nil {
		return err
	}
	blockerInternalID, err := a.resolveIssueID(ctx, args[1])
	if err != nil {
		return err
	}

	result, err := a.db.ExecContext(
		ctx,
		`DELETE FROM issue_dependencies WHERE blocked_id = ? AND blocker_id = ?`,
		blockedInternalID,
		blockerInternalID,
	)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return &CLIError{Code: "not_found", Message: "dependency not found", ExitCode: 66}
	}

	return a.runDepList(ctx, []string{args[0]})
}

func (a *App) runDepList(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return &CLIError{Code: "usage", Message: "usage: ait dep list <id>", ExitCode: 64}
	}
	internalID, err := a.resolveIssueID(ctx, args[0])
	if err != nil {
		return err
	}
	issue, err := a.fetchIssueByInternalID(ctx, internalID)
	if err != nil {
		return err
	}

	blockers, err := a.fetchBlockers(ctx, internalID)
	if err != nil {
		return err
	}
	blocks, err := a.fetchBlocks(ctx, internalID)
	if err != nil {
		return err
	}

	return PrintJSON(map[string]any{
		"issue_id": issue.ID,
		"blockers": blockers,
		"blocks":   blocks,
	})
}

func (a *App) runDepTree(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return &CLIError{Code: "usage", Message: "usage: ait dep tree <id>", ExitCode: 64}
	}
	ref, err := a.fetchIssueRef(ctx, args[0])
	if err != nil {
		return err
	}

	tree, err := a.buildDependencyTree(ctx, ref, map[string]bool{})
	if err != nil {
		return err
	}

	return PrintJSON(tree)
}

func (a *App) runNote(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return &CLIError{Code: "usage", Message: "usage: ait note <add|list> ...", ExitCode: 64}
	}

	switch args[0] {
	case "add":
		return a.runNoteAdd(ctx, args[1:])
	case "list":
		return a.runNoteList(ctx, args[1:])
	default:
		return &CLIError{Code: "usage", Message: fmt.Sprintf("unknown note subcommand %q", args[0]), ExitCode: 64}
	}
}

func (a *App) runNoteAdd(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return &CLIError{Code: "usage", Message: "usage: ait note add <id> <body>", ExitCode: 64}
	}
	internalID, err := a.resolveIssueID(ctx, args[0])
	if err != nil {
		return err
	}
	issue, err := a.fetchIssueByInternalID(ctx, internalID)
	if err != nil {
		return err
	}
	body := strings.TrimSpace(args[1])
	if body == "" {
		return &CLIError{Code: "validation", Message: "note body is required", ExitCode: 65}
	}

	noteID, err := NewID()
	if err != nil {
		return err
	}
	createdAt := NowUTC()

	_, err = a.db.ExecContext(
		ctx,
		`INSERT INTO issue_notes (id, issue_id, body, created_at) VALUES (?, ?, ?, ?)`,
		noteID,
		internalID,
		body,
		createdAt,
	)
	if err != nil {
		return err
	}

	_, err = a.db.ExecContext(ctx, `UPDATE issues SET updated_at = ? WHERE id = ?`, NowUTC(), internalID)
	if err != nil {
		return err
	}

	return PrintJSON(Note{
		ID:        noteID,
		IssueID:   issue.ID,
		Body:      body,
		CreatedAt: createdAt,
	})
}

func (a *App) runNoteList(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return &CLIError{Code: "usage", Message: "usage: ait note list <id>", ExitCode: 64}
	}
	internalID, err := a.resolveIssueID(ctx, args[0])
	if err != nil {
		return err
	}
	issue, err := a.fetchIssueByInternalID(ctx, internalID)
	if err != nil {
		return err
	}

	items, err := a.fetchNotes(ctx, internalID)
	if err != nil {
		return err
	}

	return PrintJSON(map[string]any{"issue_id": issue.ID, "notes": items})
}
