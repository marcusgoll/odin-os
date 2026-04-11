package integration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
	"odin-os/internal/app/bootstrap"
	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
	"odin-os/internal/executors/contract"
	executorrouter "odin-os/internal/executors/router"
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

type authoredProjectsConfig struct {
	Cutover cutoverMetadata `yaml:"cutover"`
}

type cutoverMetadata struct {
	PilotProjects []pilotProjectMetadata `yaml:"pilot_projects"`
}

type pilotProjectMetadata struct {
	Key                       string   `yaml:"key"`
	RuntimeOwner              string   `yaml:"runtime_owner"`
	PrimaryController         string   `yaml:"primary_controller"`
	ComparisonContext         string   `yaml:"comparison_context"`
	LegacyPrimaryRequired     bool     `yaml:"legacy_primary_required"`
	ShadowGraduation          []string `yaml:"shadow_graduation"`
	LimitedActionGraduation   []string `yaml:"limited_action_graduation"`
	CutoverGraduation         []string `yaml:"cutover_graduation"`
	LegacyDutiesToRetireOrder []string `yaml:"legacy_duties_to_retire_in_order"`
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

func TestCutoverPilotProjectsStayRunnableWithoutLegacyPrimary(t *testing.T) {
	ctx := context.Background()
	repoRoot := projectRoot(t)

	evidence := loadCutoverEvidence(t, repoRoot)
	if len(evidence.Cutover.PilotProjects) == 0 {
		t.Fatal("cutover metadata has no pilot projects, want at least one")
	}

	pilot := evidence.Cutover.PilotProjects[0]
	if pilot.Key != "pbs" {
		t.Fatalf("pilot key = %q, want pbs", pilot.Key)
	}
	if pilot.RuntimeOwner != "odin_os" {
		t.Fatalf("runtime owner = %q, want odin_os", pilot.RuntimeOwner)
	}
	if pilot.PrimaryController != "odin_os" {
		t.Fatalf("primary controller = %q, want odin_os", pilot.PrimaryController)
	}
	if pilot.ComparisonContext != "odin-orchestrator" {
		t.Fatalf("comparison context = %q, want odin-orchestrator", pilot.ComparisonContext)
	}
	if pilot.LegacyPrimaryRequired {
		t.Fatal("legacy_primary_required = true, want false for first cutover pilot")
	}
	if !slices.Equal(pilot.LegacyDutiesToRetireOrder, []string{
		"read-only observation and compare reporting",
		"limited-action handling for allowlisted low-risk mutations",
		"routine queue intake and run selection",
		"normal project mutation and merge authority",
		"legacy-primary fallback for routine completion",
	}) {
		t.Fatalf("legacy duties = %v, want documented retire order", pilot.LegacyDutiesToRetireOrder)
	}
	if !slices.Equal(pilot.ShadowGraduation, []string{
		"legacy and Odin readouts agree on project scope and ownership",
		"no mutation attempt requires an allowed action",
		"operator review confirms the project can stay read-only",
	}) {
		t.Fatalf("shadow graduation = %v, want documented criteria", pilot.ShadowGraduation)
	}
	if !slices.Equal(pilot.LimitedActionGraduation, []string{
		"allowlisted isolated mutations complete successfully under Odin ownership",
		"limited-action work never depends on legacy primary completion",
		"operator review shows no unbounded approval or recovery drift",
	}) {
		t.Fatalf("limited-action graduation = %v, want documented criteria", pilot.LimitedActionGraduation)
	}
	if !slices.Equal(pilot.CutoverGraduation, []string{
		"routine queued work completes under Odin OS ownership",
		"normal project mutations no longer need legacy-primary intervention",
		"rollback remains available and rehearsed",
	}) {
		t.Fatalf("cutover graduation = %v, want documented criteria", pilot.CutoverGraduation)
	}

	assertFileContains(t, filepath.Join(repoRoot, "docs/operations/odin-os-cutover.md"), []string{
		"pbs",
		"pilot project selection rules",
		"shadow graduation criteria",
		"limited-action graduation criteria",
		"cutover graduation criteria",
		"legacy duties to retire in order",
	})
	assertFileContains(t, filepath.Join(repoRoot, "docs/operations/odin-os-rollback.md"), []string{
		"pbs",
		"rollback triggers",
		"pause or roll back",
		"pilot requires the legacy stack for routine completion",
	})

	runtimeRoot := t.TempDir()
	store := openRuntimeStore(t, runtimeRoot)
	defer store.Close()

	registry, err := buildPilotRegistry(t)
	if err != nil {
		t.Fatalf("buildPilotRegistry() error = %v", err)
	}

	service := jobsvc.Service{
		Store:    store,
		Registry: registry,
		Executors: map[string]contract.Executor{
			"pbs_stub": pbsExecutor{},
		},
		ExecutorConfig: executorrouter.Config{
			Version: 1,
			Executors: []executorrouter.ExecutorConfig{
				{
					Key:      "pbs_stub",
					Adapter:  "stub",
					Class:    contract.ExecutorClassPlanBackedCLI,
					Enabled:  true,
					Priority: 1,
				},
			},
			Routes: []executorrouter.RouteConfig{
				{
					Name: "pbs-pilot",
					Match: executorrouter.RouteMatch{
						TaskKinds: []contract.TaskKind{contract.TaskKindGeneral},
						Scopes:    []string{"project"},
					},
					Preferred: []string{"pbs_stub"},
				},
			},
		},
		Transitions: projects.Service{Store: store},
		Leases: leases.Manager{
			Store:        store,
			Git:          pbsGit{},
			WorktreeRoot: t.TempDir(),
		},
		Now: time.Now,
	}

	task, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: pilot.Key,
	}, "Pilot cutover task")
	if err != nil {
		t.Fatalf("CreateTaskFromAct() error = %v", err)
	}

	project, err := store.GetProjectByKey(ctx, pilot.Key)
	if err != nil {
		t.Fatalf("GetProjectByKey() error = %v", err)
	}

	transition, err := service.Transitions.SetTransitionState(ctx, projects.TransitionStateInput{
		ProjectID:   project.ID,
		Actor:       projects.TransitionControllerOdinOS,
		TargetState: projects.TransitionStateCutover,
		ChangedBy:   "operator",
		Notes:       "pbs pilot owns normal mutation without legacy primary",
	})
	if err != nil {
		t.Fatalf("SetTransitionState(cutover) error = %v", err)
	}
	if transition.Controller != string(projects.TransitionControllerOdinOS) {
		t.Fatalf("transition controller = %q, want odin_os", transition.Controller)
	}
	if transition.State != string(projects.TransitionStateCutover) {
		t.Fatalf("transition state = %q, want cutover", transition.State)
	}

	if err := service.ExecuteNextQueued(ctx); err != nil {
		t.Fatalf("ExecuteNextQueued() error = %v", err)
	}

	completedTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if completedTask.Status != "completed" {
		t.Fatalf("task status = %q, want completed", completedTask.Status)
	}

	approvals, err := projections.ListPendingApprovalViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListPendingApprovalViews() error = %v", err)
	}
	if len(approvals) != 0 {
		t.Fatalf("pending approvals = %d, want 0 for normal pilot completion", len(approvals))
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

func loadCutoverEvidence(t *testing.T, repoRoot string) authoredProjectsConfig {
	t.Helper()

	configPath := filepath.Join(repoRoot, "config", "projects.yaml")
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", configPath, err)
	}

	var cfg authoredProjectsConfig
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal(projects.yaml) error = %v", err)
	}
	return cfg
}

