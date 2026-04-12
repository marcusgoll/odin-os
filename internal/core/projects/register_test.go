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
	pbsRoot := filepath.Join(root, "pbs")
	projectRoot := filepath.Join(root, "alpha")

	for _, dir := range []string{odinRoot, pbsRoot, projectRoot} {
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
      limited_actions:
        docs_audit_note:
          description: Create an additive audit note under docs/audits
          path_prefixes: [docs/audits/]
          content_mode: create_markdown_note
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
      limited_actions:
        docs_audit_note:
          description: Create an additive audit note under docs/audits
          path_prefixes: [docs/audits/]
          content_mode: create_markdown_note
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
  - key: pbs
    name: PBS
    project_class: local_git_project
    git_root: `+pbsRoot+`
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
cutover:
  pilot_projects:
    - key: pbs
      runtime_owner: odin_os
      primary_controller: odin_os
      comparison_context: odin-orchestrator
      legacy_primary_required: false
      shadow_graduation:
        - legacy and Odin readouts agree on project scope and ownership
      limited_action_graduation:
        - allowlisted isolated mutations complete successfully under Odin ownership
      cutover_graduation:
        - routine queued work completes under Odin OS ownership
      legacy_duties_to_retire_in_order:
        - read-only observation and compare reporting
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

	pilot, ok := registry.CutoverPilotProject("pbs")
	if !ok {
		t.Fatal("expected pbs cutover pilot lookup")
	}
	if pilot.PrimaryController != "odin_os" {
		t.Fatalf("pilot primary controller = %q, want odin_os", pilot.PrimaryController)
	}
}
