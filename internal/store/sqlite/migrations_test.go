package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestProfileMigrationCreatesWorkspaceProfileTable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	var tableName string
	if err := store.DB().QueryRowContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type = 'table' AND name = 'workspace_profile'
	`).Scan(&tableName); err != nil {
		t.Fatalf("workspace_profile table query error = %v", err)
	}

	var migrationCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = 17`).Scan(&migrationCount); err != nil {
		t.Fatalf("schema_migrations query error = %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("schema_migrations version 17 count = %d, want 1", migrationCount)
	}
}

func TestMemoryMigrationCreatesConversationAndMemoryTables(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	for _, table := range []string{"conversation_transcripts", "memory_summaries"} {
		var tableName string
		if err := store.DB().QueryRowContext(ctx, `
			SELECT name
			FROM sqlite_master
			WHERE type = 'table' AND name = ?
		`, table).Scan(&tableName); err != nil {
			t.Fatalf("%s table query error = %v", table, err)
		}
	}

	var migrationCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = 7`).Scan(&migrationCount); err != nil {
		t.Fatalf("schema_migrations query error = %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("schema_migrations version 7 count = %d, want 1", migrationCount)
	}
}
