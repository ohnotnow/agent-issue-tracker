package ait

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

func isHelpRequested(err error) bool {
	return errors.Is(err, flag.ErrHelp)
}

func (a *App) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		PrintHelp()
		return nil
	}

	cmd, ok := LookupCommand(args[0])
	if !ok {
		return &CLIError{
			Code:     "usage",
			Message:  fmt.Sprintf("unknown command %q", args[0]),
			ExitCode: 64,
		}
	}
	return cmd.Run(a, ctx, args[1:])
}

func (a *App) runInit(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	prefix := fs.String("prefix", "", "")
	fs.SetOutput(io.Discard)

	if err := fs.Parse(args); err != nil {
		if isHelpRequested(err) {
			PrintCommandHelp("init")
			return nil
		}
		return &CLIError{Code: "usage", Message: err.Error(), ExitCode: 64}
	}

	var (
		resolved string
		err      error
	)

	if strings.TrimSpace(*prefix) == "" {
		resolved, err = a.ensureProjectPrefix(ctx)
	} else {
		resolved, err = a.setProjectPrefix(ctx, *prefix)
	}
	if err != nil {
		return err
	}

	if err := syncPublicIDs(ctx, a.db, resolved, strings.TrimSpace(*prefix) != ""); err != nil {
		return err
	}

	return PrintJSON(map[string]any{"prefix": resolved})
}

