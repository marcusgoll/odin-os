package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"odin-os/internal/runtime/checkpoints"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/vcs/branches"
	gitadapter "odin-os/internal/vcs/git"
	"odin-os/internal/vcs/worktrees"
)

func TestRunDoctorJSONWritesStructuredReport(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)

	var stdout bytes.Buffer
	if err := Run(context.Background(), root, []string{"doctor", "--json"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(doctor --json) error = %v", err)
	}

	var report struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("doctor output is not valid json: %v\n%s", err, stdout.String())
	}
	if report.Status == "" {
		t.Fatalf("doctor report status is empty")
	}
}

func TestRunHealthcheckHealthyReturnsNil(t *testing.T) {
	root := createRuntimeRoot(t)
	t.Setenv("ODIN_CODEX_DRIVER", codexFixtureDriverPath(t))
	seedHealthyRuntime(t, root)

	var stdout bytes.Buffer
	if err := Run(context.Background(), root, []string{"healthcheck"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(healthcheck) error = %v", err)
	}

	if !strings.Contains(stdout.String(), "ready") {
		t.Fatalf("healthcheck output = %q, want readiness message", stdout.String())
	}
}

func TestRunHealthcheckFreshRuntimeReturnsNil(t *testing.T) {
	root := createRuntimeRoot(t)
	t.Setenv("ODIN_CODEX_DRIVER", codexFixtureDriverPath(t))

	var stdout bytes.Buffer
	if err := Run(context.Background(), root, []string{"healthcheck"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(healthcheck fresh runtime) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "ready") {
		t.Fatalf("healthcheck output = %q, want readiness message", stdout.String())
	}
}

func TestRunHealthcheckRejectsIrrelevantHealthyExecutorSample(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)
	seedUnrelatedHealthyRuntime(t, root)

	var stdout bytes.Buffer
	err := Run(context.Background(), root, []string{"healthcheck"}, strings.NewReader(""), &stdout)
	if err == nil {
		t.Fatalf("Run(healthcheck) error = nil, want readiness error")
	}
	if !strings.Contains(stdout.String(), "not ready") {
		t.Fatalf("healthcheck output = %q, want not ready message", stdout.String())
	}
}

func TestRunHealthcheckDegradedReturnsError(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)
	if err := os.WriteFile(filepath.Join(root, "config", "projects.yaml"), []byte(`
version: 1
projects:
  - key: odin-core
    name: Odin Core
    project_class: system_project
    system_project: true
    git_root: ..
    default_branch: main
`), 0o644); err != nil {
		t.Fatalf("write invalid projects config: %v", err)
	}

	var stdout bytes.Buffer
	err := Run(context.Background(), root, []string{"healthcheck"}, strings.NewReader(""), &stdout)
	if err == nil {
		t.Fatalf("Run(healthcheck) error = nil, want readiness error")
	}
	if !strings.Contains(stdout.String(), "not ready") {
		t.Fatalf("healthcheck output = %q, want not ready message", stdout.String())
	}
}

func TestRunServeExecutesStartupRecoveryBeforeShutdown(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)
	writeRuntimeConfig(t, root, `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:0
  startup_recovery: true
`)

	projectID, taskID, runID := seedRunningTask(t, root)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)

	var stdout bytes.Buffer
	err := Run(ctx, root, []string{"serve"}, strings.NewReader(""), &stdout)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(serve) error = %v", err)
	}

	store, openErr := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if openErr != nil {
		t.Fatalf("sqlite.Open() error = %v", openErr)
	}
	defer store.Close()

	gotRun, err := store.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if gotRun.Status != "interrupted" {
		t.Fatalf("GetRun().Status = %q, want %q", gotRun.Status, "interrupted")
	}

	packet, err := store.GetLatestTaskWakePacket(context.Background(), projectID, taskID)
	if err != nil {
		t.Fatalf("GetLatestTaskWakePacket() error = %v", err)
	}
	if packet.Trigger != string(checkpoints.TriggerRestart) {
		t.Fatalf("WakePacket.Trigger = %q, want %q", packet.Trigger, checkpoints.TriggerRestart)
	}
}

