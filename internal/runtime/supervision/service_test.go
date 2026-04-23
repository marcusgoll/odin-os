package supervision

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	runtimejobs "odin-os/internal/runtime/jobs"
	"odin-os/internal/store/sqlite"
)

func TestSchedulerPromotesDueQueuedTask(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openSupervisionStore(t)
	defer store.Close()

	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	project := mustCreateSupervisionProject(t, ctx, store)
	dueTask := mustCreateQueuedTaskAt(t, ctx, store, project.ID, "due-task", now.Add(-time.Minute))
	notYetDueTask := mustCreateQueuedTaskAt(t, ctx, store, project.ID, "future-task", now.Add(2*time.Minute))

	service := Service{
		Store: store,
		Now:   func() time.Time { return now },
	}

	result, err := service.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Promoted != 1 {
		t.Fatalf("Promoted = %d, want 1", result.Promoted)
	}

	updatedDue, err := store.GetTask(ctx, dueTask.ID)
	if err != nil {
		t.Fatalf("GetTask(due) error = %v", err)
	}
	if !updatedDue.NextEligibleAt.Equal(now.Add(-time.Minute)) {
		t.Fatalf("due task next_eligible_at = %v, want %v", updatedDue.NextEligibleAt, now.Add(-time.Minute))
	}

	updatedFuture, err := store.GetTask(ctx, notYetDueTask.ID)
	if err != nil {
		t.Fatalf("GetTask(future) error = %v", err)
	}
	if !updatedFuture.NextEligibleAt.Equal(now.Add(2 * time.Minute)) {
		t.Fatalf("future task next_eligible_at = %v, want %v", updatedFuture.NextEligibleAt, now.Add(2*time.Minute))
	}
}

func TestSchedulerLeavesNotYetDueTaskUntouched(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openSupervisionStore(t)
	defer store.Close()

	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	project := mustCreateSupervisionProject(t, ctx, store)
	task := mustCreateQueuedTaskAt(t, ctx, store, project.ID, "future-task", now.Add(3*time.Minute))

	service := Service{
		Store: store,
		Now:   func() time.Time { return now },
	}

	result, err := service.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Promoted != 0 {
		t.Fatalf("Promoted = %d, want 0", result.Promoted)
	}

	updated, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if !updated.NextEligibleAt.Equal(now.Add(3 * time.Minute)) {
		t.Fatalf("NextEligibleAt = %v, want %v", updated.NextEligibleAt, now.Add(3*time.Minute))
	}
}

func TestSchedulerDoesNotPromoteClaimedTask(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openSupervisionStore(t)
	defer store.Close()

	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	project := mustCreateSupervisionProject(t, ctx, store)
	task := mustCreateQueuedTaskAt(t, ctx, store, project.ID, "running-task", now.Add(-time.Minute))

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
		Status: "running",
	}); err != nil {
		t.Fatalf("UpdateTaskStatus(running) error = %v", err)
	}

	service := Service{
		Store: store,
		Now:   func() time.Time { return now },
	}

	result, err := service.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Promoted != 0 {
		t.Fatalf("Promoted = %d, want 0", result.Promoted)
	}

	updatedTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if updatedTask.Status != "running" {
		t.Fatalf("Task.Status = %q, want running", updatedTask.Status)
	}
	if updatedTask.CurrentRunID == nil || *updatedTask.CurrentRunID != run.ID {
		t.Fatalf("Task.CurrentRunID = %v, want %d", updatedTask.CurrentRunID, run.ID)
	}
	if !updatedTask.NextEligibleAt.Equal(now.Add(-time.Minute)) {
		t.Fatalf("Task.NextEligibleAt = %v, want %v", updatedTask.NextEligibleAt, now.Add(-time.Minute))
	}
}

func TestSchedulerPreservesDueOrderForMultipleDelayedTasks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openSupervisionStore(t)
	defer store.Close()

	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	project := mustCreateSupervisionProject(t, ctx, store)
	laterDue := mustCreateQueuedTaskAt(t, ctx, store, project.ID, "later-due", now.Add(-time.Minute))
	earlierDue := mustCreateQueuedTaskAt(t, ctx, store, project.ID, "earlier-due", now.Add(-2*time.Minute))

	beforeTick, err := store.ListEligibleQueuedTasks(ctx, now)
	if err != nil {
		t.Fatalf("ListEligibleQueuedTasks(before) error = %v", err)
	}
	if len(beforeTick) != 2 {
		t.Fatalf("eligible before tick = %d, want 2", len(beforeTick))
	}
	if beforeTick[0].ID != earlierDue.ID || beforeTick[1].ID != laterDue.ID {
		t.Fatalf("before tick order = [%d %d], want [%d %d]", beforeTick[0].ID, beforeTick[1].ID, earlierDue.ID, laterDue.ID)
	}

	service := Service{
		Store: store,
		Now:   func() time.Time { return now },
	}

	result, err := service.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Promoted != 2 {
		t.Fatalf("Promoted = %d, want 2", result.Promoted)
	}

	afterTick, err := store.ListEligibleQueuedTasks(ctx, now)
	if err != nil {
		t.Fatalf("ListEligibleQueuedTasks(after) error = %v", err)
	}
	if len(afterTick) != 2 {
		t.Fatalf("eligible after tick = %d, want 2", len(afterTick))
	}
	if afterTick[0].ID != earlierDue.ID || afterTick[1].ID != laterDue.ID {
		t.Fatalf("after tick order = [%d %d], want [%d %d]", afterTick[0].ID, afterTick[1].ID, earlierDue.ID, laterDue.ID)
	}
}

