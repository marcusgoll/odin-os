---
title: Odin OS Architecture Gap Analysis
status: draft
date: 2026-04-30
---

# Odin OS Architecture Gap Analysis

## Scope

This analysis compares the current brownfield `odin-os` checkout to the target Odin-OS agency orchestrator architecture. It treats the active checkout as evidence, but distinguishes established Odin modules from uncommitted scaffold modules and TypeScript leftovers.

`.worktrees/` and `node_modules/` are excluded from the active architecture. Legacy `odin-orchestrator` artifacts are migration evidence only.

## Current Architecture Diagram

```text
cmd/odin
cmd/odin-os (duplicate worktree-only entrypoint)
  -> internal/app/lifecycle
    -> internal/app/config
    -> internal/app/bootstrap
      -> internal/store/sqlite
      -> internal/registry loader/compiler
      -> internal/core/projects registry
      -> internal/executors/router + adapter catalog
    -> internal/cli commands + REPL
    -> internal/runtime/jobs
      -> internal/core/projects transition authorization
      -> internal/vcs/leases + internal/vcs/worktrees + git adapter
      -> internal/executors/contract
      -> internal/executors/codex deterministic alpha adapter
      -> internal/store/sqlite task/run/events state
    -> internal/runtime/recovery
    -> internal/api/http operational handler
      -> /healthz
      -> /readyz
      -> /metrics
    -> internal/telemetry/logs structured JSON logger
```

Current authored inputs:

```text
config/
registry/
prompts/ (draft scaffold prompts)
docs/contracts/
docs/adr/
docs/architecture/
```

Current duplicate/scaffold modules:

```text
internal/runner        duplicates internal/executors
internal/workspace     duplicates internal/vcs
internal/tracker       placeholder GitHub intake seam
internal/orchestrator  placeholder agency coordinator
internal/prompts       placeholder renderer interface
internal/review        placeholder reviewer interface
internal/security      detached worker-policy check
src/                   TypeScript scaffold, not Go-native target
configs/               duplicate config root
```

## Target Architecture Diagram

```text
systemd or Docker
  -> odin daemon (cmd/odin serve, or documented service alias)
    -> lifecycle composition
      -> scheduler / agency loop
        -> GitHub Issues intake adapter
        -> SQLite runtime state
        -> agent/skill/prompt registry
        -> worktree workspace manager
        -> Codex exec executor adapter
        -> optional Codex app-server executor adapter
        -> reviewer / QA / security automation
        -> PR manager
        -> human approval gates
      -> dashboard API
      -> optional tmux status surface
      -> structured logs, metrics, recovery
```

Target execution path:

```text
eligible GitHub Issue
  -> intake projection
  -> Work Item in SQLite
  -> one Run Attempt
  -> one role prompt
  -> one git worktree lease
  -> codex exec in safe mode
  -> verification/review/security runs
  -> draft PR or handoff
  -> human approval before merge or deploy
```

## Component Gap Analysis

