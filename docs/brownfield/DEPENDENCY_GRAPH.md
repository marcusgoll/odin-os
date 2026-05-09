---
title: Odin OS Dependency Graph
status: draft
date: 2026-04-30
---

# Odin OS Dependency Graph

## Current Runtime Graph

```text
cmd/odin
  -> internal/app/lifecycle
    -> internal/app/config
    -> internal/app/bootstrap
      -> internal/store/sqlite
        -> embedded migrations
      -> internal/registry/loader
        -> parser
        -> validator
        -> compiler
      -> internal/core/projects
      -> internal/executors/router
        -> internal/executors/contract
        -> adapter catalog
    -> internal/cli
      -> commands
      -> repl
      -> scope
      -> state
    -> internal/runtime/jobs
      -> internal/store/sqlite
      -> internal/core/projects
      -> internal/executors/router
      -> internal/vcs/leases
        -> internal/vcs/branches
        -> internal/vcs/worktrees
        -> internal/vcs/git
    -> internal/runtime/recovery
      -> internal/store/sqlite
      -> internal/runtime/health
      -> internal/executors/contract
    -> internal/api/http
      -> internal/runtime/health
      -> internal/telemetry/metrics
        -> internal/runtime/projections
    -> internal/telemetry/logs
```

## Target Agency Graph

```text
odin daemon
  -> lifecycle/bootstrap
    -> runtime config
      -> dry-run
      -> kill switch
    -> scheduler / agency loop
      -> GitHub intake adapter
      -> SQLite Work Item state
      -> registry + prompt renderer
      -> worktree lease manager
      -> executor router
        -> codex_exec adapter
        -> app_server adapter (future)
      -> QA/reviewer/security role attempts
      -> PR manager
      -> approval manager
    -> dashboard API
    -> recovery
    -> metrics/logging
```

## Dependency Order For Migration

```text
1. Authority cleanup
   -> binary decision
   -> duplicate scaffold cleanup
   -> role vocabulary decision

2. Runtime safety controls
   -> dry-run config
   -> kill switch config
   -> executor security policy

3. Intake
   -> GitHub adapter seam
   -> read-only issue query
   -> SQLite external issue identity
   -> Work Item projection

4. Dispatch
   -> worktree lease
   -> executor routing
   -> deterministic execution proof
   -> real codex_exec adapter

5. Role automation
   -> prompt contract
   -> prompt renderer
   -> builder role
   -> QA role
   -> reviewer role
   -> security role

6. Handoff
   -> PR manager
   -> review state
   -> human approval gates
   -> no autonomous merge/deploy proof

7. Operator visibility
   -> dashboard projections
   -> dashboard HTTP endpoints
   -> optional tmux status
   -> deployment hardening
```

## High-Risk Dependency Edges

| Edge | Risk | Required isolation |
| --- | --- | --- |
| GitHub adapter -> issue mutation | Token misuse and unintended tracker writes. | Read-only adapter first; separate write/label/comment ticket. |
| Executor router -> real `codex exec` | Unsafe subprocess, root worker, forbidden sandbox mode, secrets exposure. | Security contract and policy enforcement before adapter. |
| Runtime jobs -> worktree leases | Wrong repo/default branch mutation. | Reuse `internal/vcs`; add characterization tests. |
| Prompt renderer -> worker launch | Prompt-only safety mistaken for enforcement. | Keep enforcement in executor/security modules. |
| PR manager -> merge/deploy | Autonomous merge/deploy violation. | Draft PR only; human approval invariant. |
| Dashboard API -> mutation | Dashboard becomes second control plane. | Read-only projections first. |
| systemd -> production runtime | Broad host access and secret leakage. | Separate deployment hardening review. |

## Preserve-First Dependency Rules

- New agency scheduler logic must depend on `internal/runtime/jobs`, not bypass it.
- New worker runners must depend on `internal/executors/contract`, not `internal/runner`.
- New workspace logic must depend on `internal/vcs`, not `internal/workspace`.
- New GitHub issue/PR tracker logic must land in `internal/tracker`, not
  `internal/adapters/github`.
- New dashboard output must depend on `internal/runtime/projections` and SQLite read models, not query GitHub or worker processes directly.
- New security checks must execute before subprocess launch, not only appear in prompts.

## Suggested Issue Dependency Graph

```text
Issue 1: Binary decision
  -> Issue 2: Scaffold cleanup
  -> Issue 3: Config root cleanup
  -> Issue 4: Role vocabulary

Issue 4: Role vocabulary
  -> Issue 5: Prompt contract
  -> Issue 6: Prompt renderer
  -> Issue 15: Builder role
  -> Issue 16: QA role
  -> Issue 17: Reviewer role
  -> Issue 18: Security role

Issue 7: Runtime dry-run + kill switch
  -> Issue 8: GitHub read-only intake
  -> Issue 12: Codex exec security contract

Issue 8: GitHub read-only intake
  -> Issue 9: External issue persistence
  -> Issue 10: Issue-to-work-item dry run

Issue 11: Worktree dispatch with deterministic executor
  depends on Issue 9
  depends on existing internal/vcs
  -> Issue 13: Real codex_exec adapter

Issue 12: Codex exec security contract
  -> Issue 13: Real codex_exec adapter

Issue 13: Real codex_exec adapter
  -> Issue 15: Builder role
  -> Issue 19: PR manager

Issue 15: Builder role
  -> Issue 16: QA role
  -> Issue 17: Reviewer role
  -> Issue 18: Security role

Issue 16 + Issue 17 + Issue 18
  -> Issue 19: PR manager
  -> Issue 20: PR handoff persistence
  -> Issue 21: Human approval enforcement

Issue 20: PR handoff persistence
  -> Issue 22: Dashboard projections
  -> Issue 23: Dashboard HTTP endpoints

Issue 23: Dashboard HTTP endpoints
  -> Issue 24: Optional tmux status
  -> Issue 25: Deployment hardening
```

## Stop Conditions

Stop the migration and re-audit if a proposed ticket:

- launches a worker outside `internal/executors`
- writes to GitHub before read-only intake is proven
- creates a second worktree manager
- creates a second SQLite/state authority
- adds autonomous merge or production deploy
- exposes production secrets to prompts or workers
- removes registry assets without preserving useful behavior
