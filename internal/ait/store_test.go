package ait

import (
	"context"
	"path/filepath"
	"testing"
)

// Pragmas set with a plain Exec reach only the connection that ran them.
// database/sql pools connections, so every one must get them on open:
// without foreign_keys, ON DELETE CASCADE silently stops working.
func TestEveryPooledConnectionGetsThePragmas(t *testing.T) {
	ctx := context.Background()
	a, err := Open(ctx, filepath.Join(t.TempDir(), "pragmas.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer a.Close()

	// Hold two at once so the pool has to open a second connection.
	for i := range 2 {
		conn, err := a.db.Conn(ctx)
		if err != nil {
			t.Fatalf("conn %d: %v", i, err)
		}
		defer conn.Close()

		var foreignKeys, busyTimeout int
		var journalMode string
		if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
			t.Fatal(err)
		}
		if err := conn.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
			t.Fatal(err)
		}
		if err := conn.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&journalMode); err != nil {
			t.Fatal(err)
		}
		if foreignKeys != 1 || busyTimeout != 5000 || journalMode != "wal" {
			t.Fatalf("conn %d: foreign_keys=%d busy_timeout=%d journal_mode=%s", i, foreignKeys, busyTimeout, journalMode)
		}
	}
}