func TestRunServeExecutesStartupRecoveryWhenContextAlreadyCanceled(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)
	writeRuntimeConfig(t, root, `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:0
  startup_recovery: true
`)

	projectID, taskID, runID := seedRunningTask(t, root)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var stdout bytes.Buffer
	err := Run(ctx, root, []string{"serve"}, strings.NewReader(""), &stdout)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(serve) error = %v", err)
	}

	store, openErr := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if openErr != nil {
		t.Fatalf("sqlite.Open() error = %v", openErr)
	}
	defer store.Close()

	gotRun, err := store.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if gotRun.Status != "interrupted" {
		t.Fatalf("GetRun().Status = %q, want %q", gotRun.Status, "interrupted")
	}

	packet, err := store.GetLatestTaskWakePacket(context.Background(), projectID, taskID)
	if err != nil {
		t.Fatalf("GetLatestTaskWakePacket() error = %v", err)
	}
	if packet.Trigger != string(checkpoints.TriggerRestart) {
		t.Fatalf("WakePacket.Trigger = %q, want %q", packet.Trigger, checkpoints.TriggerRestart)
	}
}

func TestRunServeRunsSelfHealCycleBeforeShutdown(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)
	writeRuntimeConfig(t, root, `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:0
  startup_recovery: true
`)
	seedHealthyRuntime(t, root)

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	if _, err := store.RecordProjectionFreshness(context.Background(), sqlite.RecordProjectionFreshnessParams{
		Surface:     "doctor",
		Status:      "stale",
		DetailsJSON: `{"source":"serve-test"}`,
	}); err != nil {
		t.Fatalf("RecordProjectionFreshness() error = %v", err)
	}

	originalSelfHealInterval := serveSelfHealLoopInterval
	serveSelfHealLoopInterval = 20 * time.Millisecond
	defer func() {
		serveSelfHealLoopInterval = originalSelfHealInterval
	}()

	ctx, cancel := context.WithCancel(context.Background())
	actionObserved := make(chan struct{})
	time.AfterFunc(30*time.Millisecond, func() {
		staleAt := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339Nano)
		if _, err := store.DB().ExecContext(context.Background(), `
			UPDATE projection_freshness
			SET refreshed_at = ?, updated_at = ?
			WHERE surface = 'doctor'
		`, staleAt, staleAt); err != nil {
			t.Errorf("force stale projection freshness error = %v", err)
		}
	})
	go func() {
		deadline := time.NewTimer(2 * time.Second)
		defer deadline.Stop()

		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-deadline.C:
				cancel()
				return
			case <-ticker.C:
				events, err := store.ListEvents(context.Background(), sqlite.ListEventsParams{})
				if err != nil {
					continue
				}
				for _, event := range events {
					if string(event.Type) == "recovery.action_executed" {
						close(actionObserved)
						cancel()
						return
					}
				}
			}
		}
	}()

	var stdout bytes.Buffer
	err = Run(ctx, root, []string{"serve"}, strings.NewReader(""), &stdout)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(serve) error = %v", err)
	}

	events, err := store.ListEvents(context.Background(), sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}

	select {
	case <-actionObserved:
	default:
	}

	found := false
	for _, event := range events {
		if string(event.Type) == "recovery.action_executed" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("events = %+v, want recovery.action_executed from background self-heal cycle", events)
	}
}

