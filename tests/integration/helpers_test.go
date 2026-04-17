package integration_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	httpapi "odin-os/internal/api/http"
	"odin-os/internal/core/companions"
	"odin-os/internal/core/controlscope"
	"odin-os/internal/core/workitems"
	"odin-os/internal/core/workspaces"
	"odin-os/internal/runtime/checkpoints"
	runtimeevents "odin-os/internal/runtime/events"
	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/store/sqlite"
	metricsvc "odin-os/internal/telemetry/metrics"
	"odin-os/internal/tools/catalog"
)

type workspaceOperatingModelFixture struct {
	Workspace  sqlite.Workspace
	Initiative sqlite.Initiative
	Companion  sqlite.Companion
	Task       sqlite.Task
	Run        sqlite.Run
	Approval   sqlite.Approval
}

func projectRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func buildOdinBinary(t *testing.T, repoRoot string) string {
	t.Helper()

	binaryPath := filepath.Join(t.TempDir(), "odin")
	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/odin")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build ./cmd/odin error = %v\n%s", err, string(output))
	}
	return binaryPath
}

func runOdinCommand(t *testing.T, repoRoot string, binaryPath string, runtimeRoot string, extraEnv map[string]string, stdin string, args ...string) (string, error) {
	t.Helper()

	cmd := exec.Command(binaryPath, args...)
	cmd.Dir = repoRoot
	if runtimeRoot != "" {
		if err := os.MkdirAll(runtimeRoot, 0o755); err != nil {
			t.Fatalf("MkdirAll(runtimeRoot) error = %v", err)
		}
	}

	env := append([]string{}, os.Environ()...)
	if runtimeRoot != "" {
		env = append(env, "ODIN_ROOT="+runtimeRoot)
	}
	for key, value := range extraEnv {
		env = append(env, key+"="+value)
	}
	cmd.Env = env
	cmd.Stdin = bytes.NewBufferString(stdin)

	output, err := cmd.CombinedOutput()
	return string(output), err
}

func requirePathExists(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("path %s missing: %v", path, err)
	}
}

func createGitRepository(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.email", "odin@example.com")
	runGit(t, root, "config", "user.name", "Odin Acceptance")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# fixture\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(README.md) error = %v", err)
	}
	runGit(t, root, "add", "README.md")
	runGit(t, root, "commit", "-m", "initial")
	return root
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v error = %v\n%s", args, err, string(output))
	}
}

func writeProjectManifest(t *testing.T, path string, localRepo string, githubRepo string) {
	t.Helper()

	content := `
version: 1
projects:
  - key: local-demo
    name: Local Demo
    project_class: local_git_project
    git_root: ` + localRepo + `
    default_branch: main
    policy:
      allowed_commands: [status]
      branch_rules:
        protected_branches: [main]
        require_worktree: true
        require_task_branch: true
        allow_default_branch_mutation: false
      approval_gates:
        require_for_governance_changes: true
        require_for_destructive_operations: true
        require_for_system_project_changes: false
      merge_policy:
        mode: squash
        allow_direct_to_default_branch: false
      destructive_operations:
        allow_reset: false
        allow_clean: false
        allow_force_push: false
        require_explicit_approval: true
  - key: github-demo
    name: GitHub Demo
    project_class: github_backed_project
    git_root: ` + githubRepo + `
    default_branch: main
    github:
      repo: example/github-demo
    policy:
      allowed_commands: [status]
      branch_rules:
        protected_branches: [main]
        require_worktree: true
        require_task_branch: true
        allow_default_branch_mutation: false
      approval_gates:
        require_for_governance_changes: true
        require_for_destructive_operations: true
        require_for_system_project_changes: false
      merge_policy:
        mode: squash
        allow_direct_to_default_branch: false
      destructive_operations:
        allow_reset: false
        allow_clean: false
        allow_force_push: false
        require_explicit_approval: true
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(projects.yaml) error = %v", err)
	}
}

func hasCapability(cards []catalog.Card, target string) bool {
	for _, card := range cards {
		if card.Key == target {
			return true
		}
	}
	return false
}

func openTempStore(t *testing.T) *sqlite.Store {
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

func openRuntimeStore(t *testing.T, runtimeRoot string) *sqlite.Store {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(runtimeRoot, "data"), 0o755); err != nil {
		t.Fatalf("MkdirAll(data) error = %v", err)
	}
	store, err := sqlite.Open(filepath.Join(runtimeRoot, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open(runtime) error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate(runtime) error = %v", err)
	}
	return store
}

func seedTaskRunFixture(t *testing.T, ctx context.Context, store *sqlite.Store, key string, scope string, taskKey string, title string, executor string, now time.Time) (sqlite.Project, sqlite.Task, sqlite.Run) {
	t.Helper()

	store.Now = func() time.Time { return now }
	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           key,
		Name:          key,
		Scope:         scope,
		GitRoot:       filepath.Join(t.TempDir(), key),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         taskKey,
		Title:       title,
		Status:      "running",
		Scope:       scope,
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: executor,
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	return project, task, run
}

func seedHealthyObservability(t *testing.T, ctx context.Context, store *sqlite.Store, now time.Time) {
	t.Helper()

	store.Now = func() time.Time { return now }
	if _, err := store.RecordRegistryVersion(ctx, sqlite.RecordRegistryVersionParams{
		Source:      "registry",
		VersionHash: "alpha-acceptance",
		Notes:       "healthy sample",
	}); err != nil {
		t.Fatalf("RecordRegistryVersion() error = %v", err)
	}
	if _, err := store.RecordExecutorHealth(ctx, sqlite.RecordExecutorHealthParams{
		Executor:    "codex_headless",
		Status:      "healthy",
		LatencyMS:   10,
		DetailsJSON: `{"status":"healthy"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth() error = %v", err)
	}
	if _, err := store.RecordProjectionFreshness(ctx, sqlite.RecordProjectionFreshnessParams{
		Surface:     "active_runs",
		Status:      "current",
		DetailsJSON: `{"source":"acceptance"}`,
	}); err != nil {
		t.Fatalf("RecordProjectionFreshness() error = %v", err)
	}
}

