package initiatives

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestInitiativeCreateByKind(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openInitiativeStore(t)
	defer store.Close()

	workspace := createInitiativeWorkspace(t, ctx, store)
	service := Service{Store: store}

	managed, err := service.Create(ctx, CreateInput{
		WorkspaceID: workspace.ID,
		Key:         "cfipros",
		Title:       "CFI Pros",
		Kind:        KindManagedProject,
		Status:      StatusActive,
		Summary:     "Managed project for CFI Pros",
	})
	if err != nil {
		t.Fatalf("Create(managed_project) error = %v", err)
	}

	manual, err := service.Create(ctx, CreateInput{
		WorkspaceID: workspace.ID,
		Key:         "ops-rollout",
		Title:       "Ops Rollout",
		Kind:        "program",
		Status:      StatusActive,
		Summary:     "Manual program initiative",
	})
	if err != nil {
		t.Fatalf("Create(program) error = %v", err)
	}

	if managed.Kind != KindManagedProject {
		t.Fatalf("Create(managed_project).Kind = %q, want %q", managed.Kind, KindManagedProject)
	}
	if manual.Kind != "program" {
		t.Fatalf("Create(program).Kind = %q, want %q", manual.Kind, "program")
	}
}

func TestInitiativeLinkManagedProject(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openInitiativeStore(t)
	defer store.Close()

	workspace := createInitiativeWorkspace(t, ctx, store)
	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       filepath.Join(t.TempDir(), "alpha"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	service := Service{Store: store}
	initiative, err := service.ReconcileManagedProject(ctx, ManagedProjectInput{
		WorkspaceID: workspace.ID,
		ProjectID:   project.ID,
		ProjectKey:  project.Key,
		ProjectName: project.Name,
		Status:      StatusActive,
		Summary:     "Managed project initiative",
	})
	if err != nil {
		t.Fatalf("ReconcileManagedProject() error = %v", err)
	}

	if initiative.LinkedProjectID == nil || *initiative.LinkedProjectID != project.ID {
		t.Fatalf("ReconcileManagedProject().LinkedProjectID = %v, want %d", initiative.LinkedProjectID, project.ID)
	}
	if initiative.Kind != KindManagedProject {
		t.Fatalf("ReconcileManagedProject().Kind = %q, want %q", initiative.Kind, KindManagedProject)
	}
}

func TestInitiativeListForWorkspace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openInitiativeStore(t)
	defer store.Close()

	workspace := createInitiativeWorkspace(t, ctx, store)
	other, err := store.CreateWorkspace(ctx, sqlite.CreateWorkspaceParams{
		Key:        "ops",
		Name:       "Operations",
		OwnerRef:   "marcus",
		Status:     "active",
		PolicyJSON: `{}`,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace(other) error = %v", err)
	}

	service := Service{Store: store}
	if _, err := service.Create(ctx, CreateInput{
		WorkspaceID: workspace.ID,
		Key:         "cfipros",
		Title:       "CFI Pros",
		Kind:        KindManagedProject,
		Status:      StatusActive,
		Summary:     "Managed project",
	}); err != nil {
		t.Fatalf("Create(primary) error = %v", err)
	}
	if _, err := service.Create(ctx, CreateInput{
		WorkspaceID: other.ID,
		Key:         "ops-rollout",
		Title:       "Ops Rollout",
		Kind:        "program",
		Status:      StatusActive,
		Summary:     "Other workspace",
	}); err != nil {
		t.Fatalf("Create(other) error = %v", err)
	}

	initiatives, err := service.ListForWorkspace(ctx, workspace.ID)
	if err != nil {
		t.Fatalf("ListForWorkspace() error = %v", err)
	}
	if len(initiatives) != 1 {
		t.Fatalf("ListForWorkspace() len = %d, want 1", len(initiatives))
	}
	if initiatives[0].WorkspaceID != workspace.ID {
		t.Fatalf("ListForWorkspace()[0].WorkspaceID = %d, want %d", initiatives[0].WorkspaceID, workspace.ID)
	}
}

func openInitiativeStore(t *testing.T) *sqlite.Store {
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

func createInitiativeWorkspace(t *testing.T, ctx context.Context, store *sqlite.Store) sqlite.Workspace {
	t.Helper()

	workspace, err := store.EnsureDefaultWorkspace(ctx)
	if err != nil {
		t.Fatalf("EnsureDefaultWorkspace() error = %v", err)
	}
	return workspace
}
