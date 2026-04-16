package workspaces

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestWorkspaceServiceBootstrapsDefaultWorkspace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openWorkspaceServiceStore(t)
	defer store.Close()

	service := Service{Store: store}

	workspace, err := service.BootstrapDefaultWorkspace(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefaultWorkspace() error = %v", err)
	}
	if workspace.Key != DefaultWorkspaceKey {
		t.Fatalf("BootstrapDefaultWorkspace().Key = %q, want %q", workspace.Key, DefaultWorkspaceKey)
	}
	if workspace.Status != WorkspaceStatusActive {
		t.Fatalf("BootstrapDefaultWorkspace().Status = %q, want %q", workspace.Status, WorkspaceStatusActive)
	}
	if workspace.Policy != DefaultWorkspacePolicy {
		t.Fatalf("BootstrapDefaultWorkspace().Policy = %q, want %q", workspace.Policy, DefaultWorkspacePolicy)
	}

	again, err := service.BootstrapDefaultWorkspace(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefaultWorkspace() second call error = %v", err)
	}
	if again.ID != workspace.ID {
		t.Fatalf("BootstrapDefaultWorkspace() second call returned ID %d, want %d", again.ID, workspace.ID)
	}

	active, err := service.ListActiveWorkspaces(ctx)
	if err != nil {
		t.Fatalf("ListActiveWorkspaces() error = %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("ListActiveWorkspaces() len = %d, want 1", len(active))
	}
}

func TestWorkspaceServiceUpdateWorkspacePolicy(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openWorkspaceServiceStore(t)
	defer store.Close()

	service := Service{Store: store}

	if _, err := service.BootstrapDefaultWorkspace(ctx); err != nil {
		t.Fatalf("BootstrapDefaultWorkspace() error = %v", err)
	}

	updated, err := service.UpdateWorkspacePolicy(ctx, DefaultWorkspaceKey, WorkspacePolicy(`{"allow":["branch_proposal"]}`))
	if err != nil {
		t.Fatalf("UpdateWorkspacePolicy() error = %v", err)
	}
	if updated.Policy != WorkspacePolicy(`{"allow":["branch_proposal"]}`) {
		t.Fatalf("UpdateWorkspacePolicy().Policy = %q, want %q", updated.Policy, WorkspacePolicy(`{"allow":["branch_proposal"]}`))
	}

	got, err := service.GetWorkspaceByKey(ctx, DefaultWorkspaceKey)
	if err != nil {
		t.Fatalf("GetWorkspaceByKey() error = %v", err)
	}
	if got.Policy != WorkspacePolicy(`{"allow":["branch_proposal"]}`) {
		t.Fatalf("GetWorkspaceByKey().Policy = %q, want %q", got.Policy, WorkspacePolicy(`{"allow":["branch_proposal"]}`))
	}
}

func openWorkspaceServiceStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}
