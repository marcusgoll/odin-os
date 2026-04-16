package workspaces

import (
	"context"
	"path/filepath"
	"sync"
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

func TestWorkspaceServiceBootstrapsAndRepairsWorkspaceWithoutPolicyRow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openWorkspaceServiceStore(t)
	defer store.Close()

	workspace, err := store.GetWorkspaceByKey(ctx, DefaultWorkspaceKey)
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		DELETE FROM workspace_policies
		WHERE workspace_id = ?
	`, workspace.ID); err != nil {
		t.Fatalf("delete workspace policy row error = %v", err)
	}

	service := Service{Store: store}

	bootstrapped, err := service.BootstrapDefaultWorkspace(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefaultWorkspace() error = %v", err)
	}
	if bootstrapped.Policy != DefaultWorkspacePolicy {
		t.Fatalf("BootstrapDefaultWorkspace().Policy = %q, want %q", bootstrapped.Policy, DefaultWorkspacePolicy)
	}

	var policyCount int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM workspace_policies
		WHERE workspace_id = ?
	`, bootstrapped.ID).Scan(&policyCount); err != nil {
		t.Fatalf("policy row count query error = %v", err)
	}
	if policyCount != 1 {
		t.Fatalf("policy row count = %d, want 1", policyCount)
	}

	updatedPolicy := WorkspacePolicy(`{"allow":["branch_proposal"]}`)
	updated, err := service.UpdateWorkspacePolicy(ctx, DefaultWorkspaceKey, updatedPolicy)
	if err != nil {
		t.Fatalf("UpdateWorkspacePolicy() error = %v", err)
	}
	if updated.Policy != updatedPolicy {
		t.Fatalf("UpdateWorkspacePolicy().Policy = %q, want %q", updated.Policy, updatedPolicy)
	}

	got, err := service.GetWorkspaceByKey(ctx, DefaultWorkspaceKey)
	if err != nil {
		t.Fatalf("GetWorkspaceByKey() error = %v", err)
	}
	if got.Policy != updatedPolicy {
		t.Fatalf("GetWorkspaceByKey().Policy = %q, want %q", got.Policy, updatedPolicy)
	}

	active, err := service.ListActiveWorkspaces(ctx)
	if err != nil {
		t.Fatalf("ListActiveWorkspaces() error = %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("ListActiveWorkspaces() len = %d, want 1", len(active))
	}
	if active[0].Policy != updatedPolicy {
		t.Fatalf("ListActiveWorkspaces()[0].Policy = %q, want %q", active[0].Policy, updatedPolicy)
	}
}

func TestWorkspaceServiceBootstrapDefaultWorkspaceIsIdempotentUnderContention(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "odin.db")

	first := openWorkspaceServiceStoreAtPath(t, dbPath)
	defer first.Close()
	second := openWorkspaceServiceStoreAtPath(t, dbPath)
	defer second.Close()

	serviceA := Service{Store: first}
	serviceB := Service{Store: second}

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	errs := make(chan error, 2)
	workspaces := make(chan Workspace, 2)

	go func() {
		defer wg.Done()
		<-start
		workspace, err := serviceA.BootstrapDefaultWorkspace(ctx)
		if err != nil {
			errs <- err
			return
		}
		workspaces <- workspace
	}()

	go func() {
		defer wg.Done()
		<-start
		workspace, err := serviceB.BootstrapDefaultWorkspace(ctx)
		if err != nil {
			errs <- err
			return
		}
		workspaces <- workspace
	}()

	close(start)
	wg.Wait()
	close(errs)
	close(workspaces)

	for err := range errs {
		t.Fatalf("BootstrapDefaultWorkspace() concurrent call error = %v", err)
	}

	var results []Workspace
	for workspace := range workspaces {
		results = append(results, workspace)
	}
	if len(results) != 2 {
		t.Fatalf("BootstrapDefaultWorkspace() concurrent result count = %d, want 2", len(results))
	}
	if results[0].ID != results[1].ID {
		t.Fatalf("BootstrapDefaultWorkspace() concurrent IDs = %d and %d, want same workspace", results[0].ID, results[1].ID)
	}

	active, err := serviceA.ListActiveWorkspaces(ctx)
	if err != nil {
		t.Fatalf("ListActiveWorkspaces() error = %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("ListActiveWorkspaces() len = %d, want 1", len(active))
	}
}

func openWorkspaceServiceStore(t *testing.T) *sqlite.Store {
	t.Helper()

	return openWorkspaceServiceStoreAtPath(t, filepath.Join(t.TempDir(), "odin.db"))
}

func openWorkspaceServiceStoreAtPath(t *testing.T, dbPath string) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}
