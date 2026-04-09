package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"odin-os/internal/runtime/checkpoints"
	"odin-os/internal/store/sqlite"
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
	t.Parallel()

	root := createRuntimeRoot(t)
	seedHealthyRuntime(t, root)

	var stdout bytes.Buffer
	if err := Run(context.Background(), root, []string{"healthcheck"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(healthcheck) error = %v", err)
	}

	if !strings.Contains(stdout.String(), "ready") {
		t.Fatalf("healthcheck output = %q, want readiness message", stdout.String())
	}
}

func TestRunHealthcheckDegradedReturnsError(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)

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