func TestRunServeCleansReleasedWorktreeLeasesDuringBackgroundLoop(t *testing.T) {
	root := createRuntimeRoot(t)
	writeRuntimeConfig(t, root, `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:0
  startup_recovery: false
`)

	homeDir := filepath.Join(root, "home")
	t.Setenv("HOME", homeDir)
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}

	repoRoot := filepath.Join(root, "repos", "alpha")
	initServeTestRepo(t, repoRoot)

	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       repoRoot,
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "alpha-task",
		Title:       "Cleanup worktree",
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

	worktreePath := worktrees.ResolvePath(worktrees.PathParams{
		Root:       worktrees.DefaultRoot(),
		ProjectKey: project.Key,
		TaskID:     task.ID,
		RunID:      run.ID,
		Try:        1,
	})
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		t.Fatalf("mkdir worktree parent: %v", err)
	}

	branchName := branches.Name(branches.NameParams{
		ProjectKey: project.Key,
		TaskID:     task.ID,
		RunID:      run.ID,
		Try:        1,
	})
	adapter := gitadapter.Adapter{}
	if err := adapter.CreateBranch(ctx, repoRoot, branchName, "main"); err != nil {
		t.Fatalf("CreateBranch() error = %v", err)
	}
	if err := adapter.AddWorktree(ctx, repoRoot, worktreePath, branchName); err != nil {
		t.Fatalf("AddWorktree() error = %v", err)
	}

	lease, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       task.ID,
		RunID:        run.ID,
		Mode:         "mutable",
		BranchName:   branchName,
		WorktreePath: worktreePath,
		RepoRoot:     repoRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease() error = %v", err)
	}
	lease, err = store.ReleaseWorktreeLease(ctx, sqlite.ReleaseWorktreeLeaseParams{
		LeaseID: lease.ID,
		State:   "released",
	})
	if err != nil {
		t.Fatalf("ReleaseWorktreeLease() error = %v", err)
	}

	originalCleanupInterval := serveWorktreeCleanupInterval
	serveWorktreeCleanupInterval = 20 * time.Millisecond
	t.Cleanup(func() {
		serveWorktreeCleanupInterval = originalCleanupInterval
	})

	runCtx, cancel := context.WithCancel(context.Background())
	cleanupObserved := make(chan struct{})
	stdout := &lockedBuffer{}
	runDone := make(chan error, 1)
	go func() {
		runDone <- Run(runCtx, root, []string{"serve"}, strings.NewReader(""), stdout)
	}()

	if err := waitForOutput(stdout, "serving on", 2*time.Second); err != nil {
		cancel()
		<-runDone
		t.Fatal(err)
	}

	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()

		deadline := time.NewTimer(2 * time.Second)
		defer deadline.Stop()

		for {
			select {
			case <-runCtx.Done():
				return
			case <-deadline.C:
				cancel()
				return
			case <-ticker.C:
				current, err := store.GetWorktreeLease(context.Background(), lease.ID)
				if err != nil {
					continue
				}
				if current.CleanedUpAt != nil {
					close(cleanupObserved)
					cancel()
					return
				}
			}
		}
	}()

	select {
	case err := <-runDone:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Run(serve) error = %v", err)
		}
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("timed out waiting for Run(serve) to exit")
	}

	select {
	case <-cleanupObserved:
	default:
		t.Fatal("cleanup loop did not observe cleaned worktree lease")
	}

	updatedLease, err := store.GetWorktreeLease(context.Background(), lease.ID)
	if err != nil {
		t.Fatalf("GetWorktreeLease() error = %v", err)
	}
	if updatedLease.State != "cleaned" {
		t.Fatalf("lease.State = %q, want cleaned", updatedLease.State)
	}
	if updatedLease.CleanedUpAt == nil {
		t.Fatalf("lease.CleanedUpAt = nil, want value")
	}
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("worktree path still exists after cleanup: %v", err)
	}
}

func codexFixtureDriverPath(t *testing.T) string {
	t.Helper()

	return filepath.Clean(filepath.Join("..", "..", "..", "scripts", "drivers", "codex-headless.sh"))
}

