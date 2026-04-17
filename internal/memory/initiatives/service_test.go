package initiatives

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestInitiativeServiceRecordsInitiativeOwnedMemoryWithProjectLineage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openMemoryTestStore(t, "initiatives-memory.db")
	defer store.Close()

	workspace := createWorkspace(t, ctx, store, "workspace-a")
	project := createProject(t, ctx, store, "alpha")
	initiative := createInitiative(t, ctx, store, workspace.ID, "alpha-initiative", &project.ID)

	service := Service{
		Store:         store,
		WorkspaceID:   workspace.ID,
		InitiativeID:  initiative.ID,
		InitiativeKey: initiative.Key,
		ProjectID:     &project.ID,
		ProjectKey:    project.Key,
	}

	summary, err := service.Remember(ctx, "initiative_summary", "Alpha keeps its scope tight.", `{"source":"test"}`, nil)
	if err != nil {
		t.Fatalf("Remember() error = %v", err)
	}
	if summary.WorkspaceID == nil || *summary.WorkspaceID != workspace.ID {
		t.Fatalf("summary.WorkspaceID = %v, want %d", summary.WorkspaceID, workspace.ID)
	}
	if summary.InitiativeID == nil || *summary.InitiativeID != initiative.ID {
		t.Fatalf("summary.InitiativeID = %v, want %d", summary.InitiativeID, initiative.ID)
	}
	if summary.ProjectID == nil || *summary.ProjectID != project.ID {
		t.Fatalf("summary.ProjectID = %v, want %d", summary.ProjectID, project.ID)
	}
	if summary.Scope != "project" {
		t.Fatalf("summary.Scope = %q, want %q", summary.Scope, "project")
	}
	if summary.ScopeKey != project.Key {
		t.Fatalf("summary.ScopeKey = %q, want %q", summary.ScopeKey, project.Key)
	}

	listed, err := service.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != summary.ID {
		t.Fatalf("List() = %+v, want recorded initiative memory", listed)
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

func createProject(t *testing.T, ctx context.Context, store *sqlite.Store, key string) sqlite.Project {
	t.Helper()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           key,
		Name:          key,
		Scope:         "project",
		GitRoot:       filepath.Join(t.TempDir(), key),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(%s) error = %v", key, err)
	}
	return project
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
