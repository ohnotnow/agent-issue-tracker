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
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	ctx := context.Background()
	app, err := ait.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer app.Close()

	var payload map[string]map[string]int
	runJSONCommand(t, app, []string{"status"}, &payload)

	counts := payload["counts"]
	if counts["total"] != 0 {
		t.Fatalf("expected total=0, got %d", counts["total"])
	}
	if counts["ready"] != 0 {
		t.Fatalf("expected ready=0, got %d", counts["ready"])
	}

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected database to exist at %s: %v", dbPath, err)
	}
}

func TestCreateAndShowIssue(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[map[string]any](t, a, []string{"init", "--prefix", "demo"}, nil)

		var created ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Bootstrap CLI", "--description", "Implement first version"}, &created)

		if !strings.HasPrefix(created.ID, "demo-") {
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

func TestInitSetsPrefixAndHierarchicalIDs(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var initPayload map[string]string
		runJSONCommand(t, a, []string{"init", "--prefix", "deliveries"}, &initPayload)

		if initPayload["prefix"] != "deliveries" {
			t.Fatalf("expected prefix deliveries, got %q", initPayload["prefix"])
		}

		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Epic", "--type", "epic"}, &epic)
		if !strings.HasPrefix(epic.ID, "deliveries-") {
			t.Fatalf("expected deliveries root id, got %s", epic.ID)
		}

		var child ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Child", "--parent", epic.ID}, &child)
		if child.ID != epic.ID+".1" {
			t.Fatalf("expected first child id %s.1, got %s", epic.ID, child.ID)
		}

		var grandchild ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Grandchild", "--parent", child.ID}, &grandchild)
		if grandchild.ID != child.ID+".1" {
			t.Fatalf("expected first grandchild id %s.1, got %s", child.ID, grandchild.ID)
		}
	})
}

func TestOpenMigratesLegacyIDsToPublicKeys(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "legacy.db")

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

	app, err := ait.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer app.Close()

	var shown ait.ShowResponse
	runJSONCommand(t, app, []string{"show", "legacy-task"}, &shown)

	if shown.Issue.ParentID == nil {
		t.Fatalf("expected migrated parent public id, got %+v", shown.Issue.ParentID)
	}
	if !strings.HasPrefix(shown.Issue.ID, *shown.Issue.ParentID+".") {
		t.Fatalf("expected migrated child id to be hierarchical under %s, got %s", *shown.Issue.ParentID, shown.Issue.ID)
	}
	if len(shown.Notes) != 1 || shown.Notes[0].Body != "Migrated note" {
		t.Fatalf("expected migrated note, got %+v", shown.Notes)
	}

	var listed struct {
		Issues []ait.IssueRef `json:"issues"`
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
			Issues []ait.IssueRef `json:"issues"`
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

	ctx := context.Background()
	app, err := ait.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer app.Close()

	fn(ctx, app)
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

func runExpectError(t *testing.T, a *ait.App, args []string) error {
	t.Helper()
	var runErr error
	captureStdout(t, func() {
		runErr = a.Run(context.Background(), args)
	})
	return runErr
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

// --- Step 1: Output contract tests ---

func TestListReturnsIssueRefsByDefault(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Task A"}, nil)

		var result struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list"}, &result)

		if len(result.Issues) != 1 {
			t.Fatalf("expected 1 issue, got %d", len(result.Issues))
		}
		if result.Issues[0].Title != "Task A" {
			t.Fatalf("unexpected title: %s", result.Issues[0].Title)
		}

		// Verify IssueRef shape: decode raw JSON and check no extra fields
		raw := captureStdout(t, func() {
			if err := a.Run(ctx, []string{"list"}); err != nil {
				t.Fatal(err)
			}
		})
		var rawResult map[string][]map[string]any
		if err := json.Unmarshal([]byte(raw), &rawResult); err != nil {
			t.Fatal(err)
		}
		issue := rawResult["issues"][0]
		if _, ok := issue["description"]; ok {
			t.Fatal("default list should not include description field")
		}
		if _, ok := issue["created_at"]; ok {
			t.Fatal("default list should not include created_at field")
		}
	})
}

