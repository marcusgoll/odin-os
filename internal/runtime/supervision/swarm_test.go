package supervision

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	runtimejobs "odin-os/internal/runtime/jobs"
	"odin-os/internal/store/sqlite"
)

func TestSwarmAdmissionDeniesPlanWithoutValidTrigger(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openSupervisionStore(t)
	defer store.Close()

	_, parentTask, parentRun, _ := mustCreateSwarmParentContext(t, ctx, store, `{"allow":["repo_read"]}`, `{"mode":"initiative"}`, `{"swarm":{"max_children":2}}`)

	service := Service{
		Store: store,
		Jobs:  runtimejobs.Service{},
	}

	_, err := service.PlanSwarm(ctx, PlanSwarmParams{
		ParentTaskID:    parentTask.ID,
		ParentRunID:     &parentRun.ID,
		Trigger:         "single_agent",
		ConvergenceMode: "merge",
		RequestedBudget: 2,
		DelegationPlans: []DelegationPlan{
			{
				DelegationKey:  "research-a",
				Role:           "researcher",
				ActionClass:    "analysis",
				ActionKey:      "inventory",
				MutationMode:   "read_only",
				ArtifactTarget: "notes",
				Objective:      "Inspect repo state",
			},
			{
				DelegationKey:  "research-b",
				Role:           "reviewer",
				ActionClass:    "analysis",
				ActionKey:      "review",
				MutationMode:   "read_only",
				ArtifactTarget: "notes",
				Objective:      "Independently verify repo state",
			},
		},
	})
	if !errors.Is(err, ErrSwarmTriggerNotAdmitted) {
		t.Fatalf("PlanSwarm() error = %v, want %v", err, ErrSwarmTriggerNotAdmitted)
	}

	delegations, err := store.ListDelegations(ctx, sqlite.ListDelegationsParams{
		ParentTaskID: &parentTask.ID,
	})
	if err != nil {
		t.Fatalf("ListDelegations() error = %v", err)
	}
	if len(delegations) != 0 {
		t.Fatalf("delegations len = %d, want 0", len(delegations))
	}
}

func TestSwarmAdmissionDeniesRecursiveDelegation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openSupervisionStore(t)
	defer store.Close()

	project, rootTask, rootRun, companion := mustCreateSwarmParentContext(t, ctx, store, `{"allow":["repo_read","branch_proposal"]}`, `{"mode":"initiative"}`, `{"swarm":{"max_children":2}}`)
	childTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "delegated-child",
		Title:        "Delegated child task",
		ActionKey:    "inventory",
		Status:       "queued",
		Scope:        rootTask.Scope,
		RequestedBy:  "supervisor",
		WorkspaceID:  rootTask.WorkspaceID,
		InitiativeID: rootTask.InitiativeID,
		CompanionID:  rootTask.CompanionID,
		WorkKind:     "swarm_child",
	})
	if err != nil {
		t.Fatalf("CreateTask(child) error = %v", err)
	}

	parentRunID := rootRun.ID
	delegation, err := store.CreateDelegation(ctx, sqlite.CreateDelegationParams{
		ParentTaskID:    rootTask.ID,
		ParentRunID:     &parentRunID,
		ProjectID:       project.ID,
		Scope:           rootTask.Scope,
		DelegationKey:   "existing-child",
		Role:            "specialist",
		ActionClass:     "analysis",
		ActionKey:       "inventory",
		MutationMode:    "read_only",
		Status:          "queued",
		ConvergenceMode: "review_gate",
		ArtifactTarget:  "notes",
		Executor:        "codex_headless",
		DetailsJSON:     `{"objective":"existing child"}`,
	})
	if err != nil {
		t.Fatalf("CreateDelegation() error = %v", err)
	}
	if _, err := store.AttachDelegationChildTask(ctx, sqlite.AttachDelegationChildTaskParams{
		DelegationID: delegation.ID,
		ChildTaskID:  childTask.ID,
	}); err != nil {
		t.Fatalf("AttachDelegationChildTask() error = %v", err)
	}

	service := Service{
		Store: store,
		Jobs: runtimejobs.Service{
			Store: store,
		},
	}

	_, err = service.PlanSwarm(ctx, PlanSwarmParams{
		ParentTaskID:    childTask.ID,
		Trigger:         TriggerBuildPlusReview,
		ConvergenceMode: "merge",
		RequestedBudget: 2,
		DelegationPlans: []DelegationPlan{
			{
				DelegationKey:  "nested-a",
				Role:           "researcher",
				ActionClass:    "analysis",
				ActionKey:      "inventory",
				MutationMode:   "read_only",
				ArtifactTarget: "notes",
				Objective:      "Nested child A",
			},
			{
				DelegationKey:  "nested-b",
				Role:           "reviewer",
				ActionClass:    "analysis",
				ActionKey:      "review",
				MutationMode:   "read_only",
				ArtifactTarget: "notes",
				Objective:      "Nested child B",
			},
		},
	})
	if !errors.Is(err, ErrSwarmRecursiveDelegation) {
		t.Fatalf("PlanSwarm() error = %v, want %v", err, ErrSwarmRecursiveDelegation)
	}

	_ = companion
}

