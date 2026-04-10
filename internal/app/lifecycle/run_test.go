package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/vcs/worktrees"
)

const testProjectKey = "alpha-cli"

func TestRunReplStartsInteractiveShell(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)

	stdin := strings.NewReader("/help\n")
	var stdout bytes.Buffer

	err := Run(context.Background(), root, []string{"repl"}, stdin, &stdout)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "scope=") {
		t.Fatalf("Run() output = %q, want header", output)
	}
	if !strings.Contains(output, "/help") {
		t.Fatalf("Run() output = %q, want help", output)
	}
}

func TestRunWithoutArgsPrintsUsageInsteadOfStartingShell(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	var stdout bytes.Buffer

	err := Run(context.Background(), root, nil, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "Usage: odin") {
		t.Fatalf("stdout = %q, want usage banner", output)
	}
	if strings.Contains(output, "odin>") {
		t.Fatalf("stdout = %q, should not contain repl prompt", output)
	}
}

func TestRunStatusJSON(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	var stdout bytes.Buffer

	err := Run(context.Background(), root, []string{"status", "--json"}, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var payload struct {
		Health           string `json:"health"`
		PendingApprovals int    `json:"pending_approvals"`
		RegistryHealthy  bool   `json:"registry_healthy"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("status json = %v", err)
	}
	if payload.Health == "" {
		t.Fatalf("Health = %q, want non-empty", payload.Health)
	}
	if !payload.RegistryHealthy {
		t.Fatalf("RegistryHealthy = false, want true")
	}
}

func TestRunProjectListText(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	var stdout bytes.Buffer

	err := Run(context.Background(), root, []string{"project", "list"}, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "odin-core") {
		t.Fatalf("stdout = %q, want project key", stdout.String())
	}
}

func TestRunProjectSelectPersistsSession(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	var stdout bytes.Buffer

	err := Run(context.Background(), root, []string{"project", "select", testProjectKey}, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "project="+testProjectKey) {
		t.Fatalf("stdout = %q, want selection confirmation", stdout.String())
	}

	sessionBytes, err := os.ReadFile(filepath.Join(root, "state", "cache", "cli-session.json"))
	if err != nil {
		t.Fatalf("ReadFile(cli-session.json) error = %v", err)
	}
	if !strings.Contains(string(sessionBytes), "\"project_key\": \""+testProjectKey+"\"") {
		t.Fatalf("session = %q, want alpha project selection", string(sessionBytes))
	}
}

func TestRunTransitionSetUsesSelectedProject(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	if err := Run(context.Background(), root, []string{"project", "select", testProjectKey}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(project select) error = %v", err)
	}

	var stdout bytes.Buffer
	err := Run(
		context.Background(),
		root,
		[]string{"transition", "set", "cutover", "confirm", "because", "cli smoke"},
		strings.NewReader(""),
		&stdout,
	)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "project="+testProjectKey) || !strings.Contains(output, "state=cutover") {
		t.Fatalf("stdout = %q, want transition status for alpha cutover", output)
	}
}

func TestRunTaskCreateJSON(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	var stdout bytes.Buffer

	err := Run(
		context.Background(),
		root,
		[]string{"task", "create", "--project", testProjectKey, "--title", "cutover smoke"},
		strings.NewReader(""),
		&stdout,
	)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var payload struct {
		ID     int64  `json:"id"`
		Key    string `json:"key"`
		Status string `json:"status"`
		Scope  string `json:"scope"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("task create json = %v", err)
	}
	if payload.Status != "queued" {
		t.Fatalf("Status = %q, want queued", payload.Status)
	}
	if payload.Scope != "project" {
		t.Fatalf("Scope = %q, want project", payload.Scope)
	}
	if payload.ID == 0 || payload.Key == "" {
		t.Fatalf("payload = %+v, want populated task identity", payload)
	}
}

func TestRunTaskRunJSON(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	cleanupTaskRunWorktree(t, testProjectKey)
	if err := Run(context.Background(), root, []string{"project", "select", testProjectKey}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(project select) error = %v", err)
	}
	if err := Run(
		context.Background(),
		root,
		[]string{"transition", "set", "cutover", "confirm", "because", "allow cli run"},
		strings.NewReader(""),
		&bytes.Buffer{},
	); err != nil {
		t.Fatalf("Run(transition set) error = %v", err)
	}

	var stdout bytes.Buffer
	err := Run(
		context.Background(),
		root,
		[]string{"task", "run", "--project", testProjectKey, "--title", "run from cli", "--json"},
		strings.NewReader(""),
		&stdout,
	)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var payload struct {
		Task struct {
			Status string `json:"status"`
		} `json:"task"`
		Run struct {
			Status string `json:"status"`
		} `json:"run"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("task run json = %v", err)
	}
	if payload.Task.Status != "completed" {
		t.Fatalf("Task.Status = %q, want completed", payload.Task.Status)
	}
	if payload.Run.Status != "completed" {
		t.Fatalf("Run.Status = %q, want completed", payload.Run.Status)
	}
}

func testRepoRoot(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	mustMkdirAll := func(path string) {
		t.Helper()
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
	mustWriteFile := func(path string, contents string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	mustMkdirAll(filepath.Join(root, "config"))
	mustMkdirAll(filepath.Join(root, "data"))
	mustMkdirAll(filepath.Join(root, "registry"))
	mustMkdirAll(filepath.Join(root, "state", "cache"))
	mustMkdirAll(filepath.Join(root, "alpha"))

	mustWriteFile(filepath.Join(root, "config", "projects.yaml"), `
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
	mustWriteFile(filepath.Join(root, "config", "executors.yaml"), `
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
`)
	mustWriteFile(filepath.Join(root, "config", "odin.yaml"), `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:9443
  startup_recovery: true
`)

	mustWriteFile(filepath.Join(root, "README.md"), "alpha test repo\n")
	mustWriteFile(filepath.Join(root, "alpha", "README.md"), "alpha nested repo\n")
	runGitIn := func(dir string, args ...string) {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = dir
		command.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test User",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test User",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}

	runGitIn(root, "init", "-b", "main")
	runGitIn(root, "add", ".")
	runGitIn(root, "commit", "-m", "test fixture")

	runGitIn(filepath.Join(root, "alpha"), "init", "-b", "main")
	runGitIn(filepath.Join(root, "alpha"), "add", ".")
	runGitIn(filepath.Join(root, "alpha"), "commit", "-m", "alpha fixture")

	return root
}

func cleanupTaskRunWorktree(t *testing.T, projectKey string) {
	t.Helper()

	path := worktrees.ResolvePath(worktrees.PathParams{
		Root:       worktrees.DefaultRoot(),
		ProjectKey: projectKey,
		TaskID:     1,
		RunID:      1,
		Try:        1,
	})
	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("RemoveAll(%s) error = %v", path, err)
	}
}
