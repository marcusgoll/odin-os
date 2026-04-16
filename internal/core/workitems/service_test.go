package workitems

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"odin-os/internal/core/initiatives"
	"odin-os/internal/core/workspaces"
	"odin-os/internal/store/sqlite"
)

func TestWorkItemServiceRequestApprovalRollsBackBlockedStatusOnFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openWorkItemServiceStore(t)
	defer store.Close()

	workspaceID, projectID, initiativeID, companionID := seedWorkItemLinks(t, ctx, store)
	service := Service{Store: store}

	approvalTask := mustQueueWorkItemTask(t, ctx, service, projectID, workspaceID, initiativeID, companionID, "approval-item")

	if _, err := store.DB().ExecContext(ctx, `
		CREATE TRIGGER approvals_insert_blocker
		BEFORE INSERT ON approvals
		BEGIN
			SELECT RAISE(ABORT, 'approval insert blocked');
		END;
	`); err != nil {
		t.Fatalf("create trigger error = %v", err)
	}

	_, _, err := service.RequestApproval(ctx, approvalTask.ID, nil, "operator")
	if err == nil {
		t.Fatal("RequestApproval() error = nil, want approval insert failure")
	}

	gotTask, err := store.GetTask(ctx, approvalTask.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "queued" {
		t.Fatalf("GetTask().Status = %q, want queued", gotTask.Status)
	}

	var approvalCount int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM approvals
		WHERE task_id = ?
	`, approvalTask.ID).Scan(&approvalCount); err != nil {
		t.Fatalf("count approvals error = %v", err)
	}
	if approvalCount != 0 {
		t.Fatalf("approval count = %d, want 0", approvalCount)
	}
}

func TestWorkItemServiceRejectsApprovalForTerminalTasks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openWorkItemServiceStore(t)
	defer store.Close()

	workspaceID, projectID, initiativeID, companionID := seedWorkItemLinks(t, ctx, store)
	service := Service{Store: store}

	terminalTask := mustQueueWorkItemTask(t, ctx, service, projectID, workspaceID, initiativeID, companionID, "terminal-item")
	completed, err := service.Complete(ctx, terminalTask.ID)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if completed.Status != "completed" {
		t.Fatalf("Complete().Status = %q, want completed", completed.Status)
	}

	_, _, err = service.RequestApproval(ctx, terminalTask.ID, nil, "operator")
	if err == nil {
		t.Fatal("RequestApproval() error = nil, want terminal-task rejection")
	}

	gotTask, err := store.GetTask(ctx, terminalTask.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "completed" {
		t.Fatalf("GetTask().Status = %q, want completed", gotTask.Status)
	}

	var approvalCount int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM approvals
		WHERE task_id = ?
	`, terminalTask.ID).Scan(&approvalCount); err != nil {
		t.Fatalf("count approvals error = %v", err)
	}
	if approvalCount != 0 {
		t.Fatalf("approval count = %d, want 0", approvalCount)
	}
}

