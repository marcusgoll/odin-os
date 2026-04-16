package companions

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/core/initiatives"
	"odin-os/internal/core/workspaces"
	"odin-os/internal/store/sqlite"
)

func TestCompanionBootstrapDefaultOperator(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openCompanionStore(t)
	defer store.Close()

	workspaceService := workspaces.Service{Store: store}
	workspace, err := workspaceService.BootstrapDefault(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefault() error = %v", err)
	}

	service := Service{Store: store}

	first, err := service.BootstrapDefaultOperator(ctx, workspace.ID)
	if err != nil {
		t.Fatalf("BootstrapDefaultOperator() error = %v", err)
	}
	if first.WorkspaceID != workspace.ID {
		t.Fatalf("BootstrapDefaultOperator().WorkspaceID = %d, want %d", first.WorkspaceID, workspace.ID)
	}
	if first.Key != DefaultOperatorKey {
		t.Fatalf("BootstrapDefaultOperator().Key = %q, want %q", first.Key, DefaultOperatorKey)
	}
	if first.Kind != KindOperator {
		t.Fatalf("BootstrapDefaultOperator().Kind = %q, want %q", first.Kind, KindOperator)
	}
	if first.Status != StatusActive {
		t.Fatalf("BootstrapDefaultOperator().Status = %q, want %q", first.Status, StatusActive)
	}

	second, err := service.BootstrapDefaultOperator(ctx, workspace.ID)
	if err != nil {
		t.Fatalf("BootstrapDefaultOperator() second call error = %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("BootstrapDefaultOperator() second ID = %d, want %d", second.ID, first.ID)
	}

	storedWorkspace, err := store.GetWorkspace(ctx, workspace.ID)
	if err != nil {
		t.Fatalf("GetWorkspace() error = %v", err)
	}
	if storedWorkspace.DefaultCompanionKey != DefaultOperatorKey {
		t.Fatalf("workspace default companion = %q, want %q", storedWorkspace.DefaultCompanionKey, DefaultOperatorKey)
	}
}

func TestCompanionListForWorkspace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openCompanionStore(t)
	defer store.Close()

	workspace, err := store.CreateWorkspace(ctx, sqlite.CreateWorkspaceParams{
		Key:        "ops",
		Name:       "Operations",
		OwnerRef:   "marcus",
		Status:     "active",
		PolicyJSON: `{}`,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	if _, err := store.CreateCompanion(ctx, sqlite.CreateCompanionParams{
		WorkspaceID:         workspace.ID,
		Key:                 DefaultOperatorKey,
		Title:               "Operator",
		Kind:                KindOperator,
		Charter:             "Run the workspace operating rhythm.",
		Status:              StatusActive,
		InitiativeScopeJSON: `{"mode":"all"}`,
		ToolPolicyJSON:      `{"mode":"allow","allowed":["calendar.read"]}`,
		MemoryPolicyJSON:    `{"retention":"workspace"}`,
		PlanningPolicyJSON:  `{"mode":"stepwise"}`,
	}); err != nil {
		t.Fatalf("CreateCompanion(operator) error = %v", err)
	}

	if _, err := store.CreateCompanion(ctx, sqlite.CreateCompanionParams{
		WorkspaceID:         workspace.ID,
		Key:                 "research-advisor",
		Title:               "Research Advisor",
		Kind:                "advisor",
		Charter:             "Analyze research inputs conservatively.",
		Status:              StatusActive,
		InitiativeScopeJSON: `{"initiative_keys":["alpha"]}`,
		ToolPolicyJSON:      `{"mode":"allow","allowed":["web.search"]}`,
		MemoryPolicyJSON:    `{"retention":"initiative"}`,
		PlanningPolicyJSON:  `{"mode":"advisory"}`,
	}); err != nil {
		t.Fatalf("CreateCompanion(research-advisor) error = %v", err)
	}

	service := Service{Store: store}
	companions, err := service.ListForWorkspace(ctx, workspace.ID)
	if err != nil {
		t.Fatalf("ListForWorkspace() error = %v", err)
	}
	if len(companions) != 2 {
		t.Fatalf("ListForWorkspace() len = %d, want 2", len(companions))
	}
	if companions[0].Key != DefaultOperatorKey {
		t.Fatalf("ListForWorkspace()[0].Key = %q, want %q", companions[0].Key, DefaultOperatorKey)
	}
	if companions[1].Key != "research-advisor" {
		t.Fatalf("ListForWorkspace()[1].Key = %q, want %q", companions[1].Key, "research-advisor")
	}
}

func TestCompanionAssignToInitiative(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openCompanionStore(t)
	defer store.Close()

	workspaceService := workspaces.Service{Store: store}
	workspace, err := workspaceService.BootstrapDefault(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefault() error = %v", err)
	}

	companion, err := store.CreateCompanion(ctx, sqlite.CreateCompanionParams{
		WorkspaceID:         workspace.ID,
		Key:                 DefaultOperatorKey,
		Title:               "Operator",
		Kind:                KindOperator,
		Charter:             "Run the workspace operating rhythm.",
		Status:              StatusActive,
		InitiativeScopeJSON: `{"mode":"all"}`,
		ToolPolicyJSON:      `{"mode":"allow","allowed":["calendar.read"]}`,
		MemoryPolicyJSON:    `{"retention":"workspace"}`,
		PlanningPolicyJSON:  `{"mode":"stepwise"}`,
	})
	if err != nil {
		t.Fatalf("CreateCompanion() error = %v", err)
	}

	initiativeService := initiatives.Service{Store: store}
	initiative, err := initiativeService.Create(ctx, initiatives.CreateInput{
		WorkspaceID: workspace.ID,
		Key:         "alpha",
		Title:       "Alpha",
		Kind:        initiatives.KindManagedProject,
		Status:      initiatives.StatusActive,
		Summary:     "Primary managed initiative",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	service := Service{Store: store}
	if err := service.AssignToInitiative(ctx, initiative.ID, companion.ID); err != nil {
		t.Fatalf("AssignToInitiative() error = %v", err)
	}

	storedInitiative, err := store.GetInitiative(ctx, initiative.ID)
	if err != nil {
		t.Fatalf("GetInitiative() error = %v", err)
	}
	if storedInitiative.OwnerCompanionID == nil || *storedInitiative.OwnerCompanionID != companion.ID {
		t.Fatalf("GetInitiative().OwnerCompanionID = %v, want %d", storedInitiative.OwnerCompanionID, companion.ID)
	}
}

func openCompanionStore(t *testing.T) *sqlite.Store {
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
