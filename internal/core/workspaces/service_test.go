package workspaces

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestWorkspaceBootstrapCreatesDefaultMarcusWorkspace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openOwnerEntityTestStore(t, "workspaces.db")
	defer store.Close()

	service := Service{Store: store}

	workspace, err := service.BootstrapDefaultWorkspace(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefaultWorkspace() error = %v", err)
	}
	if workspace.Key != "marcus" {
		t.Fatalf("BootstrapDefaultWorkspace().Key = %q, want %q", workspace.Key, "marcus")
	}
	if workspace.Name != "Marcus" {
		t.Fatalf("BootstrapDefaultWorkspace().Name = %q, want %q", workspace.Name, "Marcus")
	}
	if workspace.Status != "active" {
		t.Fatalf("BootstrapDefaultWorkspace().Status = %q, want %q", workspace.Status, "active")
	}

	got, err := service.GetByKey(ctx, "marcus")
	if err != nil {
		t.Fatalf("GetByKey() error = %v", err)
	}
	if got.ID != workspace.ID {
		t.Fatalf("GetByKey().ID = %d, want %d", got.ID, workspace.ID)
	}
}

func openOwnerEntityTestStore(t *testing.T, name string) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}
