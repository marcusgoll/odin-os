package integration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"odin-os/internal/app/bootstrap"
	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
	jobsvc "odin-os/internal/runtime/jobs"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
	gitadapter "odin-os/internal/vcs/git"
	"odin-os/internal/vcs/leases"
	worktreemgr "odin-os/internal/vcs/worktrees"
)

type doctorReport struct {
	Status string            `json:"status"`
	Checks []json.RawMessage `json:"checks"`
}

type statusReport struct {
	ApprovalsWaiting   []json.RawMessage `json:"approvals_waiting"`
	StalledRuns        []json.RawMessage `json:"stalled_runs"`
	ActiveRuns         []json.RawMessage `json:"active_runs"`
	ProjectTransitions []json.RawMessage `json:"project_transitions"`
}

func TestOperationalAutonomyFreshRuntimeBecomesHealthy(t *testing.T) {
	repoRoot := projectRoot(t)
	odinBinary := buildOdinBinary(t, repoRoot)
	runtimeRoot := t.TempDir()

	output, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "doctor", "--json")
	if err != nil {
		t.Fatalf("runOdinCommand(doctor --json) error = %v\n%s", err, output)
	}

	var report doctorReport
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("doctor output = %q, want valid JSON: %v", output, err)
	}
	if report.Status != "healthy" {
		t.Fatalf("status = %q, want healthy", report.Status)
	}
	if len(report.Checks) == 0 {
		t.Fatal("checks empty, want readiness checks")
	}
}

func TestOperationalAutonomyStatusJsonWorksOnFreshRuntimeWithoutSeedingReadiness(t *testing.T) {
	repoRoot := projectRoot(t)
	odinBinary := buildOdinBinary(t, repoRoot)
	runtimeRoot := t.TempDir()

	output, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "status", "--json")
	if err != nil {
		t.Fatalf("runOdinCommand(status --json) error = %v\n%s", err, output)
	}

	var report statusReport
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("status output = %q, want valid JSON: %v", output, err)
	}
	if len(report.ApprovalsWaiting) != 0 {
		t.Fatalf("approvals_waiting = %d, want 0 on fresh runtime", len(report.ApprovalsWaiting))
	}
	if len(report.StalledRuns) != 0 {
		t.Fatalf("stalled_runs = %d, want 0 on fresh runtime", len(report.StalledRuns))
	}
	if len(report.ActiveRuns) != 0 {
		t.Fatalf("active_runs = %d, want 0 on fresh runtime", len(report.ActiveRuns))
	}
	if len(report.ProjectTransitions) != 0 {
		t.Fatalf("project_transitions = %d, want 0 on fresh runtime", len(report.ProjectTransitions))
	}

	store := openRuntimeStore(t, runtimeRoot)
	defer store.Close()
	assertRuntimeReadinessCounts(t, store.DB())
}

func TestOperationalAutonomyRequiresApprovalForHighRiskMutation(t *testing.T) {
	ctx := context.Background()
	repoRoot := projectRoot(t)
	runtimeRoot := t.TempDir()

	app, err := bootstrap.Load(ctx, repoRoot, runtimeRoot)
	if err != nil {
		t.Fatalf("bootstrap.Load() error = %v", err)
	}
	defer app.Store.Close()

	service := jobsvc.Service{
		Store:          app.Store,
		Registry:       app.Registry,
		Executors:      app.Executors,
		ExecutorConfig: app.ExecutorConfig,
		Transitions:    projects.Service{Store: app.Store},
		Leases: leases.Manager{
			Store:        app.Store,
			Git:          gitadapter.Adapter{},
			WorktreeRoot: worktreemgr.DefaultRoot(),
		},
		Now: time.Now,
	}

	if _, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeOdinCore,
		ProjectKey: "odin-core",
	}, "repo rewrite"); err != nil {
		t.Fatalf("CreateTaskFromAct() error = %v", err)
	}

	project, err := app.Store.GetProjectByKey(ctx, "odin-core")
	if err != nil {
		t.Fatalf("GetProjectByKey() error = %v", err)
	}
	if _, err := app.Store.SetProjectTransition(ctx, sqlite.SetProjectTransitionParams{
		ProjectID:          project.ID,
		State:              "cutover",
		Controller:         "odin_os",
		LimitedActionsJSON: "[]",
		Notes:              "enable managed mutations",
		ChangedBy:          "operator",
	}); err != nil {
		t.Fatalf("SetProjectTransition() error = %v", err)
	}

	if err := service.ExecuteNextQueued(ctx); err != nil {
		t.Fatalf("ExecuteNextQueued() error = %v", err)
	}

	approvals, err := projections.ListPendingApprovalViews(ctx, app.Store.DB())
	if err != nil {
		t.Fatalf("ListPendingApprovalViews() error = %v", err)
	}
	if len(approvals) != 1 {
		t.Fatalf("pending approvals = %d, want 1", len(approvals))
	}
}

func TestOperationalAutonomySchedulesAcrossMultipleProjects(t *testing.T) {
	ctx := context.Background()
	runtimeRoot := seededRuntimeWithProjects(t, "odin-core", "pbs", "odin-orchestrator")
	store := openRuntimeStore(t, runtimeRoot)
	defer store.Close()

	views, err := projections.ListProjectPortfolioViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListProjectPortfolioViews() error = %v", err)
	}
	if len(views) < 3 {
		t.Fatalf("portfolio len = %d, want at least 3", len(views))
	}

	gotKeys := make([]string, 0, len(views))
	for _, view := range views {
		gotKeys = append(gotKeys, view.ProjectKey)
	}
	for _, want := range []string{"odin-core", "pbs", "odin-orchestrator"} {
		if !slices.Contains(gotKeys, want) {
			t.Fatalf("portfolio keys = %v, want %q present", gotKeys, want)
		}
	}
}

