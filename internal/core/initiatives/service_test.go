package initiatives

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/core/workspaces"
	"odin-os/internal/store/sqlite"
)

func TestInitiativeCreateLinksWorkspaceAndProject(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openInitiativeTestStore(t, "initiatives.db")
	defer store.Close()

	workspaceService := workspaces.Service{Store: store}
	workspace, err := workspaceService.BootstrapDefaultWorkspace(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefaultWorkspace() error = %v", err)
	}

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
	initiative, err := service.Create(ctx, CreateInput{
		WorkspaceID:      workspace.ID,
		Key:              "alpha-initiative",
		Title:            "Alpha Initiative",
		Kind:             "delivery",
		Status:           "active",
		Summary:          "Coordinate the alpha workstream",
		LinkedProjectID:  &project.ID,
		OwnerCompanionID: nil,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if initiative.WorkspaceID != workspace.ID {
		t.Fatalf("Create().WorkspaceID = %d, want %d", initiative.WorkspaceID, workspace.ID)
	}
	if initiative.LinkedProjectID == nil || *initiative.LinkedProjectID != project.ID {
		t.Fatalf("Create().LinkedProjectID = %v, want %d", initiative.LinkedProjectID, project.ID)
	}

	listed, err := service.ListByWorkspace(ctx, workspace.ID)
	if err != nil {
		t.Fatalf("ListByWorkspace() error = %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("ListByWorkspace() len = %d, want 1", len(listed))
	}
	if listed[0].Key != "alpha-initiative" {
		t.Fatalf("ListByWorkspace()[0].Key = %q, want %q", listed[0].Key, "alpha-initiative")
	}
}

func openInitiativeTestStore(t *testing.T, name string) *sqlite.Store {
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
