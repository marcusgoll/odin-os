package recovery_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"odin-os/internal/core/workitems"
	"odin-os/internal/runtime/checkpoints"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/runtime/recovery"
	"odin-os/internal/store/sqlite"
)

func TestRunStartupRecoveryInterruptsRunsAndCreatesWakePackets(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 4, 9, 21, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	store.Now = func() time.Time { return now }

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

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

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "alpha-task",
		Title:       "Resume alpha work",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
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
	lease, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       task.ID,
		RunID:        run.ID,
		Mode:         "mutable",
		BranchName:   "odin/alpha/task-1/run-1/try-1",
		WorktreePath: "/tmp/alpha/.odin/task-1/run-1",
		RepoRoot:     project.GitRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease() error = %v", err)
	}

	service := recovery.Service{
		Store: store,
		Now:   func() time.Time { return now },
	}

	result, err := service.RunStartupRecovery(ctx)
	if err != nil {
		t.Fatalf("RunStartupRecovery() error = %v", err)
	}
	if result.RecoveredRuns != 1 {
		t.Fatalf("RecoveredRuns = %d, want 1", result.RecoveredRuns)
	}

	gotRun, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if gotRun.Status != "interrupted" {
		t.Fatalf("GetRun().Status = %q, want %q", gotRun.Status, "interrupted")
	}
	if gotRun.FinishedAt == nil {
		t.Fatalf("GetRun().FinishedAt = nil, want timestamp")
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "queued" {
		t.Fatalf("GetTask().Status = %q, want %q", gotTask.Status, "queued")
	}
	if gotTask.CurrentRunID != nil {
		t.Fatalf("GetTask().CurrentRunID = %v, want nil", gotTask.CurrentRunID)
	}

	packet, err := store.GetLatestTaskWakePacket(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("GetLatestTaskWakePacket() error = %v", err)
	}
	if packet.Trigger != string(checkpoints.TriggerRestart) {
		t.Fatalf("WakePacket.Trigger = %q, want %q", packet.Trigger, checkpoints.TriggerRestart)
	}

	resumeState, err := checkpoints.Service{Store: store}.LoadResumeState(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("LoadResumeState() error = %v", err)
	}
	if resumeState.Status != "queued" {
		t.Fatalf("ResumeState.Status = %q, want %q", resumeState.Status, "queued")
	}
	if len(resumeState.NextSteps) == 0 {
		t.Fatalf("ResumeState.NextSteps = %v, want at least one step", resumeState.NextSteps)
	}

	recoveries, err := projections.ListRecoveryViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListRecoveryViews() error = %v", err)
	}
	if len(recoveries) != 1 || recoveries[0].Status != "completed" {
		t.Fatalf("recoveries = %+v, want one completed recovery", recoveries)
	}
	releasedLease, err := store.GetWorktreeLease(ctx, lease.ID)
	if err != nil {
		t.Fatalf("GetWorktreeLease() error = %v", err)
	}
	if releasedLease.State != "released" {
		t.Fatalf("GetWorktreeLease().State = %q, want released", releasedLease.State)
	}
	if _, err := store.GetActiveWorktreeLeaseByTaskRun(ctx, task.ID, run.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetActiveWorktreeLeaseByTaskRun() error = %v, want sql.ErrNoRows", err)
	}
}

func TestRunStartupRecoveryInterruptsPreparingRuns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 4, 9, 21, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	store.Now = func() time.Time { return now }

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

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

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "alpha-task",
		Title:       "Resume alpha work",
		Status:      "preparing",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "preparing",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	lease, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       task.ID,
		RunID:        run.ID,
		Mode:         "mutable",
		BranchName:   "odin/alpha/task-1/run-1/try-1",
		WorktreePath: "/tmp/alpha/.odin/task-1/run-1",
		RepoRoot:     project.GitRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease() error = %v", err)
	}

	service := recovery.Service{
		Store: store,
		Now:   func() time.Time { return now },
	}

	result, err := service.RunStartupRecovery(ctx)
	if err != nil {
		t.Fatalf("RunStartupRecovery() error = %v", err)
	}
	if result.RecoveredRuns != 1 {
		t.Fatalf("RecoveredRuns = %d, want 1", result.RecoveredRuns)
	}

	gotRun, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if gotRun.Status != "interrupted" {
		t.Fatalf("GetRun().Status = %q, want interrupted", gotRun.Status)
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "queued" {
		t.Fatalf("GetTask().Status = %q, want queued", gotTask.Status)
	}
	releasedLease, err := store.GetWorktreeLease(ctx, lease.ID)
	if err != nil {
		t.Fatalf("GetWorktreeLease() error = %v", err)
	}
	if releasedLease.State != "released" {
		t.Fatalf("GetWorktreeLease().State = %q, want released", releasedLease.State)
	}
	if _, err := store.GetActiveWorktreeLeaseByTaskRun(ctx, task.ID, run.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetActiveWorktreeLeaseByTaskRun() error = %v, want sql.ErrNoRows", err)
	}
}

