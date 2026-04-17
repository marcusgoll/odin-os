package workitems

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/core/companions"
	"odin-os/internal/core/controlscope"
	"odin-os/internal/core/initiatives"
	"odin-os/internal/core/workspaces"
	"odin-os/internal/store/sqlite"
)

func TestWorkItemCreateFromWorkspaceContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openWorkItemStore(t)
	defer store.Close()

	workspaceService := workspaces.Service{Store: store}
	workspace, err := workspaceService.BootstrapDefault(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefault() error = %v", err)
	}
	if _, err := bootstrapWorkItemProjects(ctx, store); err != nil {
		t.Fatalf("bootstrapWorkItemProjects() error = %v", err)
	}

	service := Service{Store: store}
	workItem, err := service.Create(ctx, controlscope.Service{}.ResolveWorkspace(workspace.Key), "Workspace follow-up")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if workItem.Scope.SubjectType != controlscope.SubjectTypeWorkspace {
		t.Fatalf("Scope.SubjectType = %q, want %q", workItem.Scope.SubjectType, controlscope.SubjectTypeWorkspace)
	}
}

func TestWorkItemCreateFromInitiativeContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openWorkItemStore(t)
	defer store.Close()

	workspaceService := workspaces.Service{Store: store}
	workspace, err := workspaceService.BootstrapDefault(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefault() error = %v", err)
	}
	if _, err := bootstrapWorkItemProjects(ctx, store); err != nil {
		t.Fatalf("bootstrapWorkItemProjects() error = %v", err)
	}

	initiativeService := initiatives.Service{Store: store}
	initiative, err := initiativeService.Create(ctx, initiatives.CreateInput{
		WorkspaceID: workspace.ID,
		Key:         "ops",
		Title:       "Ops",
		Kind:        initiatives.KindManagedProject,
		Status:      initiatives.StatusActive,
		Summary:     "Ops initiative",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	service := Service{Store: store}
	workItem, err := service.Create(ctx, controlscope.Service{}.ResolveInitiative(workspace.Key, initiative.Key), "Initiative follow-up")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if workItem.Scope.SubjectType != controlscope.SubjectTypeInitiative {
		t.Fatalf("Scope.SubjectType = %q, want %q", workItem.Scope.SubjectType, controlscope.SubjectTypeInitiative)
	}
}

func TestWorkItemLinkCompanionAndProject(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openWorkItemStore(t)
	defer store.Close()

	workspaceService := workspaces.Service{Store: store}
	workspace, err := workspaceService.BootstrapDefault(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefault() error = %v", err)
	}
	projectKeys, err := bootstrapWorkItemProjects(ctx, store)
	if err != nil {
		t.Fatalf("bootstrapWorkItemProjects() error = %v", err)
	}

	companionService := companions.Service{Store: store}
	companion, err := companionService.BootstrapDefaultOperator(ctx, workspace.ID)
	if err != nil {
		t.Fatalf("BootstrapDefaultOperator() error = %v", err)
	}

	service := Service{Store: store}
	workItem, err := service.Create(ctx, controlscope.Service{}.ResolveWorkspace(workspace.Key), "Follow-up")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	linked, err := service.LinkCompanion(ctx, workItem.ID, companion.Key)
	if err != nil {
		t.Fatalf("LinkCompanion() error = %v", err)
	}
	if linked.CompanionKey != companion.Key {
		t.Fatalf("LinkCompanion().CompanionKey = %q, want %q", linked.CompanionKey, companion.Key)
	}

	project, err := service.LinkProject(ctx, workItem.ID, projectKeys["alpha"])
	if err != nil {
		t.Fatalf("LinkProject() error = %v", err)
	}
	if project.ProjectKey != "alpha" {
		t.Fatalf("LinkProject().ProjectKey = %q, want %q", project.ProjectKey, "alpha")
	}
	if project.Scope.SubjectType != controlscope.SubjectTypeWorkspace {
		t.Fatalf("LinkProject().Scope.SubjectType = %q, want %q", project.Scope.SubjectType, controlscope.SubjectTypeWorkspace)
	}
	if project.Scope.SubjectKey != workspace.Key {
		t.Fatalf("LinkProject().Scope.SubjectKey = %q, want %q", project.Scope.SubjectKey, workspace.Key)
	}
}

func TestRunAttemptHistoryAcrossRetries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openWorkItemStore(t)
	defer store.Close()

	workspaceService := workspaces.Service{Store: store}
	workspace, err := workspaceService.BootstrapDefault(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefault() error = %v", err)
	}
	if _, err := bootstrapWorkItemProjects(ctx, store); err != nil {
		t.Fatalf("bootstrapWorkItemProjects() error = %v", err)
	}

	service := Service{Store: store}
	workItem, err := service.Create(ctx, controlscope.Service{}.ResolveWorkspace(workspace.Key), "Retryable")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	firstRun, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   workItem.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(first) error = %v", err)
	}
	if _, err := store.FinishRun(ctx, sqlite.FinishRunParams{
		RunID:   firstRun.ID,
		Status:  "failed",
		Summary: "retry",
	}); err != nil {
		t.Fatalf("FinishRun(first) error = %v", err)
	}

	secondRun, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   workItem.ID,
		Executor: "codex",
		Attempt:  2,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(second) error = %v", err)
	}
	if secondRun.Attempt != 2 {
		t.Fatalf("second run attempt = %d, want 2", secondRun.Attempt)
	}
}

func openWorkItemStore(t *testing.T) *sqlite.Store {
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

func bootstrapWorkItemProjects(ctx context.Context, store *sqlite.Store) (map[string]string, error) {
	result := make(map[string]string)

	systemProject, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "odin-core",
		GitRoot:       filepath.Join("/tmp", "odin-core"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		return nil, err
	}
	result["odin-core"] = systemProject.Key

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       filepath.Join("/tmp", "alpha"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		return nil, err
	}
	result["alpha"] = project.Key

	return result, nil
}
