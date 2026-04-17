package sqlite

import (
	"context"
	"testing"
)

func TestProfileMigrationCreatesWorkspaceProfileTable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

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
