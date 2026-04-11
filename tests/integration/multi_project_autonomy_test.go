package integration_test

import (
	"context"
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
