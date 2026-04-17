package integration_test

import (
	"context"
	"testing"

	companionsvc "odin-os/internal/core/companions"
	workitemsvc "odin-os/internal/core/workitems"
	jobsvc "odin-os/internal/runtime/jobs"
	"odin-os/internal/runtime/projections"
	supervisionsvc "odin-os/internal/runtime/supervision"
	"odin-os/internal/store/sqlite"
)

func TestCompanionSwarmCreatesChildWorkItems(t *testing.T) {
	ctx := context.Background()
	store := openTempStore(t)
	defer store.Close()

	workspace, err := companionsWorkspace(ctx, store)
	if err != nil {
		t.Fatalf("companionsWorkspace() error = %v", err)
	}

	companion, err := companionsvc.Service{Store: store}.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey() error = %v", err)
	}

	initiative, err := store.UpsertInitiative(ctx, sqlite.UpsertInitiativeParams{
		WorkspaceID:      workspace.ID,
		Key:              "swarm-init",
		Title:            "Swarm Initiative",
		Kind:             "delivery",
		Status:           "active",
		Summary:          "Materialize child work items",
		OwnerCompanionID: &companion.ID,
	})
	if err != nil {
		t.Fatalf("UpsertInitiative() error = %v", err)
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

	parentTask, err := workitemsvc.Service{Store: store}.Queue(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "parent-task",
		Title:        "Parent swarm task",
		ActionKey:    "execute",
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "delivery",
	})
	if err != nil {
		t.Fatalf("Queue(parent task) error = %v", err)
	}

	service := supervisionsvc.Service{
		Store: store,
		Jobs:  jobsvc.Service{Store: store},
	}
	swarm, err := service.PlanSwarm(ctx, supervisionsvc.PlanSwarmParams{
		ParentTaskID:    parentTask.ID,
		Trigger:         supervisionsvc.TriggerBuildPlusReview,
		ConvergenceMode: "review_gate",
		RequestedBudget: 2,
		DelegationPlans: []supervisionsvc.DelegationPlan{
			{
				DelegationKey:         "implement",
				Role:                  "builder",
				ActionClass:           "mutation",
				ActionKey:             "implement",
				MutationMode:          "isolated_worktree",
				ArtifactTarget:        "branch",
				Objective:             "Implement the requested change",
				RequestedTools:        []string{"repo_read", "branch_proposal"},
				RequestedMemoryScopes: []string{"workspace", "initiative", "companion"},
			},
			{
				DelegationKey:         "review",
				Role:                  "reviewer",
				ActionClass:           "analysis",
				ActionKey:             "review",
				MutationMode:          "read_only",
				ArtifactTarget:        "report",
				Objective:             "Review the implementation",
				RequestedTools:        []string{"repo_read"},
				RequestedMemoryScopes: []string{"workspace", "initiative", "companion"},
			},
		},
	})
	if err != nil {
		t.Fatalf("PlanSwarm() error = %v", err)
	}

	materialized, err := service.MaterializeSwarm(ctx, swarm)
	if err != nil {
		t.Fatalf("MaterializeSwarm() error = %v", err)
	}

	if len(materialized.Delegations) != 2 {
		t.Fatalf("materialized delegations len = %d, want 2", len(materialized.Delegations))
	}

	childKeys := make(map[string]struct{}, len(materialized.Delegations))
	for _, delegation := range materialized.Delegations {
		if delegation.ChildTaskID == nil {
			t.Fatalf("delegation %q ChildTaskID = nil, want materialized child task", delegation.DelegationKey)
		}

		childTask, err := store.GetTask(ctx, *delegation.ChildTaskID)
		if err != nil {
			t.Fatalf("GetTask(child %q) error = %v", delegation.DelegationKey, err)
		}
		if childTask.WorkspaceID == nil || *childTask.WorkspaceID != workspace.ID {
			t.Fatalf("child %q WorkspaceID = %v, want %d", delegation.DelegationKey, childTask.WorkspaceID, workspace.ID)
		}
		if childTask.InitiativeID == nil || *childTask.InitiativeID != initiative.ID {
			t.Fatalf("child %q InitiativeID = %v, want %d", delegation.DelegationKey, childTask.InitiativeID, initiative.ID)
		}
		if childTask.CompanionID == nil || *childTask.CompanionID != companion.ID {
			t.Fatalf("child %q CompanionID = %v, want %d", delegation.DelegationKey, childTask.CompanionID, companion.ID)
		}
		if childTask.Status != "queued" {
			t.Fatalf("child %q Status = %q, want queued", delegation.DelegationKey, childTask.Status)
		}
		childKeys[childTask.Key] = struct{}{}
	}

	views, err := projections.ListTaskStatusViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListTaskStatusViews() error = %v", err)
	}

	foundViews := 0
	for _, view := range views {
		if _, ok := childKeys[view.TaskKey]; !ok {
			continue
		}
		if view.ProjectKey != project.Key {
			t.Fatalf("task view project key = %q, want %q", view.ProjectKey, project.Key)
		}
		foundViews++
	}
	if foundViews != len(childKeys) {
		t.Fatalf("child task views found = %d, want %d", foundViews, len(childKeys))
	}
}

func companionsWorkspace(ctx context.Context, store *sqlite.Store) (sqlite.Workspace, error) {
	return store.CreateWorkspace(ctx, sqlite.CreateWorkspaceParams{
		Key:                 "swarm-workspace",
		Name:                "Swarm Workspace",
		OwnerRef:            "marcus",
		DefaultCompanionKey: "primary",
		Status:              "active",
		PolicyJSON:          `{}`,
	})
}