func TestListLongReturnsFullIssues(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Task A", "--description", "Details"}, nil)

		var result struct {
			Issues []ait.Issue `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list", "--long"}, &result)

		if len(result.Issues) != 1 {
			t.Fatalf("expected 1 issue, got %d", len(result.Issues))
		}
		if result.Issues[0].Description != "Details" {
			t.Fatalf("expected description in --long output, got %q", result.Issues[0].Description)
		}
	})
}

func TestReadyReturnsIssueRefsByDefault(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Ready task"}, nil)

		raw := captureStdout(t, func() {
			if err := a.Run(ctx, []string{"ready"}); err != nil {
				t.Fatal(err)
			}
		})
		var rawResult map[string][]map[string]any
		if err := json.Unmarshal([]byte(raw), &rawResult); err != nil {
			t.Fatal(err)
		}
		issue := rawResult["issues"][0]
		if _, ok := issue["description"]; ok {
			t.Fatal("default ready should not include description field")
		}
	})
}

func TestReadyLongReturnsFullIssues(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Task", "--description", "Full"}, nil)

		var result struct {
			Issues []ait.Issue `json:"issues"`
		}
		runJSONCommand(t, a, []string{"ready", "--long"}, &result)

		if len(result.Issues) != 1 {
			t.Fatalf("expected 1 issue, got %d", len(result.Issues))
		}
		if result.Issues[0].Description != "Full" {
			t.Fatalf("expected description in --long output, got %q", result.Issues[0].Description)
		}
	})
}

// --- Step 2: Type filter ---

func TestReadyFilterByType(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "My Epic", "--type", "epic"}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "My Task", "--type", "task"}, nil)

		var tasksOnly struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"ready", "--type", "task"}, &tasksOnly)

		if len(tasksOnly.Issues) != 1 {
			t.Fatalf("expected 1 task, got %d", len(tasksOnly.Issues))
		}
		if tasksOnly.Issues[0].Type != "task" {
			t.Fatalf("expected type task, got %s", tasksOnly.Issues[0].Type)
		}

		var epicsOnly struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"ready", "--type", "epic"}, &epicsOnly)

		if len(epicsOnly.Issues) != 1 {
			t.Fatalf("expected 1 epic, got %d", len(epicsOnly.Issues))
		}
	})
}

// --- Step 3: Dependency tests ---

func TestDepAddAndList(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var a1, a2 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Issue A"}, &a1)
		runJSONCommand(t, a, []string{"create", "--title", "Issue B"}, &a2)

		var depList struct {
			IssueID  string        `json:"issue_id"`
			Blockers []ait.IssueRef `json:"blockers"`
			Blocks   []ait.IssueRef `json:"blocks"`
		}
		runJSONCommand(t, a, []string{"dep", "add", a1.ID, a2.ID}, &depList)

		if len(depList.Blockers) != 1 {
			t.Fatalf("expected 1 blocker, got %d", len(depList.Blockers))
		}
		if depList.Blockers[0].ID != a2.ID {
			t.Fatalf("expected blocker %s, got %s", a2.ID, depList.Blockers[0].ID)
		}
	})
}

func TestDepRemove(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var a1, a2 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Issue A"}, &a1)
		runJSONCommand(t, a, []string{"create", "--title", "Issue B"}, &a2)

		runJSONCommand[map[string]any](t, a, []string{"dep", "add", a1.ID, a2.ID}, nil)

		var depList struct {
			Blockers []ait.IssueRef `json:"blockers"`
		}
		runJSONCommand(t, a, []string{"dep", "remove", a1.ID, a2.ID}, &depList)

		if len(depList.Blockers) != 0 {
			t.Fatalf("expected 0 blockers after remove, got %d", len(depList.Blockers))
		}
	})
}

func TestDepTree(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var a1, a2, a3 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Root"}, &a1)
		runJSONCommand(t, a, []string{"create", "--title", "Mid"}, &a2)
		runJSONCommand(t, a, []string{"create", "--title", "Leaf"}, &a3)

		runJSONCommand[map[string]any](t, a, []string{"dep", "add", a1.ID, a2.ID}, nil)
		runJSONCommand[map[string]any](t, a, []string{"dep", "add", a2.ID, a3.ID}, nil)

		var tree ait.DependencyTree
		runJSONCommand(t, a, []string{"dep", "tree", a1.ID}, &tree)

		if tree.Issue.ID != a1.ID {
			t.Fatalf("expected root %s, got %s", a1.ID, tree.Issue.ID)
		}
		if len(tree.Blockers) != 1 || tree.Blockers[0].Issue.ID != a2.ID {
			t.Fatalf("expected mid-level blocker %s", a2.ID)
		}
		if len(tree.Blockers[0].Blockers) != 1 || tree.Blockers[0].Blockers[0].Issue.ID != a3.ID {
			t.Fatalf("expected leaf blocker %s", a3.ID)
		}
	})
}

func TestDepAddTransitiveCycleDetection(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var a1, a2, a3 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "A"}, &a1)
		runJSONCommand(t, a, []string{"create", "--title", "B"}, &a2)
		runJSONCommand(t, a, []string{"create", "--title", "C"}, &a3)

		// A blocked by B, B blocked by C
		runJSONCommand[map[string]any](t, a, []string{"dep", "add", a1.ID, a2.ID}, nil)
		runJSONCommand[map[string]any](t, a, []string{"dep", "add", a2.ID, a3.ID}, nil)

		// C blocked by A would create A->B->C->A cycle
		err := runExpectError(t, a, []string{"dep", "add", a3.ID, a1.ID})
		if err == nil {
			t.Fatal("expected cycle detection error")
		}
		if !strings.Contains(err.Error(), "cycle") {
			t.Fatalf("expected cycle error message, got: %s", err.Error())
		}
	})
}

func TestDepAddSelfDependency(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var a1 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Self"}, &a1)

		err := runExpectError(t, a, []string{"dep", "add", a1.ID, a1.ID})
		if err == nil {
			t.Fatal("expected self-dependency error")
		}
		if !strings.Contains(err.Error(), "itself") {
			t.Fatalf("expected self-dependency message, got: %s", err.Error())
		}
	})
}

// --- Step 4c: List filtering tests ---

func TestListFilterByType(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Epic", "--type", "epic"}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Task", "--type", "task"}, nil)

		var result struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list", "--type", "task"}, &result)

		if len(result.Issues) != 1 {
			t.Fatalf("expected 1 task, got %d", len(result.Issues))
		}
		if result.Issues[0].Title != "Task" {
			t.Fatalf("unexpected title: %s", result.Issues[0].Title)
		}
	})
}

func TestListFilterByPriority(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Urgent", "--priority", "P0"}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Normal", "--priority", "P2"}, nil)

		var result struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list", "--priority", "P0"}, &result)

		if len(result.Issues) != 1 {
			t.Fatalf("expected 1 issue, got %d", len(result.Issues))
		}
		if result.Issues[0].Title != "Urgent" {
			t.Fatalf("unexpected title: %s", result.Issues[0].Title)
		}
	})
}

func TestListFilterByStatus(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var created ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "To close"}, &created)
		runJSONCommand[ait.Issue](t, a, []string{"close", created.ID}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Still open"}, nil)

		var result struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list", "--status", "closed"}, &result)

		if len(result.Issues) != 1 {
			t.Fatalf("expected 1 closed issue, got %d", len(result.Issues))
		}
		if result.Issues[0].Title != "To close" {
			t.Fatalf("unexpected title: %s", result.Issues[0].Title)
		}
	})
}

func TestListFilterByParent(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Parent Epic", "--type", "epic"}, &epic)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Child 1", "--parent", epic.ID}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Child 2", "--parent", epic.ID}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Standalone"}, nil)

		var result struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list", "--parent", epic.ID}, &result)

		if len(result.Issues) != 2 {
			t.Fatalf("expected 2 children, got %d", len(result.Issues))
		}
	})
}

// --- Step 4d: Negative/error path tests ---

func TestSearchReturnsMatches(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Authentication bug", "--description", "Login fails"}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Dashboard feature"}, nil)

		var result struct {
			Issues []ait.Issue `json:"issues"`
		}
		runJSONCommand(t, a, []string{"search", "Authentication"}, &result)

		if len(result.Issues) != 1 {
			t.Fatalf("expected 1 match, got %d", len(result.Issues))
		}
		if result.Issues[0].Title != "Authentication bug" {
			t.Fatalf("unexpected title: %s", result.Issues[0].Title)
		}
	})
}

func TestShowNotFound(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		err := runExpectError(t, a, []string{"show", "nonexistent"})
		if err == nil {
			t.Fatal("expected not_found error")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Fatalf("expected not found message, got: %s", err.Error())
		}
	})
}

func TestCancelAndReopen(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var created ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "To cancel"}, &created)

		var cancelled ait.Issue
		runJSONCommand(t, a, []string{"cancel", created.ID}, &cancelled)
		if cancelled.Status != ait.StatusCancelled {
			t.Fatalf("expected cancelled, got %s", cancelled.Status)
		}

		var reopened ait.Issue
		runJSONCommand(t, a, []string{"reopen", created.ID}, &reopened)
		if reopened.Status != ait.StatusOpen {
			t.Fatalf("expected open after reopen, got %s", reopened.Status)
		}
	})
}

func TestCreateEpicWithParent(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Parent", "--type", "epic"}, &epic)

		err := runExpectError(t, a, []string{"create", "--title", "Nested epic", "--type", "epic", "--parent", epic.ID})
		if err == nil {
			t.Fatal("expected validation error for epic with parent")
		}
		if !strings.Contains(err.Error(), "epics cannot have a parent") {
			t.Fatalf("unexpected error: %s", err.Error())
		}
	})
}

func TestHelpShowsUsage(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		output := captureStdout(t, func() {
			if err := a.Run(ctx, []string{"help"}); err != nil {
				t.Fatalf("help failed: %v", err)
			}
		})

		for _, want := range []string{"Commands:", "create", "list", "ready", "dep", "note", "--db"} {
			if !strings.Contains(output, want) {
				t.Fatalf("expected help to contain %q, got:\n%s", want, output)
			}
		}
	})
}

// --- Rekey tests (ait-2KY5X.6) ---

func TestRekeyChangesAllRootIDs(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "alpha"}, nil)

		var i1, i2 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "First"}, &i1)
		runJSONCommand(t, a, []string{"create", "--title", "Second"}, &i2)

		if !strings.HasPrefix(i1.ID, "alpha-") || !strings.HasPrefix(i2.ID, "alpha-") {
			t.Fatalf("expected alpha- prefix, got %s and %s", i1.ID, i2.ID)
		}

		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "beta"}, nil)

		var listed struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list"}, &listed)

		if len(listed.Issues) != 2 {
			t.Fatalf("expected 2 issues, got %d", len(listed.Issues))
		}
		for _, issue := range listed.Issues {
			if !strings.HasPrefix(issue.ID, "beta-") {
				t.Fatalf("expected beta- prefix after rekey, got %s", issue.ID)
			}
		}
	})
}

func TestRekeyPreservesHierarchy(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "alpha"}, nil)

		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Epic", "--type", "epic"}, &epic)
		var child ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Child", "--parent", epic.ID}, &child)
		var grandchild ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Grandchild", "--parent", child.ID}, &grandchild)

		// Verify original hierarchy
		if child.ID != epic.ID+".1" {
			t.Fatalf("expected child %s.1, got %s", epic.ID, child.ID)
		}
		if grandchild.ID != child.ID+".1" {
			t.Fatalf("expected grandchild %s.1, got %s", child.ID, grandchild.ID)
		}

		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "beta"}, nil)

		var listed struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list"}, &listed)

		// Find the epic (no dot in ID)
		var newEpicID string
		for _, issue := range listed.Issues {
			if !strings.Contains(issue.ID, ".") {
				newEpicID = issue.ID
				break
			}
		}
		if newEpicID == "" || !strings.HasPrefix(newEpicID, "beta-") {
			t.Fatalf("expected beta- root epic, got %q", newEpicID)
		}

		// Verify dotted suffixes are maintained
		expectedChild := newEpicID + ".1"
		expectedGrandchild := newEpicID + ".1.1"
		found := map[string]bool{}
		for _, issue := range listed.Issues {
			found[issue.ID] = true
		}
		if !found[expectedChild] {
			t.Fatalf("expected child %s in listed issues: %v", expectedChild, listed.Issues)
		}
		if !found[expectedGrandchild] {
			t.Fatalf("expected grandchild %s in listed issues: %v", expectedGrandchild, listed.Issues)
		}
	})
}

func TestRekeyDependenciesSurvive(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "alpha"}, nil)

		var i1, i2 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Blocked"}, &i1)
		runJSONCommand(t, a, []string{"create", "--title", "Blocker"}, &i2)
		runJSONCommand[map[string]any](t, a, []string{"dep", "add", i1.ID, i2.ID}, nil)

		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "beta"}, nil)

		// Find the rekeyed IDs
		var listed struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list"}, &listed)

		var blockedID, blockerID string
		for _, issue := range listed.Issues {
			if issue.Title == "Blocked" {
				blockedID = issue.ID
			}
			if issue.Title == "Blocker" {
				blockerID = issue.ID
			}
		}

		var depList struct {
			IssueID  string         `json:"issue_id"`
			Blockers []ait.IssueRef `json:"blockers"`
		}
		runJSONCommand(t, a, []string{"dep", "list", blockedID}, &depList)

		if len(depList.Blockers) != 1 {
			t.Fatalf("expected 1 blocker after rekey, got %d", len(depList.Blockers))
		}
		if depList.Blockers[0].ID != blockerID {
			t.Fatalf("expected blocker %s, got %s", blockerID, depList.Blockers[0].ID)
		}
	})
}

func TestRekeyNotesSurvive(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "alpha"}, nil)

		var created ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Has notes"}, &created)
		runJSONCommand[ait.Note](t, a, []string{"note", "add", created.ID, "Important note"}, nil)

		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "beta"}, nil)

		// Find the rekeyed ID
		var listed struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list"}, &listed)
		if len(listed.Issues) != 1 {
			t.Fatalf("expected 1 issue, got %d", len(listed.Issues))
		}
		newID := listed.Issues[0].ID

		var noteList struct {
			IssueID string     `json:"issue_id"`
			Notes   []ait.Note `json:"notes"`
		}
		runJSONCommand(t, a, []string{"note", "list", newID}, &noteList)

		if len(noteList.Notes) != 1 {
			t.Fatalf("expected 1 note after rekey, got %d", len(noteList.Notes))
		}
		if noteList.Notes[0].Body != "Important note" {
			t.Fatalf("expected note body 'Important note', got %q", noteList.Notes[0].Body)
		}
	})
}

func TestRekeyDouble(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "alpha"}, nil)

		var i1 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Survives double rekey"}, &i1)
		if !strings.HasPrefix(i1.ID, "alpha-") {
			t.Fatalf("expected alpha- prefix, got %s", i1.ID)
		}

		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "beta"}, nil)

		var midList struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list"}, &midList)
		if !strings.HasPrefix(midList.Issues[0].ID, "beta-") {
			t.Fatalf("expected beta- prefix after first rekey, got %s", midList.Issues[0].ID)
		}

		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "gamma"}, nil)

		var finalList struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list"}, &finalList)
		if len(finalList.Issues) != 1 {
			t.Fatalf("expected 1 issue, got %d", len(finalList.Issues))
		}
		if !strings.HasPrefix(finalList.Issues[0].ID, "gamma-") {
			t.Fatalf("expected gamma- prefix after second rekey, got %s", finalList.Issues[0].ID)
		}
	})
}

func TestRekeyIdempotent(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "foo"}, nil)

		var i1, i2 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "First"}, &i1)
		runJSONCommand(t, a, []string{"create", "--title", "Second"}, &i2)

		// Capture IDs before second init
		var before struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list"}, &before)

		beforeIDs := map[string]bool{}
		for _, issue := range before.Issues {
			beforeIDs[issue.ID] = true
		}

		// Run init with same prefix again
		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "foo"}, nil)

		var after struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list"}, &after)

		if len(after.Issues) != len(before.Issues) {
			t.Fatalf("expected same number of issues, got %d vs %d", len(before.Issues), len(after.Issues))
		}
		for _, issue := range after.Issues {
			if !beforeIDs[issue.ID] {
				t.Fatalf("ID changed after idempotent rekey: %s not in original set", issue.ID)
			}
		}
	})
}

func TestListHuman(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "test"}, nil)

		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Stabilize v1", "--type", "epic", "--priority", "P1"}, &epic)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Add schema versioning", "--parent", epic.ID, "--priority", "P1"}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Improve prioritization", "--parent", epic.ID, "--priority", "P2"}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Standalone task"}, nil)

		output := captureStdout(t, func() {
			if err := a.Run(ctx, []string{"list", "--human"}); err != nil {
				t.Fatalf("list --human failed: %v", err)
			}
		})

		// Should contain the epic ID
		if !strings.Contains(output, epic.ID) {
			t.Fatalf("expected epic ID %s in output:\n%s", epic.ID, output)
		}
		// Should contain child suffixes
		if !strings.Contains(output, ".1") {
			t.Fatalf("expected child suffix .1 in output:\n%s", output)
		}
		if !strings.Contains(output, ".2") {
			t.Fatalf("expected child suffix .2 in output:\n%s", output)
		}
		// Should contain titles
		if !strings.Contains(output, "Stabilize v1") {
			t.Fatalf("expected epic title in output:\n%s", output)
		}
		if !strings.Contains(output, "Add schema versioning") {
			t.Fatalf("expected child title in output:\n%s", output)
		}
		// Should contain the type label for epics
		if !strings.Contains(output, "epic") {
			t.Fatalf("expected 'epic' type label in output:\n%s", output)
		}
		// Should not be JSON
		if strings.HasPrefix(strings.TrimSpace(output), "{") {
			t.Fatalf("expected non-JSON output, got:\n%s", output)
		}
	})
}

func TestListTree(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "tree"}, nil)

		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Epic One", "--type", "epic", "--priority", "P1"}, &epic)
		var child1 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Child One", "--parent", epic.ID}, &child1)
		var child2 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Child Two", "--parent", epic.ID}, &child2)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Solo task"}, nil)

		output := captureStdout(t, func() {
			if err := a.Run(ctx, []string{"list", "--tree"}); err != nil {
				t.Fatalf("list --tree failed: %v", err)
			}
		})

		// Should contain tree connectors
		if !strings.Contains(output, "├── ") {
			t.Fatalf("expected ├── connector in output:\n%s", output)
		}
		if !strings.Contains(output, "└── ") {
			t.Fatalf("expected └── connector in output:\n%s", output)
		}
		// Should contain full child IDs
		if !strings.Contains(output, child1.ID) {
			t.Fatalf("expected child ID %s in output:\n%s", child1.ID, output)
		}
		if !strings.Contains(output, child2.ID) {
			t.Fatalf("expected child ID %s in output:\n%s", child2.ID, output)
		}
		// Should contain metadata in parentheses
		if !strings.Contains(output, "(epic, P1, open)") {
			t.Fatalf("expected '(epic, P1, open)' in output:\n%s", output)
		}
		// Children should have (priority, status) format
		if !strings.Contains(output, "(P2, open)") {
			t.Fatalf("expected '(P2, open)' in output:\n%s", output)
		}
		// Should not be JSON
		if strings.HasPrefix(strings.TrimSpace(output), "{") {
			t.Fatalf("expected non-JSON output, got:\n%s", output)
		}
	})
}

func TestListHumanAndTreeMutuallyExclusive(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		err := runExpectError(t, a, []string{"list", "--human", "--tree"})
		if err == nil {
			t.Fatal("expected error for --human --tree")
		}
		if !strings.Contains(err.Error(), "mutually exclusive") {
			t.Fatalf("unexpected error: %s", err.Error())
		}
	})
}

func TestCreateInvalidType(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		err := runExpectError(t, a, []string{"create", "--title", "Bad type", "--type", "story"})
		if err == nil {
			t.Fatal("expected validation error for invalid type")
		}
		if !strings.Contains(err.Error(), "type must be one of") {
			t.Fatalf("unexpected error: %s", err.Error())
		}
	})
}

// --- Schema versioning tests ---

func TestSchemaVersionTableCreatedOnFreshDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "fresh.db")

	ctx := context.Background()
	app, err := ait.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer app.Close()

	// Verify schema_version table exists and has a version
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer db.Close()

	var version int
	if err := db.QueryRow(`SELECT version FROM schema_version WHERE id = 1`).Scan(&version); err != nil {
		t.Fatalf("expected schema_version row: %v", err)
	}
	if version < 1 {
		t.Fatalf("expected version >= 1, got %d", version)
	}
}

func TestSchemaVersionSetOnPreExistingDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "existing.db")

	// Create a DB with the current schema but no schema_version table
	// (simulates a pre-migration database).
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE issues (
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
	)`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	db.Close()

	ctx := context.Background()
	app, err := ait.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer app.Close()

	// Verify schema_version was backfilled
	db2, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()

	var version int
	if err := db2.QueryRow(`SELECT version FROM schema_version WHERE id = 1`).Scan(&version); err != nil {
		t.Fatalf("expected schema_version row: %v", err)
	}
	if version < 1 {
		t.Fatalf("expected version >= 1, got %d", version)
	}
}

// --- Cascade close tests ---

func TestCascadeCloseClosesEpicAndChildren(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Epic", "--type", "epic"}, &epic)
		var child1 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Child 1", "--parent", epic.ID}, &child1)
		var child2 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Child 2", "--parent", epic.ID}, &child2)

		var result struct {
			Closed []ait.IssueRef `json:"closed"`
		}
		runJSONCommand(t, a, []string{"close", epic.ID, "--cascade"}, &result)

		if len(result.Closed) != 3 {
			t.Fatalf("expected 3 closed issues, got %d", len(result.Closed))
		}

		// Verify all are actually closed
		for _, id := range []string{epic.ID, child1.ID, child2.ID} {
			var shown ait.ShowResponse
			runJSONCommand(t, a, []string{"show", id}, &shown)
			if shown.Issue.Status != ait.StatusClosed {
				t.Fatalf("expected %s to be closed, got %s", id, shown.Issue.Status)
			}
		}
	})
}

