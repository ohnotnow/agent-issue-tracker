package ait

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	_ "modernc.org/sqlite"
)

type App struct {
	db *sql.DB
}

func Open(ctx context.Context, dbPath string) (*App, error) {
	if dbPath == "" {
		var err error
		dbPath, err = DatabasePath()
		if err != nil {
			return nil, err
		}
	}

	if dbPath == ":memory:" {
		// Keep all queries on a single connection so they share the same
		// in-memory database. Safe now that no code path calls a.db while
		// holding an open transaction.
	} else {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			return nil, err
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	if dbPath == ":memory:" {
		db.SetMaxOpenConns(1)
	}

	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON;`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `PRAGMA busy_timeout = 5000;`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode = WAL;`); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureSchema(ctx, db); err != nil {
		db.Close()
		return nil, err
	}

	return &App{db: db}, nil
}

func (a *App) Close() error {
	if a == nil || a.db == nil {
		return nil
	}
	return a.db.Close()
}

func issueSelectColumns(alias string) string {
	return fmt.Sprintf(
		`%s.public_id, %s.type, %s.title, %s.description, %s.status, parent.public_id, %s.priority, %s.claimed_by, %s.claimed_at, %s.created_at, %s.updated_at, %s.closed_at`,
		alias, alias, alias, alias, alias, alias, alias, alias, alias, alias, alias,
	)
}

func issueRefSelectColumns(alias string) string {
	return fmt.Sprintf(`%s.public_id, %s.title, %s.status, %s.type, %s.priority`, alias, alias, alias, alias, alias)
}

func (a *App) resolveIssueID(ctx context.Context, key string) (int64, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return 0, &CLIError{Code: "validation", Message: "issue id is required", ExitCode: 65}
	}

	var internalID int64
	row := a.db.QueryRowContext(ctx, `SELECT id FROM issues WHERE public_id = ?`, key)
	if err := row.Scan(&internalID); err == nil {
		return internalID, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	row = a.db.QueryRowContext(ctx, `SELECT id FROM issues WHERE legacy_id = ?`, key)
	if err := row.Scan(&internalID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, &CLIError{Code: "not_found", Message: fmt.Sprintf("issue %s not found", key), ExitCode: 66}
		}
		return 0, err
	}

	return internalID, nil
}

func (a *App) fetchIssue(ctx context.Context, key string) (Issue, error) {
	internalID, err := a.resolveIssueID(ctx, key)
	if err != nil {
		return Issue{}, err
	}
	return a.fetchIssueByInternalID(ctx, internalID)
}

func (a *App) fetchIssueByInternalID(ctx context.Context, internalID int64) (Issue, error) {
	row := a.db.QueryRowContext(
		ctx,
		fmt.Sprintf(
			`SELECT %s
			 FROM issues i
			 LEFT JOIN issues parent ON parent.id = i.parent_id
			 WHERE i.id = ?`,
			issueSelectColumns("i"),
		),
		internalID,
	)
	iss, err := scanIssue(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Issue{}, &CLIError{Code: "not_found", Message: fmt.Sprintf("issue %d not found", internalID), ExitCode: 66}
		}
		return Issue{}, err
	}
	return iss, nil
}

func (a *App) fetchIssueRef(ctx context.Context, key string) (IssueRef, error) {
	internalID, err := a.resolveIssueID(ctx, key)
	if err != nil {
		return IssueRef{}, err
	}
	return a.fetchIssueRefByInternalID(ctx, internalID)
}

func (a *App) fetchIssueRefByInternalID(ctx context.Context, internalID int64) (IssueRef, error) {
	row := a.db.QueryRowContext(
		ctx,
		fmt.Sprintf(`SELECT %s FROM issues i WHERE i.id = ?`, issueRefSelectColumns("i")),
		internalID,
	)
	var ref IssueRef
	if err := row.Scan(&ref.ID, &ref.Title, &ref.Status, &ref.Type, &ref.Priority); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return IssueRef{}, &CLIError{Code: "not_found", Message: fmt.Sprintf("issue %d not found", internalID), ExitCode: 66}
		}
		return IssueRef{}, err
	}
	return ref, nil
}