| Target module | Current evidence | Gap | Recommendation | Risk |
| --- | --- | --- | --- | --- |
| Go daemon | `cmd/odin`, `internal/app/lifecycle.Run`, `runServe`, systemd unit. `cmd/odin-os` exists as duplicate worktree-only entrypoint. | Daemon exists but agency loop is not complete; binary naming is unresolved. | Preserve `cmd/odin` and lifecycle. Decide whether `cmd/odin-os` is a supported alias. | Medium |
| GitHub Issues adapter | `internal/tracker/github/client.go` placeholder; `internal/adapters/github/.gitkeep`; `config/projects.yaml` supports GitHub repo metadata. | No real GitHub issue query, labels, comments, or token policy. Duplicate adapter root unresolved. | Pick one GitHub intake seam and start read-only. | High |
| SQLite runtime state | `internal/store/sqlite`, embedded migrations, task/run/approval/event/action/lease/recovery/knowledge tables. | Strong existing authority; store is large and mixes many domains. | Preserve. Split by domain files only after characterization tests. | Medium |
| Git worktree workspace manager | `internal/vcs/leases`, `internal/vcs/worktrees`, `internal/vcs/git`, `docs/contracts/git-worktrees.md`. | Real worktree lease path exists; `internal/workspace` duplicates it. Cleanup/recovery exists but agency-facing workspace commands are partial. | Use `internal/vcs` as canonical. Remove/merge `internal/workspace`. | High |
| Codex exec runner | `internal/executors/contract`, router, `codex_headless` deterministic adapter; `internal/runner/codexexec` placeholder. | No real `codex exec` subprocess. Security policy not enforced in canonical executor path. | Implement `codex_exec` as an `internal/executors` adapter after security contract. | High |
| Optional app-server runner | `internal/runner/appserver` placeholder. | Not behind canonical executor seam; app-server is experimental. | Defer. Add only as an `internal/executors` adapter after Codex exec works. | Medium |
| Prompt renderer | Prompt files in `prompts/`; `internal/prompts/renderer.go`; historical TypeScript prompt renderer inventory for removed `src/prompts`. | No prompt frontmatter contract. Duplicate builder prompts remain. | Define prompt contract, choose one prompt layout, wire through Go renderer. Do not recreate the removed TypeScript renderer. | Medium |
| Agent/skill registry | `registry/`, `internal/registry`, `docs/contracts/registry-format.md`, active triage agent/skill. | Strong existing registry. Builder/QA/reviewer/security agents missing. Registry watcher is noop. | Preserve registry. Add missing agents only after role vocabulary is settled. | Medium |
| PR manager | PR template validator and GitHub Actions CI exist; no runtime PR creation/update module. | No draft PR creation, branch push, review handoff, or merge-blocking PR manager. | Add after GitHub intake and worktree execution are proven. Human approval remains required. | High |
| Reviewer/QA/security automation | Executor task kinds include review/QA/research; prompt drafts; `internal/workers` placeholders; `internal/review` and `internal/security` scaffold. | Planner exists; reviewer/QA/security execution is not implemented. Security policy is detached from executor launch. | Implement one role at a time through runtime jobs and executor contract. | High |
| Dashboard API | `internal/api/http/operational.go` exposes `/healthz`, `/readyz`, `/metrics`; `internal/dashboard` scaffold status struct. | Operational API exists, but no agency dashboard endpoints for issues, work items, runs, PRs, workers, approvals. | Extend existing HTTP handler over projections; do not create a second dashboard stack. | Medium |
| Optional tmux status surface | Docs mention live execution sessions and tmux; no active module. | No tmux probe/adopt/status commands in current source. | Keep optional. Add as read-only projection over Run Attempts, not durable authority. | Medium |
| systemd deployment | `deploy/systemd/odin.service`, `odin.env.example`, install script. | Exists but lacks hardening; working directory/env naming may lag live service names. | Preserve and harden in separate security-reviewed ticket. | Medium |
| Docker deployment | No Docker Compose file in active repo. | Missing. | Defer unless deployment choice is made. Do not add until daemon contract stabilizes. | Low |
| Human approval before merge/deploy | Project manifest policy, approvals table, action-bound approvals, workflow docs, PR template. | Existing approval substrate is useful but no PR/deploy workflow manager enforces merge/deploy approvals end to end. | Reuse approvals/action evidence; add PR/deploy gates later. | High |
| Dry-run / kill switch | Scaffold config has fields; target docs mention hard constraints. | Not wired into lifecycle, scheduler, runner, or GitHub adapter. | Add as runtime config and surface in readiness before worker launch. | High |
| Structured logs | `internal/telemetry/logs` JSON logger; `serve` writes `runs/logs/odin-service.log`. | Exists for service loops; not consistently threaded through all runtime modules. | Preserve and propagate through new agency modules. | Medium |

## Current To Target Summary