func TestCascadeCloseSkipsAlreadyClosed(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Epic", "--type", "epic"}, &epic)
		var child ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Child", "--parent", epic.ID}, &child)

		// Close the child first
		runJSONCommand[ait.Issue](t, a, []string{"close", child.ID}, nil)

		var result struct {
			Closed []ait.IssueRef `json:"closed"`
		}
		runJSONCommand(t, a, []string{"close", epic.ID, "--cascade"}, &result)

		// Only the epic should be in the closed list (child was already closed)
		if len(result.Closed) != 1 {
			t.Fatalf("expected 1 newly closed issue, got %d", len(result.Closed))
		}
		if result.Closed[0].ID != epic.ID {
			t.Fatalf("expected epic %s to be closed, got %s", epic.ID, result.Closed[0].ID)
		}
	})
}

func TestCascadeCloseGrandchildren(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Epic", "--type", "epic"}, &epic)
		var child ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Child", "--parent", epic.ID}, &child)
		var grandchild ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Grandchild", "--parent", child.ID}, &grandchild)

		var result struct {
			Closed []ait.IssueRef `json:"closed"`
		}
		runJSONCommand(t, a, []string{"close", epic.ID, "--cascade"}, &result)

		if len(result.Closed) != 3 {
			t.Fatalf("expected 3 closed issues, got %d", len(result.Closed))
		}

		var shown ait.ShowResponse
		runJSONCommand(t, a, []string{"show", grandchild.ID}, &shown)
		if shown.Issue.Status != ait.StatusClosed {
			t.Fatalf("expected grandchild to be closed, got %s", shown.Issue.Status)
		}
	})
}