func (a *App) fetchChildren(ctx context.Context, parentID int64) ([]Issue, error) {
	return a.queryIssues(
		ctx,
		fmt.Sprintf(
			`SELECT %s
			 FROM issues i
			 LEFT JOIN issues parent ON parent.id = i.parent_id
			 WHERE i.parent_id = ?
			 ORDER BY i.created_at ASC`,
			issueSelectColumns("i"),
		),
		parentID,
	)
}

func (a *App) fetchBlockers(ctx context.Context, id int64) ([]IssueRef, error) {
	rows, err := a.db.QueryContext(
		ctx,
		fmt.Sprintf(
			`SELECT %s
		 FROM issue_dependencies d
		 JOIN issues i ON i.id = d.blocker_id
		 WHERE d.blocked_id = ?
		 ORDER BY i.created_at ASC`,
			issueRefSelectColumns("i"),
		),
		id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanIssueRefs(rows)
}

func (a *App) fetchBlocks(ctx context.Context, id int64) ([]IssueRef, error) {
	rows, err := a.db.QueryContext(
		ctx,
		fmt.Sprintf(
			`SELECT %s
		 FROM issue_dependencies d
		 JOIN issues i ON i.id = d.blocked_id
		 WHERE d.blocker_id = ?
		 ORDER BY i.created_at ASC`,
			issueRefSelectColumns("i"),
		),
		id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanIssueRefs(rows)
}

func (a *App) fetchNotes(ctx context.Context, issueID int64) ([]Note, error) {
	rows, err := a.db.QueryContext(
		ctx,
		`SELECT n.id, i.public_id, n.body, n.created_at
		 FROM issue_notes n
		 JOIN issues i ON i.id = n.issue_id
		 WHERE n.issue_id = ?
		 ORDER BY n.created_at ASC`,
		issueID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Note, 0)
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.IssueID, &n.Body, &n.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, n)
	}
	return items, rows.Err()
}

func (a *App) queryIssues(ctx context.Context, query string, params ...any) ([]Issue, error) {
	rows, err := a.db.QueryContext(ctx, query, params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Issue, 0)
	for rows.Next() {
		iss, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, iss)
	}
	return items, rows.Err()
}