The current system is already a governed Go control plane with deep modules for state, registry, executor routing, worktree leases, recovery, health, and operator CLI. The target architecture should deepen those modules rather than adding a sidecar orchestrator.

The largest gaps are:

1. GitHub issue intake and PR management.
2. Real Codex CLI execution.
3. Prompt rendering and role vocabulary.
4. Review/QA/security role execution.
5. Agency dashboard projections.
6. Dry-run and kill-switch enforcement.

## Existing Modules To Preserve

- `cmd/odin`
- `internal/app/lifecycle`
- `internal/app/bootstrap`
- `internal/store/sqlite`
- `internal/runtime/jobs`
- `internal/runtime/recovery`
- `internal/runtime/projections`
- `internal/runtime/health`
- `internal/executors/contract`
- `internal/executors/router`
- `internal/vcs/leases`
- `internal/vcs/worktrees`
- `internal/vcs/git`
- `internal/registry`
- `registry/`
- `internal/core/projects`
- `internal/telemetry/logs`
- `internal/telemetry/metrics`
- `internal/api/http`
- `scripts/ci/verify-pr-template.sh`
- `deploy/systemd/*`

## Existing Modules To Refactor

- `cmd/odin-os`: decide supported alias vs remove.
- `internal/executors/codex`: evolve from deterministic alpha to real Codex CLI adapter, or add sibling adapter under `internal/executors`.
- `internal/workers`: keep planner, implement other roles through existing runtime/executor seams.
- `prompts/`: choose one layout and add a prompt contract.
- `internal/prompts`: wire only if it becomes the canonical Go prompt renderer.
- `internal/security`: move checks into executor launch policy.
- `internal/api/http`: extend with dashboard/read-model endpoints over existing projections.
- `scripts/dev/install-systemd-service.sh`: harden deployment behavior after service mode decision.

## Existing Modules To Replace

- `internal/runner/*`: replace with `internal/executors` adapters.
- `internal/workspace/manager.go`: replace with `internal/vcs` lease/worktree manager.
- `internal/tracker/*`: replace or move into the single chosen GitHub intake adapter seam.
- `internal/orchestrator/service.go`: replace with lifecycle-composed runtime modules rather than a shallow standalone coordinator.
- `internal/db`, `internal/config`, `internal/logging`, `internal/dashboard`, `internal/review`, `internal/utils`: replace only if their useful concepts are promoted into existing deeper modules.

## Existing Modules To Remove Later

Remove only after explicit cleanup approval and after preserving useful knowledge:

- TypeScript scaffold: `src/`, `package.json`, `package-lock.json`, `tsconfig.json`, `eslint.config.js`, `tests/agency-scaffold.test.ts`.
- Duplicate config root: `configs/`.
- Duplicate agency examples under `config/agency.example.yaml` if not merged into active config.
- Root junk file `--help`.
- Migration drafts that are not core to Odin agency orchestration.

## Migration Phases

### Phase 0: Stabilize Authority

- Freeze `cmd/odin` as current operator path.
- Decide `cmd/odin-os`.
- Remove or quarantine TypeScript scaffold and duplicate config roots.
- Keep all changes docs-only or cleanup-only.

### Phase 1: Runtime Readiness Projection

- Add `odin work readiness` or deepen `odin work status`.
- Surface dry-run, kill-switch, GitHub adapter availability, executor readiness, worktree readiness, and approval readiness.
- No GitHub mutation and no worker launch.

### Phase 2: GitHub Intake Read-Only

- Choose GitHub intake seam.
- Read eligible issues into normalized intake records or Work Items.
- Store external issue identity in SQLite.
- Add dry-run output and structured logs.

### Phase 3: Worktree Dispatch With Deterministic Executor

- Reuse `internal/runtime/jobs`, `internal/vcs/leases`, and `codex_headless`.
- Prove one issue maps to one Work Item, one Run Attempt, and one worktree.
- Keep Codex subprocess disabled.