func seedWorkspaceOperatingModelFixture(t *testing.T, ctx context.Context, store *sqlite.Store, now time.Time) workspaceOperatingModelFixture {
	t.Helper()

	store.Now = func() time.Time { return now }

	bootstrapped, err := workspaces.Service{Store: store}.BootstrapDefault(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefault() error = %v", err)
	}
	workspace, err := store.GetWorkspaceByKey(ctx, bootstrapped.Key)
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(%s) error = %v", bootstrapped.Key, err)
	}
	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "odin-core",
		GitRoot:       filepath.Join(t.TempDir(), "odin-core"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(odin-core) error = %v", err)
	}

	companion, err := store.CreateCompanion(ctx, sqlite.CreateCompanionParams{
		WorkspaceID:         workspace.ID,
		Key:                 "operator",
		Title:               "Operator",
		Kind:                companions.KindOperator,
		Charter:             "Run the workspace operating rhythm.",
		Status:              companions.StatusActive,
		InitiativeScopeJSON: `{"mode":"all"}`,
		ToolPolicyJSON:      `{"mode":"deny","allowed":[]}`,
		MemoryPolicyJSON:    `{"retention":"workspace"}`,
		PlanningPolicyJSON:  `{"mode":"stepwise"}`,
	})
	if err != nil {
		t.Fatalf("CreateCompanion() error = %v", err)
	}

	initiative, err := store.GetInitiativeByProjectID(ctx, project.ID)
	if err != nil {
		t.Fatalf("GetInitiativeByProjectID() error = %v", err)
	}
	if err := store.AssignInitiativeCompanion(ctx, initiative.ID, companion.ID); err != nil {
		t.Fatalf("AssignInitiativeCompanion() error = %v", err)
	}
	initiative, err = store.GetInitiative(ctx, initiative.ID)
	if err != nil {
		t.Fatalf("GetInitiative() error = %v", err)
	}

	workItem, err := workitems.Service{Store: store}.Create(ctx, controlscope.Service{}.ResolveInitiative(workspace.Key, initiative.Key), "Follow up on approvals")
	if err != nil {
		t.Fatalf("Create(work item) error = %v", err)
	}

	task, err := store.GetTask(ctx, workItem.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
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
	if _, err := store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID: task.ID,
		Status: "blocked",
	}); err != nil {
		t.Fatalf("UpdateTaskStatus(blocked) error = %v", err)
	}
	approval, err := store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      task.ID,
		RunID:       &run.ID,
		Status:      "pending",
		RequestedBy: "system",
	})
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}
	if _, err := (checkpoints.Service{Store: store}).Compact(ctx, checkpoints.CompactParams{
		TaskID:          task.ID,
		RunID:           &run.ID,
		Trigger:         checkpoints.TriggerApprovalWait,
		CheckpointKey:   "workspace-home",
		Objective:       task.Title,
		TaskStatus:      "blocked",
		BlockingReason:  "awaiting operator approval",
		NextSteps:       []string{"resume once approved"},
		ManifestSummary: "workspace task",
		PolicySummary:   "approval required",
		OpenTaskSummary: "one blocked task",
		ApprovalSummary: "one pending approval",
	}); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	return workspaceOperatingModelFixture{
		Workspace:  workspace,
		Initiative: initiative,
		Companion:  companion,
		Task:       task,
		Run:        run,
		Approval:   approval,
	}
}

func newWorkspaceAPIHandler(store *sqlite.Store) *httpapi.Dependencies {
	return &httpapi.Dependencies{
		Store: store,
		Health: healthsvc.Service{
			DB: store.DB(),
		},
		Metrics: metricsvc.Service{
			DB: store.DB(),
		},
		RegistryHealthy: true,
	}
}

func hasEventType(events []runtimeevents.Record, target string) bool {
	for _, event := range events {
		if string(event.Type) == target {
			return true
		}
	}
	return false
}