func (a *App) queryIssueRefs(ctx context.Context, query string, params ...any) ([]IssueRef, error) {
	rows, err := a.db.QueryContext(ctx, query, params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanIssueRefs(rows)
}

func (a *App) readyIssues(ctx context.Context, typeFilter string) ([]Issue, error) {
	query := fmt.Sprintf(
		`SELECT %s
		 FROM issues i
		 LEFT JOIN issues parent ON parent.id = i.parent_id
		 WHERE i.status IN (?, ?)
		   AND NOT EXISTS (
		     SELECT 1
		     FROM issue_dependencies d
		     JOIN issues blockers ON blockers.id = d.blocker_id
		     WHERE d.blocked_id = i.id
		       AND blockers.status != ?
		   )`,
		issueSelectColumns("i"),
	)
	params := []any{StatusOpen, StatusInProgress, StatusClosed}
	if typeFilter != "" {
		query += ` AND i.type = ?`
		params = append(params, typeFilter)
	}
	query += ` ORDER BY i.priority ASC, i.created_at ASC`
	return a.queryIssues(ctx, query, params...)
}

func (a *App) readyIssueRefs(ctx context.Context, typeFilter string) ([]IssueRef, error) {
	query := fmt.Sprintf(
		`SELECT %s
		 FROM issues i
		 WHERE i.status IN (?, ?)
		   AND NOT EXISTS (
		     SELECT 1
		     FROM issue_dependencies d
		     JOIN issues blockers ON blockers.id = d.blocker_id
		     WHERE d.blocked_id = i.id
		       AND blockers.status != ?
		   )`,
		issueRefSelectColumns("i"),
	)
	params := []any{StatusOpen, StatusInProgress, StatusClosed}
	if typeFilter != "" {
		query += ` AND i.type = ?`
		params = append(params, typeFilter)
	}
	query += ` ORDER BY i.priority ASC, i.created_at ASC`
	return a.queryIssueRefs(ctx, query, params...)
}

func (a *App) fetchAllDescendants(ctx context.Context, parentID int64) ([]Issue, error) {
	children, err := a.fetchChildren(ctx, parentID)
	if err != nil {
		return nil, err
	}
	all := make([]Issue, 0, len(children))
	for _, child := range children {
		all = append(all, child)
		internalID, err := a.resolveIssueID(ctx, child.ID)
		if err != nil {
			return nil, err
		}
		grandchildren, err := a.fetchAllDescendants(ctx, internalID)
		if err != nil {
			return nil, err
		}
		all = append(all, grandchildren...)
	}
	return all, nil
}

func (a *App) validateParent(ctx context.Context, parentID string) error {
	_, err := a.fetchIssue(ctx, parentID)
	return err
}

func (a *App) hasDirectDependency(ctx context.Context, blockedID, blockerID int64) bool {
	row := a.db.QueryRowContext(
		ctx,
		`SELECT 1 FROM issue_dependencies WHERE blocked_id = ? AND blocker_id = ?`,
		blockedID,
		blockerID,
	)
	var found int
	return row.Scan(&found) == nil
}

func (a *App) isReachable(ctx context.Context, fromID, toID int64) (bool, error) {
	visited := map[int64]bool{fromID: true}
	queue := []int64{fromID}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		rows, err := a.db.QueryContext(ctx,
			`SELECT blocker_id FROM issue_dependencies WHERE blocked_id = ?`, current)
		if err != nil {
			return false, err
		}

		for rows.Next() {
			var blockerID int64
			if err := rows.Scan(&blockerID); err != nil {
				rows.Close()
				return false, err
			}
			if blockerID == toID {
				rows.Close()
				return true, nil
			}
			if !visited[blockerID] {
				visited[blockerID] = true
				queue = append(queue, blockerID)
			}
		}
		if err := rows.Close(); err != nil {
			return false, err
		}
	}
	return false, nil
}

func (a *App) buildDependencyTree(ctx context.Context, ref IssueRef, seen map[string]bool) (DependencyTree, error) {
	if seen[ref.ID] {
		return DependencyTree{
			Issue:  ref,
			Cycles: []string{ref.ID},
		}, nil
	}

	nextSeen := make(map[string]bool, len(seen)+1)
	for k, v := range seen {
		nextSeen[k] = v
	}
	nextSeen[ref.ID] = true

	internalID, err := a.resolveIssueID(ctx, ref.ID)
	if err != nil {
		return DependencyTree{}, err
	}

	blockers, err := a.fetchBlockers(ctx, internalID)
	if err != nil {
		return DependencyTree{}, err
	}

	tree := DependencyTree{Issue: ref, Blockers: make([]DependencyTree, 0)}
	for _, blocker := range blockers {
		child, err := a.buildDependencyTree(ctx, blocker, nextSeen)
		if err != nil {
			return DependencyTree{}, err
		}
		tree.Blockers = append(tree.Blockers, child)
	}
	return tree, nil
}

func DatabasePath() (string, error) {
	root, err := ProjectRoot()
	if err != nil {
		return "", err
	}

	return filepath.Join(root, ".ait", "ait.db"), nil
}

func ensureSchema(ctx context.Context, db *sql.DB) error {
	legacy, err := usesLegacyIssueSchema(ctx, db)
	if err != nil {
		return err
	}
	if legacy {
		if err := migrateLegacySchema(ctx, db); err != nil {
			return err
		}
	}

	if err := runMigrations(ctx, db); err != nil {
		return err
	}

	prefix, err := ensureProjectPrefix(ctx, db)
	if err != nil {
		return err
	}

	return syncPublicIDs(ctx, db, prefix, false)
}

func dependencyAlreadyExists(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "unique")
}

