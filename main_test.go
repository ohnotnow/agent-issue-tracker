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
		runJSONCommand(t, a, []string{"close", created.ID, "--long"}, &closed)
		if closed.Status != ait.StatusClosed {
			t.Fatalf("expected closed, got %s", closed.Status)
		}
		if closed.ClosedAt == nil {
			t.Fatalf("expected closed_at to be set")
		}

		var reopened ait.Issue
		runJSONCommand(t, a, []string{"reopen", created.ID, "--long"}, &reopened)
		if reopened.Status != ait.StatusOpen {
			t.Fatalf("expected reopened status open, got %s", reopened.Status)
		}
		if reopened.ClosedAt != nil {
			t.Fatalf("expected closed_at to be cleared")
		}
	})
}

func TestCaseInsensitiveSearch(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Auth Flow"}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "AUTH_TOKEN handler"}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Unrelated task"}, nil)

		var result struct {
			Issues []ait.Issue `json:"issues"`
		}
		runJSONCommand(t, a, []string{"search", "auth"}, &result)

		if len(result.Issues) != 2 {
			t.Fatalf("expected 2 results for case-insensitive search, got %d", len(result.Issues))
		}
	})
}

func TestSubcommandHelp(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		// Commands that use FlagSet
		for _, cmd := range [][]string{
			{"list", "--help"},
			{"create", "-h"},
			{"ready", "--help"},
			{"flush", "-h"},
		} {
			output := captureStdout(t, func() {
				if err := a.Run(context.Background(), cmd); err != nil {
					t.Fatalf("run(%v) failed: %v", cmd, err)
				}
			})
			if output == "" {
				t.Fatalf("expected help output for %v, got empty", cmd)
			}
		}

		// Commands without FlagSet
		for _, cmd := range [][]string{
			{"show", "--help"},
			{"search", "-h"},
			{"reopen", "--help"},
			{"close", "-h"},
			{"dep", "--help"},
			{"note", "--help"},
			{"export", "--help"},
		} {
			output := captureStdout(t, func() {
				if err := a.Run(context.Background(), cmd); err != nil {
					t.Fatalf("run(%v) failed: %v", cmd, err)
				}
			})
			if output == "" {
				t.Fatalf("expected help output for %v, got empty", cmd)
			}
		}
	})
}

func TestCompletionScripts(t *testing.T) {
	output := captureStdout(t, func() {
		if err := ait.RunCompletion("bash"); err != nil {
			t.Fatalf("RunCompletion(bash) failed: %v", err)
		}
	})
	if output == "" {
		t.Fatal("expected non-empty bash completion script")
	}

	output = captureStdout(t, func() {
		if err := ait.RunCompletion("zsh"); err != nil {
			t.Fatalf("RunCompletion(zsh) failed: %v", err)
		}
	})
	if output == "" {
		t.Fatal("expected non-empty zsh completion script")
	}

	if err := ait.RunCompletion("fish"); err == nil {
		t.Fatal("expected error for unsupported shell 'fish'")
	}
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
		var rawResult map[string]any
		if err := json.Unmarshal([]byte(raw), &rawResult); err != nil {
			t.Fatal(err)
		}
		issues, ok := rawResult["issues"].([]any)
		if !ok {
			t.Fatalf("expected issues to be an array, got %T", rawResult["issues"])
		}
		issue := issues[0].(map[string]any)
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

// --- Slim ref / slim ack output for mutating commands ---

func TestCreateReturnsIssueRefByDefault(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		raw := captureStdout(t, func() {
			if err := a.Run(ctx, []string{"create", "--title", "New", "--description", "Body"}); err != nil {
				t.Fatal(err)
			}
		})
		var fields map[string]any
		if err := json.Unmarshal([]byte(raw), &fields); err != nil {
			t.Fatal(err)
		}
		if _, ok := fields["description"]; ok {
			t.Fatal("default create should not include description")
		}
		if _, ok := fields["created_at"]; ok {
			t.Fatal("default create should not include created_at")
		}
		if _, ok := fields["id"]; !ok {
			t.Fatal("default create should include id")
		}
	})
}

func TestCreateLongReturnsFullIssue(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var created ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "New", "--description", "Body", "--long"}, &created)
		if created.Description != "Body" {
			t.Fatalf("expected description in --long output, got %q", created.Description)
		}
		if created.CreatedAt == "" {
			t.Fatal("expected created_at in --long output")
		}
	})
}

func TestUpdateReturnsIssueRefByDefault(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var created ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Original", "--long"}, &created)

		raw := captureStdout(t, func() {
			if err := a.Run(ctx, []string{"update", created.ID, "--title", "Renamed"}); err != nil {
				t.Fatal(err)
			}
		})
		var fields map[string]any
		if err := json.Unmarshal([]byte(raw), &fields); err != nil {
			t.Fatal(err)
		}
		if _, ok := fields["description"]; ok {
			t.Fatal("default update should not include description")
		}
		if fields["title"] != "Renamed" {
			t.Fatalf("expected title=Renamed, got %v", fields["title"])
		}
	})
}

func TestCascadeCloseReturnsRefsByDefault(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Epic", "--type", "epic", "--long"}, &epic)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Child", "--parent", epic.ID}, nil)

		raw := captureStdout(t, func() {
			if err := a.Run(ctx, []string{"close", epic.ID, "--cascade"}); err != nil {
				t.Fatal(err)
			}
		})
		var result map[string]any
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			t.Fatal(err)
		}
		closed, ok := result["closed"].([]any)
		if !ok {
			t.Fatalf("expected closed to be an array, got %T", result["closed"])
		}
		if len(closed) != 2 {
			t.Fatalf("expected 2 closed entries, got %d", len(closed))
		}
		first := closed[0].(map[string]any)
		if _, ok := first["description"]; ok {
			t.Fatal("default cascade close should return slim refs (no description)")
		}
	})
}