func TestSwarmPlansBoundedDelegationsForEligibleParentTask(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openSupervisionStore(t)
	defer store.Close()

	_, parentTask, parentRun, _ := mustCreateSwarmParentContext(t, ctx, store, `{"allow":["repo_read","branch_proposal","merge_to_main"]}`, `{"mode":"initiative"}`, `{"swarm":{"max_children":2}}`)
	service := Service{
		Store: store,
		Jobs: runtimejobs.Service{
			Store: store,
		},
	}

	plan, err := service.PlanSwarm(ctx, PlanSwarmParams{
		ParentTaskID:    parentTask.ID,
		ParentRunID:     &parentRun.ID,
		Trigger:         TriggerBuildPlusReview,
		ConvergenceMode: "review_gate",
		RequestedBudget: 4,
		RetryBudget:     1,
		DelegationPlans: []DelegationPlan{
			{
				DelegationKey:      "implement",
				Role:               "builder",
				ActionClass:        "mutation",
				ActionKey:          "implement",
				MutationMode:       "isolated_worktree",
				ArtifactTarget:     "branch",
				Objective:          "Implement the requested change",
				RequestedTools:     []string{"repo_read", "branch_proposal"},
				AcceptanceCriteria: []string{"Code compiles"},
			},
			{
				DelegationKey:      "review",
				Role:               "reviewer",
				ActionClass:        "analysis",
				ActionKey:          "review",
				MutationMode:       "read_only",
				ArtifactTarget:     "report",
				Objective:          "Review the implementation",
				RequestedTools:     []string{"repo_read"},
				AcceptanceCriteria: []string{"Risks are identified"},
			},
			{
				DelegationKey:      "extra",
				Role:               "observer",
				ActionClass:        "analysis",
				ActionKey:          "summarize",
				MutationMode:       "read_only",
				ArtifactTarget:     "notes",
				Objective:          "Should be trimmed by budget",
				RequestedTools:     []string{"repo_read"},
				AcceptanceCriteria: []string{"Not planned"},
			},
		},
	})
	if err != nil {
		t.Fatalf("PlanSwarm() error = %v", err)
	}

	if plan.MaxChildren != 2 {
		t.Fatalf("MaxChildren = %d, want 2", plan.MaxChildren)
	}
	if len(plan.Delegations) != 2 {
		t.Fatalf("plan delegations len = %d, want 2", len(plan.Delegations))
	}

	persisted, err := store.ListDelegations(ctx, sqlite.ListDelegationsParams{
		ParentTaskID: &parentTask.ID,
	})
	if err != nil {
		t.Fatalf("ListDelegations() error = %v", err)
	}
	if len(persisted) != 2 {
		t.Fatalf("persisted delegations len = %d, want 2", len(persisted))
	}
	if persisted[0].ParentRunID == nil || *persisted[0].ParentRunID != parentRun.ID {
		t.Fatalf("persisted[0].ParentRunID = %v, want %d", persisted[0].ParentRunID, parentRun.ID)
	}
	if persisted[0].Status != "queued" {
		t.Fatalf("persisted[0].Status = %q, want queued", persisted[0].Status)
	}

	var details map[string]any
	if err := json.Unmarshal([]byte(persisted[0].DetailsJSON), &details); err != nil {
		t.Fatalf("json.Unmarshal(details) error = %v", err)
	}
	swarmMeta, ok := details["swarm"].(map[string]any)
	if !ok {
		t.Fatalf("details.swarm = %#v, want object", details["swarm"])
	}
	if got := swarmMeta["trigger"]; got != TriggerBuildPlusReview {
		t.Fatalf("details.swarm.trigger = %#v, want %q", got, TriggerBuildPlusReview)
	}
	if got := int(swarmMeta["max_children"].(float64)); got != 2 {
		t.Fatalf("details.swarm.max_children = %d, want 2", got)
	}
}