func usesLegacyIssueSchema(ctx context.Context, db *sql.DB) (bool, error) {
	if !tableExists(ctx, db, "issues") {
		return false, nil
	}

	rows, err := db.QueryContext(ctx, `PRAGMA table_info(issues)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	var hasPublicID bool
	var usesTextID bool

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			return false, err
		}
		if name == "public_id" {
			hasPublicID = true
		}
		if name == "id" && strings.EqualFold(columnType, "TEXT") {
			usesTextID = true
		}
	}

	if err := rows.Err(); err != nil {
		return false, err
	}

	return usesTextID && !hasPublicID, nil
}

func tableExists(ctx context.Context, db *sql.DB, name string) bool {
	row := db.QueryRowContext(ctx, `SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?`, name)
	var found int
	return row.Scan(&found) == nil
}

func migrateLegacySchema(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	statements := []string{
		`CREATE TABLE issues_v2 (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			legacy_id TEXT UNIQUE,
			public_id TEXT UNIQUE,
			type TEXT NOT NULL CHECK (type IN ('task', 'epic')),
			title TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL CHECK (status IN ('open', 'in_progress', 'closed', 'cancelled')),
			parent_id INTEGER NULL,
			priority TEXT NOT NULL DEFAULT 'P2' CHECK (priority IN ('P0', 'P1', 'P2', 'P3', 'P4')),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			closed_at TEXT NULL,
			FOREIGN KEY (parent_id) REFERENCES issues_v2(id)
		);`,
		`CREATE TABLE issue_dependencies_v2 (
			blocked_id INTEGER NOT NULL,
			blocker_id INTEGER NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY (blocked_id, blocker_id),
			FOREIGN KEY (blocked_id) REFERENCES issues_v2(id) ON DELETE CASCADE,
			FOREIGN KEY (blocker_id) REFERENCES issues_v2(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE issue_notes_v2 (
			id TEXT PRIMARY KEY,
			issue_id INTEGER NOT NULL,
			body TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY (issue_id) REFERENCES issues_v2(id) ON DELETE CASCADE
		);`,
	}

	for _, stmt := range statements {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}

	type pendingParent struct {
		internalID int64
		legacyID   string
	}

	legacyToInternal := make(map[string]int64)
	parentUpdates := make([]pendingParent, 0)

	rows, err := tx.QueryContext(
		ctx,
		`SELECT id, type, title, description, status, parent_id, priority, created_at, updated_at, closed_at
		 FROM issues
		 ORDER BY created_at ASC`,
	)
	if err != nil {
		return err
	}

	for rows.Next() {
		var (
			legacyID    string
			issueType   string
			title       string
			description string
			status      string
			parentID    sql.NullString
			priority    string
			createdAt   string
			updatedAt   string
			closedAt    sql.NullString
		)
		if err := rows.Scan(&legacyID, &issueType, &title, &description, &status, &parentID, &priority, &createdAt, &updatedAt, &closedAt); err != nil {
			rows.Close()
			return err
		}

		result, err := tx.ExecContext(
			ctx,
			`INSERT INTO issues_v2 (legacy_id, public_id, type, title, description, status, parent_id, priority, created_at, updated_at, closed_at)
			 VALUES (?, NULL, ?, ?, ?, ?, NULL, ?, ?, ?, ?)`,
			legacyID,
			issueType,
			title,
			description,
			status,
			priority,
			createdAt,
			updatedAt,
			closedAt,
		)
		if err != nil {
			rows.Close()
			return err
		}

		internalID, err := result.LastInsertId()
		if err != nil {
			rows.Close()
			return err
		}

		legacyToInternal[legacyID] = internalID
		if parentID.Valid {
			parentUpdates = append(parentUpdates, pendingParent{internalID: internalID, legacyID: parentID.String})
		}
	}

	if err := rows.Close(); err != nil {
		return err
	}

	for _, update := range parentUpdates {
		parentInternalID, ok := legacyToInternal[update.legacyID]
		if !ok {
			return fmt.Errorf("legacy parent %s was not found during migration", update.legacyID)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE issues_v2 SET parent_id = ? WHERE id = ?`, parentInternalID, update.internalID); err != nil {
			return err
		}
	}

	dependencyRows, err := tx.QueryContext(ctx, `SELECT blocked_id, blocker_id, created_at FROM issue_dependencies`)
	if err != nil {
		return err
	}
	for dependencyRows.Next() {
		var blockedLegacyID, blockerLegacyID, createdAt string
		if err := dependencyRows.Scan(&blockedLegacyID, &blockerLegacyID, &createdAt); err != nil {
			dependencyRows.Close()
			return err
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO issue_dependencies_v2 (blocked_id, blocker_id, created_at) VALUES (?, ?, ?)`,
			legacyToInternal[blockedLegacyID],
			legacyToInternal[blockerLegacyID],
			createdAt,
		); err != nil {
			dependencyRows.Close()
			return err
		}
	}
	if err := dependencyRows.Close(); err != nil {
		return err
	}

	noteRows, err := tx.QueryContext(ctx, `SELECT id, issue_id, body, created_at FROM issue_notes`)
	if err != nil {
		return err
	}
	for noteRows.Next() {
		var noteID, legacyIssueID, body, createdAt string
		if err := noteRows.Scan(&noteID, &legacyIssueID, &body, &createdAt); err != nil {
			noteRows.Close()
			return err
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO issue_notes_v2 (id, issue_id, body, created_at) VALUES (?, ?, ?, ?)`,
			noteID,
			legacyToInternal[legacyIssueID],
			body,
			createdAt,
		); err != nil {
			noteRows.Close()
			return err
		}
	}
	if err := noteRows.Close(); err != nil {
		return err
	}

	cleanupStatements := []string{
		`DROP TABLE issue_notes;`,
		`DROP TABLE issue_dependencies;`,
		`DROP TABLE issues;`,
		`ALTER TABLE issues_v2 RENAME TO issues;`,
		`ALTER TABLE issue_dependencies_v2 RENAME TO issue_dependencies;`,
		`ALTER TABLE issue_notes_v2 RENAME TO issue_notes;`,
	}

	for _, stmt := range cleanupStatements {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func syncPublicIDs(ctx context.Context, db *sql.DB, prefix string, forceRoot bool) error {
	type issueNode struct {
		id       int64
		parentID sql.NullInt64
		publicID sql.NullString
	}

	rows, err := db.QueryContext(
		ctx,
		`SELECT id, parent_id, public_id
		 FROM issues
		 ORDER BY created_at ASC, id ASC`,
	)
	if err != nil {
		return err
	}

	nodes := make(map[int64]issueNode)
	children := make(map[int64][]int64)
	roots := make([]int64, 0)

	for rows.Next() {
		var node issueNode
		if err := rows.Scan(&node.id, &node.parentID, &node.publicID); err != nil {
			rows.Close()
			return err
		}
		nodes[node.id] = node
		if node.parentID.Valid {
			children[node.parentID.Int64] = append(children[node.parentID.Int64], node.id)
		} else {
			roots = append(roots, node.id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var assign func(issueID int64, expected string) error
	assign = func(issueID int64, expected string) error {
		node := nodes[issueID]
		if !node.publicID.Valid || node.publicID.String != expected {
			if _, err := tx.ExecContext(ctx, `UPDATE issues SET public_id = ? WHERE id = ?`, expected, issueID); err != nil {
				return err
			}
			node.publicID = sql.NullString{String: expected, Valid: true}
			nodes[issueID] = node
		}

		for idx, childID := range children[issueID] {
			childPublicID := fmt.Sprintf("%s.%d", expected, idx+1)
			if err := assign(childID, childPublicID); err != nil {
				return err
			}
		}

		return nil
	}

	for _, issueID := range roots {
		node := nodes[issueID]
		expected := ""
		if !forceRoot && node.publicID.Valid && node.publicID.String != "" {
			expected = node.publicID.String
		} else {
			expected, err = RootPublicID(prefix, issueID)
			if err != nil {
				return err
			}
		}

		if err := assign(issueID, expected); err != nil {
			return err
		}
	}

	return tx.Commit()
}
