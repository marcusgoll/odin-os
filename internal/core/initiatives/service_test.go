package initiatives

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestInitiativeServiceReconcilesManagedProjectInitiative(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openInitiativeServiceStore(t)
	defer store.Close()

	workspace := createInitiativeServiceWorkspace(t, ctx, store, "default")
	project := createInitiativeServiceProject(t, ctx, store, "alpha")

	service := Service{Store: store}

	created, err := service.ReconcileManagedProject(ctx, workspace.ID, project, nil)
	if err != nil {
		t.Fatalf("ReconcileManagedProject() error = %v", err)
	}

	if created.WorkspaceID != workspace.ID {
		t.Fatalf("created.WorkspaceID = %d, want %d", created.WorkspaceID, workspace.ID)
	}
	if created.Key != project.Key {
		t.Fatalf("created.Key = %q, want %q", created.Key, project.Key)
	}
	if created.Title != project.Name {
		t.Fatalf("created.Title = %q, want %q", created.Title, project.Name)
	}
	if created.Kind != KindManagedProject {
		t.Fatalf("created.Kind = %q, want %q", created.Kind, KindManagedProject)
	}
	if created.Status != "active" {
		t.Fatalf("created.Status = %q, want %q", created.Status, "active")
	}
	if created.LinkedProjectID == nil || *created.LinkedProjectID != project.ID {
		t.Fatalf("created.LinkedProjectID = %v, want %d", created.LinkedProjectID, project.ID)
	}

	if _, err := store.DB().ExecContext(ctx, `
		UPDATE initiatives
		SET title = ?, kind = ?, status = ?, summary = ?, owner_companion_id = NULL, linked_project_id = NULL
		WHERE workspace_id = ? AND key = ?
	`, "stale title", "goal", "archived", "stale summary", workspace.ID, project.Key); err != nil {
		t.Fatalf("seed initiative drift error = %v", err)
	}

	reconciled, err := service.ReconcileManagedProject(ctx, workspace.ID, project, nil)
	if err != nil {
		t.Fatalf("ReconcileManagedProject() reconcile error = %v", err)
	}

	if reconciled.ID != created.ID {
		t.Fatalf("reconciled.ID = %d, want %d", reconciled.ID, created.ID)
	}
	if reconciled.Title != project.Name {
		t.Fatalf("reconciled.Title = %q, want %q", reconciled.Title, project.Name)
	}
	if reconciled.Kind != KindManagedProject {
		t.Fatalf("reconciled.Kind = %q, want %q", reconciled.Kind, KindManagedProject)
	}
	if reconciled.Status != "active" {
		t.Fatalf("reconciled.Status = %q, want %q", reconciled.Status, "active")
	}
	if reconciled.LinkedProjectID == nil || *reconciled.LinkedProjectID != project.ID {
		t.Fatalf("reconciled.LinkedProjectID = %v, want %d", reconciled.LinkedProjectID, project.ID)
	}
}

func TestInitiativeServiceUpsertsAndListsNonProjectInitiatives(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openInitiativeServiceStore(t)
	defer store.Close()

	workspace := createInitiativeServiceWorkspace(t, ctx, store, "default")
	service := Service{Store: store}

	created, err := service.UpsertNonProject(ctx, workspace.ID, UpsertInput{
		Key:    "life-admin",
		Title:  "Life Admin",
		Kind:   KindRoutine,
		Status: "",
	})
	if err != nil {
		t.Fatalf("UpsertNonProject() error = %v", err)
	}
	if created.Kind != KindRoutine {
		t.Fatalf("created.Kind = %q, want %q", created.Kind, KindRoutine)
	}
	if created.Status != statusActive {
		t.Fatalf("created.Status = %q, want %q", created.Status, statusActive)
	}
	if created.LinkedProjectID != nil {
		t.Fatalf("created.LinkedProjectID = %v, want nil", created.LinkedProjectID)
	}

	again, err := service.UpsertNonProject(ctx, workspace.ID, UpsertInput{
		Key:    "life-admin",
		Title:  "Life Admin",
		Kind:   KindRoutine,
		Status: statusActive,
	})
	if err != nil {
		t.Fatalf("UpsertNonProject() second call error = %v", err)
	}
	if again.ID != created.ID {
		t.Fatalf("again.ID = %d, want %d", again.ID, created.ID)
	}

	initiatives, err := service.ListInitiatives(ctx, workspace.ID)
	if err != nil {
		t.Fatalf("ListInitiatives() error = %v", err)
	}
	if len(initiatives) != 1 {
		t.Fatalf("ListInitiatives() len = %d, want 1", len(initiatives))
	}
	if initiatives[0].Key != "life-admin" {
		t.Fatalf("ListInitiatives()[0].Key = %q, want life-admin", initiatives[0].Key)
	}
	if initiatives[0].Kind != KindRoutine {
		t.Fatalf("ListInitiatives()[0].Kind = %q, want %q", initiatives[0].Kind, KindRoutine)
	}
}

func TestInitiativeServicePausesAndArchivesInitiatives(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openInitiativeServiceStore(t)
	defer store.Close()

	workspace := createInitiativeServiceWorkspace(t, ctx, store, "default")
	service := Service{Store: store}

	created, err := service.UpsertNonProject(ctx, workspace.ID, UpsertInput{
		Key:   "life-admin",
		Title: "Life Admin",
		Kind:  KindRoutine,
	})
	if err != nil {
		t.Fatalf("UpsertNonProject() error = %v", err)
	}

	paused, err := service.PauseInitiative(ctx, workspace.ID, created.Key)
	if err != nil {
		t.Fatalf("PauseInitiative() error = %v", err)
	}
	if paused.Status != statusPaused {
		t.Fatalf("PauseInitiative().Status = %q, want %q", paused.Status, statusPaused)
	}

	archived, err := service.ArchiveInitiative(ctx, workspace.ID, created.Key)
	if err != nil {
		t.Fatalf("ArchiveInitiative() error = %v", err)
	}
	if archived.Status != statusArchived {
		t.Fatalf("ArchiveInitiative().Status = %q, want %q", archived.Status, statusArchived)
	}
}

func TestInitiativeServiceRejectsManagedProjectKinds(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openInitiativeServiceStore(t)
	defer store.Close()

	workspace := createInitiativeServiceWorkspace(t, ctx, store, "default")
	service := Service{Store: store}

	if _, err := service.UpsertNonProject(ctx, workspace.ID, UpsertInput{
		Key:   "life-admin",
		Title: "Life Admin",
		Kind:  KindManagedProject,
	}); err == nil {
		t.Fatal("UpsertNonProject() error = nil, want managed project rejection")
	}
}

func openInitiativeServiceStore(t *testing.T) *sqlite.Store {
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

func createInitiativeServiceWorkspace(t *testing.T, ctx context.Context, store *sqlite.Store, key string) sqlite.Workspace {
	t.Helper()

	workspace, err := store.GetWorkspaceByKey(ctx, key)
	if err == nil {
		return workspace
	}

	workspace, err = store.CreateWorkspace(ctx, sqlite.CreateWorkspaceParams{
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

func createInitiativeServiceProject(t *testing.T, ctx context.Context, store *sqlite.Store, key string) sqlite.Project {
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