func TestCascadeCloseLongReturnsFullIssues(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Epic", "--type", "epic", "--description", "Epic body", "--long"}, &epic)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Child", "--parent", epic.ID, "--description", "Child body"}, nil)

		var result struct {
			Closed []ait.Issue `json:"closed"`
		}
		runJSONCommand(t, a, []string{"close", epic.ID, "--cascade", "--long"}, &result)
		if len(result.Closed) != 2 {
			t.Fatalf("expected 2 closed, got %d", len(result.Closed))
		}
		if result.Closed[0].Description == "" {
			t.Fatal("expected description in --long cascade close output")
		}
		if result.Closed[0].ClosedAt == nil {
			t.Fatal("expected closed_at in --long cascade close output")
		}
	})
}

func TestClaimLongIncludesClaimFields(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var created ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Claimable", "--long"}, &created)

		var claimed ait.Issue
		runJSONCommand(t, a, []string{"claim", created.ID, "agent-x", "--long"}, &claimed)
		if claimed.ClaimedBy == nil || *claimed.ClaimedBy != "agent-x" {
			t.Fatalf("expected claimed_by=agent-x, got %v", claimed.ClaimedBy)
		}
	})
}

func TestClaimReturnsIssueRefByDefault(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var created ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Claimable", "--long"}, &created)

		raw := captureStdout(t, func() {
			if err := a.Run(ctx, []string{"claim", created.ID, "agent-x"}); err != nil {
				t.Fatal(err)
			}
		})
		var fields map[string]any
		if err := json.Unmarshal([]byte(raw), &fields); err != nil {
			t.Fatal(err)
		}
		if _, ok := fields["claimed_by"]; ok {
			t.Fatal("default claim should not include claimed_by (use --long)")
		}
		if _, ok := fields["id"]; !ok {
			t.Fatal("default claim should include id")
		}
	})
}

func TestDepAddReturnsSlimAckByDefault(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var a1, a2 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "A", "--long"}, &a1)
		runJSONCommand(t, a, []string{"create", "--title", "B", "--long"}, &a2)

		var ack struct {
			OK        bool   `json:"ok"`
			BlockedID string `json:"blocked_id"`
			BlockerID string `json:"blocker_id"`
		}
		runJSONCommand(t, a, []string{"dep", "add", a1.ID, a2.ID}, &ack)
		if !ack.OK {
			t.Fatal("expected ok=true in slim ack")
		}
		if ack.BlockedID != a1.ID || ack.BlockerID != a2.ID {
			t.Fatalf("ack ids wrong: blocked=%s blocker=%s", ack.BlockedID, ack.BlockerID)
		}
	})
}

func TestDepAddLongReturnsBlockerList(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var a1, a2 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "A", "--long"}, &a1)
		runJSONCommand(t, a, []string{"create", "--title", "B", "--long"}, &a2)

		var depList struct {
			Blockers []ait.IssueRef `json:"blockers"`
		}
		runJSONCommand(t, a, []string{"dep", "add", a1.ID, a2.ID, "--long"}, &depList)
		if len(depList.Blockers) != 1 {
			t.Fatalf("expected 1 blocker in --long output, got %d", len(depList.Blockers))
		}
	})
}

func TestNoteAddReturnsSlimAckByDefault(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var created ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Notable", "--long"}, &created)

		var ack struct {
			OK      bool   `json:"ok"`
			IssueID string `json:"issue_id"`
			NoteID  string `json:"note_id"`
		}
		runJSONCommand(t, a, []string{"note", "add", created.ID, "Investigated"}, &ack)
		if !ack.OK {
			t.Fatal("expected ok=true in slim ack")
		}
		if ack.IssueID != created.ID {
			t.Fatalf("ack issue id wrong: %s", ack.IssueID)
		}
		if ack.NoteID == "" {
			t.Fatal("ack should include note_id")
		}
	})
}

func TestNoteAddLongReturnsFullNote(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var created ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Notable", "--long"}, &created)

		var note ait.Note
		runJSONCommand(t, a, []string{"note", "add", created.ID, "Investigated", "--long"}, &note)
		if note.Body != "Investigated" {
			t.Fatalf("expected note body, got %q", note.Body)
		}
		if note.CreatedAt == "" {
			t.Fatal("expected created_at on --long note add")
		}
	})
}

// --- list hidden_count ---

func TestListIncludesHiddenCountByDefault(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var open ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Open", "--long"}, &open)
		var toClose ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Closing", "--long"}, &toClose)
		runJSONCommand[map[string]any](t, a, []string{"close", toClose.ID}, nil)
		var toCancel ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Cancelling", "--long"}, &toCancel)
		runJSONCommand[map[string]any](t, a, []string{"cancel", toCancel.ID}, nil)

		var result struct {
			Issues      []ait.IssueRef `json:"issues"`
			HiddenCount int            `json:"hidden_count"`
		}
		runJSONCommand(t, a, []string{"list"}, &result)
		if len(result.Issues) != 1 {
			t.Fatalf("expected 1 visible issue, got %d", len(result.Issues))
		}
		if result.HiddenCount != 2 {
			t.Fatalf("expected hidden_count=2 (1 closed + 1 cancelled), got %d", result.HiddenCount)
		}
	})
}