func TestCloseWithoutCascadeStillWorks(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Epic", "--type", "epic"}, &epic)
		var child ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Child", "--parent", epic.ID}, &child)

		var closed ait.Issue
		runJSONCommand(t, a, []string{"close", epic.ID}, &closed)
		if closed.Status != ait.StatusClosed {
			t.Fatalf("expected closed, got %s", closed.Status)
		}

		// Child should still be open
		var shown ait.ShowResponse
		runJSONCommand(t, a, []string{"show", child.ID}, &shown)
		if shown.Issue.Status != ait.StatusOpen {
			t.Fatalf("expected child still open, got %s", shown.Issue.Status)
		}
	})
}

// --- Claim/unclaim tests ---

// --- Config command tests ---

func TestConfigShowsPrefixAndVersion(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "myproject"}, nil)

		var config struct {
			Prefix        string `json:"prefix"`
			SchemaVersion int    `json:"schema_version"`
		}
		runJSONCommand(t, a, []string{"config"}, &config)

		if config.Prefix != "myproject" {
			t.Fatalf("expected prefix 'myproject', got %q", config.Prefix)
		}
		if config.SchemaVersion < 1 {
			t.Fatalf("expected schema_version >= 1, got %d", config.SchemaVersion)
		}
	})
}

// --- Ready prioritisation tests ---

