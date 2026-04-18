package projections_test

import (
	"context"
	"testing"
	"time"

	"odin-os/internal/core/initiatives"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
)

func TestCompanionSwarmViewsSummarizeActiveWorkAndBacklog(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openObservabilityStore(t)
	defer store.Close()

	workspace, project, initiative, companion := seedCompanionSwarmState(t, ctx, store)
	_ = workspace
	_ = initiative
	_ = companion

	activeTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "active-swarm",
		Title:        "Active companion swarm",
		ActionKey:    "execute",
		Status:       "running",
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "delivery",
	})
	if err != nil {
		t.Fatalf("CreateTask(active) error = %v", err)
	}
	if _, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:     activeTask.ID,
		Executor:   "codex",
		Attempt:    1,
		Status:     "running",
		TaskStatus: "running",
	}); err != nil {
		t.Fatalf("StartRun(active) error = %v", err)
	}

	activeDelegation, err := store.CreateDelegation(ctx, sqlite.CreateDelegationParams{
		ParentTaskID:    activeTask.ID,
		ProjectID:       project.ID,
		Scope:           activeTask.Scope,
		DelegationKey:   "active-a",
		Role:            "builder",
		ActionClass:     "mutation",
		ActionKey:       "implement",
		MutationMode:    "read_only",
		Status:          "queued",
		ConvergenceMode: "merge",
		ArtifactTarget:  "branch",
		Executor:        "codex",
		DetailsJSON:     `{"objective":"implement active swarm","swarm":{"requested_budget":2,"max_children":2}}`,
	})
	if err != nil {
		t.Fatalf("CreateDelegation(active) error = %v", err)
	}
	activeChild, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "active-child",
		Title:       "Active child task",
		ActionKey:   "implement",
		Status:      "running",
		Scope:       activeTask.Scope,
		RequestedBy: "supervisor",
	})
	if err != nil {
		t.Fatalf("CreateTask(active child) error = %v", err)
	}
	activeRun, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:     activeChild.ID,
		Executor:   "codex",
		Attempt:    1,
		Status:     "running",
		TaskStatus: "running",
	})
	if err != nil {
		t.Fatalf("StartRun(active child) error = %v", err)
	}
	if _, err := store.AttachDelegationChildTask(ctx, sqlite.AttachDelegationChildTaskParams{
		DelegationID: activeDelegation.ID,
		ChildTaskID:  activeChild.ID,
		ChildRunID:   &activeRun.ID,
	}); err != nil {
		t.Fatalf("AttachDelegationChildTask(active) error = %v", err)
	}
	if _, err := store.CreateDelegationArtifact(ctx, sqlite.CreateDelegationArtifactParams{
		DelegationID: activeDelegation.ID,
		ArtifactType: "result",
		Summary:      "Active child completed",
		DetailsJSON:  `{"status":"completed","confidence":0.9,"evidence_refs":["active/child"],"unresolved_risks":[],"proposed_next_actions":[],"proposed_memory_candidates":[]}`,
	}); err != nil {
		t.Fatalf("CreateDelegationArtifact(active) error = %v", err)
	}

	queuedDelegation, err := store.CreateDelegation(ctx, sqlite.CreateDelegationParams{
		ParentTaskID:    activeTask.ID,
		ProjectID:       project.ID,
		Scope:           activeTask.Scope,
		DelegationKey:   "active-b",
		Role:            "reviewer",
		ActionClass:     "analysis",
		ActionKey:       "review",
		MutationMode:    "read_only",
		Status:          "queued",
		ConvergenceMode: "merge",
		ArtifactTarget:  "report",
		Executor:        "codex",
		DetailsJSON:     `{"objective":"queue remaining work","swarm":{"requested_budget":2,"max_children":2}}`,
	})
	if err != nil {
		t.Fatalf("CreateDelegation(active queued) error = %v", err)
	}
	queuedChild, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "active-backlog-child",
		Title:       "Backlog child task",
		ActionKey:   "review",
		Status:      "queued",
		Scope:       activeTask.Scope,
		RequestedBy: "supervisor",
	})
	if err != nil {
		t.Fatalf("CreateTask(active backlog child) error = %v", err)
	}
	if _, err := store.AttachDelegationChildTask(ctx, sqlite.AttachDelegationChildTaskParams{
		DelegationID: queuedDelegation.ID,
		ChildTaskID:  queuedChild.ID,
	}); err != nil {
		t.Fatalf("AttachDelegationChildTask(active backlog) error = %v", err)
	}

	views, err := projections.ListCompanionSwarmViews(ctx, store.DB(), workspace.Key)
	if err != nil {
		t.Fatalf("ListCompanionSwarmViews() error = %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("swarm views len = %d, want 1", len(views))
	}
	view := views[0]
	if view.ParentTaskKey != activeTask.Key {
		t.Fatalf("parent task key = %q, want %q", view.ParentTaskKey, activeTask.Key)
	}
	if view.Status != "running" {
		t.Fatalf("swarm status = %q, want running", view.Status)
	}
	if view.DelegationCount != 2 {
		t.Fatalf("delegation count = %d, want 2", view.DelegationCount)
	}
	if view.CompletedDelegationCount != 1 {
		t.Fatalf("completed delegation count = %d, want 1", view.CompletedDelegationCount)
	}
	if view.ActiveChildRunCount != 1 {
		t.Fatalf("active child run count = %d, want 1", view.ActiveChildRunCount)
	}
	if view.BacklogCount != 1 {
		t.Fatalf("backlog count = %d, want 1", view.BacklogCount)
	}
	if view.BlockedReason != "" {
		t.Fatalf("blocked reason = %q, want empty", view.BlockedReason)
	}
}