func TestListAllOmitsHiddenCount(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var iss ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "X", "--long"}, &iss)
		runJSONCommand[map[string]any](t, a, []string{"close", iss.ID}, nil)

		raw := captureStdout(t, func() {
			if err := a.Run(ctx, []string{"list", "--all"}); err != nil {
				t.Fatal(err)
			}
		})
		var fields map[string]any
		if err := json.Unmarshal([]byte(raw), &fields); err != nil {
			t.Fatal(err)
		}
		if _, ok := fields["hidden_count"]; ok {
			t.Fatal("list --all should not include hidden_count (nothing is hidden)")
		}
	})
}

func TestListExplicitStatusOmitsHiddenCount(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var iss ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "X", "--long"}, &iss)
		runJSONCommand[map[string]any](t, a, []string{"close", iss.ID}, nil)

		raw := captureStdout(t, func() {
			if err := a.Run(ctx, []string{"list", "--status", "closed"}); err != nil {
				t.Fatal(err)
			}
		})
		var fields map[string]any
		if err := json.Unmarshal([]byte(raw), &fields); err != nil {
			t.Fatal(err)
		}
		if _, ok := fields["hidden_count"]; ok {
			t.Fatal("list --status <x> should not include hidden_count (the user picked the filter)")
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
		runJSONCommand(t, a, []string{"dep", "add", a1.ID, a2.ID, "--long"}, &depList)

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
		runJSONCommand(t, a, []string{"dep", "remove", a1.ID, a2.ID, "--long"}, &depList)

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

func TestCreateEpicWithEpicParent(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Parent", "--type", "epic"}, &epic)

		err := runExpectError(t, a, []string{"create", "--title", "Nested epic", "--type", "epic", "--parent", epic.ID})
		if err == nil {
			t.Fatal("expected validation error for epic with epic parent")
		}
		if !strings.Contains(err.Error(), "epics can only have an initiative as parent") {
			t.Fatalf("unexpected error: %s", err.Error())
		}
	})
}

func TestCreateInitiative(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var init ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Auth Vision", "--type", "initiative", "--priority", "P0"}, &init)
		if init.Type != "initiative" {
			t.Fatalf("expected type initiative, got %s", init.Type)
		}
		if init.ParentID != nil {
			t.Fatal("expected initiative to have no parent")
		}
		if init.Priority != "P0" {
			t.Fatalf("expected P0, got %s", init.Priority)
		}
	})
}

func TestCreateInitiativeWithParent(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Parent", "--type", "epic"}, &epic)

		err := runExpectError(t, a, []string{"create", "--title", "Nested init", "--type", "initiative", "--parent", epic.ID})
		if err == nil {
			t.Fatal("expected validation error for initiative with parent")
		}
		if !strings.Contains(err.Error(), "initiatives cannot have a parent") {
			t.Fatalf("unexpected error: %s", err.Error())
		}
	})
}

func TestCreateEpicWithInitiativeParent(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var init ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Vision", "--type", "initiative"}, &init)

		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Auth Epic", "--type", "epic", "--parent", init.ID, "--long"}, &epic)
		if epic.ParentID == nil || *epic.ParentID != init.ID {
			t.Fatalf("expected epic parent to be %s, got %v", init.ID, epic.ParentID)
		}
		if !strings.HasPrefix(epic.ID, init.ID+".") {
			t.Fatalf("expected epic ID to be hierarchical under %s, got %s", init.ID, epic.ID)
		}
	})
}

func TestCreateTaskWithInitiativeParent(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var init ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Vision", "--type", "initiative"}, &init)

		err := runExpectError(t, a, []string{"create", "--title", "Bad task", "--parent", init.ID})
		if err == nil {
			t.Fatal("expected validation error for task with initiative parent")
		}
		if !strings.Contains(err.Error(), "tasks cannot be direct children of initiatives") {
			t.Fatalf("unexpected error: %s", err.Error())
		}
	})
}

func TestThreeTierHierarchy(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var init ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Vision", "--type", "initiative"}, &init)
		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Auth Epic", "--type", "epic", "--parent", init.ID}, &epic)
		var task ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Login page", "--parent", epic.ID}, &task)

		// Tree view should show all three levels
		treeOut := captureStdout(t, func() {
			if err := a.Run(ctx, []string{"list", "--tree", "--all"}); err != nil {
				t.Fatalf("list --tree: %v", err)
			}
		})
		if !strings.Contains(treeOut, init.ID) {
			t.Fatalf("expected initiative %s in tree output:\n%s", init.ID, treeOut)
		}
		if !strings.Contains(treeOut, epic.ID) {
			t.Fatalf("expected epic %s in tree output:\n%s", epic.ID, treeOut)
		}
		if !strings.Contains(treeOut, task.ID) {
			t.Fatalf("expected task %s in tree output:\n%s", task.ID, treeOut)
		}
	})
}

func TestExportInitiative(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var init ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Vision", "--type", "initiative"}, &init)
		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Auth Epic", "--type", "epic", "--parent", init.ID}, &epic)

		mdOut := captureStdout(t, func() {
			if err := a.Run(ctx, []string{"export", init.ID}); err != nil {
				t.Fatalf("export: %v", err)
			}
		})
		if !strings.Contains(mdOut, "## Epics") {
			t.Fatalf("expected '## Epics' heading in initiative export, got:\n%s", mdOut)
		}
	})
}