func TestReadyOrdersByPriorityThenCreation(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		// Create in reverse priority order
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Low pri", "--priority", "P3"}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "High pri", "--priority", "P0"}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Mid pri", "--priority", "P1"}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Also mid", "--priority", "P1"}, nil)

		var ready struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"ready"}, &ready)

		if len(ready.Issues) != 4 {
			t.Fatalf("expected 4 ready issues, got %d", len(ready.Issues))
		}

		// P0 first, then P1s in creation order, then P3
		expected := []string{"High pri", "Mid pri", "Also mid", "Low pri"}
		for i, want := range expected {
			if ready.Issues[i].Title != want {
				t.Fatalf("position %d: expected %q, got %q", i, want, ready.Issues[i].Title)
			}
		}
	})
}

// --- Claim/unclaim tests ---

func TestClaimAndUnclaim(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var created ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Claimable task"}, &created)

		var claimed ait.Issue
		runJSONCommand(t, a, []string{"claim", created.ID, "agent-1"}, &claimed)

		if claimed.ClaimedBy == nil || *claimed.ClaimedBy != "agent-1" {
			t.Fatalf("expected claimed_by=agent-1, got %v", claimed.ClaimedBy)
		}
		if claimed.ClaimedAt == nil {
			t.Fatalf("expected claimed_at to be set")
		}

		var unclaimed ait.Issue
		runJSONCommand(t, a, []string{"unclaim", created.ID}, &unclaimed)

		if unclaimed.ClaimedBy != nil {
			t.Fatalf("expected claimed_by=nil after unclaim, got %v", unclaimed.ClaimedBy)
		}
		if unclaimed.ClaimedAt != nil {
			t.Fatalf("expected claimed_at=nil after unclaim, got %v", unclaimed.ClaimedAt)
		}
	})
}