func TestSwarmPlanNarrowsChildPermissionsRelativeToParentAndCompanion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openSupervisionStore(t)
	defer store.Close()

	_, parentTask, parentRun, _ := mustCreateSwarmParentContext(t, ctx, store, `{"allow":["repo_read","branch_proposal"]}`, `{"mode":"initiative"}`, `{"swarm":{"max_children":2}}`)
	service := Service{
		Store: store,
		Jobs: runtimejobs.Service{
			Store: store,
		},
	}

	plan, err := service.PlanSwarm(ctx, PlanSwarmParams{
		ParentTaskID:    parentTask.ID,
		ParentRunID:     &parentRun.ID,
		Trigger:         TriggerParallelResearch,
		ConvergenceMode: "merge",
		RequestedBudget: 2,
		DelegationPlans: []DelegationPlan{
			{
				DelegationKey:         "research-a",
				Role:                  "researcher",
				ActionClass:           "analysis",
				ActionKey:             "inventory",
				MutationMode:          "read_only",
				ArtifactTarget:        "notes",
				Objective:             "Inspect repository state",
				RequestedTools:        []string{"repo_read", "merge_to_main"},
				RequestedMemoryScopes: []string{"workspace", "initiative", "global", "companion"},
			},
			{
				DelegationKey:         "research-b",
				Role:                  "reviewer",
				ActionClass:           "analysis",
				ActionKey:             "review",
				MutationMode:          "read_only",
				ArtifactTarget:        "notes",
				Objective:             "Cross-check repository state",
				RequestedTools:        []string{"repo_read"},
				RequestedMemoryScopes: []string{"workspace", "initiative"},
			},
		},
	})
	if err != nil {
		t.Fatalf("PlanSwarm() error = %v", err)
	}

	var details map[string]any
	if err := json.Unmarshal([]byte(plan.Delegations[0].DetailsJSON), &details); err != nil {
		t.Fatalf("json.Unmarshal(details) error = %v", err)
	}
	admission, ok := details["admission"].(map[string]any)
	if !ok {
		t.Fatalf("details.admission = %#v, want object", details["admission"])
	}
	allowedTools, ok := admission["allowed_tools"].([]any)
	if !ok {
		t.Fatalf("details.admission.allowed_tools = %#v, want array", admission["allowed_tools"])
	}
	if len(allowedTools) != 1 || allowedTools[0] != "repo_read" {
		t.Fatalf("allowed_tools = %#v, want [repo_read]", allowedTools)
	}
	memoryView, ok := admission["memory_view"].(map[string]any)
	if !ok {
		t.Fatalf("details.admission.memory_view = %#v, want object", admission["memory_view"])
	}
	if got := memoryView["mode"]; got != "initiative" {
		t.Fatalf("memory_view.mode = %#v, want initiative", got)
	}
	scopes, ok := memoryView["scopes"].([]any)
	if !ok {
		t.Fatalf("memory_view.scopes = %#v, want array", memoryView["scopes"])
	}
	wantScopes := []string{"workspace", "initiative", "companion"}
	if len(scopes) != len(wantScopes) {
		t.Fatalf("memory_view.scopes len = %d, want %d", len(scopes), len(wantScopes))
	}
	for i, want := range wantScopes {
		if scopes[i] != want {
			t.Fatalf("memory_view.scopes[%d] = %#v, want %q", i, scopes[i], want)
		}
	}
	if _, exists := memoryView["global"]; exists {
		t.Fatalf("memory_view should not include global scope: %#v", memoryView)
	}
}

func mustCreateSwarmParentContext(t *testing.T, ctx context.Context, store *sqlite.Store, toolPolicyJSON, memoryPolicyJSON, planningPolicyJSON string) (sqlite.Project, sqlite.Task, sqlite.Run, sqlite.Companion) {
	t.Helper()

	workspace, err := store.CreateWorkspace(ctx, sqlite.CreateWorkspaceParams{
		Key:                 "swarm-workspace",
		Name:                "Swarm Workspace",
		OwnerRef:            "marcus",
		DefaultCompanionKey: "primary",
		Status:              "active",
		PolicyJSON:          `{}`,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	initiative, err := store.UpsertInitiative(ctx, sqlite.UpsertInitiativeParams{
		WorkspaceID: workspace.ID,
		Key:         "swarm-initiative",
		Title:       "Swarm Initiative",
		Kind:        "delivery",
		Status:      "active",
		Summary:     "Test initiative",
	})
	if err != nil {
		t.Fatalf("UpsertInitiative() error = %v", err)
	}

	companion, err := store.UpsertCompanion(ctx, sqlite.UpsertCompanionParams{
		WorkspaceID:         workspace.ID,
		Key:                 "builder",
		Title:               "Builder",
		Kind:                "assistant",
		Charter:             "Coordinates bounded swarm work.",
		Status:              "active",
		InitiativeScopeJSON: `{"allow":["swarm-initiative"]}`,
		ToolPolicyJSON:      toolPolicyJSON,
		MemoryPolicyJSON:    memoryPolicyJSON,
		PlanningPolicyJSON:  planningPolicyJSON,
	})
	if err != nil {
		t.Fatalf("UpsertCompanion() error = %v", err)
	}

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "swarm-project",
		Name:          "Swarm Project",
		Scope:         "project",
		GitRoot:       "/tmp/swarm-project",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "parent-task",
		Title:        "Parent swarm task",
		ActionKey:    "execute",
		Status:       "queued",
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "project",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:     task.ID,
		Executor:   "codex_headless",
		Attempt:    1,
		Status:     "running",
		TaskStatus: "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	task, err = store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}

	return project, task, run, companion
}
