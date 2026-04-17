package supervision

import (
	"context"
	"path/filepath"
	"testing"
	"time"

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
	if !updatedDue.NextEligibleAt.IsZero() {
		t.Fatalf("due task next_eligible_at = %v, want immediate", updatedDue.NextEligibleAt)
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

	guarded, promoted, err := store.PromoteQueuedTaskIfDue(ctx, sqlite.PromoteQueuedTaskIfDueParams{
		TaskID: task.ID,
		Now:    now,
	})
	if err != nil {
		t.Fatalf("PromoteQueuedTaskIfDue() error = %v", err)
	}
	if promoted {
		t.Fatal("PromoteQueuedTaskIfDue() promoted running task, want no-op")
	}
	if guarded.Status != "running" {
		t.Fatalf("PromoteQueuedTaskIfDue().Status = %q, want running", guarded.Status)
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