func TestClaimAlreadyClaimedReturnsConflict(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var created ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Contested task"}, &created)

		runJSONCommand[ait.Issue](t, a, []string{"claim", created.ID, "agent-1"}, nil)

		err := runExpectError(t, a, []string{"claim", created.ID, "agent-2"})
		if err == nil {
			t.Fatal("expected conflict error")
		}
		if !strings.Contains(err.Error(), "already claimed") {
			t.Fatalf("expected 'already claimed' message, got: %s", err.Error())
		}
	})
}

func TestClaimVisibleInShow(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var created ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Visible claim"}, &created)

		runJSONCommand[ait.Issue](t, a, []string{"claim", created.ID, "claude"}, nil)

		var shown ait.ShowResponse
		runJSONCommand(t, a, []string{"show", created.ID}, &shown)

		if shown.Issue.ClaimedBy == nil || *shown.Issue.ClaimedBy != "claude" {
			t.Fatalf("expected show to reflect claim, got %v", shown.Issue.ClaimedBy)
		}
	})
}

func TestMigrationsAreIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "idempotent.db")

	ctx := context.Background()

	// Open twice — second open should be a no-op for migrations.
	app1, err := ait.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("first Open failed: %v", err)
	}
	runJSONCommand[ait.Issue](t, app1, []string{"create", "--title", "Survives reopen"}, nil)
	app1.Close()

	app2, err := ait.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("second Open failed: %v", err)
	}
	defer app2.Close()

	var listed struct {
		Issues []ait.IssueRef `json:"issues"`
	}
	runJSONCommand(t, app2, []string{"list"}, &listed)
	if len(listed.Issues) != 1 {
		t.Fatalf("expected 1 issue after reopen, got %d", len(listed.Issues))
	}
	if listed.Issues[0].Title != "Survives reopen" {
		t.Fatalf("unexpected title: %s", listed.Issues[0].Title)
	}
}

