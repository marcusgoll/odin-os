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
    scheduler:
      max_concurrent_runs: 2
      max_starts_per_cycle: 3
      stalled_run_retry_limit: 4
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
	if cfg.Projects[0].Scheduler.MaxConcurrentRuns != 2 {
		t.Fatalf("alpha max concurrent runs = %d, want 2", cfg.Projects[0].Scheduler.MaxConcurrentRuns)
	}
	if cfg.Projects[0].Scheduler.MaxStartsPerCycle != 3 {
		t.Fatalf("alpha max starts per cycle = %d, want 3", cfg.Projects[0].Scheduler.MaxStartsPerCycle)
	}
	if cfg.Projects[0].Scheduler.StalledRunRetryLimit != 4 {
		t.Fatalf("alpha stalled retry limit = %d, want 4", cfg.Projects[0].Scheduler.StalledRunRetryLimit)
	}
	if !cfg.Projects[1].SystemProject {
		t.Fatalf("expected odin-core to be marked as system project")
	}
	if cfg.Projects[1].GitRoot != root {
		t.Fatalf("odin git root = %q, want %q", cfg.Projects[1].GitRoot, root)
	}
}

func TestLoadManifestFileParsesCutoverPilotProjects(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir repo git root: %v", err)
	}

	configPath := filepath.Join(root, "projects.yaml")
	if err := os.WriteFile(configPath, []byte(`
version: 1
projects:
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

	cfg, err := LoadManifestFile(configPath)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}

	pilot, ok := cfg.Cutover.PilotProject("pbs")
	if !ok {
		t.Fatal("expected pbs pilot lookup")
	}
	if pilot.Key != "pbs" {
		t.Fatalf("pilot key = %q, want pbs", pilot.Key)
	}
	if pilot.RuntimeOwner != "odin_os" {
		t.Fatalf("runtime owner = %q, want odin_os", pilot.RuntimeOwner)
	}
	if pilot.PrimaryController != "odin_os" {
		t.Fatalf("primary controller = %q, want odin_os", pilot.PrimaryController)
	}
	if pilot.ComparisonContext != "odin-orchestrator" {
		t.Fatalf("comparison context = %q, want odin-orchestrator", pilot.ComparisonContext)
	}
}
