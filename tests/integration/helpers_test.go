package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/tools/catalog"
)

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

func acceptanceHarnessDriverEnv(t *testing.T) map[string]string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "harness-driver.sh")
	if err := os.WriteFile(path, []byte(`#!/usr/bin/env bash
payload="$(cat)"
PAYLOAD="$payload" python3 - <<'PY'
import json
import os

request = json.loads(os.environ["PAYLOAD"])
action = request.get("action")

if action == "health":
    response = {
        "status": "healthy",
        "details": "acceptance harness driver healthy",
    }
else:
    response = {
        "status": "completed",
        "output": "driver test ok",
        "metadata": {
            "driver": "acceptance_harness",
        },
        "handle": {
            "external_id": "fixture-driver",
        },
    }

print(json.dumps(response))
PY
`), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("Chmod(driver) error = %v", err)
	}
	return map[string]string{
		"ODIN_CODEX_DRIVER":  path,
		"ODIN_CLAUDE_DRIVER": path,
	}
}

func TestAcceptanceHarnessDriverEnvProvidesCodexAndClaudeDrivers(t *testing.T) {
	t.Parallel()

	env := acceptanceHarnessDriverEnv(t)
	if strings.TrimSpace(env["ODIN_CODEX_DRIVER"]) == "" {
		t.Fatalf("ODIN_CODEX_DRIVER missing from acceptance env: %#v", env)
	}
	if strings.TrimSpace(env["ODIN_CLAUDE_DRIVER"]) == "" {
		t.Fatalf("ODIN_CLAUDE_DRIVER missing from acceptance env: %#v", env)
	}
}

func acceptanceWorktreeRoot(extraEnv map[string]string) string {
	homePath := strings.TrimSpace(extraEnv["HOME"])
	if homePath == "" {
		return ""
	}
	return filepath.Join(homePath, ".config", "superpowers", "worktrees", "odin-os")
}

func createCLIRepoRootWithPreferredExecutor(t *testing.T, executorKey string) string {
	t.Helper()

	root := t.TempDir()
	for _, dir := range []string{
		filepath.Join(root, "config"),
		filepath.Join(root, "data"),
		filepath.Join(root, "registry"),
		filepath.Join(root, "state", "cache"),
		filepath.Join(root, "alpha"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", dir, err)
		}
	}

	writeTextFile(t, filepath.Join(root, "config", "projects.yaml"), `
version: 1
projects:
  - key: alpha-cli
    name: Alpha
    project_class: github_backed_project
    git_root: ../alpha
    default_branch: main
    github:
      repo: acme/alpha
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
`)
	writeTextFile(t, filepath.Join(root, "config", "executors.yaml"), fmt.Sprintf(`
version: 1
executors:
  - key: %s
    adapter: %s
    class: plan_backed_cli
    enabled: true
    priority: 10
routes:
  - name: default
    match:
      task_kinds: [general, plan, build, review, qa, research]
      scopes: [global, odin-core, project, new-project]
    preferred: [%s]
`, executorKey, executorKey, executorKey))
	writeTextFile(t, filepath.Join(root, "config", "odin.yaml"), `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:9443
  startup_recovery: true
`)
	writeTextFile(t, filepath.Join(root, "README.md"), "alpha test repo\n")
	writeTextFile(t, filepath.Join(root, "alpha", "README.md"), "alpha nested repo\n")

	initializeGitRepo(t, root)
	initializeGitRepo(t, filepath.Join(root, "alpha"))

	return root
}

func writeTextFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func initializeGitRepo(t *testing.T, dir string) {
	t.Helper()

	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "odin@example.com")
	runGit(t, dir, "config", "user.name", "Odin Acceptance")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "fixture")
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

func hasEventType(events []runtimeevents.Record, target string) bool {
	for _, event := range events {
		if string(event.Type) == target {
			return true
		}
	}
	return false
}
