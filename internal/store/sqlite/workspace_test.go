package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestWorkspaceStoreMigrationAndRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	workspace, err := store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey() error = %v", err)
	}

	if workspace.PolicyJSON != `{}` {
		t.Fatalf("GetWorkspaceByKey().PolicyJSON = %q, want %q", workspace.PolicyJSON, `{}`)
	}

	updated, err := store.UpdateWorkspacePolicy(ctx, UpdateWorkspacePolicyParams{
		WorkspaceID: workspace.ID,
		PolicyJSON:  `{"allow":["branch_proposal","merge_to_main"]}`,
	})
	if err != nil {
		t.Fatalf("UpdateWorkspacePolicy() error = %v", err)
	}
	if updated.PolicyJSON != `{"allow":["branch_proposal","merge_to_main"]}` {
		t.Fatalf("UpdateWorkspacePolicy().PolicyJSON = %q, want %q", updated.PolicyJSON, `{"allow":["branch_proposal","merge_to_main"]}`)
	}

	active, err := store.ListActiveWorkspaces(ctx)
	if err != nil {
		t.Fatalf("ListActiveWorkspaces() error = %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("ListActiveWorkspaces() len = %d, want 1", len(active))
	}

	var migrationCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&migrationCount); err != nil {
		t.Fatalf("schema_migrations count query error = %v", err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations() error = %v", err)
	}
	if migrationCount != len(migrations) {
		t.Fatalf("schema_migrations count = %d, want %d", migrationCount, len(migrations))
	}
}

func TestWorkspaceStoreListsAndUpdatesInitiatives(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openWorkspaceTestStore(t)
	defer store.Close()

	workspace, err := store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}

	initiative, err := store.UpsertInitiative(ctx, UpsertInitiativeParams{
		WorkspaceID: workspace.ID,
		Key:         "life-admin",
		Title:       "Life Admin",
		Kind:        "routine",
		Status:      "active",
		Summary:     "Recurring life admin work",
	})
	if err != nil {
		t.Fatalf("UpsertInitiative() error = %v", err)
	}

	views, err := store.ListInitiativesByWorkspace(ctx, workspace.ID)
	if err != nil {
		t.Fatalf("ListInitiativesByWorkspace() error = %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("ListInitiativesByWorkspace() len = %d, want 1", len(views))
	}
	if views[0].Key != initiative.Key {
		t.Fatalf("ListInitiativesByWorkspace()[0].Key = %q, want %q", views[0].Key, initiative.Key)
	}

	updated, err := store.UpdateInitiativeStatus(ctx, UpdateInitiativeStatusParams{
		InitiativeID: initiative.ID,
		Status:       "paused",
	})
	if err != nil {
		t.Fatalf("UpdateInitiativeStatus() error = %v", err)
	}
	if updated.Status != "paused" {
		t.Fatalf("UpdateInitiativeStatus().Status = %q, want paused", updated.Status)
	}
	if updated.ID != initiative.ID {
		t.Fatalf("UpdateInitiativeStatus().ID = %d, want %d", updated.ID, initiative.ID)
	}
}

func openWorkspaceTestStore(t *testing.T) *Store {
	t.Helper()

	store, err := Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}
