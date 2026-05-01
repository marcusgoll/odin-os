package commands

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/core/projects"
	"odin-os/internal/registry"
)

func TestRunWorkWorkerDryRunConstructsSafeLocalWorkerPlan(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	repoRoot := initWorkerDryRunGitRepo(t)
	projectRegistry := workerDryRunProjectRegistry(t, repoRoot)
	worktreeRoot := filepath.Join(t.TempDir(), "worktrees")
	fixturePath := filepath.Join(t.TempDir(), "issue.json")
	if err := os.WriteFile(fixturePath, []byte(`{"number":456,"title":"Build the safe worker proof","body":"Use existing Odin surfaces."}`), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	t.Setenv("ODIN_WORKTREE_ROOT", worktreeRoot)
	t.Setenv("GITHUB_TOKEN", "github_pat_1234567890abcdefghijklmnopqrstuvwxyz")
	t.Setenv("GH_TOKEN", "ghp_1234567890abcdefghijklmnopqrst")
	t.Setenv("API_TOKEN", "api-secret")
	t.Setenv("ODIN_TRADEBOARD_API_TOKEN", "tradeboard-secret")

	var output strings.Builder
	err := RunWork(ctx, store, projectRegistry, registry.Snapshot{}, []string{
		"worker-dry-run",
		"--issue-fixture", fixturePath,
		"--json",
	}, &output)
	if err != nil {
		t.Fatalf("RunWork(worker-dry-run) error = %v", err)
	}
	for _, secret := range []string{
		"github_pat_1234567890abcdefghijklmnopqrstuvwxyz",
		"ghp_1234567890abcdefghijklmnopqrst",
		"api-secret",
		"tradeboard-secret",
	} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("output leaked secret %q:\n%s", secret, output.String())
		}
	}

	var report struct {
		Project string `json:"project"`
		Issue   struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
		} `json:"issue"`
		Worktree struct {
			Root        string `json:"root"`
			Path        string `json:"path"`
			Branch      string `json:"branch"`
			Created     bool   `json:"created"`
			InsideRoot  bool   `json:"inside_root"`
			CleanedUp   bool   `json:"cleaned_up"`
			Kept        bool   `json:"kept"`
			LeaseStored bool   `json:"lease_stored"`
		} `json:"worktree"`
		Prompt     string          `json:"prompt"`
		Guardrails map[string]bool `json:"guardrails"`
		Command    struct {
			Executable string   `json:"executable"`
			Args       []string `json:"args"`
			Redacted   string   `json:"redacted"`
			Launched   bool     `json:"launched"`
		} `json:"codex_command"`
		Environment struct {
			Excluded      []string `json:"excluded"`
			TokenExposure bool     `json:"token_exposure"`
		} `json:"environment"`
		Timeout struct {
			Simulated bool   `json:"simulated"`
			Result    string `json:"result"`
		} `json:"timeout"`
		FinalOutput    string `json:"final_output"`
		GitHubWrites   int    `json:"github_writes"`
		PRs            string `json:"prs"`
		Dispatch       string `json:"dispatch"`
		CodexExecution string `json:"codex_execution"`
	}
	if err := json.Unmarshal([]byte(output.String()), &report); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\noutput:\n%s", err, output.String())
	}
	if report.Project != "odin-core" || report.Issue.Number != 456 || report.Issue.Title == "" {
		t.Fatalf("target = project %q issue %+v, want odin-core issue fixture", report.Project, report.Issue)
	}
	if report.Worktree.Root != worktreeRoot || report.Worktree.Path == "" || !report.Worktree.Created || !report.Worktree.InsideRoot || !report.Worktree.CleanedUp || report.Worktree.Kept || report.Worktree.LeaseStored {
		t.Fatalf("worktree = %+v, want created under root then cleaned without lease", report.Worktree)
	}
	if _, err := os.Stat(report.Worktree.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree path still exists or stat failed unexpectedly: path=%s err=%v", report.Worktree.Path, err)
	}
	for _, key := range []string{
		"audit_existing_repo",
		"reuse_existing_odin_primitives",
		"no_parallel_surfaces",
		"canonical_odin_work_surface",
		"no_tokens_or_secrets",
		"no_pr_creation",
		"run_make_odin_e2e_local",
	} {
		if !report.Guardrails[key] {
			t.Fatalf("guardrail %q = false in %+v", key, report.Guardrails)
		}
	}
	for _, fragment := range []string{
		"Audit the existing repo before editing.",
		"Reuse existing Odin commands, services, contracts, registries, schemas, docs, and tests.",
		"Do not create parallel command surfaces, registries, or sidecar tools.",
		"Use odin work ... as the canonical Delivery Workflow operator surface.",
		"Do not expose tokens or secrets.",
		"Do not create or update a pull request.",
		"make odin-e2e-local",
	} {
		if !strings.Contains(report.Prompt, fragment) {
			t.Fatalf("prompt missing %q:\n%s", fragment, report.Prompt)
		}
	}
	if report.Command.Executable != "codex" || !containsString(report.Command.Args, "exec") || !strings.Contains(report.Command.Redacted, "codex exec -C "+report.Worktree.Path) || report.Command.Launched {
		t.Fatalf("codex command = %+v, want constructed dry-run command only", report.Command)
	}
	if report.Environment.TokenExposure {
		t.Fatalf("environment token_exposure = true")
	}
	for _, excluded := range []string{"GITHUB_TOKEN", "GH_TOKEN", "API_TOKEN", "ODIN_TRADEBOARD_API_TOKEN"} {
		if !containsString(report.Environment.Excluded, excluded) {
			t.Fatalf("environment excluded = %#v, want %s", report.Environment.Excluded, excluded)
		}
	}
	if !report.Timeout.Simulated || !strings.Contains(report.Timeout.Result, "timed out") {
		t.Fatalf("timeout = %+v, want simulated timeout result", report.Timeout)
	}
	if !strings.Contains(report.FinalOutput, "make odin-e2e-local") {
		t.Fatalf("final_output = %q, want make odin-e2e-local", report.FinalOutput)
	}
	if report.GitHubWrites != 0 || report.PRs != "not_created" || report.Dispatch != "not_started" || report.CodexExecution != "not_started" {
		t.Fatalf("side effects = writes %d prs %q dispatch %q codex %q", report.GitHubWrites, report.PRs, report.Dispatch, report.CodexExecution)
	}
	for _, table := range []string{"external_issues", "tasks", "runs", "approvals", "worktree_leases"} {
		var count int
		if err := store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s count = %d, want no durable worker dry-run state", table, count)
		}
	}
}

func initWorkerDryRunGitRepo(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	runWorkerDryRunGit(t, root, "init", "-b", "main")
	runWorkerDryRunGit(t, root, "config", "user.email", "odin@example.invalid")
	runWorkerDryRunGit(t, root, "config", "user.name", "Odin Test")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Test\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runWorkerDryRunGit(t, root, "add", "README.md")
	runWorkerDryRunGit(t, root, "commit", "-m", "initial")
	return root
}

func runWorkerDryRunGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(output))
	}
}

func workerDryRunProjectRegistry(t *testing.T, repoRoot string) projects.Registry {
	t.Helper()

	path := filepath.Join(t.TempDir(), "projects.yaml")
	if err := os.WriteFile(path, []byte(`
version: 1
projects:
  - key: odin-core
    name: Odin Core
    project_class: system_project
    system_project: true
    git_root: `+repoRoot+`
    default_branch: main
    github:
      repo: marcusgoll/odin-os
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
		t.Fatalf("write projects: %v", err)
	}
	registry, diagnostics, err := projects.Register(path)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v, want none", diagnostics)
	}
	return registry
}
