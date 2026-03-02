package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-issue-tracker/internal/ait"
	_ "modernc.org/sqlite"
)

func TestStatusInitializesEmptyDatabase(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var payload map[string]map[string]int
		runJSONCommand(t, a, []string{"status"}, &payload)

		counts := payload["counts"]
		if counts["total"] != 0 {
			t.Fatalf("expected total=0, got %d", counts["total"])
		}
		if counts["ready"] != 0 {
			t.Fatalf("expected ready=0, got %d", counts["ready"])
		}

		dbPath := mustDatabasePath(t)
		if _, err := os.Stat(dbPath); err != nil {
			t.Fatalf("expected database to exist at %s: %v", dbPath, err)
		}
	})
}

func TestCreateAndShowIssue(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var created ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Bootstrap CLI", "--description", "Implement first version"}, &created)

		if !strings.HasPrefix(created.ID, "ait-") {
			t.Fatalf("expected public issue id, got %s", created.ID)
		}
		if created.Title != "Bootstrap CLI" {
			t.Fatalf("unexpected title: %s", created.Title)
		}
		if created.Status != ait.StatusOpen {
			t.Fatalf("expected status %s, got %s", ait.StatusOpen, created.Status)
		}

		var shown ait.ShowResponse
		runJSONCommand(t, a, []string{"show", created.ID}, &shown)

		if shown.Issue.ID != created.ID {
			t.Fatalf("expected show to return issue %s, got %s", created.ID, shown.Issue.ID)
		}
		if len(shown.Children) != 0 {
			t.Fatalf("expected no children, got %d", len(shown.Children))
		}
		if len(shown.Notes) != 0 {
			t.Fatalf("expected no notes, got %d", len(shown.Notes))
		}
	})
}

func TestOpenMigratesLegacyIDsToPublicKeys(t *testing.T) {
	tmpDir := t.TempDir()
	restoreCWD := withWorkingDir(t, tmpDir)
	defer restoreCWD()

	dbPath := mustDatabasePath(t)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db failed: %v", err)
	}

	legacyStatements := []string{
		`CREATE TABLE issues (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL CHECK (type IN ('task', 'epic')),
			title TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL CHECK (status IN ('open', 'in_progress', 'closed', 'cancelled')),
			parent_id TEXT NULL,
			priority TEXT NOT NULL DEFAULT 'P2' CHECK (priority IN ('P0', 'P1', 'P2', 'P3', 'P4')),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			closed_at TEXT NULL,
			FOREIGN KEY (parent_id) REFERENCES issues(id)
		);`,
		`CREATE TABLE issue_dependencies (
			blocked_id TEXT NOT NULL,
			blocker_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY (blocked_id, blocker_id),
			FOREIGN KEY (blocked_id) REFERENCES issues(id) ON DELETE CASCADE,
			FOREIGN KEY (blocker_id) REFERENCES issues(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE issue_notes (
			id TEXT PRIMARY KEY,
			issue_id TEXT NOT NULL,
			body TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
		);`,
		`INSERT INTO issues (id, type, title, description, status, parent_id, priority, created_at, updated_at, closed_at)
		 VALUES ('legacy-epic', 'epic', 'Legacy Epic', 'Old schema parent', 'open', NULL, 'P1', '2026-03-01T10:00:00Z', '2026-03-01T10:00:00Z', NULL);`,
		`INSERT INTO issues (id, type, title, description, status, parent_id, priority, created_at, updated_at, closed_at)
		 VALUES ('legacy-task', 'task', 'Legacy Task', 'Old schema child', 'open', 'legacy-epic', 'P2', '2026-03-01T10:05:00Z', '2026-03-01T10:05:00Z', NULL);`,
		`INSERT INTO issue_notes (id, issue_id, body, created_at)
		 VALUES ('note-1', 'legacy-task', 'Migrated note', '2026-03-01T10:06:00Z');`,
	}

	for _, stmt := range legacyStatements {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed legacy schema failed: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db failed: %v", err)
	}

	app, err := ait.Open(context.Background())
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer app.Close()

	var shown ait.ShowResponse
	runJSONCommand(t, app, []string{"show", "legacy-task"}, &shown)

	if !strings.HasPrefix(shown.Issue.ID, "ait-") {
		t.Fatalf("expected migrated public issue id, got %s", shown.Issue.ID)
	}
	if shown.Issue.ParentID == nil || !strings.HasPrefix(*shown.Issue.ParentID, "ait-") {
		t.Fatalf("expected migrated parent public id, got %+v", shown.Issue.ParentID)
	}
	if len(shown.Notes) != 1 || shown.Notes[0].Body != "Migrated note" {
		t.Fatalf("expected migrated note, got %+v", shown.Notes)
	}

	var listed struct {
		Issues []ait.Issue `json:"issues"`
	}
	runJSONCommand(t, app, []string{"list"}, &listed)
	if len(listed.Issues) != 2 {
		t.Fatalf("expected 2 migrated issues, got %d", len(listed.Issues))
	}
}

