package workitems

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/core/initiatives"
	"odin-os/internal/core/workspaces"
	"odin-os/internal/store/sqlite"
)

func TestWorkItemServiceQueuesTasksWithSemanticLinks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openWorkItemServiceStore(t)
	defer store.Close()

	workspaceID, projectID, initiativeID, companionID := seedWorkItemLinks(t, ctx, store)
	service := Service{Store: store}

	task, err := service.Queue(ctx, sqlite.CreateTaskParams{
		ProjectID:    projectID,
		Key:          "work-item-queue",
		Title:        "Queue work item",
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspaceID,
		InitiativeID: &initiativeID,
		CompanionID:  &companionID,
		WorkKind:     "delivery",
	})
	if err != nil {
		t.Fatalf("Queue() error = %v", err)
	}

	if task.Status != "queued" {
		t.Fatalf("Queue().Status = %q, want queued", task.Status)
	}
	if task.WorkspaceID == nil || *task.WorkspaceID != workspaceID {
		t.Fatalf("Queue().WorkspaceID = %v, want %d", task.WorkspaceID, workspaceID)
	}
	if task.InitiativeID == nil || *task.InitiativeID != initiativeID {
		t.Fatalf("Queue().InitiativeID = %v, want %d", task.InitiativeID, initiativeID)
	}
	if task.CompanionID == nil || *task.CompanionID != companionID {
		t.Fatalf("Queue().CompanionID = %v, want %d", task.CompanionID, companionID)
	}
	if task.WorkKind != "delivery" {
		t.Fatalf("Queue().WorkKind = %q, want delivery", task.WorkKind)
	}

	item, err := service.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if item.ID != task.ID {
		t.Fatalf("Get().ID = %d, want %d", item.ID, task.ID)
	}
	if item.WorkspaceID != workspaceID {
		t.Fatalf("Get().WorkspaceID = %d, want %d", item.WorkspaceID, workspaceID)
	}
	if item.InitiativeID == nil || *item.InitiativeID != initiativeID {
		t.Fatalf("Get().InitiativeID = %v, want %d", item.InitiativeID, initiativeID)
	}
	if item.CompanionID == nil || *item.CompanionID != companionID {
		t.Fatalf("Get().CompanionID = %v, want %d", item.CompanionID, companionID)
	}
	if item.WorkKind != "delivery" {
		t.Fatalf("Get().WorkKind = %q, want delivery", item.WorkKind)
	}
	if item.Status != "queued" {
		t.Fatalf("Get().Status = %q, want queued", item.Status)
	}
}

func TestWorkItemServiceTransitionsAndApproval(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openWorkItemServiceStore(t)
	defer store.Close()

	workspaceID, projectID, initiativeID, companionID := seedWorkItemLinks(t, ctx, store)
	service := Service{Store: store}

	startingTask := mustQueueWorkItemTask(t, ctx, service, projectID, workspaceID, initiativeID, companionID, "start-item")
	started, err := service.Start(ctx, startingTask.ID)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if started.Status != "running" {
		t.Fatalf("Start().Status = %q, want running", started.Status)
	}

	blockingTask := mustQueueWorkItemTask(t, ctx, service, projectID, workspaceID, initiativeID, companionID, "block-item")
	blocked, err := service.Block(ctx, blockingTask.ID)
	if err != nil {
		t.Fatalf("Block() error = %v", err)
	}
	if blocked.Status != "blocked" {
		t.Fatalf("Block().Status = %q, want blocked", blocked.Status)
	}

	completingTask := mustQueueWorkItemTask(t, ctx, service, projectID, workspaceID, initiativeID, companionID, "complete-item")
	completed, err := service.Complete(ctx, completingTask.ID)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if completed.Status != "completed" {
		t.Fatalf("Complete().Status = %q, want completed", completed.Status)
	}

	failingTask := mustQueueWorkItemTask(t, ctx, service, projectID, workspaceID, initiativeID, companionID, "fail-item")
	failed, err := service.Fail(ctx, failingTask.ID)
	if err != nil {
		t.Fatalf("Fail() error = %v", err)
	}
	if failed.Status != "failed" {
		t.Fatalf("Fail().Status = %q, want failed", failed.Status)
	}

	approvalTask := mustQueueWorkItemTask(t, ctx, service, projectID, workspaceID, initiativeID, companionID, "approval-item")
	approval, approvalItem, err := service.RequestApproval(ctx, approvalTask.ID, nil, "operator")
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}
	if approval.Status != "pending" {
		t.Fatalf("RequestApproval().Status = %q, want pending", approval.Status)
	}
	if approvalItem.Status != "blocked" {
		t.Fatalf("RequestApproval().item.Status = %q, want blocked", approvalItem.Status)
	}

	gotApproval, err := store.GetApproval(ctx, approval.ID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if gotApproval.Status != "pending" {
		t.Fatalf("GetApproval().Status = %q, want pending", gotApproval.Status)
	}
}

func openWorkItemServiceStore(t *testing.T) *sqlite.Store {
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

func seedWorkItemLinks(t *testing.T, ctx context.Context, store *sqlite.Store) (workspaceID, projectID, initiativeID, companionID int64) {
	t.Helper()

	workspace, err := workspaces.Service{Store: store}.BootstrapDefaultWorkspace(ctx)
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

	initiative, err := initiatives.Service{Store: store}.ReconcileManagedProject(ctx, workspace.ID, project, nil)
	if err != nil {
		t.Fatalf("ReconcileManagedProject() error = %v", err)
	}

	companion, err := store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey() error = %v", err)
	}

	return workspace.ID, project.ID, initiative.ID, companion.ID
}

func mustQueueWorkItemTask(t *testing.T, ctx context.Context, service Service, projectID, workspaceID int64, initiativeID, companionID int64, key string) sqlite.Task {
	t.Helper()

	task, err := service.Queue(ctx, sqlite.CreateTaskParams{
		ProjectID:    projectID,
		Key:          key,
		Title:        key,
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspaceID,
		InitiativeID: &initiativeID,
		CompanionID:  &companionID,
		WorkKind:     "delivery",
	})
	if err != nil {
		t.Fatalf("Queue(%s) error = %v", key, err)
	}
	return task
}
