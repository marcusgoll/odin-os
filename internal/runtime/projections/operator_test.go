package projections_test

import (
	"context"
	"testing"
	"time"

	"odin-os/internal/core/initiatives"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
)

func TestOperatorProjectionsExposeWorkspaceInitiativesCompanionsAndBlockedWork(t *testing.T) {
	ctx := context.Background()
	store := openObservabilityStore(t)
	defer store.Close()

	project, workspace, initiative, companion := seedOperatorReadModelState(t, ctx, store)

	if _, err := store.CreateFollowUpObligation(ctx, sqlite.CreateFollowUpObligationParams{
		WorkspaceID:     workspace.ID,
		InitiativeID:    &initiative.ID,
		CompanionID:     &companion.ID,
		TargetProjectID: project.ID,
		Title:           "Overdue review",
		Status:          "active",
		CadenceJSON:     `{"mode":"once"}`,
		NextDueAt:       time.Now().UTC().Add(-48 * time.Hour),
		PolicyJSON:      `{}`,
	}); err != nil {
		t.Fatalf("CreateFollowUpObligation() error = %v", err)
	}

	workspaceView, err := projections.GetWorkspaceOverviewView(ctx, store.DB(), workspace.Key)
	if err != nil {
		t.Fatalf("GetWorkspaceOverviewView() error = %v", err)
	}
	if workspaceView.WorkspaceKey != workspace.Key {
		t.Fatalf("workspace key = %q, want %q", workspaceView.WorkspaceKey, workspace.Key)
	}
	if workspaceView.ActiveInitiativeCount != 1 {
		t.Fatalf("workspace active initiatives = %d, want 1", workspaceView.ActiveInitiativeCount)
	}
	if workspaceView.ActiveCompanionCount != 1 {
		t.Fatalf("workspace active companions = %d, want 1", workspaceView.ActiveCompanionCount)
	}
	if workspaceView.OpenWorkItemCount != 1 {
		t.Fatalf("workspace open work items = %d, want 1", workspaceView.OpenWorkItemCount)
	}
	if workspaceView.BlockedWorkItemCount != 1 {
		t.Fatalf("workspace blocked work items = %d, want 1", workspaceView.BlockedWorkItemCount)
	}
	if workspaceView.OverdueFollowUpCount != 1 {
		t.Fatalf("workspace overdue follow-ups = %d, want 1", workspaceView.OverdueFollowUpCount)
	}

	initiativeViews, err := projections.ListInitiativePortfolioViews(ctx, store.DB(), workspace.Key)
	if err != nil {
		t.Fatalf("ListInitiativePortfolioViews() error = %v", err)
	}
	if len(initiativeViews) != 1 {
		t.Fatalf("initiative views len = %d, want 1", len(initiativeViews))
	}
	if initiativeViews[0].InitiativeKey != initiative.Key {
		t.Fatalf("initiative key = %q, want %q", initiativeViews[0].InitiativeKey, initiative.Key)
	}
	if initiativeViews[0].OwnerCompanionKey == nil || *initiativeViews[0].OwnerCompanionKey != companion.Key {
		t.Fatalf("owner companion = %v, want %q", initiativeViews[0].OwnerCompanionKey, companion.Key)
	}
	if initiativeViews[0].LinkedProjectKey == nil || *initiativeViews[0].LinkedProjectKey != project.Key {
		t.Fatalf("linked project key = %v, want %q", initiativeViews[0].LinkedProjectKey, project.Key)
	}
	if initiativeViews[0].OverdueFollowUpCount != 1 {
		t.Fatalf("initiative overdue follow-ups = %d, want 1", initiativeViews[0].OverdueFollowUpCount)
	}

	companionViews, err := projections.ListCompanionAssignmentViews(ctx, store.DB(), workspace.Key)
	if err != nil {
		t.Fatalf("ListCompanionAssignmentViews() error = %v", err)
	}
	if len(companionViews) != 1 {
		t.Fatalf("companion views len = %d, want 1", len(companionViews))
	}
	if companionViews[0].CompanionKey != companion.Key {
		t.Fatalf("companion key = %q, want %q", companionViews[0].CompanionKey, companion.Key)
	}
	if companionViews[0].OwnedInitiativeCount != 1 {
		t.Fatalf("owned initiatives = %d, want 1", companionViews[0].OwnedInitiativeCount)
	}
	if companionViews[0].BlockedWorkItemCount != 1 {
		t.Fatalf("companion blocked work items = %d, want 1", companionViews[0].BlockedWorkItemCount)
	}
	if companionViews[0].OverdueFollowUpCount != 1 {
		t.Fatalf("companion overdue follow-ups = %d, want 1", companionViews[0].OverdueFollowUpCount)
	}

	blockedViews, err := projections.ListBlockedItemViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListBlockedItemViews() error = %v", err)
	}
	if len(blockedViews) != 1 {
		t.Fatalf("blocked views len = %d, want 1", len(blockedViews))
	}
	if blockedViews[0].WorkspaceKey != workspace.Key {
		t.Fatalf("blocked workspace key = %q, want %q", blockedViews[0].WorkspaceKey, workspace.Key)
	}
	if blockedViews[0].InitiativeKey == nil || *blockedViews[0].InitiativeKey != initiative.Key {
		t.Fatalf("blocked initiative key = %v, want %q", blockedViews[0].InitiativeKey, initiative.Key)
	}
	if blockedViews[0].CompanionKey == nil || *blockedViews[0].CompanionKey != companion.Key {
		t.Fatalf("blocked companion key = %v, want %q", blockedViews[0].CompanionKey, companion.Key)
	}
}

func seedOperatorReadModelState(t *testing.T, ctx context.Context, store *sqlite.Store) (sqlite.Project, sqlite.Workspace, sqlite.Initiative, sqlite.Companion) {
	t.Helper()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	workspace, err := store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}
	companion, err := store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey(default) error = %v", err)
	}
	initiative, err := store.UpsertInitiative(ctx, sqlite.UpsertInitiativeParams{
		WorkspaceID:      workspace.ID,
		Key:              project.Key,
		Title:            project.Name,
		Kind:             string(initiatives.KindManagedProject),
		Status:           "active",
		Summary:          "Alpha initiative",
		OwnerCompanionID: &companion.ID,
		LinkedProjectID:  &project.ID,
	})
	if err != nil {
		t.Fatalf("UpsertInitiative() error = %v", err)
	}

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "alpha-task",
		Title:        "Alpha task",
		Status:       "blocked",
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "automation",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	if _, err := store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      task.ID,
		RunID:       &run.ID,
		Status:      "pending",
		RequestedBy: "system",
	}); err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}

	return project, workspace, initiative, companion
}
