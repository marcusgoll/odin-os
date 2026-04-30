package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestSQLiteRepositoryWrapsExistingAgentRunAndRunEvents(t *testing.T) {
	ctx := context.Background()
	store := openMigratedStore(t)
	defer store.Close()

	_, task, run := seedRuntimeState(t, ctx, store)
	repository := NewSQLiteRepository(store)

	agentRun, err := repository.GetAgentRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetAgentRun() error = %v", err)
	}
	if agentRun.ID != run.ID {
		t.Fatalf("AgentRun.ID = %d, want %d", agentRun.ID, run.ID)
	}
	if agentRun.WorkItemID != task.ID {
		t.Fatalf("AgentRun.WorkItemID = %d, want existing task ID %d", agentRun.WorkItemID, task.ID)
	}
	if agentRun.Executor != "codex_headless" || agentRun.Status != "running" || agentRun.Attempt != 1 {
		t.Fatalf("AgentRun = %+v, want existing run values", agentRun)
	}

	events, err := repository.ListRunEvents(ctx, RunEventFilter{AgentRunID: &run.ID})
	if err != nil {
		t.Fatalf("ListRunEvents() error = %v", err)
	}
	if len(events) == 0 {
		t.Fatal("ListRunEvents() len = 0, want existing run events")
	}
	var foundRunStarted bool
	for _, event := range events {
		if event.AgentRunID == nil || *event.AgentRunID != run.ID {
			t.Fatalf("RunEvent.AgentRunID = %v, want %d", event.AgentRunID, run.ID)
		}
		if event.Type == "run.started" {
			foundRunStarted = true
		}
	}
	if !foundRunStarted {
		t.Fatalf("ListRunEvents() = %+v, want run.started event", events)
	}
}

func TestSQLiteRepositoryWrapsExistingWorkspaceAndFailureState(t *testing.T) {
	ctx := context.Background()
	store := openMigratedStore(t)
	defer store.Close()

	project, task, run := seedRuntimeState(t, ctx, store)
	lease, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       task.ID,
		RunID:        run.ID,
		Mode:         "mutable",
		BranchName:   "odin/odin-core/task-1/run-1/try-1",
		WorktreePath: "/tmp/odin/worktrees/odin-core/task-1/run-1/try-1",
		RepoRoot:     project.GitRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease() error = %v", err)
	}
	runID := run.ID
	incident, err := store.OpenIncident(ctx, sqlite.OpenIncidentParams{
		RunID:       &runID,
		Severity:    "high",
		Status:      "open",
		Summary:     "runner failed",
		DetailsJSON: `{"cause":"test"}`,
	})
	if err != nil {
		t.Fatalf("OpenIncident() error = %v", err)
	}

	repository := NewSQLiteRepository(store)
	workspace, err := repository.GetWorkspace(ctx, lease.ID)
	if err != nil {
		t.Fatalf("GetWorkspace() error = %v", err)
	}
	if workspace.ID != lease.ID || workspace.WorktreePath != lease.WorktreePath || workspace.State != "active" {
		t.Fatalf("Workspace = %+v, want existing lease values", workspace)
	}

	failure, err := repository.GetFailure(ctx, incident.ID)
	if err != nil {
		t.Fatalf("GetFailure() error = %v", err)
	}
	if failure.ID != incident.ID || failure.AgentRunID == nil || *failure.AgentRunID != run.ID {
		t.Fatalf("Failure = %+v, want existing incident linked to run %d", failure, run.ID)
	}
	if failure.Summary != "runner failed" || failure.Status != "open" {
		t.Fatalf("Failure = %+v, want existing incident values", failure)
	}
}

func TestSQLiteRepositoryReportsUnmigratedTargetRepositoriesExplicitly(t *testing.T) {
	ctx := context.Background()
	store := openMigratedStore(t)
	defer store.Close()

	repository := NewSQLiteRepository(store)
	if _, err := repository.ListPullRequests(ctx, PullRequestFilter{}); !errors.Is(err, ErrRepositoryNotMigrated) {
		t.Fatalf("ListPullRequests() error = %v, want %v", err, ErrRepositoryNotMigrated)
	}
	if _, err := repository.ListLocks(ctx, LockFilter{}); !errors.Is(err, ErrRepositoryNotMigrated) {
		t.Fatalf("ListLocks() error = %v, want %v", err, ErrRepositoryNotMigrated)
	}
}

func TestSQLiteRepositoryListsPersistedExternalIssues(t *testing.T) {
	ctx := context.Background()
	store := openMigratedStore(t)
	defer store.Close()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		GitHubRepo:    "acme/alpha",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if _, err := store.UpsertExternalIssue(ctx, sqlite.UpsertExternalIssueParams{
		ProjectID:  project.ID,
		Provider:   "github",
		Repo:       "acme/alpha",
		Number:     7,
		Title:      "Implement intake",
		BodyHash:   "sha256:body",
		URL:        "https://github.example/acme/alpha/issues/7",
		State:      "open",
		LabelsJSON: `["odin:ready"]`,
		SyncStatus: "eligible",
	}); err != nil {
		t.Fatalf("UpsertExternalIssue() error = %v", err)
	}

	repository := NewSQLiteRepository(store)
	issues, err := repository.ListIssues(ctx, IssueFilter{Repo: "acme/alpha", Status: "eligible"})
	if err != nil {
		t.Fatalf("ListIssues() error = %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("ListIssues() len = %d, want 1: %+v", len(issues), issues)
	}
	if issues[0].Provider != "github" || issues[0].Repo != "acme/alpha" || issues[0].Number != 7 || issues[0].Status != "eligible" {
		t.Fatalf("Issue = %+v, want persisted external issue", issues[0])
	}
}

func openMigratedStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func seedRuntimeState(t *testing.T, ctx context.Context, store *sqlite.Store) (sqlite.Project, sqlite.Task, sqlite.Run) {
	t.Helper()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "odin-core",
		GitRoot:       "/home/orchestrator/odin-os",
		DefaultBranch: "main",
		GitHubRepo:    "example/odin-os",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "runtime-state",
		Title:       "Characterize runtime state",
		Status:      "running",
		Scope:       "odin-core",
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

	return project, task, run
}
