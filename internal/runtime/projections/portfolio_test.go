package projections_test

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/core/companions"
	"odin-os/internal/core/controlscope"
	"odin-os/internal/core/workitems"
	"odin-os/internal/core/workspaces"
	"odin-os/internal/runtime/checkpoints"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
)

func TestWorkspacePortfolioProjectionsExposeHomeInitiativesCompanionsAndWorkItems(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openWorkspacePortfolioStore(t)
	defer store.Close()

	fixture := seedWorkspacePortfolioFixture(t, ctx, store)

	homes, err := projections.ListWorkspaceHomeViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListWorkspaceHomeViews() error = %v", err)
	}
	if len(homes) != 1 {
		t.Fatalf("ListWorkspaceHomeViews() len = %d, want 1", len(homes))
	}
	if homes[0].WorkspaceKey != fixture.Workspace.Key {
		t.Fatalf("WorkspaceKey = %q, want %q", homes[0].WorkspaceKey, fixture.Workspace.Key)
	}
	if homes[0].InitiativeCount != 1 || homes[0].CompanionCount != 1 || homes[0].PendingApprovalCount != 1 || homes[0].BlockedItemCount != 1 {
		t.Fatalf("workspace home = %+v, want one initiative/companion/approval/blocked item", homes[0])
	}

	initiatives, err := projections.ListInitiativePortfolioViews(ctx, store.DB(), fixture.Workspace.Key)
	if err != nil {
		t.Fatalf("ListInitiativePortfolioViews() error = %v", err)
	}
	if len(initiatives) != 1 {
		t.Fatalf("ListInitiativePortfolioViews() len = %d, want 1", len(initiatives))
	}
	if initiatives[0].InitiativeKey != fixture.Initiative.Key || initiatives[0].OwnerCompanionKey != fixture.Companion.Key || initiatives[0].OpenWorkItemCount != 1 {
		t.Fatalf("initiative portfolio = %+v, want linked initiative owner and work-item count", initiatives[0])
	}

	workItems, err := projections.ListInitiativeWorkItemViews(ctx, store.DB(), fixture.Workspace.Key, fixture.Initiative.Key)
	if err != nil {
		t.Fatalf("ListInitiativeWorkItemViews() error = %v", err)
	}
	if len(workItems) != 1 || workItems[0].TaskKey != fixture.Task.Key {
		t.Fatalf("initiative work items = %+v, want task %q", workItems, fixture.Task.Key)
	}

	blocked, err := projections.ListWorkspaceBlockedItemViews(ctx, store.DB(), fixture.Workspace.Key)
	if err != nil {
		t.Fatalf("ListWorkspaceBlockedItemViews() error = %v", err)
	}
	if len(blocked) != 1 || blocked[0].TaskKey != fixture.Task.Key || blocked[0].NextStep != "resume once approved" {
		t.Fatalf("blocked items = %+v, want deduped blocked follow-up", blocked)
	}

	approvals, err := projections.ListWorkspacePendingApprovalViews(ctx, store.DB(), fixture.Workspace.Key)
	if err != nil {
		t.Fatalf("ListWorkspacePendingApprovalViews() error = %v", err)
	}
	if len(approvals) != 1 || approvals[0].TaskKey != fixture.Task.Key || approvals[0].Status != "pending" {
		t.Fatalf("pending approvals = %+v, want pending approval for %q", approvals, fixture.Task.Key)
	}
}

func TestWorkspaceBlockedItemsDoNotCollapseTasksFromDifferentProjectsWithSameKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openWorkspacePortfolioStore(t)
	defer store.Close()

	bootstrapped, err := workspaces.Service{Store: store}.BootstrapDefault(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefault() error = %v", err)
	}
	workspace, err := store.GetWorkspaceByKey(ctx, bootstrapped.Key)
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(%s) error = %v", bootstrapped.Key, err)
	}

	projectAlpha, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       filepath.Join(t.TempDir(), "alpha"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(alpha) error = %v", err)
	}
	projectBeta, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "beta",
		Name:          "Beta",
		Scope:         "project",
		GitRoot:       filepath.Join(t.TempDir(), "beta"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(beta) error = %v", err)
	}

	initiativeAlpha, err := store.GetInitiativeByProjectID(ctx, projectAlpha.ID)
	if err != nil {
		t.Fatalf("GetInitiativeByProjectID(alpha) error = %v", err)
	}
	initiativeBeta, err := store.GetInitiativeByProjectID(ctx, projectBeta.ID)
	if err != nil {
		t.Fatalf("GetInitiativeByProjectID(beta) error = %v", err)
	}

	taskAlpha, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    projectAlpha.ID,
		WorkspaceID:  workspace.ID,
		InitiativeID: &initiativeAlpha.ID,
		Key:          "shared-task",
		Title:        "Alpha blocked task",
		Status:       "blocked",
		Scope:        "project",
		RequestedBy:  "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(alpha) error = %v", err)
	}
	taskBeta, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    projectBeta.ID,
		WorkspaceID:  workspace.ID,
		InitiativeID: &initiativeBeta.ID,
		Key:          "shared-task",
		Title:        "Beta blocked task",
		Status:       "blocked",
		Scope:        "project",
		RequestedBy:  "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(beta) error = %v", err)
	}

	if _, err := store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      taskAlpha.ID,
		Status:      "pending",
		RequestedBy: "system",
	}); err != nil {
		t.Fatalf("RequestApproval(alpha) error = %v", err)
	}
	if _, err := store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      taskBeta.ID,
		Status:      "pending",
		RequestedBy: "system",
	}); err != nil {
		t.Fatalf("RequestApproval(beta) error = %v", err)
	}

	blocked, err := projections.ListWorkspaceBlockedItemViews(ctx, store.DB(), workspace.Key)
	if err != nil {
		t.Fatalf("ListWorkspaceBlockedItemViews() error = %v", err)
	}
	if len(blocked) != 2 {
		t.Fatalf("ListWorkspaceBlockedItemViews() len = %d, want 2 distinct tasks", len(blocked))
	}
}

