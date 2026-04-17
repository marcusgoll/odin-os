# Project Overlays

Canonical project config stays in `config/projects.yaml` and must remain portable.

Machine-local managed projects belong in an overlay, not the canonical manifest.

## Supported overlay sources

Odin loads project manifests in this order:

1. `config/projects.yaml`
2. `ODIN_PROJECTS_OVERLAY` when set
3. `config/projects.local.yaml` when present

Overlay projects are appended to the canonical manifest set and validated together.

## When to use overlays

Use an overlay for:

- local homelab repos that do not exist on every machine
- shadow-only project supervision on one operator host
- temporary portfolio experiments that should not become canonical repo config

Do not put machine-specific absolute repo paths into `config/projects.yaml`.

## Local file convention

`config/projects.local.yaml` is ignored by git and can be used for one machine’s managed projects.

Example shape:

```yaml
version: 1
projects:
  - key: family-ops
    name: Family Ops
    project_class: github_backed_project
    git_root: /home/orchestrator/family-ops
    default_branch: main
    github:
      repo: marcusgoll/family-ops
    policy:
      allowed_commands: [status, test]
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
```

## Safety notes

- overlays do not broaden mutation authority by themselves
- `shadow` and `compare` remain read-only
- invalid overlay entries will surface as registry diagnostics during bootstrap
