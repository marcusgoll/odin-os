package workspaces

import (
	"context"
	"path/filepath"
	"sync"
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

func TestWorkspaceBootstrapIsIdempotentUnderConcurrentCallers(t *testing.T) {
	ctx := context.Background()
	store := openOwnerEntityTestStore(t, "workspaces-concurrent.db")
	defer store.Close()

	service := Service{Store: store}

	start := make(chan struct{})
	results := make([]sqlite.Workspace, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	wg.Add(len(results))
	for i := range results {
		go func(idx int) {
			defer wg.Done()
			<-start
			results[idx], errs[idx] = service.BootstrapDefaultWorkspace(ctx)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("BootstrapDefaultWorkspace(%d) error = %v", i, err)
		}
	}
	if results[0].ID != results[1].ID {
		t.Fatalf("bootstrap workspace IDs = %d, %d, want same row", results[0].ID, results[1].ID)
	}
	if results[0].Key != "marcus" || results[1].Key != "marcus" {
		t.Fatalf("bootstrap workspace keys = %q, %q, want marcus", results[0].Key, results[1].Key)
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
