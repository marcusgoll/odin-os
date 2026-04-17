package workspaces

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestWorkspaceServiceRecordsWorkspaceOwnedDurableMemory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openMemoryTestStore(t, "workspaces-memory.db")
	defer store.Close()

	workspace := mustCreateWorkspace(t, ctx, store, "workspace-a")
	service := Service{
		Store:        store,
		WorkspaceID:  workspace.ID,
		WorkspaceKey: workspace.Key,
	}

	summary, err := service.Remember(ctx, "workspace_preference", "Prefer concise replies.", `{"source":"test"}`)
	if err != nil {
		t.Fatalf("Remember() error = %v", err)
	}
	if summary.WorkspaceID == nil || *summary.WorkspaceID != workspace.ID {
		t.Fatalf("summary.WorkspaceID = %v, want %d", summary.WorkspaceID, workspace.ID)
	}
	if summary.Scope != "workspace" {
		t.Fatalf("summary.Scope = %q, want %q", summary.Scope, "workspace")
	}
	if summary.ScopeKey != workspace.Key {
		t.Fatalf("summary.ScopeKey = %q, want %q", summary.ScopeKey, workspace.Key)
	}
	if summary.VisibilityScope != "workspace" {
		t.Fatalf("summary.VisibilityScope = %q, want %q", summary.VisibilityScope, "workspace")
	}
	if summary.RetentionClass != "durable" {
		t.Fatalf("summary.RetentionClass = %q, want %q", summary.RetentionClass, "durable")
	}

	listed, err := service.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != summary.ID {
		t.Fatalf("List() = %+v, want recorded workspace memory", listed)
	}
}

func openMemoryTestStore(t *testing.T, name string) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func mustCreateWorkspace(t *testing.T, ctx context.Context, store *sqlite.Store, key string) sqlite.Workspace {
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
