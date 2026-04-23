package integration_test

import (
	"bytes"
	"context"
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
	buildOdinBinaryAt(t, repoRoot, binaryPath)
	return binaryPath
}

func buildOdinBinaryAt(t *testing.T, repoRoot string, binaryPath string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(binary dir) error = %v", err)
	}
	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/odin")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build ./cmd/odin error = %v\n%s", err, string(output))
	}
}

func runOdinCommand(t *testing.T, repoRoot string, binaryPath string, runtimeRoot string, extraEnv map[string]string, stdin string, args ...string) (string, error) {
	t.Helper()

	return runOdinCommandInDir(t, repoRoot, binaryPath, runtimeRoot, extraEnv, stdin, args...)
}

func runOdinCommandInDir(t *testing.T, workDir string, binaryPath string, runtimeRoot string, extraEnv map[string]string, stdin string, args ...string) (string, error) {
	t.Helper()

	cmd := exec.Command(binaryPath, args...)
	cmd.Dir = workDir
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

func runOdinPTYCommandInDir(t *testing.T, workDir string, binaryPath string, runtimeRoot string, extraEnv map[string]string, stdin string, args ...string) (string, error) {
	t.Helper()

	commandParts := make([]string, 0, len(args)+1)
	commandParts = append(commandParts, shellSingleQuote(binaryPath))
	for _, arg := range args {
		commandParts = append(commandParts, shellSingleQuote(arg))
	}

	cmd := exec.Command("script", "-qec", strings.Join(commandParts, " "), "/dev/null")
	cmd.Dir = workDir
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
	normalized := strings.ReplaceAll(string(output), "\r\n", "\n")
	return normalized, err
}

func writeCodexDriverFixture(t *testing.T, output string) string {
	t.Helper()

	driverPath := filepath.Join(t.TempDir(), "codex-driver.sh")
	script := "#!/usr/bin/env bash\nset -euo pipefail\ncat >/dev/null\nprintf " + shellSingleQuote(output) + "\n"
	if err := os.WriteFile(driverPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(driver fixture) error = %v", err)
	}
	return driverPath
}

func shellSingleQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
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

func createLegacyOrchestratorFixture(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	files := map[string]string{
		".claude/skills/social-copy/SKILL.md":         "# Social Copy\n\nLegacy skill body.\n",
		".agents/skills/runtime-auditor/SKILL.md":     "# Runtime Auditor\n\nLegacy mirror body.\n",
		"docs/adr/orchestration-model.md":             "# Orchestration Model\n\nLegacy architecture notes.\n",
		"docs/process/release-checklist.md":           "# Release Checklist\n\nLegacy operator checklist.\n",
		"ops/github-runner/README.md":                 "# Runner\n\nLegacy runner notes.\n",
		"prompts/triage-prompt.md":                    "# Triage Prompt\n\nLegacy prompt content.\n",
		"specs/marcus-social-growth-workflow.md":      "# Marcus Social Growth Workflow\n\nLegacy workflow draft.\n",
		"tmp/ignored/generated.md":                    "# Ignored\n\nThis should stay ignored.\n",
		".git/should-not-exist-but-is-ignored/config": "[core]\n\trepositoryformatversion = 0\n",
	}
	for relativePath, content := range files {
		fullPath := filepath.Join(root, relativePath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", filepath.Dir(fullPath), err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", relativePath, err)
		}
	}

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