func TestWorkItemServiceRejectsApprovalForBlockedTasks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openWorkItemServiceStore(t)
	defer store.Close()

	workspaceID, projectID, initiativeID, companionID := seedWorkItemLinks(t, ctx, store)
	service := Service{Store: store}

	blockedTask := mustQueueWorkItemTask(t, ctx, service, projectID, workspaceID, initiativeID, companionID, "blocked-item")
	blocked, err := service.Block(ctx, blockedTask.ID)
	if err != nil {
		t.Fatalf("Block() error = %v", err)
	}
	if blocked.Status != "blocked" {
		t.Fatalf("Block().Status = %q, want blocked", blocked.Status)
	}

	_, _, err = service.RequestApproval(ctx, blockedTask.ID, nil, "operator")
	if err == nil {
		t.Fatal("RequestApproval() error = nil, want blocked-task rejection")
	}

	gotTask, err := store.GetTask(ctx, blockedTask.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "blocked" {
		t.Fatalf("GetTask().Status = %q, want blocked", gotTask.Status)
	}

	var approvalCount int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM approvals
		WHERE task_id = ?
	`, blockedTask.ID).Scan(&approvalCount); err != nil {
		t.Fatalf("count approvals error = %v", err)
	}
	if approvalCount != 0 {
		t.Fatalf("approval count = %d, want 0", approvalCount)
	}
}

func TestWorkItemServiceRequeuesTasks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openWorkItemServiceStore(t)
	defer store.Close()

	workspaceID, projectID, initiativeID, companionID := seedWorkItemLinks(t, ctx, store)
	service := Service{Store: store}

	runningTask := mustQueueWorkItemTask(t, ctx, service, projectID, workspaceID, initiativeID, companionID, "running-item")
	started, err := service.Start(ctx, runningTask.ID)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if started.Status != "running" {
		t.Fatalf("Start().Status = %q, want running", started.Status)
	}

	requeued, err := service.Requeue(ctx, runningTask.ID)
	if err != nil {
		t.Fatalf("Requeue() error = %v", err)
	}
	if requeued.Status != "queued" {
		t.Fatalf("Requeue().Status = %q, want queued", requeued.Status)
	}
}

func TestWorkItemServiceFinalizesTaskFromExecutorStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openWorkItemServiceStore(t)
	defer store.Close()

	workspaceID, projectID, initiativeID, companionID := seedWorkItemLinks(t, ctx, store)
	service := Service{Store: store}

	completedTask := mustQueueWorkItemTask(t, ctx, service, projectID, workspaceID, initiativeID, companionID, "complete-item")
	if _, err := service.Start(ctx, completedTask.ID); err != nil {
		t.Fatalf("Start(completed) error = %v", err)
	}
	completed, err := service.Finalize(ctx, completedTask.ID, "")
	if err != nil {
		t.Fatalf("Finalize(completed) error = %v", err)
	}
	if completed.Status != "completed" {
		t.Fatalf("Finalize(completed).Status = %q, want completed", completed.Status)
	}

	failedTask := mustQueueWorkItemTask(t, ctx, service, projectID, workspaceID, initiativeID, companionID, "fail-item")
	if _, err := service.Start(ctx, failedTask.ID); err != nil {
		t.Fatalf("Start(failed) error = %v", err)
	}
	failed, err := service.Finalize(ctx, failedTask.ID, "timed_out")
	if err != nil {
		t.Fatalf("Finalize(failed) error = %v", err)
	}
	if failed.Status != "failed" {
		t.Fatalf("Finalize(failed).Status = %q, want failed", failed.Status)
	}
}

func TestWorkItemServiceRejectsStartForBlockedTasks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openWorkItemServiceStore(t)
	defer store.Close()

	workspaceID, projectID, initiativeID, companionID := seedWorkItemLinks(t, ctx, store)
	service := Service{Store: store}

	blockedTask := mustQueueWorkItemTask(t, ctx, service, projectID, workspaceID, initiativeID, companionID, "blocked-start-item")
	if _, err := service.Block(ctx, blockedTask.ID); err != nil {
		t.Fatalf("Block() error = %v", err)
	}

	if _, err := service.Start(ctx, blockedTask.ID); err == nil {
		t.Fatal("Start() error = nil, want blocked-task rejection")
	}

	gotTask, err := store.GetTask(ctx, blockedTask.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "blocked" {
		t.Fatalf("GetTask().Status = %q, want blocked", gotTask.Status)
	}
}

func TestWorkItemServiceRejectsFinalizeForBlockedApprovalTask(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openWorkItemServiceStore(t)
	defer store.Close()

	workspaceID, projectID, initiativeID, companionID := seedWorkItemLinks(t, ctx, store)
	service := Service{Store: store}

	approvalTask := mustQueueWorkItemTask(t, ctx, service, projectID, workspaceID, initiativeID, companionID, "blocked-finalize-item")
	approval, approvalItem, err := service.RequestApproval(ctx, approvalTask.ID, nil, "operator")
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}
	if approvalItem.Status != "blocked" {
		t.Fatalf("RequestApproval().item.Status = %q, want blocked", approvalItem.Status)
	}

	if _, err := service.Finalize(ctx, approvalTask.ID, "completed"); err == nil {
		t.Fatal("Finalize() error = nil, want blocked-task rejection")
	}

	gotTask, err := store.GetTask(ctx, approvalTask.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "blocked" {
		t.Fatalf("GetTask().Status = %q, want blocked", gotTask.Status)
	}

	gotApproval, err := store.GetApproval(ctx, approval.ID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if gotApproval.Status != "pending" {
		t.Fatalf("GetApproval().Status = %q, want pending", gotApproval.Status)
	}
}

func TestWorkItemServiceQueuesTasksAsQueuedEvenWithCallerStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openWorkItemServiceStore(t)
	defer store.Close()

	workspaceID, projectID, initiativeID, companionID := seedWorkItemLinks(t, ctx, store)
	service := Service{Store: store}

	task, err := service.Queue(ctx, sqlite.CreateTaskParams{
		ProjectID:    projectID,
		Key:          "caller-status-item",
		Title:        "Queue work item",
		Status:       "blocked",
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

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "queued" {
		t.Fatalf("GetTask().Status = %q, want queued", gotTask.Status)
	}
}

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
	if item.WorkspaceID == nil || *item.WorkspaceID != workspaceID {
		t.Fatalf("Get().WorkspaceID = %v, want %d", item.WorkspaceID, workspaceID)
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

func TestWorkItemServicePreservesNilWorkspaceID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openWorkItemServiceStore(t)
	defer store.Close()

	_, projectID, initiativeID, companionID := seedWorkItemLinks(t, ctx, store)
	service := Service{Store: store}

	task, err := service.Queue(ctx, sqlite.CreateTaskParams{
		ProjectID:    projectID,
		Key:          "nil-workspace-item",
		Title:        "Queue work item",
		Scope:        "project",
		RequestedBy:  "operator",
		InitiativeID: &initiativeID,
		CompanionID:  &companionID,
		WorkKind:     "delivery",
	})
	if err != nil {
		t.Fatalf("Queue() error = %v", err)
	}
	if task.WorkspaceID != nil {
		t.Fatalf("Queue().WorkspaceID = %v, want nil", task.WorkspaceID)
	}

	item, err := service.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if item.WorkspaceID != nil {
		t.Fatalf("Get().WorkspaceID = %v, want nil", item.WorkspaceID)
	}

	field, ok := reflect.TypeOf(item).FieldByName("WorkspaceID")
	if !ok {
		t.Fatal("WorkItem.WorkspaceID field is missing")
	}
	if field.Type.Kind() != reflect.Ptr || field.Type.Elem().Kind() != reflect.Int64 {
		t.Fatalf("WorkItem.WorkspaceID type = %s, want *int64", field.Type)
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