// --- Export command tests ---

func TestExportEpicWithChildren(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "exp"}, nil)

		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Auth System", "--type", "epic", "--description", "Implement authentication", "--priority", "P1"}, &epic)
		var child1 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Add login endpoint", "--parent", epic.ID, "--priority", "P1"}, &child1)
		var child2 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Add logout endpoint", "--parent", epic.ID, "--priority", "P2"}, &child2)

		// Add a note to child1
		runJSONCommand[ait.Note](t, a, []string{"note", "add", child1.ID, "Started implementation"}, nil)

		// Add a dependency: child2 blocked by child1
		runJSONCommand[map[string]any](t, a, []string{"dep", "add", child2.ID, child1.ID}, nil)

		// Close child1
		runJSONCommand[ait.Issue](t, a, []string{"close", child1.ID}, nil)

		output := captureStdout(t, func() {
			if err := a.Run(ctx, []string{"export", epic.ID}); err != nil {
				t.Fatalf("export failed: %v", err)
			}
		})

		// Check header
		if !strings.Contains(output, "# Auth System (`"+epic.ID+"`) — P1") {
			t.Fatalf("expected header with title and ID, got:\n%s", output)
		}
		if !strings.Contains(output, "Implement authentication") {
			t.Fatalf("expected description in output:\n%s", output)
		}

		// Check tasks section
		if !strings.Contains(output, "## Tasks") {
			t.Fatalf("expected Tasks section:\n%s", output)
		}

		// Check checkboxes
		if !strings.Contains(output, "[x] **Add login endpoint**") {
			t.Fatalf("expected closed task checkbox:\n%s", output)
		}
		if !strings.Contains(output, "[ ] **Add logout endpoint**") {
			t.Fatalf("expected open task checkbox:\n%s", output)
		}

		// Check dependency
		if !strings.Contains(output, "**Dependencies:** blocked by `"+child1.ID+"`") {
			t.Fatalf("expected dependency line:\n%s", output)
		}

		// Check note
		if !strings.Contains(output, "Started implementation") {
			t.Fatalf("expected note in output:\n%s", output)
		}

		// Check summary
		if !strings.Contains(output, "Total: 2") {
			t.Fatalf("expected summary with Total: 2:\n%s", output)
		}

		// Check priority ordering (P1 before P2)
		p1Pos := strings.Index(output, "Add login endpoint")
		p2Pos := strings.Index(output, "Add logout endpoint")
		if p1Pos > p2Pos {
			t.Fatalf("expected P1 task before P2 task in output")
		}

		// Should not be JSON
		if strings.HasPrefix(strings.TrimSpace(output), "{") {
			t.Fatalf("expected markdown, not JSON:\n%s", output)
		}
	})
}

