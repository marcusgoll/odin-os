package knowledge

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestMemoryKnowledgeServiceListsExactScopedKnowledge(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	workspace := createWorkspace(t, ctx, store, "workspace-a")
	initiative := createInitiative(t, ctx, store, workspace.ID, "initiative-a", nil)
	service := Service{Store: store}

	workspaceEntry, err := service.Record(ctx, Scope{
		WorkspaceID:     &workspace.ID,
		Value:           "workspace",
		Key:             workspace.Key,
		VisibilityScope: "workspace",
		RetentionClass:  "durable",
	}, "knowledge", "Workspace convention", `{"source":"test"}`, nil)
	if err != nil {
		t.Fatalf("Record(workspace) error = %v", err)
	}
	initiativeEntry, err := service.Record(ctx, Scope{
		WorkspaceID:     &workspace.ID,
		InitiativeID:    &initiative.ID,
		Value:           "initiative",
		Key:             initiative.Key,
		VisibilityScope: "initiative",
		RetentionClass:  "durable",
	}, "knowledge", "Initiative convention", `{"source":"test"}`, nil)
	if err != nil {
		t.Fatalf("Record(initiative) error = %v", err)
	}

	workspaceEntries, err := service.List(ctx, Scope{
		WorkspaceID: &workspace.ID,
		Value:       "workspace",
		Key:         workspace.Key,
	}, "knowledge")
	if err != nil {
		t.Fatalf("List(workspace) error = %v", err)
	}
	if len(workspaceEntries) != 1 || workspaceEntries[0].ID != workspaceEntry.ID {
		t.Fatalf("workspace entries = %+v, want only workspace knowledge", workspaceEntries)
	}

	initiativeEntries, err := service.List(ctx, Scope{
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		Value:        "initiative",
		Key:          initiative.Key,
	}, "knowledge")
	if err != nil {
		t.Fatalf("List(initiative) error = %v", err)
	}
	if len(initiativeEntries) != 1 || initiativeEntries[0].ID != initiativeEntry.ID {
		t.Fatalf("initiative entries = %+v, want only initiative knowledge", initiativeEntries)
	}
}

func openTestStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
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

func createInitiative(t *testing.T, ctx context.Context, store *sqlite.Store, workspaceID int64, key string, linkedProjectID *int64) sqlite.Initiative {
	t.Helper()

	initiative, err := store.CreateInitiative(ctx, sqlite.CreateInitiativeParams{
		WorkspaceID:     workspaceID,
		Key:             key,
		Title:           key,
		Kind:            "delivery",
		Status:          "active",
		Summary:         "initiative summary",
		LinkedProjectID: linkedProjectID,
	})
	if err != nil {
		t.Fatalf("CreateInitiative(%s) error = %v", key, err)
	}
	return initiative
}
