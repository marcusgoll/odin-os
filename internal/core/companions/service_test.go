package companions

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/core/workspaces"
	"odin-os/internal/store/sqlite"
)

func TestCompanionCreatePersistsMemoryPolicy(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openCompanionTestStore(t, "companions.db")
	defer store.Close()

	workspaceService := workspaces.Service{Store: store}
	workspace, err := workspaceService.BootstrapDefaultWorkspace(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefaultWorkspace() error = %v", err)
	}

	service := Service{Store: store}
	companion, err := service.Create(ctx, CreateInput{
		WorkspaceID:         workspace.ID,
		Key:                 "planner",
		Title:               "Planner Companion",
		Kind:                "planning",
		Charter:             "Keep the initiative scope tight.",
		Status:              "active",
		InitiativeScopeJSON: `{"allowed_initiatives":["alpha-initiative"]}`,
		MemoryPolicyJSON:    `{"retention":"short_term"}`,
		PlanningPolicyJSON:  `{"review_required":true}`,
		ToolPolicyJSON:      `{"allow":["read","search"]}`,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if companion.WorkspaceID != workspace.ID {
		t.Fatalf("Create().WorkspaceID = %d, want %d", companion.WorkspaceID, workspace.ID)
	}
	if companion.MemoryPolicyJSON != `{"retention":"short_term"}` {
		t.Fatalf("Create().MemoryPolicyJSON = %q, want %q", companion.MemoryPolicyJSON, `{"retention":"short_term"}`)
	}

	listed, err := service.ListByWorkspace(ctx, workspace.ID)
	if err != nil {
		t.Fatalf("ListByWorkspace() error = %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("ListByWorkspace() len = %d, want 1", len(listed))
	}
	if listed[0].Key != "planner" {
		t.Fatalf("ListByWorkspace()[0].Key = %q, want %q", listed[0].Key, "planner")
	}
}

func openCompanionTestStore(t *testing.T, name string) *sqlite.Store {
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