func buildPilotRegistry(t *testing.T) (projects.Registry, error) {
	t.Helper()

	root := t.TempDir()
	for _, key := range []string{"pbs", "odin-orchestrator"} {
		if err := os.MkdirAll(filepath.Join(root, key, ".git"), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s/.git) error = %v", key, err)
		}
	}

	manifestPath := filepath.Join(root, "projects.yaml")
	if err := os.WriteFile(manifestPath, []byte(`
version: 1
projects:
  - key: pbs
    name: PBS
    project_class: local_git_project
    git_root: pbs
    default_branch: main
    scheduler:
      max_concurrent_runs: 1
      max_starts_per_cycle: 1
      stalled_run_retry_limit: 2
    policy:
      allowed_commands:
        - status
        - test
      branch_rules:
        protected_branches:
          - main
        require_worktree: true
        require_task_branch: true
        allow_default_branch_mutation: false
      approval_gates:
        require_for_governance_changes: false
        require_for_destructive_operations: false
        require_for_system_project_changes: false
      merge_policy:
        mode: squash
        allow_direct_to_default_branch: false
      destructive_operations:
        allow_reset: false
        allow_clean: false
        allow_force_push: false
        require_explicit_approval: true
  - key: odin-orchestrator
    name: Odin Orchestrator
    project_class: local_git_project
    git_root: odin-orchestrator
    default_branch: main
    scheduler:
      max_concurrent_runs: 1
      max_starts_per_cycle: 1
      stalled_run_retry_limit: 2
    policy:
      allowed_commands:
        - status
        - test
      branch_rules:
        protected_branches:
          - main
        require_worktree: true
        require_task_branch: true
        allow_default_branch_mutation: false
      approval_gates:
        require_for_governance_changes: false
        require_for_destructive_operations: false
        require_for_system_project_changes: false
      merge_policy:
        mode: squash
        allow_direct_to_default_branch: false
      destructive_operations:
        allow_reset: false
        allow_clean: false
        allow_force_push: false
        require_explicit_approval: true
`), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", manifestPath, err)
	}

	registry, diagnostics, err := projects.Register(manifestPath)
	if err != nil {
		return projects.Registry{}, err
	}
	if len(diagnostics) != 0 {
		return projects.Registry{}, fmt.Errorf("registry diagnostics: %+v", diagnostics)
	}
	return registry, nil
}

