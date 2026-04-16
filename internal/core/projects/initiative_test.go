package projects

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/core/initiatives"
	"odin-os/internal/store/sqlite"
)

func TestProjectBackedInitiativeRegistersManagedProjectInitiative(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openProjectBackedInitiativeStore(t)
	defer store.Close()

	workspace := createProjectBackedInitiativeWorkspace(t, ctx, store, "default")
	project := createProjectBackedInitiativeProject(t, ctx, store, "alpha")

	service := Service{Store: store}

	initiative, err := service.RegisterManagedProjectInitiative(ctx, workspace.ID, project, nil)
	if err != nil {
		t.Fatalf("RegisterManagedProjectInitiative() error = %v", err)
	}

	if initiative.WorkspaceID != workspace.ID {
		t.Fatalf("initiative.WorkspaceID = %d, want %d", initiative.WorkspaceID, workspace.ID)
	}
	if initiative.Key != project.Key {
		t.Fatalf("initiative.Key = %q, want %q", initiative.Key, project.Key)
	}
	if initiative.Kind != initiatives.KindManagedProject {
		t.Fatalf("initiative.Kind = %q, want %q", initiative.Kind, initiatives.KindManagedProject)
	}
	if initiative.LinkedProjectID == nil || *initiative.LinkedProjectID != project.ID {
		t.Fatalf("initiative.LinkedProjectID = %v, want %d", initiative.LinkedProjectID, project.ID)
	}
}

func openProjectBackedInitiativeStore(t *testing.T) *sqlite.Store {
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

func createProjectBackedInitiativeWorkspace(t *testing.T, ctx context.Context, store *sqlite.Store, key string) sqlite.Workspace {
	t.Helper()

	workspace, err := store.CreateWorkspace(ctx, sqlite.CreateWorkspaceParams{
		Key:                 key,
		Name:                "Default Workspace",
		OwnerRef:            "operator",
		DefaultCompanionKey: "primary",
		Status:              "active",
		PolicyJSON:          `{"allow":["branch_proposal"]}`,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	return workspace
}

func createProjectBackedInitiativeProject(t *testing.T, ctx context.Context, store *sqlite.Store, key string) sqlite.Project {
	t.Helper()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           key,
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       filepath.Join(t.TempDir(), key),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	return project
}
