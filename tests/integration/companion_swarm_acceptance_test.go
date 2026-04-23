package integration_test

import (
	"context"
	"encoding/json"
	"strings"
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
	var details struct {
		Objective string `json:"objective"`
		Swarm     struct {
			ConvergenceMode string `json:"convergence_mode"`
		} `json:"swarm"`
	}
	if err := json.Unmarshal([]byte(materialized.Delegations[0].DetailsJSON), &details); err != nil {
		t.Fatalf("json.Unmarshal(delegation details) error = %v", err)
	}
	if details.Swarm.ConvergenceMode != "review_gate" {
		t.Fatalf("delegation convergence mode = %q, want review_gate", details.Swarm.ConvergenceMode)
	}
	if details.Objective == "" {
		t.Fatalf("delegation objective = empty, want preserved objective")
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

	for _, delegation := range materialized.Delegations {
		detailsJSON := mustMarshalJSON(t, map[string]any{
			"status":                     "completed",
			"confidence":                 0.84,
			"evidence_refs":              []string{"swarm/" + delegation.DelegationKey},
			"unresolved_risks":           []string{},
			"proposed_next_actions":      []string{"continue"},
			"proposed_memory_candidates": []string{"swarm-" + delegation.DelegationKey},
		})
		summary := "Implementation ready"
		if delegation.DelegationKey == "review" {
			summary = "Review approved"
		}
		if _, err := store.CreateDelegationArtifact(ctx, sqlite.CreateDelegationArtifactParams{
			DelegationID: delegation.ID,
			ArtifactType: "result",
			Summary:      summary,
			DetailsJSON:  detailsJSON,
		}); err != nil {
			t.Fatalf("CreateDelegationArtifact(%s) error = %v", delegation.DelegationKey, err)
		}
	}

	aggregation, err := service.AggregateSwarm(ctx, parentTask.ID)
	if err != nil {
		t.Fatalf("AggregateSwarm() error = %v", err)
	}
	if aggregation.Status != "completed" {
		t.Fatalf("AggregateSwarm().Status = %q, want completed", aggregation.Status)
	}
	if aggregation.ParentTask.Status != "completed" {
		t.Fatalf("AggregateSwarm().ParentTask.Status = %q, want completed", aggregation.ParentTask.Status)
	}
	if aggregation.VerifierDelegationID == nil {
		t.Fatalf("AggregateSwarm().VerifierDelegationID = nil, want reviewer delegation ID")
	}
	if !strings.Contains(aggregation.Summary, "Implementation ready") {
		t.Fatalf("AggregateSwarm().Summary = %q, want implementation summary", aggregation.Summary)
	}

	persistedParent, err := store.GetTask(ctx, parentTask.ID)
	if err != nil {
		t.Fatalf("GetTask(parent) error = %v", err)
	}
	if persistedParent.Status != "completed" {
		t.Fatalf("GetTask(parent).Status = %q, want completed", persistedParent.Status)
	}

	var parentArtifacts []map[string]any
	if err := json.Unmarshal([]byte(persistedParent.ArtifactsJSON), &parentArtifacts); err != nil {
		t.Fatalf("json.Unmarshal(parent artifacts) error = %v", err)
	}
	if len(parentArtifacts) != 1 {
		t.Fatalf("parent artifacts len = %d, want 1", len(parentArtifacts))
	}
	if got := parentArtifacts[0]["convergence_mode"]; got != "review_gate" {
		t.Fatalf("parent artifacts convergence_mode = %#v, want review_gate", got)
	}
	if _, ok := parentArtifacts[0]["verifier_delegation_id"]; !ok {
		t.Fatalf("parent artifacts missing verifier_delegation_id: %#v", parentArtifacts[0])
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

func mustMarshalJSON(t *testing.T, value any) string {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return string(encoded)
}