func TestRunStartupRecoveryPreservesBlockedApprovalTasks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 4, 9, 22, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	store.Now = func() time.Time { return now }

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

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

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "approval-task",
		Title:       "Await approval",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
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

	approval, _, err := workitems.Service{Store: store}.RequestApproval(ctx, task.ID, &run.ID, "operator")
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}

	service := recovery.Service{
		Store: store,
		Now:   func() time.Time { return now },
	}

	result, err := service.RunStartupRecovery(ctx)
	if err != nil {
		t.Fatalf("RunStartupRecovery() error = %v", err)
	}
	if result.RecoveredRuns != 1 {
		t.Fatalf("RecoveredRuns = %d, want 1", result.RecoveredRuns)
	}

	gotRun, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if gotRun.Status != "interrupted" {
		t.Fatalf("GetRun().Status = %q, want %q", gotRun.Status, "interrupted")
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "blocked" {
		t.Fatalf("GetTask().Status = %q, want %q", gotTask.Status, "blocked")
	}

	gotApproval, err := store.GetApproval(ctx, approval.ID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if gotApproval.Status != "pending" {
		t.Fatalf("GetApproval().Status = %q, want %q", gotApproval.Status, "pending")
	}

	resumeState, err := checkpoints.Service{Store: store}.LoadResumeState(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("LoadResumeState() error = %v", err)
	}
	if resumeState.Status != "blocked" {
		t.Fatalf("LoadResumeState().Status = %q, want %q", resumeState.Status, "blocked")
	}
}

func TestRunStartupRecoveryNormalizesRunningTasksWithPendingApprovals(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 4, 9, 23, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	store.Now = func() time.Time { return now }

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

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

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "legacy-approval-task",
		Title:       "Legacy approval state",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
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

	approval, err := store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      task.ID,
		RunID:       &run.ID,
		Status:      "pending",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}

	service := recovery.Service{
		Store: store,
		Now:   func() time.Time { return now },
	}

	result, err := service.RunStartupRecovery(ctx)
	if err != nil {
		t.Fatalf("RunStartupRecovery() error = %v", err)
	}
	if result.RecoveredRuns != 1 {
		t.Fatalf("RecoveredRuns = %d, want 1", result.RecoveredRuns)
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "blocked" {
		t.Fatalf("GetTask().Status = %q, want %q", gotTask.Status, "blocked")
	}

	views, err := projections.ListTaskStatusViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListTaskStatusViews() error = %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("ListTaskStatusViews() len = %d, want 1", len(views))
	}
	if views[0].Status != "blocked" {
		t.Fatalf("ListTaskStatusViews()[0].Status = %q, want %q", views[0].Status, "blocked")
	}

	gotApproval, err := store.GetApproval(ctx, approval.ID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if gotApproval.Status != "pending" {
		t.Fatalf("GetApproval().Status = %q, want %q", gotApproval.Status, "pending")
	}
}

func TestRunStartupRecoveryToleratesDuplicateRunningRowsForOneTask(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	store.Now = func() time.Time { return now }

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

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

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "duplicate-run-task",
		Title:       "Recover duplicate runs",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	runOne, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(runOne) error = %v", err)
	}
	runTwo, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  2,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(runTwo) error = %v", err)
	}

	service := recovery.Service{
		Store: store,
		Now:   func() time.Time { return now },
	}

	result, err := service.RunStartupRecovery(ctx)
	if err != nil {
		t.Fatalf("RunStartupRecovery() error = %v", err)
	}
	if result.RecoveredRuns != 2 {
		t.Fatalf("RecoveredRuns = %d, want 2", result.RecoveredRuns)
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "queued" {
		t.Fatalf("GetTask().Status = %q, want queued", gotTask.Status)
	}

	gotRunOne, err := store.GetRun(ctx, runOne.ID)
	if err != nil {
		t.Fatalf("GetRun(runOne) error = %v", err)
	}
	if gotRunOne.Status != "interrupted" {
		t.Fatalf("GetRun(runOne).Status = %q, want interrupted", gotRunOne.Status)
	}

	gotRunTwo, err := store.GetRun(ctx, runTwo.ID)
	if err != nil {
		t.Fatalf("GetRun(runTwo) error = %v", err)
	}
	if gotRunTwo.Status != "interrupted" {
		t.Fatalf("GetRun(runTwo).Status = %q, want interrupted", gotRunTwo.Status)
	}
}