func TestReadyExcludesBlockedIssues(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var blocker ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Blocker"}, &blocker)

		var blocked ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Blocked"}, &blocked)

		runJSONCommand[map[string]any](t, a, []string{"dep", "add", blocked.ID, blocker.ID}, nil)

		var ready struct {
			Issues []ait.Issue `json:"issues"`
		}
		runJSONCommand(t, a, []string{"ready"}, &ready)

		if len(ready.Issues) != 1 {
			t.Fatalf("expected exactly one ready issue, got %d", len(ready.Issues))
		}
		if ready.Issues[0].ID != blocker.ID {
			t.Fatalf("expected blocker to be ready, got %s", ready.Issues[0].ID)
		}
	})
}

func TestNotesAreReturnedByShow(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var created ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Task with notes"}, &created)

		var createdNote ait.Note
		runJSONCommand(t, a, []string{"note", "add", created.ID, "Investigated root cause"}, &createdNote)

		if createdNote.IssueID != created.ID {
			t.Fatalf("expected note issue id %s, got %s", created.ID, createdNote.IssueID)
		}

		var shown ait.ShowResponse
		runJSONCommand(t, a, []string{"show", created.ID}, &shown)

		if len(shown.Notes) != 1 {
			t.Fatalf("expected 1 note, got %d", len(shown.Notes))
		}
		if shown.Notes[0].Body != "Investigated root cause" {
			t.Fatalf("unexpected note body: %s", shown.Notes[0].Body)
		}
	})
}

func TestStatusTransitions(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var created ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Transition me"}, &created)

		var updated ait.Issue
		runJSONCommand(t, a, []string{"update", created.ID, "--status", ait.StatusInProgress}, &updated)
		if updated.Status != ait.StatusInProgress {
			t.Fatalf("expected in_progress, got %s", updated.Status)
		}

		var closed ait.Issue
		runJSONCommand(t, a, []string{"close", created.ID}, &closed)
		if closed.Status != ait.StatusClosed {
			t.Fatalf("expected closed, got %s", closed.Status)
		}
		if closed.ClosedAt == nil {
			t.Fatalf("expected closed_at to be set")
		}

		var reopened ait.Issue
		runJSONCommand(t, a, []string{"reopen", created.ID}, &reopened)
		if reopened.Status != ait.StatusOpen {
			t.Fatalf("expected reopened status open, got %s", reopened.Status)
		}
		if reopened.ClosedAt != nil {
			t.Fatalf("expected closed_at to be cleared")
		}
	})
}

func testApp(t *testing.T, fn func(ctx context.Context, a *ait.App)) {
	t.Helper()

	tmpDir := t.TempDir()
	restoreCWD := withWorkingDir(t, tmpDir)
	defer restoreCWD()

	ctx := context.Background()
	app, err := ait.Open(ctx)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer app.Close()

	fn(ctx, app)
}

func withWorkingDir(t *testing.T, dir string) func() {
	t.Helper()

	current, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir to temp dir failed: %v", err)
	}

	return func() {
		if err := os.Chdir(current); err != nil {
			t.Fatalf("restore cwd failed: %v", err)
		}
	}
}

func mustDatabasePath(t *testing.T) string {
	t.Helper()

	path, err := ait.DatabasePath()
	if err != nil {
		t.Fatalf("databasePath failed: %v", err)
	}
	return path
}

func runJSONCommand[T any](t *testing.T, a *ait.App, args []string, target *T) {
	t.Helper()

	output := captureStdout(t, func() {
		if err := a.Run(context.Background(), args); err != nil {
			t.Fatalf("run(%v) failed: %v", args, err)
		}
	})

	if target == nil {
		return
	}
	if err := json.Unmarshal([]byte(output), target); err != nil {
		t.Fatalf("failed to decode JSON output %q: %v", output, err)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe failed: %v", err)
	}

	originalStdout := os.Stdout
	os.Stdout = writer
	defer func() {
		os.Stdout = originalStdout
	}()

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("close writer failed: %v", err)
	}
	bytes, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stdout failed: %v", err)
	}
	return string(bytes)
}