func TestSwarmTickAggregatesReadyResults(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openSupervisionStore(t)
	defer store.Close()

	_, parentTask, parentRun, _ := mustCreateSwarmParentContext(t, ctx, store, `{"allow":["repo_read","branch_proposal"]}`, `{"mode":"initiative"}`, `{"swarm":{"max_children":2}}`)
	service := Service{
		Store: store,
		Jobs:  runtimejobs.Service{Store: store},
		Now: func() time.Time {
			return time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
		},
	}

	plan, err := service.PlanSwarm(ctx, PlanSwarmParams{
		ParentTaskID:    parentTask.ID,
		ParentRunID:     &parentRun.ID,
		Trigger:         TriggerParallelResearch,
		ConvergenceMode: "merge",
		RequestedBudget: 2,
		DelegationPlans: []DelegationPlan{
			{DelegationKey: "docs", Role: "writer", ActionClass: "mutation", ActionKey: "document", MutationMode: "read_only", ArtifactTarget: "docs", Objective: "Document the change"},
			{DelegationKey: "tests", Role: "tester", ActionClass: "mutation", ActionKey: "test", MutationMode: "read_only", ArtifactTarget: "tests", Objective: "Add regression coverage"},
		},
	})
	if err != nil {
		t.Fatalf("PlanSwarm() error = %v", err)
	}

	mustCreateDelegationResultArtifact(t, ctx, store, plan.Delegations[0], "Updated docs", resultEnvelopeJSON(t, "completed", 0.72, []string{"docs/companion.md"}, nil, []string{"merge docs"}, []string{"docs updated"}))
	mustCreateDelegationResultArtifact(t, ctx, store, plan.Delegations[1], "Added tests", resultEnvelopeJSON(t, "completed", 0.81, []string{"tests/companion_test.go"}, nil, []string{"run regression suite"}, []string{"test coverage updated"}))

	result, err := service.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Reconciled != 1 {
		t.Fatalf("Reconciled = %d, want 1", result.Reconciled)
	}

	updatedParent, err := store.GetTask(ctx, parentTask.ID)
	if err != nil {
		t.Fatalf("GetTask(parent) error = %v", err)
	}
	if updatedParent.Status != "completed" {
		t.Fatalf("parent status = %q, want completed", updatedParent.Status)
	}
}

func TestSwarmTickBlocksQueuedChildrenWhenApprovalIsPending(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openSupervisionStore(t)
	defer store.Close()

	_, parentTask, parentRun, _ := mustCreateSwarmParentContext(t, ctx, store, `{"allow":["repo_read","branch_proposal"]}`, `{"mode":"initiative"}`, `{"swarm":{"max_children":2}}`)
	service := Service{
		Store: store,
		Jobs:  runtimejobs.Service{Store: store},
	}

	plan, err := service.PlanSwarm(ctx, PlanSwarmParams{
		ParentTaskID:    parentTask.ID,
		ParentRunID:     &parentRun.ID,
		Trigger:         TriggerBuildPlusReview,
		ConvergenceMode: "review_gate",
		RequestedBudget: 2,
		DelegationPlans: []DelegationPlan{
			{DelegationKey: "implement", Role: "builder", ActionClass: "mutation", ActionKey: "implement", MutationMode: "isolated_worktree", ArtifactTarget: "branch", Objective: "Implement the change"},
			{DelegationKey: "review", Role: "reviewer", ActionClass: "analysis", ActionKey: "review", MutationMode: "read_only", ArtifactTarget: "report", Objective: "Review the change"},
		},
	})
	if err != nil {
		t.Fatalf("PlanSwarm() error = %v", err)
	}
	materialized, err := service.MaterializeSwarm(ctx, plan)
	if err != nil {
		t.Fatalf("MaterializeSwarm() error = %v", err)
	}
	if _, err := store.BlockTask(ctx, sqlite.BlockTaskParams{
		TaskID: parentTask.ID,
		Reason: "approval_required",
	}); err != nil {
		t.Fatalf("BlockTask(parent) error = %v", err)
	}

	result, err := service.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Reconciled != 1 {
		t.Fatalf("Reconciled = %d, want 1", result.Reconciled)
	}
	for _, childTask := range materialized.Tasks {
		updated, err := store.GetTask(ctx, childTask.ID)
		if err != nil {
			t.Fatalf("GetTask(child %d) error = %v", childTask.ID, err)
		}
		if updated.Status != "blocked" {
			t.Fatalf("child task %d status = %q, want blocked", childTask.ID, updated.Status)
		}
		if updated.BlockedReason != "approval_required" {
			t.Fatalf("child task %d blocked_reason = %q, want approval_required", childTask.ID, updated.BlockedReason)
		}
	}
	for _, delegation := range materialized.Delegations {
		updated, err := store.GetDelegation(ctx, delegation.ID)
		if err != nil {
			t.Fatalf("GetDelegation(%d) error = %v", delegation.ID, err)
		}
		if updated.Status != "blocked" {
			t.Fatalf("delegation %d status = %q, want blocked", delegation.ID, updated.Status)
		}
	}
}

