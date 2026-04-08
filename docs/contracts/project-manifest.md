---
title: Project Manifest Contract
status: active
date: 2026-04-08
phase: "04"
---

# Project Manifest Contract

`config/projects.yaml` is the authored enrollment contract for managed projects.

Each manifest entry defines one managed project and its governance policy. The file is operator-authored, reviewed in Git, and validated before Odin treats a project as enrolled.

## Top-level shape

```yaml
version: 1
projects:
  - key: odin-core
    name: Odin Core
    project_class: system_project
    system_project: true
    git_root: ../repo
    default_branch: main
    github:
      repo: owner/name
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
```

## Required fields

- `version`
- `projects`
- `projects[].key`
- `projects[].name`
- `projects[].project_class`
- `projects[].git_root`
- `projects[].default_branch`
- `projects[].policy.allowed_commands`
- `projects[].policy.branch_rules`
- `projects[].policy.approval_gates`
- `projects[].policy.merge_policy`
- `projects[].policy.destructive_operations`

`git_root` may be absolute or relative to the directory that contains `config/projects.yaml`.

## Supported project classes

- `local_git_project`
- `github_backed_project`
- `system_project`

## Validation rules

- Every manifest must point at a Git repository.
- `github_backed_project` must declare `github.repo`.
- `system_project` is reserved for `odin-core` and must set `system_project: true`.
- `system_project` must require worktrees and task-owned branches.
- `system_project` must reject direct mutation of its default branch.
- Destructive operations must declare whether they are allowed and whether explicit approval is required.
- Duplicate project keys are invalid.

## Policy fields

### `allowed_commands`

The explicit command names Odin may run inside the project scope.

### `branch_rules`

- `protected_branches`
- `require_worktree`
- `require_task_branch`
- `allow_default_branch_mutation`

### `approval_gates`

- `require_for_governance_changes`
- `require_for_destructive_operations`
- `require_for_system_project_changes`

### `merge_policy`

- `mode`
- `allow_direct_to_default_branch`

### `destructive_operations`

- `allow_reset`
- `allow_clean`
- `allow_force_push`
- `require_explicit_approval`
