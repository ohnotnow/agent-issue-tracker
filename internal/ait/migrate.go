package ait

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// migration is a forward-only schema change. Each migration receives
// a transaction and must apply exactly one version step.
type migration struct {
	version     int
	description string
	apply       func(ctx context.Context, tx *sql.Tx) error
}

// migrations is the ordered list of all schema migrations. The baseline
// (version 1) creates the initial tables. Future changes append here.
var migrations = []migration{
	{
		version:     1,
		description: "baseline schema",
		apply: func(ctx context.Context, tx *sql.Tx) error {
			statements := []string{
				`CREATE TABLE IF NOT EXISTS issues (
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
					FOREIGN KEY (parent_id) REFERENCES issues(id)
				);`,
				`CREATE TABLE IF NOT EXISTS issue_dependencies (
					blocked_id INTEGER NOT NULL,
					blocker_id INTEGER NOT NULL,
					created_at TEXT NOT NULL,
					PRIMARY KEY (blocked_id, blocker_id),
					FOREIGN KEY (blocked_id) REFERENCES issues(id) ON DELETE CASCADE,
					FOREIGN KEY (blocker_id) REFERENCES issues(id) ON DELETE CASCADE
				);`,
				`CREATE TABLE IF NOT EXISTS issue_notes (
					id TEXT PRIMARY KEY,
					issue_id INTEGER NOT NULL,
					body TEXT NOT NULL,
					created_at TEXT NOT NULL,
					FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
				);`,
				`CREATE TABLE IF NOT EXISTS project_config (
					id INTEGER PRIMARY KEY CHECK (id = 1),
					prefix TEXT NOT NULL,
					updated_at TEXT NOT NULL
				);`,
				`CREATE UNIQUE INDEX IF NOT EXISTS idx_issues_public_id ON issues(public_id);`,
				`CREATE UNIQUE INDEX IF NOT EXISTS idx_issues_legacy_id ON issues(legacy_id) WHERE legacy_id IS NOT NULL;`,
				`CREATE INDEX IF NOT EXISTS idx_issues_status ON issues(status);`,
				`CREATE INDEX IF NOT EXISTS idx_issues_parent_id ON issues(parent_id);`,
				`CREATE INDEX IF NOT EXISTS idx_issue_dependencies_blocker_id ON issue_dependencies(blocker_id);`,
				`CREATE INDEX IF NOT EXISTS idx_issue_notes_issue_id ON issue_notes(issue_id);`,
			}
			for _, stmt := range statements {
				if _, err := tx.ExecContext(ctx, stmt); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		version:     2,
		description: "add issue claiming columns",
		apply: func(ctx context.Context, tx *sql.Tx) error {
			statements := []string{
				`ALTER TABLE issues ADD COLUMN claimed_by TEXT NULL;`,
				`ALTER TABLE issues ADD COLUMN claimed_at TEXT NULL;`,
			}
			for _, stmt := range statements {
				if _, err := tx.ExecContext(ctx, stmt); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		version:     3,
		description: "add initiative issue type",
		apply: func(ctx context.Context, tx *sql.Tx) error {
			// SQLite cannot ALTER CHECK constraints, so we rebuild the
			// issues table. To avoid FK-reference rewriting problems
			// during renames, we back up all three related tables, drop
			// them, recreate with the updated schema, and restore data.
			statements := []string{
				// Back up existing data
				`CREATE TABLE _issues_backup AS SELECT * FROM issues;`,
				`CREATE TABLE _deps_backup AS SELECT * FROM issue_dependencies;`,
				`CREATE TABLE _notes_backup AS SELECT * FROM issue_notes;`,

				// Drop in dependency order
				`DROP TABLE issue_notes;`,
				`DROP TABLE issue_dependencies;`,
				`DROP TABLE issues;`,

				// Recreate issues with initiative in the CHECK constraint
				`CREATE TABLE issues (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					legacy_id TEXT UNIQUE,
					public_id TEXT UNIQUE,
					type TEXT NOT NULL CHECK (type IN ('task', 'epic', 'initiative')),
					title TEXT NOT NULL,
					description TEXT NOT NULL DEFAULT '',
					status TEXT NOT NULL CHECK (status IN ('open', 'in_progress', 'closed', 'cancelled')),
					parent_id INTEGER NULL,
					priority TEXT NOT NULL DEFAULT 'P2' CHECK (priority IN ('P0', 'P1', 'P2', 'P3', 'P4')),
					created_at TEXT NOT NULL,
					updated_at TEXT NOT NULL,
					closed_at TEXT NULL,
					claimed_by TEXT NULL,
					claimed_at TEXT NULL,
					FOREIGN KEY (parent_id) REFERENCES issues(id)
				);`,
				`INSERT INTO issues SELECT * FROM _issues_backup;`,

				// Recreate dependent tables
				`CREATE TABLE issue_dependencies (
					blocked_id INTEGER NOT NULL,
					blocker_id INTEGER NOT NULL,
					created_at TEXT NOT NULL,
					PRIMARY KEY (blocked_id, blocker_id),
					FOREIGN KEY (blocked_id) REFERENCES issues(id) ON DELETE CASCADE,
					FOREIGN KEY (blocker_id) REFERENCES issues(id) ON DELETE CASCADE
				);`,
				`INSERT INTO issue_dependencies SELECT * FROM _deps_backup;`,

				`CREATE TABLE issue_notes (
					id TEXT PRIMARY KEY,
					issue_id INTEGER NOT NULL,
					body TEXT NOT NULL,
					created_at TEXT NOT NULL,
					FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
				);`,
				`INSERT INTO issue_notes SELECT * FROM _notes_backup;`,

				// Clean up backups
				`DROP TABLE _issues_backup;`,
				`DROP TABLE _deps_backup;`,
				`DROP TABLE _notes_backup;`,

				// Recreate indexes
				`CREATE UNIQUE INDEX idx_issues_public_id ON issues(public_id);`,
				`CREATE UNIQUE INDEX idx_issues_legacy_id ON issues(legacy_id) WHERE legacy_id IS NOT NULL;`,
				`CREATE INDEX idx_issues_status ON issues(status);`,
				`CREATE INDEX idx_issues_parent_id ON issues(parent_id);`,
				`CREATE INDEX idx_issue_dependencies_blocker_id ON issue_dependencies(blocker_id);`,
				`CREATE INDEX idx_issue_notes_issue_id ON issue_notes(issue_id);`,
			}
			for _, stmt := range statements {
				if _, err := tx.ExecContext(ctx, stmt); err != nil {
					return fmt.Errorf("migration 3: %w", err)
				}
			}
			return nil
		},
	},
}

// currentSchemaVersion returns the version recorded in schema_version,
// or 0 if the table does not exist or has no row yet.
func currentSchemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	if !tableExists(ctx, db, "schema_version") {
		// If the issues table already exists, this is a pre-migration
		// database that is already at the baseline schema (version 1).
		if tableExists(ctx, db, "issues") {
			return 1, nil
		}
		return 0, nil
	}
	var version int
	err := db.QueryRowContext(ctx, `SELECT version FROM schema_version WHERE id = 1`).Scan(&version)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return version, nil
}

// setSchemaVersion records the current schema version. Called outside
// the migration transaction so it works with both fresh and existing DBs.
func setSchemaVersion(ctx context.Context, db *sql.DB, version int) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO schema_version (id, version, updated_at)
		 VALUES (1, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET version = excluded.version, updated_at = excluded.updated_at`,
		version, NowUTC(),
	)
	return err
}

// runMigrations applies all pending migrations in order. Each migration
// runs in its own transaction for isolation.
func runMigrations(ctx context.Context, db *sql.DB) error {
	// Ensure the schema_version table exists before anything else.
	if _, err := db.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS schema_version (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			version INTEGER NOT NULL,
			updated_at TEXT NOT NULL
		);`,
	); err != nil {
		return err
	}

	current, err := currentSchemaVersion(ctx, db)
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("migration %d (%s): begin tx: %w", m.version, m.description, err)
		}
		if err := m.apply(ctx, tx); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d (%s): %w", m.version, m.description, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migration %d (%s): commit: %w", m.version, m.description, err)
		}
	}

	// Record the final version (handles both fresh DB and upgrades).
	latest := migrations[len(migrations)-1].version
	if latest > current {
		if err := setSchemaVersion(ctx, db, latest); err != nil {
			return err
		}
	}

	return nil
}