func (a *App) runConfig(ctx context.Context) error {
	prefix, err := a.ensureProjectPrefix(ctx)
	if err != nil {
		return err
	}

	var version int
	err = a.db.QueryRowContext(ctx, `SELECT version FROM schema_version WHERE id = 1`).Scan(&version)
	if err != nil {
		return err
	}

	return PrintJSON(map[string]any{
		"prefix":         prefix,
		"schema_version": version,
	})
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
		if isHelpRequested(err) {
			PrintCommandHelp("create")
			return nil
		}
		return &CLIError{Code: "usage", Message: err.Error(), ExitCode: 64}
	}

	if strings.TrimSpace(*title) == "" {
		return &CLIError{Code: "validation", Message: "title is required", ExitCode: 65}
	}
	resolved, err := ResolveDescription(*description)
	if err != nil {
		return err
	}
	*description = resolved
	if err := ValidateIssueType(*issueType); err != nil {
		return err
	}
	if err := ValidatePriority(*priority); err != nil {
		return err
	}
	var parent *string
	var parentInternalID any
	if strings.TrimSpace(*parentID) != "" {
		parent = parentID
		parentIssue, err := a.fetchIssue(ctx, *parent)
		if err != nil {
			return err
		}
		if err := ValidateParentType(*issueType, parentIssue.Type); err != nil {
			return err
		}
		resolvedParentID, err := a.resolveIssueID(ctx, *parent)
		if err != nil {
			return err
		}
		parentInternalID = resolvedParentID
	}

	prefix, err := a.ensureProjectPrefix(ctx)
	if err != nil {
		return err
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

	var publicID string
	if parentInternalID == nil {
		publicID, err = RootPublicID(prefix, internalID)
		if err != nil {
			return err
		}
	} else {
		var siblingCount int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM issues WHERE parent_id = ?`, parentInternalID).Scan(&siblingCount); err != nil {
			return err
		}

		var parentPublicID string
		if err := tx.QueryRowContext(ctx, `SELECT public_id FROM issues WHERE id = ?`, parentInternalID).Scan(&parentPublicID); err != nil {
			return err
		}
		publicID = fmt.Sprintf("%s.%d", parentPublicID, siblingCount)
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
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		PrintCommandHelp("show")
		return nil
	}
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
	long := fs.Bool("long", false, "")
	human := fs.Bool("human", false, "")
	tree := fs.Bool("tree", false, "")
	fs.SetOutput(io.Discard)

	if err := fs.Parse(args); err != nil {
		if isHelpRequested(err) {
			PrintCommandHelp("list")
			return nil
		}
		return &CLIError{Code: "usage", Message: err.Error(), ExitCode: 64}
	}
	if *human && *tree {
		return &CLIError{Code: "usage", Message: "--human and --tree are mutually exclusive", ExitCode: 64}
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

	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}

	if *human || *tree {
		query := fmt.Sprintf(
			`SELECT %s FROM issues i LEFT JOIN issues parent ON parent.id = i.parent_id%s ORDER BY i.created_at ASC`,
			issueSelectColumns("i"), where,
		)
		items, err := a.queryIssues(ctx, query, params...)
		if err != nil {
			return err
		}
		if *human {
			fmt.Print(FormatHumanList(items))
		} else {
			fmt.Print(FormatTreeList(items))
		}
		return nil
	}

	if *long {
		query := fmt.Sprintf(
			`SELECT %s FROM issues i LEFT JOIN issues parent ON parent.id = i.parent_id%s ORDER BY i.created_at ASC`,
			issueSelectColumns("i"), where,
		)
		items, err := a.queryIssues(ctx, query, params...)
		if err != nil {
			return err
		}
		return PrintJSON(map[string]any{"issues": items})
	}

	query := fmt.Sprintf(
		`SELECT %s FROM issues i%s ORDER BY i.created_at ASC`,
		issueRefSelectColumns("i"), where,
	)
	items, err := a.queryIssueRefs(ctx, query, params...)
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

	readyItems, err := a.readyIssueRefs(ctx, "")
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
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		PrintCommandHelp("search")
		return nil
	}
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
			 WHERE i.title LIKE ? COLLATE NOCASE OR i.description LIKE ? COLLATE NOCASE
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
	if args[0] == "--help" || args[0] == "-h" {
		PrintCommandHelp("update")
		return nil
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

	if *description != "" {
		resolved, err := ResolveDescription(*description)
		if err != nil {
			return err
		}
		*description = resolved
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
		if current.Type == "initiative" {
			return &CLIError{Code: "validation", Message: "initiatives cannot have a parent", ExitCode: 65}
		}
		return &CLIError{Code: "validation", Message: "changing parent is not supported once hierarchical ids are enabled", ExitCode: 65}
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
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		PrintCommandHelp(CommandNameForStatus(nextStatus))
		return nil
	}
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

func (a *App) runClaim(ctx context.Context, args []string) error {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		PrintCommandHelp("claim")
		return nil
	}
	if len(args) != 2 {
		return &CLIError{Code: "usage", Message: "usage: ait claim <id> <agent-name>", ExitCode: 64}
	}
	internalID, err := a.resolveIssueID(ctx, args[0])
	if err != nil {
		return err
	}
	agentName := strings.TrimSpace(args[1])
	if agentName == "" {
		return &CLIError{Code: "validation", Message: "agent name is required", ExitCode: 65}
	}

	current, err := a.fetchIssueByInternalID(ctx, internalID)
	if err != nil {
		return err
	}
	if current.ClaimedBy != nil {
		return &CLIError{
			Code:     "conflict",
			Message:  fmt.Sprintf("issue %s is already claimed by %s", current.ID, *current.ClaimedBy),
			ExitCode: 65,
		}
	}

	now := NowUTC()
	_, err = a.db.ExecContext(ctx,
		`UPDATE issues SET claimed_by = ?, claimed_at = ?, updated_at = ? WHERE id = ?`,
		agentName, now, now, internalID,
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

func (a *App) runUnclaim(ctx context.Context, args []string) error {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		PrintCommandHelp("unclaim")
		return nil
	}
	if len(args) != 1 {
		return &CLIError{Code: "usage", Message: "usage: ait unclaim <id>", ExitCode: 64}
	}
	internalID, err := a.resolveIssueID(ctx, args[0])
	if err != nil {
		return err
	}

	now := NowUTC()
	_, err = a.db.ExecContext(ctx,
		`UPDATE issues SET claimed_by = NULL, claimed_at = NULL, updated_at = ? WHERE id = ?`,
		now, internalID,
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

func (a *App) runClose(ctx context.Context, args []string) error {
	// Extract --cascade and --reason from anywhere in the args since
	// flag.Parse stops at the first non-flag argument and the ID is positional.
	cascade := false
	var reason string
	var filtered []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--help" || arg == "-h" {
			PrintCommandHelp("close")
			return nil
		}
		if arg == "--cascade" {
			cascade = true
		} else if arg == "--reason" && i+1 < len(args) {
			reason = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "--reason=") {
			reason = strings.TrimPrefix(arg, "--reason=")
		} else {
			filtered = append(filtered, arg)
		}
	}

	if len(filtered) != 1 {
		return &CLIError{Code: "usage", Message: "usage: ait close <id> [--cascade] [--reason <text>]", ExitCode: 64}
	}

	// If --reason was given, add a note before closing.
	if strings.TrimSpace(reason) != "" {
		if err := a.runNoteAdd(ctx, []string{filtered[0], "Closed: " + reason}); err != nil {
			return err
		}
	}

	if cascade {
		return a.runCascadeClose(ctx, filtered[0])
	}
	return a.runStatusChange(ctx, filtered, StatusClosed)
}

func (a *App) runCascadeClose(ctx context.Context, key string) error {
	internalID, err := a.resolveIssueID(ctx, key)
	if err != nil {
		return err
	}

	now := NowUTC()
	closed := make([]IssueRef, 0)

	var closeTree func(id int64) error
	closeTree = func(id int64) error {
		issue, err := a.fetchIssueByInternalID(ctx, id)
		if err != nil {
			return err
		}

		// Only close issues that are open or in_progress.
		if issue.Status == StatusOpen || issue.Status == StatusInProgress {
			if _, err := a.db.ExecContext(ctx,
				`UPDATE issues SET status = ?, updated_at = ?, closed_at = ? WHERE id = ?`,
				StatusClosed, now, now, id,
			); err != nil {
				return err
			}
			closed = append(closed, IssueRef{
				ID:       issue.ID,
				Title:    issue.Title,
				Status:   StatusClosed,
				Type:     issue.Type,
				Priority: issue.Priority,
			})
		}

		// Recurse into children.
		rows, err := a.db.QueryContext(ctx, `SELECT id FROM issues WHERE parent_id = ?`, id)
		if err != nil {
			return err
		}
		var childIDs []int64
		for rows.Next() {
			var childID int64
			if err := rows.Scan(&childID); err != nil {
				rows.Close()
				return err
			}
			childIDs = append(childIDs, childID)
		}
		if err := rows.Close(); err != nil {
			return err
		}

		for _, childID := range childIDs {
			if err := closeTree(childID); err != nil {
				return err
			}
		}
		return nil
	}

	if err := closeTree(internalID); err != nil {
		return err
	}

	return PrintJSON(map[string]any{"closed": closed})
}

func (a *App) runReopen(ctx context.Context, args []string) error {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		PrintCommandHelp("reopen")
		return nil
	}
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

func (a *App) runReady(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("ready", flag.ContinueOnError)
	long := fs.Bool("long", false, "")
	issueType := fs.String("type", "", "")
	fs.SetOutput(io.Discard)

	if err := fs.Parse(args); err != nil {
		if isHelpRequested(err) {
			PrintCommandHelp("ready")
			return nil
		}
		return &CLIError{Code: "usage", Message: err.Error(), ExitCode: 64}
	}
	if *issueType != "" {
		if err := ValidateIssueType(*issueType); err != nil {
			return err
		}
	}

	if *long {
		items, err := a.readyIssues(ctx, *issueType)
		if err != nil {
			return err
		}
		return PrintJSON(map[string]any{"issues": items})
	}

	items, err := a.readyIssueRefs(ctx, *issueType)
	if err != nil {
		return err
	}
	return PrintJSON(map[string]any{"issues": items})
}

func (a *App) runDependency(ctx context.Context, args []string) error {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		PrintCommandHelp("dep")
		return nil
	}
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
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		PrintCommandHelp("dep add")
		return nil
	}
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

	if blockedInternalID == blockerInternalID {
		return &CLIError{Code: "validation", Message: "an issue cannot depend on itself", ExitCode: 65}
	}
	wouldCycle, err := a.isReachable(ctx, blockerInternalID, blockedInternalID)
	if err != nil {
		return err
	}
	if wouldCycle {
		return &CLIError{Code: "validation", Message: "adding this dependency would create a cycle", ExitCode: 65}
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
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		PrintCommandHelp("dep remove")
		return nil
	}
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
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		PrintCommandHelp("dep list")
		return nil
	}
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
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		PrintCommandHelp("dep tree")
		return nil
	}
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
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		PrintCommandHelp("note")
		return nil
	}
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
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		PrintCommandHelp("note add")
		return nil
	}
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
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		PrintCommandHelp("note list")
		return nil
	}
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

func (a *App) runExport(ctx context.Context, args []string) error {
	// Extract --output from anywhere in args since the ID is positional.
	var outputPath string
	var filtered []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--help" || args[i] == "-h" {
			PrintCommandHelp("export")
			return nil
		}
		if args[i] == "--output" && i+1 < len(args) {
			outputPath = args[i+1]
			i++ // skip value
		} else if strings.HasPrefix(args[i], "--output=") {
			outputPath = strings.TrimPrefix(args[i], "--output=")
		} else {
			filtered = append(filtered, args[i])
		}
	}

	if len(filtered) != 1 {
		return &CLIError{Code: "usage", Message: "usage: ait export <id> [--output path.md]", ExitCode: 64}
	}

	internalID, err := a.resolveIssueID(ctx, filtered[0])
	if err != nil {
		return err
	}

	root, err := a.fetchIssueByInternalID(ctx, internalID)
	if err != nil {
		return err
	}

	descendants, err := a.fetchAllDescendants(ctx, internalID)
	if err != nil {
		return err
	}

	// Build notes and blockers maps for root + all descendants
	notesMap := make(map[string][]Note)
	blockersMap := make(map[string][]IssueRef)

	allIssues := append([]Issue{root}, descendants...)
	for _, iss := range allIssues {
		issInternalID, err := a.resolveIssueID(ctx, iss.ID)
		if err != nil {
			return err
		}
		notes, err := a.fetchNotes(ctx, issInternalID)
		if err != nil {
			return err
		}
		if len(notes) > 0 {
			notesMap[iss.ID] = notes
		}
		blockers, err := a.fetchBlockers(ctx, issInternalID)
		if err != nil {
			return err
		}
		if len(blockers) > 0 {
			blockersMap[iss.ID] = blockers
		}
	}

	md := FormatMarkdownExport(root, descendants, notesMap, blockersMap)

	if strings.TrimSpace(outputPath) != "" {
		return os.WriteFile(outputPath, []byte(md), 0o644)
	}

	fmt.Print(md)
	return nil
}

func (a *App) runFlush(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("flush", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "")
	summary := fs.String("summary", "", "")
	fs.SetOutput(io.Discard)

	if err := fs.Parse(args); err != nil {
		if isHelpRequested(err) {
			PrintCommandHelp("flush")
			return nil
		}
		return &CLIError{Code: "usage", Message: err.Error(), ExitCode: 64}
	}

	result, err := a.flushTerminalIssues(ctx, *dryRun, *summary)
	if err != nil {
		return err
	}

	return PrintJSON(result)
}

func (a *App) runLog(ctx context.Context, args []string) error {
	// Check for subcommands before flag parsing.
	if len(args) > 0 && args[0] == "purge" {
		return a.runLogPurge(ctx, args[1:])
	}
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		PrintCommandHelp("log")
		return nil
	}

	fs := flag.NewFlagSet("log", flag.ContinueOnError)
	last := fs.Int("last", 0, "")
	since := fs.String("since", "", "")
	long := fs.Bool("long", false, "")
	search := fs.String("search", "", "")
	fs.SetOutput(io.Discard)

	if err := fs.Parse(args); err != nil {
		if isHelpRequested(err) {
			PrintCommandHelp("log")
			return nil
		}
		return &CLIError{Code: "usage", Message: err.Error(), ExitCode: 64}
	}

	entries, err := a.fetchFlushHistory(ctx, *last, *since)
	if err != nil {
		return err
	}

	// Apply search filter if set.
	if strings.TrimSpace(*search) != "" {
		term := strings.ToLower(strings.TrimSpace(*search))
		var filtered []FlushHistoryEntry
		for _, e := range entries {
			var matchingItems []FlushHistoryItem
			for _, item := range e.Items {
				if strings.Contains(strings.ToLower(item.Title), term) ||
					strings.Contains(strings.ToLower(item.CloseReason), term) {
					matchingItems = append(matchingItems, item)
				}
			}
			if len(matchingItems) > 0 {
				e.Items = matchingItems
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	if *long {
		if entries == nil {
			entries = make([]FlushHistoryEntry, 0)
		}
		return PrintJSON(entries)
	}

	// Slim output: root items only (unless searching), with item count.
	summaries := make([]FlushHistoryEntrySummary, 0, len(entries))
	isSearching := strings.TrimSpace(*search) != ""
	for _, e := range entries {
		s := FlushHistoryEntrySummary{
			ID:        e.ID,
			Summary:   e.Summary,
			FlushedAt: e.FlushedAt,
			ItemCount: len(e.Items),
			Items:     make([]FlushHistoryItemRef, 0),
		}
		for _, item := range e.Items {
			if isSearching || item.ParentPublicID == nil {
				s.Items = append(s.Items, FlushHistoryItemRef{
					PublicID:  item.PublicID,
					Type:      item.Type,
					Title:     item.Title,
					Priority:  item.Priority,
				})
			}
		}
		summaries = append(summaries, s)
	}
	return PrintJSON(summaries)
}

func (a *App) runLogPurge(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("log purge", flag.ContinueOnError)
	before := fs.String("before", "", "")
	keep := fs.Int("keep", 0, "")
	full := fs.Bool("full", false, "")
	fs.SetOutput(io.Discard)

	if err := fs.Parse(args); err != nil {
		if isHelpRequested(err) {
			PrintCommandHelp("log purge")
			return nil
		}
		return &CLIError{Code: "usage", Message: err.Error(), ExitCode: 64}
	}

	result, err := a.purgeFlushHistory(ctx, *before, *keep, *full)
	if err != nil {
		return err
	}

	return PrintJSON(result)
}