func TestCascadeCloseInitiative(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var init ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Vision", "--type", "initiative"}, &init)
		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Auth Epic", "--type", "epic", "--parent", init.ID}, &epic)
		var task ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Login page", "--parent", epic.ID}, &task)

		// Cascade close from initiative
		runJSONCommand(t, a, []string{"close", init.ID, "--cascade"}, new(json.RawMessage))

		// All three should be closed
		for _, id := range []string{init.ID, epic.ID, task.ID} {
			var shown ait.ShowResponse
			runJSONCommand(t, a, []string{"show", id}, &shown)
			if shown.Issue.Status != ait.StatusClosed {
				t.Fatalf("expected %s to be closed, got %s", id, shown.Issue.Status)
			}
		}
	})
}

func TestReadyFilterByInitiative(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var init ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Vision", "--type", "initiative"}, &init)
		runJSONCommand(t, a, []string{"create", "--title", "Some task"}, new(ait.Issue))

		var readyResult struct{ Issues []ait.IssueRef }
		runJSONCommand(t, a, []string{"ready", "--type", "initiative"}, &readyResult)
		if len(readyResult.Issues) != 1 {
			t.Fatalf("expected 1 initiative in ready, got %d", len(readyResult.Issues))
		}
		if readyResult.Issues[0].ID != init.ID {
			t.Fatalf("expected %s, got %s", init.ID, readyResult.Issues[0].ID)
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

func TestListTreeThreeLevels(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "deep"}, nil)

		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Epic", "--type", "epic"}, &epic)
		var phase ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Phase 1", "--parent", epic.ID}, &phase)
		var task ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Impl task", "--parent", phase.ID}, &task)

		// --tree should show all three levels
		treeOut := captureStdout(t, func() {
			if err := a.Run(ctx, []string{"list", "--tree"}); err != nil {
				t.Fatalf("list --tree failed: %v", err)
			}
		})

		if !strings.Contains(treeOut, task.ID) {
			t.Fatalf("expected grandchild %s in --tree output:\n%s", task.ID, treeOut)
		}
		if !strings.Contains(treeOut, "Impl task") {
			t.Fatalf("expected grandchild title in --tree output:\n%s", treeOut)
		}

		// --human should also show all three levels
		humanOut := captureStdout(t, func() {
			if err := a.Run(ctx, []string{"list", "--human"}); err != nil {
				t.Fatalf("list --human failed: %v", err)
			}
		})

		if !strings.Contains(humanOut, "Impl task") {
			t.Fatalf("expected grandchild title in --human output:\n%s", humanOut)
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
		runJSONCommand(t, a, []string{"claim", created.ID, "agent-1", "--long"}, &claimed)

		if claimed.ClaimedBy == nil || *claimed.ClaimedBy != "agent-1" {
			t.Fatalf("expected claimed_by=agent-1, got %v", claimed.ClaimedBy)
		}
		if claimed.ClaimedAt == nil {
			t.Fatalf("expected claimed_at to be set")
		}

		var unclaimed ait.Issue
		runJSONCommand(t, a, []string{"unclaim", created.ID, "--long"}, &unclaimed)

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

// --- Flush tests ---

func TestFlushDeletesClosedIssues(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var task ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Done task"}, &task)
		runJSONCommand[ait.Issue](t, a, []string{"close", task.ID}, nil)

		var result ait.FlushResult
		runJSONCommand(t, a, []string{"flush"}, &result)

		if result.DryRun {
			t.Fatal("expected dry_run=false")
		}
		if len(result.Flushed) != 1 {
			t.Fatalf("expected 1 flushed, got %d", len(result.Flushed))
		}
		if result.Flushed[0].ID != task.ID {
			t.Fatalf("expected flushed ID %s, got %s", task.ID, result.Flushed[0].ID)
		}

		// Verify it's actually gone.
		err := runExpectError(t, a, []string{"show", task.ID})
		if err == nil {
			t.Fatal("expected not_found after flush")
		}
	})
}

func TestFlushDeletesCancelledIssues(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var task ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Cancelled task"}, &task)
		runJSONCommand[ait.Issue](t, a, []string{"cancel", task.ID}, nil)

		var result ait.FlushResult
		runJSONCommand(t, a, []string{"flush"}, &result)

		if len(result.Flushed) != 1 {
			t.Fatalf("expected 1 flushed, got %d", len(result.Flushed))
		}
	})
}

func TestFlushSkipsOpenIssues(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var open ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Still open"}, &open)
		var closed ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Done"}, &closed)
		runJSONCommand[ait.Issue](t, a, []string{"close", closed.ID}, nil)

		var result ait.FlushResult
		runJSONCommand(t, a, []string{"flush"}, &result)

		if len(result.Flushed) != 1 {
			t.Fatalf("expected 1 flushed, got %d", len(result.Flushed))
		}
		if result.Flushed[0].ID != closed.ID {
			t.Fatalf("expected flushed %s, got %s", closed.ID, result.Flushed[0].ID)
		}

		// Open issue should still exist.
		var shown ait.ShowResponse
		runJSONCommand(t, a, []string{"show", open.ID}, &shown)
		if shown.Issue.Status != ait.StatusOpen {
			t.Fatalf("expected open issue to survive flush")
		}
	})
}

