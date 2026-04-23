package projects

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegisterBuildsLookupAndSystemProjectView(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	odinRoot := filepath.Join(root, "odin")
	projectRoot := filepath.Join(root, "alpha")

	for _, dir := range []string{odinRoot, projectRoot} {
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
			t.Fatalf("mkdir git root: %v", err)
		}
	}

	configPath := filepath.Join(root, "projects.yaml")
	if err := os.WriteFile(configPath, []byte(`
version: 1
projects:
  - key: odin-core
    name: Odin Core
    project_class: system_project
    system_project: true
    git_root: `+odinRoot+`
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
  - key: alpha
    name: Alpha
    project_class: github_backed_project
    git_root: `+projectRoot+`
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
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	registry, diagnostics, err := Register(configPath)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", diagnostics)
	}

	alpha, ok := registry.Lookup("alpha")
	if !ok {
		t.Fatalf("expected alpha lookup")
	}
	if alpha.ProjectClass != ProjectClassGitHubBacked {
		t.Fatalf("alpha class = %q, want %q", alpha.ProjectClass, ProjectClassGitHubBacked)
	}

	systemProject, ok := registry.SystemProject()
	if !ok {
		t.Fatalf("expected system project")
	}
	if systemProject.Key != "odin-core" {
		t.Fatalf("system project key = %q, want odin-core", systemProject.Key)
	}
}

func TestUpdateProjectMutatesExistingManifest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	projectRoot := filepath.Join(root, "alpha")
	if err := os.MkdirAll(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir git root: %v", err)
	}

	configPath := filepath.Join(root, "projects.yaml")
	if err := os.WriteFile(configPath, []byte(`
version: 1
projects:
  - key: alpha
    name: Alpha
    project_class: local_git_project
    git_root: `+projectRoot+`
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
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	registry, diagnostics, err := UpdateProject(configPath, "alpha", func(manifest *Manifest) error {
		manifest.Name = "Alpha Renamed"
		manifest.DefaultBranch = "develop"
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateProject() error = %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", diagnostics)
	}

	alpha, ok := registry.Lookup("alpha")
	if !ok {
		t.Fatalf("expected alpha lookup")
	}
	if alpha.Name != "Alpha Renamed" {
		t.Fatalf("Name = %q, want Alpha Renamed", alpha.Name)
	}
	if alpha.DefaultBranch != "develop" {
		t.Fatalf("DefaultBranch = %q, want develop", alpha.DefaultBranch)
	}
}

func TestRegisterReturnsRegistryAlongsideDiagnostics(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	projectRoot := filepath.Join(root, "alpha")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("mkdir project root: %v", err)
	}

	configPath := filepath.Join(root, "projects.yaml")
	if err := os.WriteFile(configPath, []byte(`
version: 1
projects:
  - key: alpha
    name: Alpha
    project_class: local_git_project
    git_root: `+projectRoot+`
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
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	registry, diagnostics, err := Register(configPath)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if len(diagnostics) == 0 {
		t.Fatalf("expected diagnostics for missing .git")
	}

	alpha, ok := registry.Lookup("alpha")
	if !ok {
		t.Fatalf("expected alpha lookup despite diagnostics")
	}
	if alpha.GitRoot != projectRoot {
		t.Fatalf("GitRoot = %q, want %q", alpha.GitRoot, projectRoot)
	}
}
