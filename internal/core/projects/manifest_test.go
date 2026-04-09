package projects

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadManifestFileParsesProjects(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	projectRoot := filepath.Join(root, "alpha")
	if err := os.MkdirAll(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir git root: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir repo git root: %v", err)
	}

	configPath := filepath.Join(root, "projects.yaml")
	if err := os.WriteFile(configPath, []byte(`
version: 1
projects:
  - key: alpha
    name: Alpha
    project_class: local_git_project
    git_root: alpha
    default_branch: main
    policy:
      allowed_commands:
        - status
      limited_actions:
        docs_audit_note:
          description: Create an additive audit note under docs/audits
          path_prefixes:
            - docs/audits/
          content_mode: create_markdown_note
        docs_update:
          description: Append a bounded note to an existing docs file
          path_prefixes:
            - docs/
          target_path: docs/plans/2026-03-27-aviation-tooling-audit-report.md
          content_mode: append_markdown_note
      branch_rules:
        protected_branches:
          - main
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
    git_root: .
    default_branch: main
    policy:
      allowed_commands:
        - status
      branch_rules:
        protected_branches:
          - main
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
		t.Fatalf("write manifest: %v", err)
	}

	cfg, err := LoadManifestFile(configPath)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}

	if cfg.Version != 1 {
		t.Fatalf("version = %d, want 1", cfg.Version)
	}
	if len(cfg.Projects) != 2 {
		t.Fatalf("project count = %d, want 2", len(cfg.Projects))
	}
	if cfg.Projects[0].ProjectClass != ProjectClassLocalGit {
		t.Fatalf("project class = %q, want %q", cfg.Projects[0].ProjectClass, ProjectClassLocalGit)
	}
	if cfg.Projects[0].GitRoot != projectRoot {
		t.Fatalf("alpha git root = %q, want %q", cfg.Projects[0].GitRoot, projectRoot)
	}
	if cfg.Projects[0].Policy.LimitedActions["docs_audit_note"].ContentMode != "create_markdown_note" {
		t.Fatalf("docs_audit_note content mode = %q, want create_markdown_note", cfg.Projects[0].Policy.LimitedActions["docs_audit_note"].ContentMode)
	}
	if cfg.Projects[0].Policy.LimitedActions["docs_update"].TargetPath != "docs/plans/2026-03-27-aviation-tooling-audit-report.md" {
		t.Fatalf("docs_update target path = %q, want configured path", cfg.Projects[0].Policy.LimitedActions["docs_update"].TargetPath)
	}
	if !cfg.Projects[1].SystemProject {
		t.Fatalf("expected odin-core to be marked as system project")
	}
	if cfg.Projects[1].GitRoot != root {
		t.Fatalf("odin git root = %q, want %q", cfg.Projects[1].GitRoot, root)
	}
}