func TestFlushDryRunDoesNotDelete(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var task ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Will survive"}, &task)
		runJSONCommand[ait.Issue](t, a, []string{"close", task.ID}, nil)

		var result ait.FlushResult
		runJSONCommand(t, a, []string{"flush", "--dry-run"}, &result)

		if !result.DryRun {
			t.Fatal("expected dry_run=true")
		}
		if len(result.Flushed) != 1 {
			t.Fatalf("expected 1 flushed in dry-run, got %d", len(result.Flushed))
		}

		// Issue should still exist after dry-run.
		var shown ait.ShowResponse
		runJSONCommand(t, a, []string{"show", task.ID}, &shown)
		if shown.Issue.Status != ait.StatusClosed {
			t.Fatalf("expected issue to survive dry-run")
		}
	})
}

func TestFlushSkipsClosedEpicWithOpenChildren(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Mixed epic", "--type", "epic"}, &epic)
		var closedChild ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Done child", "--parent", epic.ID}, &closedChild)
		var openChild ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Open child", "--parent", epic.ID}, &openChild)
		runJSONCommand[ait.Issue](t, a, []string{"close", closedChild.ID}, nil)
		// Close the epic itself but leave one child open.
		runJSONCommand[ait.Issue](t, a, []string{"close", epic.ID}, nil)

		var result ait.FlushResult
		runJSONCommand(t, a, []string{"flush"}, &result)

		if len(result.Flushed) != 0 {
			t.Fatalf("expected 0 flushed (epic has open child), got %d", len(result.Flushed))
		}
		if len(result.Skipped) != 1 {
			t.Fatalf("expected 1 skipped, got %d", len(result.Skipped))
		}
		if result.Skipped[0].ID != epic.ID {
			t.Fatalf("expected skipped %s, got %s", epic.ID, result.Skipped[0].ID)
		}
	})
}

func TestFlushDeletesEpicWithAllClosedChildren(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Complete epic", "--type", "epic"}, &epic)
		var child1 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Child 1", "--parent", epic.ID}, &child1)
		var child2 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Child 2", "--parent", epic.ID}, &child2)
		runJSONCommand[ait.Issue](t, a, []string{"close", child1.ID}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"close", child2.ID}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"close", epic.ID}, nil)

		var result ait.FlushResult
		runJSONCommand(t, a, []string{"flush"}, &result)

		// Should flush the epic + both children.
		if len(result.Flushed) != 3 {
			t.Fatalf("expected 3 flushed, got %d", len(result.Flushed))
		}
		if len(result.Skipped) != 0 {
			t.Fatalf("expected 0 skipped, got %d", len(result.Skipped))
		}

		// All should be gone.
		for _, id := range []string{epic.ID, child1.ID, child2.ID} {
			err := runExpectError(t, a, []string{"show", id})
			if err == nil {
				t.Fatalf("expected %s to be deleted", id)
			}
		}
	})
}

func TestFlushDeletesNotesAndDependencies(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var task1 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Task with note"}, &task1)
		runJSONCommand[ait.Note](t, a, []string{"note", "add", task1.ID, "A progress note"}, nil)

		var task2 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Blocker task"}, &task2)
		runJSONCommand[map[string]any](t, a, []string{"dep", "add", task1.ID, task2.ID}, nil)

		runJSONCommand[ait.Issue](t, a, []string{"close", task2.ID}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"close", task1.ID}, nil)

		var result ait.FlushResult
		runJSONCommand(t, a, []string{"flush"}, &result)

		if len(result.Flushed) != 2 {
			t.Fatalf("expected 2 flushed, got %d", len(result.Flushed))
		}

		// Both gone — notes and deps should be cascade-deleted.
		for _, id := range []string{task1.ID, task2.ID} {
			err := runExpectError(t, a, []string{"show", id})
			if err == nil {
				t.Fatalf("expected %s to be deleted", id)
			}
		}
	})
}

func TestFlushOnEmptyDatabase(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var result ait.FlushResult
		runJSONCommand(t, a, []string{"flush"}, &result)

		if len(result.Flushed) != 0 {
			t.Fatalf("expected 0 flushed on empty db, got %d", len(result.Flushed))
		}
		if len(result.Skipped) != 0 {
			t.Fatalf("expected 0 skipped on empty db, got %d", len(result.Skipped))
		}
	})
}

func TestUpdateDescription(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[map[string]any](t, a, []string{"init", "--prefix", "desc"}, nil)

		var created ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Test issue", "--description", "Original"}, &created)

		var updated ait.Issue
		runJSONCommand(t, a, []string{"update", created.ID, "--description", "Updated description", "--long"}, &updated)

		if updated.Description != "Updated description" {
			t.Fatalf("expected description %q, got %q", "Updated description", updated.Description)
		}
		if updated.Title != "Test issue" {
			t.Fatalf("title should be unchanged, got %q", updated.Title)
		}
	})
}

// --- Ready respects parent epic dependencies ---