func TestInitiativePortfolioOpenWorkItemCountMatchesProjectPortfolioSemantics(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openWorkspacePortfolioStore(t)
	defer store.Close()

	fixture := seedWorkspacePortfolioFixture(t, ctx, store)

	if _, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    fixture.Task.ProjectID,
		WorkspaceID:  fixture.Workspace.ID,
		InitiativeID: &fixture.Initiative.ID,
		Key:          "failed-task-1",
		Title:        "Failed task 1",
		Status:       "failed",
		Scope:        "odin-core",
		RequestedBy:  "operator",
	}); err != nil {
		t.Fatalf("CreateTask(failed-task-1) error = %v", err)
	}
	if _, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    fixture.Task.ProjectID,
		WorkspaceID:  fixture.Workspace.ID,
		InitiativeID: &fixture.Initiative.ID,
		Key:          "failed-task-2",
		Title:        "Failed task 2",
		Status:       "failed",
		Scope:        "odin-core",
		RequestedBy:  "operator",
	}); err != nil {
		t.Fatalf("CreateTask(failed-task-2) error = %v", err)
	}

	initiatives, err := projections.ListInitiativePortfolioViews(ctx, store.DB(), fixture.Workspace.Key)
	if err != nil {
		t.Fatalf("ListInitiativePortfolioViews() error = %v", err)
	}
	if len(initiatives) != 1 {
		t.Fatalf("ListInitiativePortfolioViews() len = %d, want 1", len(initiatives))
	}
	if initiatives[0].OpenWorkItemCount != 3 {
		t.Fatalf("OpenWorkItemCount = %d, want 3 (blocked + 2 failed tasks)", initiatives[0].OpenWorkItemCount)
	}
}

type workspacePortfolioFixture struct {
	Workspace  sqlite.Workspace
	Initiative sqlite.Initiative
	Companion  sqlite.Companion
	Task       sqlite.Task
}

func openWorkspacePortfolioStore(t *testing.T) *sqlite.Store {
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

func seedWorkspacePortfolioFixture(t *testing.T, ctx context.Context, store *sqlite.Store) workspacePortfolioFixture {
	t.Helper()

	bootstrapped, err := workspaces.Service{Store: store}.BootstrapDefault(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefault() error = %v", err)
	}
	workspace, err := store.GetWorkspaceByKey(ctx, bootstrapped.Key)
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(%s) error = %v", bootstrapped.Key, err)
	}
	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "odin-core",
		GitRoot:       filepath.Join(t.TempDir(), "odin-core"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(odin-core) error = %v", err)
	}

	companion, err := store.CreateCompanion(ctx, sqlite.CreateCompanionParams{
		WorkspaceID:         workspace.ID,
		Key:                 "operator",
		Title:               "Operator",
		Kind:                companions.KindOperator,
		Charter:             "Run the workspace rhythm.",
		Status:              companions.StatusActive,
		InitiativeScopeJSON: `{"mode":"all"}`,
		ToolPolicyJSON:      `{"mode":"deny","allowed":[]}`,
		MemoryPolicyJSON:    `{"retention":"workspace"}`,
		PlanningPolicyJSON:  `{"mode":"stepwise"}`,
	})
	if err != nil {
		t.Fatalf("CreateCompanion() error = %v", err)
	}

	initiative, err := store.GetInitiativeByProjectID(ctx, project.ID)
	if err != nil {
		t.Fatalf("GetInitiativeByProjectID() error = %v", err)
	}
	if err := store.AssignInitiativeCompanion(ctx, initiative.ID, companion.ID); err != nil {
		t.Fatalf("AssignInitiativeCompanion() error = %v", err)
	}
	initiative, err = store.GetInitiative(ctx, initiative.ID)
	if err != nil {
		t.Fatalf("GetInitiative() error = %v", err)
	}

	workItem, err := workitems.Service{Store: store}.Create(ctx, controlscope.Service{}.ResolveInitiative(workspace.Key, initiative.Key), "Follow up on approvals")
	if err != nil {
		t.Fatalf("Create(work item) error = %v", err)
	}

	task, err := store.GetTask(ctx, workItem.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	if _, err := store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID: task.ID,
		Status: "blocked",
	}); err != nil {
		t.Fatalf("UpdateTaskStatus(blocked) error = %v", err)
	}
	if _, err := store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      task.ID,
		RunID:       &run.ID,
		Status:      "pending",
		RequestedBy: "system",
	}); err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}
	if _, err := (checkpoints.Service{Store: store}).Compact(ctx, checkpoints.CompactParams{
		TaskID:          task.ID,
		RunID:           &run.ID,
		Trigger:         checkpoints.TriggerApprovalWait,
		CheckpointKey:   "workspace-home",
		Objective:       task.Title,
		TaskStatus:      "blocked",
		BlockingReason:  "awaiting operator approval",
		NextSteps:       []string{"resume once approved"},
		ManifestSummary: "workspace task",
		PolicySummary:   "approval required",
		OpenTaskSummary: "one blocked task",
		ApprovalSummary: "one pending approval",
	}); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	return workspacePortfolioFixture{
		Workspace:  workspace,
		Initiative: initiative,
		Companion:  companion,
		Task:       task,
	}
}