func assertFileContains(t *testing.T, path string, required []string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	text := strings.ToLower(string(content))
	for _, needle := range required {
		if !strings.Contains(text, strings.ToLower(needle)) {
			t.Fatalf("%s missing required text %q", path, needle)
		}
	}
}

type pbsExecutor struct{}

func (pbsExecutor) Key() string { return "pbs_stub" }

func (pbsExecutor) Class() contract.ExecutorClass { return contract.ExecutorClassPlanBackedCLI }

func (pbsExecutor) Health(context.Context) (contract.HealthReport, error) {
	return contract.HealthReport{Status: contract.HealthStatusHealthy, CheckedAt: time.Now().UTC()}, nil
}

func (pbsExecutor) Capabilities(context.Context) (contract.Capabilities, error) {
	return contract.Capabilities{
		ExecutorClass:        contract.ExecutorClassPlanBackedCLI,
		SupportsResume:       true,
		SupportsCancel:       true,
		SupportsTools:        true,
		SupportsHeadlessPlan: true,
		TaskKinds:            []contract.TaskKind{contract.TaskKindGeneral},
		Scopes:               []string{"project"},
	}, nil
}

func (pbsExecutor) RunTask(context.Context, contract.TaskSpec) (contract.ExecutionResult, error) {
	return contract.ExecutionResult{
		Status: "completed",
		Output: "pbs pilot task completed",
	}, nil
}

func (pbsExecutor) ResumeTask(context.Context, contract.TaskHandle, contract.ResumePacket) (contract.ExecutionResult, error) {
	return contract.ExecutionResult{}, fmt.Errorf("resume not supported in test executor")
}

func (pbsExecutor) CancelTask(context.Context, contract.TaskHandle) error {
	return fmt.Errorf("cancel not supported in test executor")
}

func (pbsExecutor) EstimateCost(context.Context, contract.TaskSpec) (contract.CostEstimate, error) {
	return contract.CostEstimate{}, fmt.Errorf("estimate cost not supported in test executor")
}

type pbsGit struct{}

func (pbsGit) BranchExists(context.Context, string, string) (bool, error) { return false, nil }
func (pbsGit) CreateBranch(context.Context, string, string, string) error { return nil }
func (pbsGit) AddWorktree(context.Context, string, string, string) error  { return nil }
func (pbsGit) RemoveWorktree(context.Context, string, string) error       { return nil }

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