func TestReadyRespectsParentEpicDependencies(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		// Create an initiative with two epics (phases).
		var init ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Migration", "--type", "initiative"}, &init)

		var phase1 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Phase 1: Scaffolding", "--type", "epic", "--parent", init.ID}, &phase1)

		var phase2 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Phase 2: API", "--type", "epic", "--parent", init.ID}, &phase2)

		// Phase 2 is blocked by Phase 1.
		runJSONCommand[map[string]any](t, a, []string{"dep", "add", phase2.ID, phase1.ID}, nil)

		// Add tasks to each phase.
		var task1 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Scaffold models", "--type", "task", "--parent", phase1.ID}, &task1)

		var task2 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Build endpoints", "--type", "task", "--parent", phase2.ID}, &task2)

		// task2 has no direct task-level blockers, but its parent epic is blocked.
		// It should NOT appear in ready.
		var ready struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"ready", "--type", "task"}, &ready)

		for _, iss := range ready.Issues {
			if iss.ID == task2.ID {
				t.Fatalf("task %s should not be ready — its parent epic is blocked by %s", task2.ID, phase1.ID)
			}
		}

		// task1 should be ready (its parent epic has no blockers).
		found := false
		for _, iss := range ready.Issues {
			if iss.ID == task1.ID {
				found = true
			}
		}
		if !found {
			t.Fatal("task1 should be ready but was not returned")
		}

		// Now close Phase 1 — task2 should become ready.
		runJSONCommand[ait.Issue](t, a, []string{"close", phase1.ID, "--cascade"}, nil)

		var readyAfter struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"ready", "--type", "task"}, &readyAfter)

		found = false
		for _, iss := range readyAfter.Issues {
			if iss.ID == task2.ID {
				found = true
			}
		}
		if !found {
			t.Fatal("task2 should be ready after Phase 1 is closed, but was not returned")
		}
	})
}

func TestReadyRespectsParentEpicDepsLongFormat(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var init ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Project", "--type", "initiative"}, &init)

		var epicA ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Epic A", "--type", "epic", "--parent", init.ID}, &epicA)

		var epicB ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Epic B", "--type", "epic", "--parent", init.ID}, &epicB)

		runJSONCommand[map[string]any](t, a, []string{"dep", "add", epicB.ID, epicA.ID}, nil)

		var task ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Task in B", "--type", "task", "--parent", epicB.ID}, &task)

		// --long uses readyIssues (full Issue objects), verify it also respects parent deps.
		var ready struct {
			Issues []ait.Issue `json:"issues"`
		}
		runJSONCommand(t, a, []string{"ready", "--type", "task", "--long"}, &ready)

		for _, iss := range ready.Issues {
			if iss.ID == task.ID {
				t.Fatalf("task %s should not be ready (--long) — parent epic is blocked", task.ID)
			}
		}
	})
}

// --- Flush history (log) tests ---

func TestFlushRecordsHistory(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var task ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Done task"}, &task)
		runJSONCommand[ait.Issue](t, a, []string{"close", task.ID}, nil)

		runJSONCommand[ait.FlushResult](t, a, []string{"flush"}, nil)

		var entries []ait.FlushHistoryEntrySummary
		runJSONCommand(t, a, []string{"log"}, &entries)

		if len(entries) != 1 {
			t.Fatalf("expected 1 log entry, got %d", len(entries))
		}
		if entries[0].ItemCount != 1 {
			t.Fatalf("expected item_count 1, got %d", entries[0].ItemCount)
		}
		if len(entries[0].Items) != 1 {
			t.Fatalf("expected 1 item in log entry, got %d", len(entries[0].Items))
		}
		if entries[0].Items[0].Title != "Done task" {
			t.Fatalf("expected title %q, got %q", "Done task", entries[0].Items[0].Title)
		}
	})
}

func TestFlushRecordsSummary(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var task ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Task"}, &task)
		runJSONCommand[ait.Issue](t, a, []string{"close", task.ID}, nil)

		runJSONCommand[ait.FlushResult](t, a, []string{"flush", "--summary", "Effing postgres"}, nil)

		var entries []ait.FlushHistoryEntrySummary
		runJSONCommand(t, a, []string{"log"}, &entries)

		if len(entries) != 1 {
			t.Fatalf("expected 1 log entry, got %d", len(entries))
		}
		if entries[0].Summary != "Effing postgres" {
			t.Fatalf("expected summary %q, got %q", "Effing postgres", entries[0].Summary)
		}
	})
}

func TestFlushRecordsCloseReason(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var task ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Auth bug"}, &task)
		runJSONCommand[ait.Issue](t, a, []string{"close", task.ID, "--reason", "Fixed LIKE case sensitivity"}, nil)

		runJSONCommand[ait.FlushResult](t, a, []string{"flush"}, nil)

		// Close reason only visible in --long output.
		var entries []ait.FlushHistoryEntry
		runJSONCommand(t, a, []string{"log", "--long"}, &entries)

		if len(entries) != 1 {
			t.Fatalf("expected 1 log entry, got %d", len(entries))
		}
		if entries[0].Items[0].CloseReason != "Fixed LIKE case sensitivity" {
			t.Fatalf("expected close reason %q, got %q", "Fixed LIKE case sensitivity", entries[0].Items[0].CloseReason)
		}
	})
}

func TestCloseWithNoteFlag(t *testing.T) {
	// --note is the canonical spelling; --reason remains as an alias.
	testApp(t, func(ctx context.Context, a *ait.App) {
		var task ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Migration bug"}, &task)
		runJSONCommand[ait.Issue](t, a, []string{"close", task.ID, "--note", "Fixed ordering"}, nil)

		runJSONCommand[ait.FlushResult](t, a, []string{"flush"}, nil)

		var entries []ait.FlushHistoryEntry
		runJSONCommand(t, a, []string{"log", "--long"}, &entries)

		if len(entries) != 1 {
			t.Fatalf("expected 1 log entry, got %d", len(entries))
		}
		if entries[0].Items[0].CloseReason != "Fixed ordering" {
			t.Fatalf("expected close reason %q, got %q", "Fixed ordering", entries[0].Items[0].CloseReason)
		}
	})
}

