package workspaces

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestWorkspaceBootstrapDefault(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openWorkspaceStore(t)
	defer store.Close()

	service := Service{Store: store}

	first, err := service.BootstrapDefault(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefault() error = %v", err)
	}
	if first.Key != DefaultWorkspaceKey {
		t.Fatalf("BootstrapDefault().Key = %q, want %q", first.Key, DefaultWorkspaceKey)
	}
	if first.Status != StatusActive {
		t.Fatalf("BootstrapDefault().Status = %q, want %q", first.Status, StatusActive)
	}

	second, err := service.BootstrapDefault(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefault() second call error = %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("BootstrapDefault() second ID = %d, want %d", second.ID, first.ID)
	}
}

func TestWorkspaceGetByKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openWorkspaceStore(t)
	defer store.Close()

	service := Service{Store: store}
	want, err := service.BootstrapDefault(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefault() error = %v", err)
	}

	got, err := service.GetByKey(ctx, DefaultWorkspaceKey)
	if err != nil {
		t.Fatalf("GetByKey() error = %v", err)
	}
	if got.ID != want.ID {
		t.Fatalf("GetByKey().ID = %d, want %d", got.ID, want.ID)
	}
	if got.OwnerRef != want.OwnerRef {
		t.Fatalf("GetByKey().OwnerRef = %q, want %q", got.OwnerRef, want.OwnerRef)
	}
}

func TestWorkspaceListActive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openWorkspaceStore(t)
	defer store.Close()

	service := Service{Store: store}
	primary, err := service.BootstrapDefault(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefault() error = %v", err)
	}

	active, err := store.CreateWorkspace(ctx, sqlite.CreateWorkspaceParams{
		Key:                 "ops",
		Name:                "Operations",
		OwnerRef:            "marcus",
		Status:              StatusActive,
		DefaultCompanionKey: "daily-assistant",
		PolicyJSON:          `{"mode":"active"}`,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace(active) error = %v", err)
	}

	if _, err := store.CreateWorkspace(ctx, sqlite.CreateWorkspaceParams{
		Key:        "archive",
		Name:       "Archive",
		OwnerRef:   "marcus",
		Status:     "archived",
		PolicyJSON: `{"mode":"archived"}`,
	}); err != nil {
		t.Fatalf("CreateWorkspace(archived) error = %v", err)
	}

	workspaces, err := service.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive() error = %v", err)
	}
	if len(workspaces) != 2 {
		t.Fatalf("ListActive() len = %d, want 2", len(workspaces))
	}
	if workspaces[0].ID != primary.ID {
		t.Fatalf("ListActive()[0].ID = %d, want %d", workspaces[0].ID, primary.ID)
	}
	if workspaces[1].ID != active.ID {
		t.Fatalf("ListActive()[1].ID = %d, want %d", workspaces[1].ID, active.ID)
	}
}

func openWorkspaceStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}