func TestOperationalAutonomyStatusJsonIncludesBlockedAndRunningWork(t *testing.T) {
	ctx := context.Background()
	repoRoot := projectRoot(t)
	odinBinary := buildOdinBinary(t, repoRoot)
	runtimeRoot := t.TempDir()

	store := openRuntimeStore(t, runtimeRoot)
	now := time.Now().UTC()
	store.Now = func() time.Time { return now.Add(-2 * time.Hour) }

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "odin-core",
		GitRoot:       filepath.Join(runtimeRoot, "repos", "odin-core"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(runtimeRoot, "repos", "odin-core"), 0o755); err != nil {
		t.Fatalf("MkdirAll(repo) error = %v", err)
	}

	stalledTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "stalled-task",
		Title:       "Stalled task",
		Status:      "running",
		Scope:       "odin-core",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(stalled) error = %v", err)
	}
	if _, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   stalledTask.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	}); err != nil {
		t.Fatalf("StartRun(stalled) error = %v", err)
	}

	approvalTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "approval-task",
		Title:       "Approval task",
		Status:      "running",
		Scope:       "odin-core",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(approval) error = %v", err)
	}
	approvalRun, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   approvalTask.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(approval) error = %v", err)
	}
	if _, _, _, err := store.AwaitApproval(ctx, sqlite.AwaitApprovalParams{
		TaskID:         approvalTask.ID,
		RunID:          approvalRun.ID,
		RequestedBy:    "operator",
		Summary:        "awaiting approval",
		TerminalReason: "awaiting approval",
		ArtifactsJSON:  `[]`,
	}); err != nil {
		t.Fatalf("AwaitApproval() error = %v", err)
	}

	if _, err := store.SetProjectTransition(ctx, sqlite.SetProjectTransitionParams{
		ProjectID:          project.ID,
		State:              "cutover",
		Controller:         "odin_os",
		LimitedActionsJSON: "[]",
		Notes:              "primary controller",
		ChangedBy:          "operator",
	}); err != nil {
		t.Fatalf("SetProjectTransition() error = %v", err)
	}

	assertRuntimeReadinessCounts(t, store.DB())
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	output, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "status", "--json")
	if err != nil {
		t.Fatalf("runOdinCommand(status --json) error = %v\n%s", err, output)
	}

	var report statusReport
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("status output = %q, want valid JSON: %v", output, err)
	}
	if len(report.ApprovalsWaiting) == 0 {
		t.Fatalf("approvals_waiting empty, want pending approvals")
	}
	if len(report.StalledRuns) == 0 {
		t.Fatalf("stalled_runs empty, want stalled running work")
	}
	if len(report.ActiveRuns) == 0 {
		t.Fatalf("active_runs empty, want running work summary")
	}
	if len(report.ProjectTransitions) == 0 {
		t.Fatalf("project_transitions empty, want ownership summary")
	}

	postStore := openRuntimeStore(t, runtimeRoot)
	defer postStore.Close()
	assertRuntimeReadinessCounts(t, postStore.DB())
}

func seededRuntimeWithProjects(t *testing.T, projectKeys ...string) string {
	t.Helper()

	runtimeRoot := t.TempDir()
	store := openRuntimeStore(t, runtimeRoot)
	defer store.Close()

	for _, key := range projectKeys {
		scope := "project"
		if key == "odin-core" {
			scope = "odin-core"
		}
		repoDir := filepath.Join(runtimeRoot, "repos", key)
		if err := os.MkdirAll(repoDir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", repoDir, err)
		}
		project, err := store.CreateProject(context.Background(), sqlite.CreateProjectParams{
			Key:           key,
			Name:          key,
			Scope:         scope,
			GitRoot:       repoDir,
			DefaultBranch: "main",
			ManifestPath:  filepath.Join("seed", key+".yaml"),
		})
		if err != nil {
			t.Fatalf("CreateProject(%s) error = %v", key, err)
		}
		if _, err := store.CreateTask(context.Background(), sqlite.CreateTaskParams{
			ProjectID:   project.ID,
			Key:         key + "-queued-task",
			Title:       key + " queued task",
			Status:      "queued",
			Scope:       scope,
			RequestedBy: "operator",
		}); err != nil {
			t.Fatalf("CreateTask(%s) error = %v", key, err)
		}
	}

	return runtimeRoot
}

func assertRuntimeReadinessCounts(t *testing.T, db *sql.DB) {
	t.Helper()

	assertCount := func(query string, want int) {
		row := db.QueryRowContext(context.Background(), query)
		var count int
		if err := row.Scan(&count); err != nil {
			t.Fatalf("Scan(%s) error = %v", query, err)
		}
		if count != want {
			t.Fatalf("%s count = %d, want %d", query, count, want)
		}
	}

	assertCount("SELECT COUNT(*) FROM registry_versions", 0)
	assertCount("SELECT COUNT(*) FROM executor_health", 0)
	assertCount("SELECT COUNT(*) FROM projection_freshness", 0)
}