func TestFlushRecordsHierarchy(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Auth Epic", "--type", "epic"}, &epic)
		var task1 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Login page", "--parent", epic.ID}, &task1)
		var task2 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Logout page", "--parent", epic.ID}, &task2)

		runJSONCommand[ait.Issue](t, a, []string{"close", task1.ID}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"close", task2.ID}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"close", epic.ID}, nil)

		runJSONCommand[ait.FlushResult](t, a, []string{"flush"}, nil)

		// Slim: only the root (epic), with item_count covering all 3.
		var slim []ait.FlushHistoryEntrySummary
		runJSONCommand(t, a, []string{"log"}, &slim)

		if len(slim) != 1 {
			t.Fatalf("expected 1 log entry, got %d", len(slim))
		}
		if slim[0].ItemCount != 3 {
			t.Fatalf("expected item_count 3, got %d", slim[0].ItemCount)
		}
		if len(slim[0].Items) != 1 {
			t.Fatalf("expected 1 root item in slim output, got %d", len(slim[0].Items))
		}
		if slim[0].Items[0].PublicID != epic.ID {
			t.Fatalf("expected root item to be %s, got %s", epic.ID, slim[0].Items[0].PublicID)
		}

		// Long: all 3 items with parent relationships.
		var full []ait.FlushHistoryEntry
		runJSONCommand(t, a, []string{"log", "--long"}, &full)

		if len(full[0].Items) != 3 {
			t.Fatalf("expected 3 items in long output, got %d", len(full[0].Items))
		}
		if full[0].Items[0].ParentPublicID != nil {
			t.Fatalf("expected root item to have no parent, got %v", full[0].Items[0].ParentPublicID)
		}
		if full[0].Items[1].ParentPublicID == nil || *full[0].Items[1].ParentPublicID != epic.ID {
			t.Fatalf("expected child parent to be %s", epic.ID)
		}
	})
}

func TestFlushDryRunDoesNotRecordHistory(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var task ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Task"}, &task)
		runJSONCommand[ait.Issue](t, a, []string{"close", task.ID}, nil)

		runJSONCommand[ait.FlushResult](t, a, []string{"flush", "--dry-run"}, nil)

		var entries []ait.FlushHistoryEntrySummary
		runJSONCommand(t, a, []string{"log"}, &entries)

		if len(entries) != 0 {
			t.Fatalf("expected 0 log entries after dry-run, got %d", len(entries))
		}
	})
}

func TestLogLastFlag(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		for _, title := range []string{"First", "Second", "Third"} {
			var task ait.Issue
			runJSONCommand(t, a, []string{"create", "--title", title}, &task)
			runJSONCommand[ait.Issue](t, a, []string{"close", task.ID}, nil)
			runJSONCommand[ait.FlushResult](t, a, []string{"flush"}, nil)
		}

		var all []ait.FlushHistoryEntrySummary
		runJSONCommand(t, a, []string{"log"}, &all)
		if len(all) != 3 {
			t.Fatalf("expected 3 log entries, got %d", len(all))
		}

		var limited []ait.FlushHistoryEntrySummary
		runJSONCommand(t, a, []string{"log", "--last", "2"}, &limited)
		if len(limited) != 2 {
			t.Fatalf("expected 2 log entries with --last 2, got %d", len(limited))
		}
	})
}

func TestLogEmptyDatabase(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var entries []ait.FlushHistoryEntrySummary
		runJSONCommand(t, a, []string{"log"}, &entries)

		if len(entries) != 0 {
			t.Fatalf("expected 0 log entries on empty db, got %d", len(entries))
		}
	})
}

func TestFlushEmptyDoesNotRecordHistory(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[ait.FlushResult](t, a, []string{"flush"}, nil)

		var entries []ait.FlushHistoryEntrySummary
		runJSONCommand(t, a, []string{"log"}, &entries)

		if len(entries) != 0 {
			t.Fatalf("expected 0 log entries when nothing flushed, got %d", len(entries))
		}
	})
}

// --- Log search tests ---

func TestLogSearchMatchesTitle(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var t1 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Fix migration bug"}, &t1)
		var t2 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Add login page"}, &t2)
		runJSONCommand[ait.Issue](t, a, []string{"close", t1.ID}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"close", t2.ID}, nil)
		runJSONCommand[ait.FlushResult](t, a, []string{"flush"}, nil)

		var entries []ait.FlushHistoryEntrySummary
		runJSONCommand(t, a, []string{"log", "--search", "migration"}, &entries)

		if len(entries) != 1 {
			t.Fatalf("expected 1 log entry, got %d", len(entries))
		}
		if len(entries[0].Items) != 1 {
			t.Fatalf("expected 1 matching item, got %d", len(entries[0].Items))
		}
		if entries[0].Items[0].Title != "Fix migration bug" {
			t.Fatalf("expected matching title %q, got %q", "Fix migration bug", entries[0].Items[0].Title)
		}
	})
}

func TestLogSearchMatchesCloseReason(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var task ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Schema work"}, &task)
		runJSONCommand[ait.Issue](t, a, []string{"close", task.ID, "--reason", "Fixed migration ordering"}, nil)
		runJSONCommand[ait.FlushResult](t, a, []string{"flush"}, nil)

		var entries []ait.FlushHistoryEntrySummary
		runJSONCommand(t, a, []string{"log", "--search", "migration"}, &entries)

		if len(entries) != 1 {
			t.Fatalf("expected 1 log entry, got %d", len(entries))
		}
		if entries[0].Items[0].Title != "Schema work" {
			t.Fatalf("expected title %q, got %q", "Schema work", entries[0].Items[0].Title)
		}
	})
}