func TestExportSingleTask(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var task ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Standalone task", "--description", "Just a task"}, &task)

		output := captureStdout(t, func() {
			if err := a.Run(ctx, []string{"export", task.ID}); err != nil {
				t.Fatalf("export failed: %v", err)
			}
		})

		if !strings.Contains(output, "# Standalone task") {
			t.Fatalf("expected task title in header:\n%s", output)
		}
		if !strings.Contains(output, "Just a task") {
			t.Fatalf("expected description:\n%s", output)
		}
		// No children, so no Tasks section or Summary
		if strings.Contains(output, "## Tasks") {
			t.Fatalf("expected no Tasks section for childless issue:\n%s", output)
		}
		if strings.Contains(output, "## Summary") {
			t.Fatalf("expected no Summary for childless issue:\n%s", output)
		}
	})
}

func TestExportNotFound(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		err := runExpectError(t, a, []string{"export", "nonexistent"})
		if err == nil {
			t.Fatal("expected not_found error")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Fatalf("expected not found message, got: %s", err.Error())
		}
	})
}

func TestExportToFile(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var task ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "File export test"}, &task)

		outPath := filepath.Join(t.TempDir(), "briefing.md")

		// Should not print to stdout when --output is used
		output := captureStdout(t, func() {
			if err := a.Run(ctx, []string{"export", task.ID, "--output", outPath}); err != nil {
				t.Fatalf("export --output failed: %v", err)
			}
		})
		if strings.TrimSpace(output) != "" {
			t.Fatalf("expected no stdout with --output, got: %s", output)
		}

		// Check file was written
		data, err := os.ReadFile(outPath)
		if err != nil {
			t.Fatalf("failed to read output file: %v", err)
		}
		content := string(data)
		if !strings.Contains(content, "# File export test") {
			t.Fatalf("expected title in file content:\n%s", content)
		}
	})
}

func TestExportCancelledTask(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Epic", "--type", "epic"}, &epic)
		var child ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Cancelled task", "--parent", epic.ID}, &child)
		runJSONCommand[ait.Issue](t, a, []string{"cancel", child.ID}, nil)

		output := captureStdout(t, func() {
			if err := a.Run(ctx, []string{"export", epic.ID}); err != nil {
				t.Fatalf("export failed: %v", err)
			}
		})

		if !strings.Contains(output, "[-] **Cancelled task**") {
			t.Fatalf("expected cancelled checkbox [-]:\n%s", output)
		}
	})
}