func TestCompanionSwarmViewsReportApprovalAndBudgetBlocks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openObservabilityStore(t)
	defer store.Close()

	workspace, project, initiative, companion := seedCompanionSwarmState(t, ctx, store)

	approvalTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "approval-swarm",
		Title:        "Approval blocked swarm",
		ActionKey:    "execute",
		Status:       "running",
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "delivery",
	})
	if err != nil {
		t.Fatalf("CreateTask(approval) error = %v", err)
	}
	approvalDelegation, err := store.CreateDelegation(ctx, sqlite.CreateDelegationParams{
		ParentTaskID:    approvalTask.ID,
		ProjectID:       project.ID,
		Scope:           approvalTask.Scope,
		DelegationKey:   "approval-child",
		Role:            "builder",
		ActionClass:     "mutation",
		ActionKey:       "implement",
		MutationMode:    "read_only",
		Status:          "queued",
		ConvergenceMode: "review_gate",
		ArtifactTarget:  "branch",
		Executor:        "codex",
		DetailsJSON:     `{"objective":"await approval","swarm":{"requested_budget":1,"max_children":1}}`,
	})
	if err != nil {
		t.Fatalf("CreateDelegation(approval) error = %v", err)
	}
	approvalChild, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "approval-child",
		Title:       "Approval child task",
		ActionKey:   "implement",
		Status:      "running",
		Scope:       approvalTask.Scope,
		RequestedBy: "supervisor",
	})
	if err != nil {
		t.Fatalf("CreateTask(approval child) error = %v", err)
	}
	if _, _, err := store.BlockTaskAndRequestApproval(ctx, sqlite.BlockTaskAndRequestApprovalParams{
		TaskID:      approvalChild.ID,
		RunID:       nil,
		RequestedBy: "system",
	}); err != nil {
		t.Fatalf("BlockTaskAndRequestApproval() error = %v", err)
	}
	if _, err := store.AttachDelegationChildTask(ctx, sqlite.AttachDelegationChildTaskParams{
		DelegationID: approvalDelegation.ID,
		ChildTaskID:  approvalChild.ID,
	}); err != nil {
		t.Fatalf("AttachDelegationChildTask(approval) error = %v", err)
	}

	budgetTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "budget-swarm",
		Title:        "Budget blocked swarm",
		ActionKey:    "execute",
		Status:       "blocked",
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "delivery",
	})
	if err != nil {
		t.Fatalf("CreateTask(budget) error = %v", err)
	}
	if _, err := store.BlockTask(ctx, sqlite.BlockTaskParams{
		TaskID: budgetTask.ID,
		Reason: "budget_exhausted",
	}); err != nil {
		t.Fatalf("BlockTask(budget) error = %v", err)
	}
	budgetDelegation, err := store.CreateDelegation(ctx, sqlite.CreateDelegationParams{
		ParentTaskID:    budgetTask.ID,
		ProjectID:       project.ID,
		Scope:           budgetTask.Scope,
		DelegationKey:   "budget-child",
		Role:            "reviewer",
		ActionClass:     "analysis",
		ActionKey:       "review",
		MutationMode:    "read_only",
		Status:          "queued",
		ConvergenceMode: "merge",
		ArtifactTarget:  "report",
		Executor:        "codex",
		DetailsJSON:     `{"objective":"bounded by budget","swarm":{"requested_budget":3,"max_children":1}}`,
	})
	if err != nil {
		t.Fatalf("CreateDelegation(budget) error = %v", err)
	}
	budgetChild, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "budget-child",
		Title:       "Budget child task",
		ActionKey:   "review",
		Status:      "queued",
		Scope:       budgetTask.Scope,
		RequestedBy: "supervisor",
	})
	if err != nil {
		t.Fatalf("CreateTask(budget child) error = %v", err)
	}
	if _, err := store.AttachDelegationChildTask(ctx, sqlite.AttachDelegationChildTaskParams{
		DelegationID: budgetDelegation.ID,
		ChildTaskID:  budgetChild.ID,
	}); err != nil {
		t.Fatalf("AttachDelegationChildTask(budget) error = %v", err)
	}

	views, err := projections.ListCompanionSwarmViews(ctx, store.DB(), workspace.Key)
	if err != nil {
		t.Fatalf("ListCompanionSwarmViews() error = %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("swarm views len = %d, want 2", len(views))
	}

	foundApproval := false
	foundBudget := false
	for _, view := range views {
		switch view.ParentTaskKey {
		case approvalTask.Key:
			foundApproval = true
			if view.Status != "blocked" {
				t.Fatalf("approval swarm status = %q, want blocked", view.Status)
			}
			if view.BlockedReason != "approval_required" {
				t.Fatalf("approval swarm blocked reason = %q, want approval_required", view.BlockedReason)
			}
		case budgetTask.Key:
			foundBudget = true
			if view.Status != "blocked" {
				t.Fatalf("budget swarm status = %q, want blocked", view.Status)
			}
			if view.BlockedReason != "budget_exhausted" {
				t.Fatalf("budget swarm blocked reason = %q, want budget_exhausted", view.BlockedReason)
			}
		}
	}
	if !foundApproval {
		t.Fatal("approval swarm view missing")
	}
	if !foundBudget {
		t.Fatal("budget swarm view missing")
	}
}