func createRuntimeRoot(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "registry"), 0o755); err != nil {
		t.Fatalf("mkdir registry: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "config", "odin.yaml"), []byte(`
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:9443
  startup_recovery: true
`), 0o644); err != nil {
		t.Fatalf("write odin config: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "config", "projects.yaml"), []byte(`
version: 1
projects:
  - key: odin-core
    name: Odin Core
    project_class: system_project
    system_project: true
    git_root: ..
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
        require_for_system_project_changes: true
      merge_policy:
        mode: squash
        allow_direct_to_default_branch: false
      destructive_operations:
        allow_reset: false
        allow_clean: false
        allow_force_push: false
        require_explicit_approval: true
`), 0o644); err != nil {
		t.Fatalf("write projects config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "executors.yaml"), []byte(`
version: 1
executors:
  - key: codex_headless
    adapter: codex_headless
    class: plan_backed_cli
    enabled: true
    priority: 10
routes:
  - name: default
    match:
      task_kinds: [general, plan, build, review, qa, research]
      scopes: [global, odin-core, project, new-project]
    preferred: [codex_headless]
`), 0o644); err != nil {
		t.Fatalf("write executors config: %v", err)
	}

	return root
}

func seedHealthyRuntime(t *testing.T, root string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	if _, err := store.RecordRegistryVersion(ctx, sqlite.RecordRegistryVersionParams{
		Source:      "registry",
		VersionHash: "phase-15",
		Notes:       "healthy test sample",
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
		DetailsJSON: `{"source":"test"}`,
	}); err != nil {
		t.Fatalf("RecordProjectionFreshness() error = %v", err)
	}
}

func seedUnrelatedHealthyRuntime(t *testing.T, root string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	if _, err := store.RecordRegistryVersion(ctx, sqlite.RecordRegistryVersionParams{
		Source:      "registry",
		VersionHash: "phase-15",
		Notes:       "healthy test sample",
	}); err != nil {
		t.Fatalf("RecordRegistryVersion() error = %v", err)
	}

	if _, err := store.RecordExecutorHealth(ctx, sqlite.RecordExecutorHealthParams{
		Executor:    "openai_api",
		Status:      "healthy",
		LatencyMS:   10,
		DetailsJSON: `{"status":"healthy"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth() error = %v", err)
	}

	if _, err := store.RecordProjectionFreshness(ctx, sqlite.RecordProjectionFreshnessParams{
		Surface:     "active_runs",
		Status:      "current",
		DetailsJSON: `{"source":"test"}`,
	}); err != nil {
		t.Fatalf("RecordProjectionFreshness() error = %v", err)
	}
}

func seedRunningTask(t *testing.T, root string) (int64, int64, int64) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       filepath.Join(root, "repos", "alpha"),
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

	return project.ID, task.ID, run.ID
}

func writeRuntimeConfig(t *testing.T, root string, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(root, "config", "odin.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write odin config: %v", err)
	}
}

func initServeTestRepo(t *testing.T, repoRoot string) {
	t.Helper()

	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}
	if err := runServeGit(context.Background(), repoRoot, "init", "-b", "main"); err != nil {
		t.Fatalf("git init error = %v", err)
	}
	if err := runServeGit(context.Background(), repoRoot, "config", "user.name", "Odin Test"); err != nil {
		t.Fatalf("git config user.name error = %v", err)
	}
	if err := runServeGit(context.Background(), repoRoot, "config", "user.email", "odin@example.com"); err != nil {
		t.Fatalf("git config user.email error = %v", err)
	}

	readmePath := filepath.Join(repoRoot, "README.md")
	if err := os.WriteFile(readmePath, []byte("# temp repo\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(README.md) error = %v", err)
	}
	if err := runServeGit(context.Background(), repoRoot, "add", "README.md"); err != nil {
		t.Fatalf("git add error = %v", err)
	}
	if err := runServeGit(context.Background(), repoRoot, "commit", "-m", "initial commit"); err != nil {
		t.Fatalf("git commit error = %v", err)
	}
}

func runServeGit(ctx context.Context, repoRoot string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repoRoot}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %v: %w: %s", args, err, string(output))
	}
	return nil
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (buffer *lockedBuffer) Write(p []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()

	return buffer.b.Write(p)
}

func (buffer *lockedBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()

	return buffer.b.String()
}

func waitForOutput(buffer *lockedBuffer, want string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for output %q; got %q", want, buffer.String())
		case <-ticker.C:
			if strings.Contains(buffer.String(), want) {
				return nil
			}
		}
	}
}
