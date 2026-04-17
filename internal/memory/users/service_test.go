package users

import (
	"context"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestWorkspaceUserServiceListsOnlyWorkspaceMemory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	workspace := createWorkspace(t, ctx, store, "workspace-a")
	service := Service{Store: store, WorkspaceID: workspace.ID, WorkspaceKey: workspace.Key}

	workspaceEntry, err := service.Remember(ctx, "user_preference", "Prefer concise replies.", `{"source":"test"}`)
	if err != nil {
		t.Fatalf("Remember() error = %v", err)
	}

	entries, err := service.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 1 || entries[0].ID != workspaceEntry.ID {
		t.Fatalf("entries = %+v, want only workspace memory", entries)
	}
	if entries[0].WorkspaceID == nil || *entries[0].WorkspaceID != workspace.ID {
		t.Fatalf("entries[0].WorkspaceID = %v, want %d", entries[0].WorkspaceID, workspace.ID)
	}
	if entries[0].Scope != "workspace" {
		t.Fatalf("entries[0].Scope = %q, want %q", entries[0].Scope, "workspace")
	}
}

func openTestStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(t.TempDir() + "/odin.db")
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func createWorkspace(t *testing.T, ctx context.Context, store *sqlite.Store, key string) sqlite.Workspace {
	t.Helper()

	workspace, err := store.CreateWorkspace(ctx, sqlite.CreateWorkspaceParams{
		Key:        key,
		Name:       key,
		OwnerRef:   key,
		Status:     "active",
		PolicyJSON: "{}",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace(%s) error = %v", key, err)
	}
	return workspace
}