func TestAgendaViewIncludesCompanionOwnedSwarms(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openObservabilityStore(t)
	defer store.Close()

	workspace, project, initiative, companion := seedCompanionSwarmState(t, ctx, store)
	_, _ = project, initiative

	activeTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "agenda-active-swarm",
		Title:        "Agenda active swarm",
		ActionKey:    "execute",
		Status:       "running",
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "delivery",
	})
	if err != nil {
		t.Fatalf("CreateTask(active) error = %v", err)
	}
	activeDelegation, err := store.CreateDelegation(ctx, sqlite.CreateDelegationParams{
		ParentTaskID:    activeTask.ID,
		ProjectID:       project.ID,
		Scope:           activeTask.Scope,
		DelegationKey:   "agenda-active-child",
		Role:            "builder",
		ActionClass:     "mutation",
		ActionKey:       "implement",
		MutationMode:    "read_only",
		Status:          "queued",
		ConvergenceMode: "merge",
		ArtifactTarget:  "branch",
		Executor:        "codex",
		DetailsJSON:     `{"objective":"agenda active","swarm":{"requested_budget":1,"max_children":1}}`,
	})
	if err != nil {
		t.Fatalf("CreateDelegation(active) error = %v", err)
	}
	activeChild, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "agenda-active-child",
		Title:       "Agenda active child",
		ActionKey:   "implement",
		Status:      "running",
		Scope:       activeTask.Scope,
		RequestedBy: "supervisor",
	})
	if err != nil {
		t.Fatalf("CreateTask(active child) error = %v", err)
	}
	if _, err := store.AttachDelegationChildTask(ctx, sqlite.AttachDelegationChildTaskParams{
		DelegationID: activeDelegation.ID,
		ChildTaskID:  activeChild.ID,
	}); err != nil {
		t.Fatalf("AttachDelegationChildTask(active) error = %v", err)
	}

	blockedTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "agenda-blocked-swarm",
		Title:        "Agenda blocked swarm",
		ActionKey:    "execute",
		Status:       "running",
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "delivery",
	})
	if err != nil {
		t.Fatalf("CreateTask(blocked) error = %v", err)
	}
	blockedDelegation, err := store.CreateDelegation(ctx, sqlite.CreateDelegationParams{
		ParentTaskID:    blockedTask.ID,
		ProjectID:       project.ID,
		Scope:           blockedTask.Scope,
		DelegationKey:   "agenda-blocked-child",
		Role:            "builder",
		ActionClass:     "mutation",
		ActionKey:       "implement",
		MutationMode:    "read_only",
		Status:          "queued",
		ConvergenceMode: "review_gate",
		ArtifactTarget:  "branch",
		Executor:        "codex",
		DetailsJSON:     `{"objective":"agenda blocked","swarm":{"requested_budget":1,"max_children":1}}`,
	})
	if err != nil {
		t.Fatalf("CreateDelegation(blocked) error = %v", err)
	}
	blockedChild, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "agenda-blocked-child",
		Title:       "Agenda blocked child",
		ActionKey:   "implement",
		Status:      "running",
		Scope:       blockedTask.Scope,
		RequestedBy: "supervisor",
	})
	if err != nil {
		t.Fatalf("CreateTask(blocked child) error = %v", err)
	}
	if _, _, err := store.BlockTaskAndRequestApproval(ctx, sqlite.BlockTaskAndRequestApprovalParams{
		TaskID:      blockedChild.ID,
		RunID:       nil,
		RequestedBy: "system",
	}); err != nil {
		t.Fatalf("BlockTaskAndRequestApproval(blocked child) error = %v", err)
	}
	if _, err := store.AttachDelegationChildTask(ctx, sqlite.AttachDelegationChildTaskParams{
		DelegationID: blockedDelegation.ID,
		ChildTaskID:  blockedChild.ID,
	}); err != nil {
		t.Fatalf("AttachDelegationChildTask(blocked) error = %v", err)
	}

	agenda, err := projections.GetAgendaView(ctx, store.DB(), workspace.Key, time.Now().UTC())
	if err != nil {
		t.Fatalf("GetAgendaView() error = %v", err)
	}
	if len(agenda.CompanionSwarms) != 2 {
		t.Fatalf("agenda companion swarms len = %d, want 2", len(agenda.CompanionSwarms))
	}
	if agenda.CompanionSwarms[0].ParentTaskKey != activeTask.Key && agenda.CompanionSwarms[1].ParentTaskKey != activeTask.Key {
		t.Fatalf("agenda companion swarms = %+v, want active swarm included", agenda.CompanionSwarms)
	}
	if agenda.CompanionSwarms[0].ParentTaskKey != blockedTask.Key && agenda.CompanionSwarms[1].ParentTaskKey != blockedTask.Key {
		t.Fatalf("agenda companion swarms = %+v, want blocked swarm included", agenda.CompanionSwarms)
	}
}

func seedCompanionSwarmState(t *testing.T, ctx context.Context, store *sqlite.Store) (sqlite.Workspace, sqlite.Project, sqlite.Initiative, sqlite.Companion) {
	t.Helper()

	workspace, err := store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}
	companion, err := store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey(default) error = %v", err)
	}
	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "swarm-project",
		Name:          "Swarm Project",
		Scope:         "project",
		GitRoot:       t.TempDir(),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	initiative, err := store.UpsertInitiative(ctx, sqlite.UpsertInitiativeParams{
		WorkspaceID:      workspace.ID,
		Key:              project.Key,
		Title:            project.Name,
		Kind:             string(initiatives.KindManagedProject),
		Status:           "active",
		Summary:          "Swarm initiative",
		OwnerCompanionID: &companion.ID,
		LinkedProjectID:  &project.ID,
	})
	if err != nil {
		t.Fatalf("UpsertInitiative() error = %v", err)
	}

	return workspace, project, initiative, companion
}