### Phase 4: Real Codex Exec Adapter

- Add security-reviewed `codex_exec` adapter under `internal/executors`.
- Enforce non-root, no `danger-full-access`, explicit sandbox mode, workspace path, and no production secrets.
- Add cancellation/resume behavior only if supported by the adapter.

### Phase 5: Review / QA / Security Roles

- Settle role vocabulary.
- Add registry agents and prompts one role at a time.
- Run reviewer/QA/security as separate Run Attempts against the same PR/worktree outputs.

### Phase 6: PR Handoff

- Add PR manager after GitHub intake and execution are stable.
- Create draft PRs only from task branches.
- Never merge automatically.
- Record PR identity and review state in SQLite.

### Phase 7: Dashboard API And Operator Surface

- Extend existing HTTP handler over projections for work items, runs, workers, approvals, PRs, and incidents.
- Keep CLI proof authoritative.
- Add optional tmux status as read-only liveness projection.

### Phase 8: Deployment Hardening

- Harden systemd.
- Decide Docker Compose only if it solves an actual deployment need.
- Document live service env and runtime root.

## Risk Ranking

| Rank | Risk | Severity | Isolation strategy |
| --- | --- | --- | --- |
| 1 | Real Codex subprocess runs unsafe mode or sees secrets. | Critical | Dedicated security-reviewed executor ticket before any launch. |
| 2 | GitHub token misuse mutates issues/PRs unexpectedly. | High | Read-only intake first; write actions behind approvals. |
| 3 | Worktree manager mutates default branch or wrong repo. | High | Reuse `internal/vcs` and project manifest policy; characterize before changes. |
| 4 | Duplicate runner/config/workspace seams split safety checks. | High | Collapse duplicates before feature work. |
| 5 | PR manager enables autonomous merge/deploy by accident. | High | Draft PR only; human approval invariant in tests/docs. |
| 6 | Prompt-only safety rules are mistaken for enforcement. | High | Enforce policy in executor/worktree/runtime modules. |
| 7 | Dashboard API becomes a second control plane. | Medium | Read-only projection API first. |
| 8 | systemd service runs with broad host access. | Medium | Separate hardening ticket. |
| 9 | Migration drafts become active assets accidentally. | Medium | Keep `state/migration/drafts` review-only. |
| 10 | Dirty worktree causes unrelated changes to ship. | Medium | Use clean worktree for implementation tickets. |

## Suggested Issue List

1. Decide canonical binary entrypoint: `cmd/odin` only or supported `cmd/odin-os` alias.
2. Remove/quarantine accidental TypeScript scaffold after preserving useful prompt text.
3. Collapse duplicate config roots into `config/`.
4. Collapse `internal/runner` into `internal/executors`.
5. Collapse `internal/workspace` into `internal/vcs`.
6. Choose GitHub intake adapter seam and document token policy.
7. Add `odin work readiness` with dry-run and kill-switch visibility.
8. Add read-only GitHub Issues intake with fixture-backed tests.
9. Persist external issue identity in SQLite without making GitHub runtime authority.
10. Add deterministic issue-to-work-item dry run.
11. Add worktree dispatch proof using `codex_headless`.
12. Write Codex exec security contract.
13. Implement `codex_exec` adapter behind `internal/executors/contract`.
14. Add prompt frontmatter contract and Go prompt renderer.
15. Reconcile role vocabulary across registry, workers, prompts, and executor task kinds.
16. Add builder role registry/prompt using existing executor path.
17. Add QA role as separate Run Attempt.
18. Add reviewer role as separate Run Attempt.
19. Add security review role and policy checks.
20. Add draft PR manager with no merge path.
21. Add PR identity and handoff state to SQLite.
22. Extend dashboard API with read-only work item/run/approval projections.
23. Add optional tmux status projection after live-session contract review.
24. Harden systemd deployment.
25. Decide whether Docker Compose is needed for deployment.
