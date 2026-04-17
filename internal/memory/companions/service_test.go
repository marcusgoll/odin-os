package companions

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestCompanionServiceRecordsCompanionOwnedOverlayMemory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openMemoryTestStore(t, "companions-memory.db")
	defer store.Close()

	workspace := createWorkspace(t, ctx, store, "workspace-a")
	companion := createCompanion(t, ctx, store, workspace.ID, "planner")

	service := Service{
		Store:        store,
		WorkspaceID:  workspace.ID,
		CompanionID:  companion.ID,
		CompanionKey: companion.Key,
	}

	summary, err := service.Remember(ctx, "companion_overlay", "Remember the operator prefers concise replies.", `{"source":"test"}`)
	if err != nil {
		t.Fatalf("Remember() error = %v", err)
	}
	if summary.WorkspaceID == nil || *summary.WorkspaceID != workspace.ID {
		t.Fatalf("summary.WorkspaceID = %v, want %d", summary.WorkspaceID, workspace.ID)
	}
	if summary.CompanionID == nil || *summary.CompanionID != companion.ID {
		t.Fatalf("summary.CompanionID = %v, want %d", summary.CompanionID, companion.ID)
	}
	if summary.Scope != "companion" {
		t.Fatalf("summary.Scope = %q, want %q", summary.Scope, "companion")
	}
	if summary.ScopeKey != companion.Key {
		t.Fatalf("summary.ScopeKey = %q, want %q", summary.ScopeKey, companion.Key)
	}
	if summary.VisibilityScope != "companion" {
		t.Fatalf("summary.VisibilityScope = %q, want %q", summary.VisibilityScope, "companion")
	}
	if summary.RetentionClass != "working" {
		t.Fatalf("summary.RetentionClass = %q, want %q", summary.RetentionClass, "working")
	}

	listed, err := service.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != summary.ID {
		t.Fatalf("List() = %+v, want recorded companion memory", listed)
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

func createCompanion(t *testing.T, ctx context.Context, store *sqlite.Store, workspaceID int64, key string) sqlite.Companion {
	t.Helper()

	companion, err := store.CreateCompanion(ctx, sqlite.CreateCompanionParams{
		WorkspaceID:         workspaceID,
		Key:                 key,
		Title:               key,
		Kind:                "assistant",
		Charter:             "Keep the initiative scope tight.",
		Status:              "active",
		InitiativeScopeJSON: "{}",
		MemoryPolicyJSON:    "{}",
		PlanningPolicyJSON:  "{}",
		ToolPolicyJSON:      "{}",
	})
	if err != nil {
		t.Fatalf("CreateCompanion(%s) error = %v", key, err)
	}
	return companion
}