func TestLogSearchCaseInsensitive(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var task ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Fix migration bug"}, &task)
		runJSONCommand[ait.Issue](t, a, []string{"close", task.ID}, nil)
		runJSONCommand[ait.FlushResult](t, a, []string{"flush"}, nil)

		var entries []ait.FlushHistoryEntrySummary
		runJSONCommand(t, a, []string{"log", "--search", "MIGRATION"}, &entries)

		if len(entries) != 1 {
			t.Fatalf("expected 1 log entry, got %d", len(entries))
		}
	})
}

func TestLogSearchNoMatch(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var task ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Something"}, &task)
		runJSONCommand[ait.Issue](t, a, []string{"close", task.ID}, nil)
		runJSONCommand[ait.FlushResult](t, a, []string{"flush"}, nil)

		var entries []ait.FlushHistoryEntrySummary
		runJSONCommand(t, a, []string{"log", "--search", "nonexistent"}, &entries)

		if len(entries) != 0 {
			t.Fatalf("expected 0 log entries, got %d", len(entries))
		}
	})
}

func TestLogSearchWithLong(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var t1 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Migration fix"}, &t1)
		var t2 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Unrelated"}, &t2)
		runJSONCommand[ait.Issue](t, a, []string{"close", t1.ID, "--reason", "Fixed ordering"}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"close", t2.ID}, nil)
		runJSONCommand[ait.FlushResult](t, a, []string{"flush"}, nil)

		var entries []ait.FlushHistoryEntry
		runJSONCommand(t, a, []string{"log", "--search", "migration", "--long"}, &entries)

		if len(entries) != 1 {
			t.Fatalf("expected 1 log entry, got %d", len(entries))
		}
		if len(entries[0].Items) != 1 {
			t.Fatalf("expected 1 matching item, got %d", len(entries[0].Items))
		}
		if entries[0].Items[0].CloseReason != "Fixed ordering" {
			t.Fatalf("expected close reason %q, got %q", "Fixed ordering", entries[0].Items[0].CloseReason)
		}
	})
}

// --- Log purge tests ---

func TestLogPurgeCompactKeepsEntries(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		// Create 3 flush entries.
		for _, title := range []string{"First", "Second", "Third"} {
			var task ait.Issue
			runJSONCommand(t, a, []string{"create", "--title", title}, &task)
			runJSONCommand[ait.Issue](t, a, []string{"close", task.ID}, nil)
			runJSONCommand[ait.FlushResult](t, a, []string{"flush", "--summary", title + " summary"}, nil)
		}

		// Compact: keep last 1.
		var result ait.PurgeResult
		runJSONCommand(t, a, []string{"log", "purge", "--keep", "1"}, &result)

		if result.Compact != true {
			t.Fatal("expected compact=true")
		}
		if result.EntriesPurged != 0 {
			t.Fatalf("expected 0 entries purged in compact mode, got %d", result.EntriesPurged)
		}
		if result.ItemsRemoved != 2 {
			t.Fatalf("expected 2 items removed, got %d", result.ItemsRemoved)
		}

		// All 3 entries should still exist (summaries preserved).
		var entries []ait.FlushHistoryEntrySummary
		runJSONCommand(t, a, []string{"log"}, &entries)
		if len(entries) != 3 {
			t.Fatalf("expected 3 entries after compact, got %d", len(entries))
		}

		// The two oldest should have item_count 0.
		// Entries are newest-first, so [2] and [1] are the oldest.
		if entries[2].ItemCount != 0 {
			t.Fatalf("expected oldest entry to have 0 items after compact, got %d", entries[2].ItemCount)
		}
		if entries[1].ItemCount != 0 {
			t.Fatalf("expected second oldest entry to have 0 items after compact, got %d", entries[1].ItemCount)
		}
		// Most recent should still have its item.
		if entries[0].ItemCount != 1 {
			t.Fatalf("expected newest entry to have 1 item, got %d", entries[0].ItemCount)
		}
	})
}

func TestLogPurgeFullDeletesEntries(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		for _, title := range []string{"First", "Second", "Third"} {
			var task ait.Issue
			runJSONCommand(t, a, []string{"create", "--title", title}, &task)
			runJSONCommand[ait.Issue](t, a, []string{"close", task.ID}, nil)
			runJSONCommand[ait.FlushResult](t, a, []string{"flush"}, nil)
		}

		var result ait.PurgeResult
		runJSONCommand(t, a, []string{"log", "purge", "--keep", "1", "--full"}, &result)

		if result.Compact != false {
			t.Fatal("expected compact=false for full purge")
		}
		if result.EntriesPurged != 2 {
			t.Fatalf("expected 2 entries purged, got %d", result.EntriesPurged)
		}

		// Only 1 entry should remain.
		var entries []ait.FlushHistoryEntrySummary
		runJSONCommand(t, a, []string{"log"}, &entries)
		if len(entries) != 1 {
			t.Fatalf("expected 1 entry after full purge, got %d", len(entries))
		}
	})
}

func TestLogPurgeRequiresScope(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		err := runExpectError(t, a, []string{"log", "purge"})
		if err == nil {
			t.Fatal("expected error when no --keep or --before given")
		}
	})
}

func TestLogPurgeBeforeAndKeepMutuallyExclusive(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		err := runExpectError(t, a, []string{"log", "purge", "--keep", "5", "--before", "2026-01-01"})
		if err == nil {
			t.Fatal("expected error when both --keep and --before given")
		}
	})
}