func TestRunStartupRecoveryRequeuesBlockedTasksWithoutPendingApprovals(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 4, 10, 2, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	store.Now = func() time.Time { return now }

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

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

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "blocked-no-approval",
		Title:       "Repair blocked state",
		Status:      "blocked",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
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

	service := recovery.Service{
		Store: store,
		Now:   func() time.Time { return now },
	}

	result, err := service.RunStartupRecovery(ctx)
	if err != nil {
		t.Fatalf("RunStartupRecovery() error = %v", err)
	}
	if result.RecoveredRuns != 1 {
		t.Fatalf("RecoveredRuns = %d, want 1", result.RecoveredRuns)
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "queued" {
		t.Fatalf("GetTask().Status = %q, want queued", gotTask.Status)
	}

	gotRun, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if gotRun.Status != "interrupted" {
		t.Fatalf("GetRun().Status = %q, want interrupted", gotRun.Status)
	}
}

func TestRunStartupRecoverySkipsTerminalTaskStateAndContinues(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 4, 10, 3, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	store.Now = func() time.Time { return now }

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

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

	terminalTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "terminal-task",
		Title:       "Already complete",
		Status:      "completed",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(terminal) error = %v", err)
	}
	terminalRun, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   terminalTask.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(terminal) error = %v", err)
	}

	activeTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "active-task",
		Title:       "Resume active task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(active) error = %v", err)
	}
	activeRun, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   activeTask.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(active) error = %v", err)
	}

	service := recovery.Service{
		Store: store,
		Now:   func() time.Time { return now },
	}

	result, err := service.RunStartupRecovery(ctx)
	if err != nil {
		t.Fatalf("RunStartupRecovery() error = %v", err)
	}
	if result.RecoveredRuns != 2 {
		t.Fatalf("RecoveredRuns = %d, want 2", result.RecoveredRuns)
	}

	gotTerminalTask, err := store.GetTask(ctx, terminalTask.ID)
	if err != nil {
		t.Fatalf("GetTask(terminal) error = %v", err)
	}
	if gotTerminalTask.Status != "completed" {
		t.Fatalf("GetTask(terminal).Status = %q, want completed", gotTerminalTask.Status)
	}

	gotActiveTask, err := store.GetTask(ctx, activeTask.ID)
	if err != nil {
		t.Fatalf("GetTask(active) error = %v", err)
	}
	if gotActiveTask.Status != "queued" {
		t.Fatalf("GetTask(active).Status = %q, want queued", gotActiveTask.Status)
	}

	gotTerminalRun, err := store.GetRun(ctx, terminalRun.ID)
	if err != nil {
		t.Fatalf("GetRun(terminal) error = %v", err)
	}
	if gotTerminalRun.Status != "interrupted" {
		t.Fatalf("GetRun(terminal).Status = %q, want interrupted", gotTerminalRun.Status)
	}

	gotActiveRun, err := store.GetRun(ctx, activeRun.ID)
	if err != nil {
		t.Fatalf("GetRun(active) error = %v", err)
	}
	if gotActiveRun.Status != "interrupted" {
		t.Fatalf("GetRun(active).Status = %q, want interrupted", gotActiveRun.Status)
	}
}

func TestRunStartupRecoveryLeavesRunRunningWhenTaskRepairFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 4, 10, 4, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	store.Now = func() time.Time { return now }

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

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

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "repair-failure-task",
		Title:       "Repair failure",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
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

	if _, err := store.DB().ExecContext(ctx, `
		CREATE TRIGGER task_requeue_blocker
		BEFORE UPDATE OF status ON tasks
		WHEN NEW.status = 'queued'
		BEGIN
			SELECT RAISE(ABORT, 'requeue blocked');
		END;
	`); err != nil {
		t.Fatalf("create trigger error = %v", err)
	}

	service := recovery.Service{
		Store: store,
		Now:   func() time.Time { return now },
	}

	_, err = service.RunStartupRecovery(ctx)
	if err == nil {
		t.Fatal("RunStartupRecovery() error = nil, want requeue failure")
	}

	gotRun, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if gotRun.Status != "running" {
		t.Fatalf("GetRun().Status = %q, want running", gotRun.Status)
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "running" {
		t.Fatalf("GetTask().Status = %q, want running", gotTask.Status)
	}
}

func TestRunStartupRecoveryPreservesRejectedApprovalBlocks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 4, 10, 5, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	store.Now = func() time.Time { return now }

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

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

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "rejected-approval-task",
		Title:       "Preserve rejection",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
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

	approval, _, err := workitems.Service{Store: store}.RequestApproval(ctx, task.ID, &run.ID, "operator")
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}
	if _, err := store.ResolveApproval(ctx, sqlite.ResolveApprovalParams{
		ApprovalID: approval.ID,
		Status:     "rejected",
		DecisionBy: "reviewer",
		Reason:     "unsafe",
	}); err != nil {
		t.Fatalf("ResolveApproval(rejected) error = %v", err)
	}

	service := recovery.Service{
		Store: store,
		Now:   func() time.Time { return now },
	}

	result, err := service.RunStartupRecovery(ctx)
	if err != nil {
		t.Fatalf("RunStartupRecovery() error = %v", err)
	}
	if result.RecoveredRuns != 1 {
		t.Fatalf("RecoveredRuns = %d, want 1", result.RecoveredRuns)
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "blocked" {
		t.Fatalf("GetTask().Status = %q, want blocked", gotTask.Status)
	}

	gotRun, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if gotRun.Status != "interrupted" {
		t.Fatalf("GetRun().Status = %q, want interrupted", gotRun.Status)
	}
}