func TestSwarmTickFailsQueuedChildrenWhenBudgetIsExhausted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openSupervisionStore(t)
	defer store.Close()

	_, parentTask, parentRun, _ := mustCreateSwarmParentContext(t, ctx, store, `{"allow":["repo_read","branch_proposal"]}`, `{"mode":"initiative"}`, `{"swarm":{"max_children":2}}`)
	service := Service{
		Store: store,
		Jobs:  runtimejobs.Service{Store: store},
	}

	plan, err := service.PlanSwarm(ctx, PlanSwarmParams{
		ParentTaskID:    parentTask.ID,
		ParentRunID:     &parentRun.ID,
		Trigger:         TriggerBuildPlusReview,
		ConvergenceMode: "review_gate",
		RequestedBudget: 2,
		DelegationPlans: []DelegationPlan{
			{DelegationKey: "implement", Role: "builder", ActionClass: "mutation", ActionKey: "implement", MutationMode: "isolated_worktree", ArtifactTarget: "branch", Objective: "Implement the change"},
			{DelegationKey: "review", Role: "reviewer", ActionClass: "analysis", ActionKey: "review", MutationMode: "read_only", ArtifactTarget: "report", Objective: "Review the change"},
		},
	})
	if err != nil {
		t.Fatalf("PlanSwarm() error = %v", err)
	}
	materialized, err := service.MaterializeSwarm(ctx, plan)
	if err != nil {
		t.Fatalf("MaterializeSwarm() error = %v", err)
	}
	if _, err := store.BlockTask(ctx, sqlite.BlockTaskParams{
		TaskID: parentTask.ID,
		Reason: "budget_exhausted",
	}); err != nil {
		t.Fatalf("BlockTask(parent) error = %v", err)
	}

	result, err := service.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Reconciled != 1 {
		t.Fatalf("Reconciled = %d, want 1", result.Reconciled)
	}
	for _, childTask := range materialized.Tasks {
		updated, err := store.GetTask(ctx, childTask.ID)
		if err != nil {
			t.Fatalf("GetTask(child %d) error = %v", childTask.ID, err)
		}
		if updated.Status != "failed" {
			t.Fatalf("child task %d status = %q, want failed", childTask.ID, updated.Status)
		}
		if updated.TerminalReason != "swarm_budget_exhausted" {
			t.Fatalf("child task %d terminal_reason = %q, want swarm_budget_exhausted", childTask.ID, updated.TerminalReason)
		}
	}
	for _, delegation := range materialized.Delegations {
		updated, err := store.GetDelegation(ctx, delegation.ID)
		if err != nil {
			t.Fatalf("GetDelegation(%d) error = %v", delegation.ID, err)
		}
		if updated.Status != "failed" {
			t.Fatalf("delegation %d status = %q, want failed", delegation.ID, updated.Status)
		}
	}
}

func TestShutdownRequestedSkipsSwarmTick(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openSupervisionStore(t)
	defer store.Close()

	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	project := mustCreateSupervisionProject(t, ctx, store)
	_ = mustCreateQueuedTaskAt(t, ctx, store, project.ID, "due-task", now.Add(-time.Minute))
	var shutdownRequested atomic.Bool
	shutdownRequested.Store(true)

	service := Service{
		Store:             store,
		Now:               func() time.Time { return now },
		ShutdownRequested: &shutdownRequested,
	}

	result, err := service.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Promoted != 0 || result.Reconciled != 0 {
		t.Fatalf("Tick() = %+v, want no work while shutdown is requested", result)
	}
}

func openSupervisionStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func mustCreateSupervisionProject(t *testing.T, ctx context.Context, store *sqlite.Store) sqlite.Project {
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
	return project
}

func mustCreateQueuedTaskAt(t *testing.T, ctx context.Context, store *sqlite.Store, projectID int64, key string, nextEligibleAt time.Time) sqlite.Task {
	t.Helper()

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   projectID,
		Key:         key,
		Title:       key,
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, err := store.RequeueTaskAt(ctx, sqlite.RequeueTaskAtParams{
		TaskID:         task.ID,
		NextEligibleAt: nextEligibleAt,
	}); err != nil {
		t.Fatalf("RequeueTaskAt() error = %v", err)
	}
	updated, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	return updated
}
