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

	workspace, err := store.CreateWorkspace(ctx, CreateWorkspaceParams{
		Key:                 "default",
		Name:                "Default Workspace",
		OwnerRef:            "operator",
		DefaultCompanionKey: "primary",
		Status:              "active",
		PolicyJSON:          `{"allow":["branch_proposal"]}`,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	got, err := store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey() error = %v", err)
	}
	if got.ID != workspace.ID {
		t.Fatalf("GetWorkspaceByKey().ID = %d, want %d", got.ID, workspace.ID)
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
	if migrationCount != 8 {
		t.Fatalf("schema_migrations count = %d, want 8", migrationCount)
	}
}
