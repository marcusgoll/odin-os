---
title: Odin OS Current To Target Map
status: draft
date: 2026-04-30
---

# Odin OS Current To Target Map

## Map

| Target architecture item | Current module / asset | Current status | Target state | Action |
| --- | --- | --- | --- | --- |
| Go daemon | `cmd/odin`, `internal/app/lifecycle`, `deploy/systemd/odin.service` | Partial target met. `serve` runs loops and HTTP endpoints. | Long-running agency daemon with scheduler/intake/dispatch/review loops. | Preserve and deepen. |
| Binary entrypoint | `cmd/odin`; duplicate `cmd/odin-os` | `cmd/odin` established; `cmd/odin-os` duplicate. | One documented operator binary plus optional alias if justified. | Refactor decision. |
| Runtime composition | `internal/app/bootstrap`, `internal/app/lifecycle` | Strong existing composition root. | All agency modules wired through lifecycle/bootstrap. | Preserve. |
| GitHub Issues adapter | `internal/tracker`, `internal/tracker/github`, `internal/tracker/intake`, `config/projects.yaml`; reserved empty `internal/adapters/github` | Canonical tracker seam selected and implemented for read-only intake plus gated projection operations; `internal/adapters/github` is not an active root. | One typed tracker seam for eligible issues, comments, labels, and follow-up issue creation behind approval gates. | Preserve `internal/tracker`; keep `internal/adapters/github` empty unless an ADR assigns a non-tracker role. |
| GitHub token policy | `config/agency.example.yaml`, `configs/*.yaml`, docs security rules | Scaffold only. | Token env names and scopes documented; no production secrets to workers. | Add contract before code. |
| SQLite runtime state | `internal/store/sqlite`, migrations | Strong existing runtime authority. | Work Items, Run Attempts, external issues, PR handoffs, approvals all persisted. | Preserve and extend. |
| Work Items | `tasks` table, `jobs.Service`, `odin work start/status` | Present under task terminology. | Canonical Work Item projection over existing tasks or renamed vocabulary. | Refactor vocabulary, preserve table initially. |
| Run Attempts | `runs` table, `runtime/runs`, `runtime/jobs` | Present. | One run attempt per role execution, resumable after restart. | Preserve and extend. |
| Event audit | `events` table, runtime events contract | Present. | All important agency mutations append events. | Preserve and require for new modules. |
| Approvals | `approvals` table, action-bound approvals, REPL `/approvals` | Present but incomplete operator surface. | Human approval before merge/deploy and sensitive actions. | Preserve and extend. |
| Git worktree workspace manager | `internal/vcs/leases`, `internal/vcs/worktrees`, `internal/vcs/git` | Present and useful. | Every mutating worker gets one worktree and task branch. | Preserve. |
| Duplicate workspace seam | Historical `internal/workspace/manager.go` | Removed scaffold duplicate. | No separate workspace manager outside `internal/vcs`. | Keep removed; use `internal/vcs/leases` and `internal/vcs/worktrees`. |
| Executor seam | `internal/executors/contract`, `internal/executors/router` | Strong existing seam. | All runner adapters implement this interface. | Preserve. |
| Codex exec runner | `internal/executors/codex`, `internal/runner/codexexec` | Deterministic alpha adapter plus placeholder duplicate. | Real `codex exec` adapter with security checks. | Refactor under `internal/executors`. |
| Future app-server runner | `internal/runner/appserver` | Placeholder duplicate. | Optional phase-two adapter behind executor seam. | Defer/replace later. |
| Prompt renderer | `prompts/`, `internal/prompts/renderer.go`; removed `src/prompts` preserved only in inventory notes | Draft prompts and Go renderer exist; the TypeScript prompt scaffold is absent. | Go renderer loads validated prompt templates. | Refactor after prompt contract; do not recreate TypeScript prompt scaffolds. |
| Agent registry | `registry/agents`, `internal/registry` | Active triage agent only. | Agents for intake, builder, QA, reviewer, security. | Preserve and extend. |
| Skill registry | `registry/skills`, `state/migration/drafts/skills` | Active triage skill; migration drafts review-only. | Small Odin-native active skill set. | Preserve active, review drafts selectively. |
| Registry validation | `internal/registry/parser`, `validator`, `compiler` | Present and tested. | Same loader validates new agent/skill/workflow assets. | Preserve. |
| Registry hot reload | `internal/registry/watcher` | Noop watcher. | Optional deterministic reload. | Defer. |
| PR manager | PR template, CI, project manifest merge policy | Missing runtime PR manager. | Draft PR creation/update/handoff, no autonomous merge. | Add after execution path. |
| Reviewer automation | Executor `TaskKindReview`, prompt draft, `internal/review` | Placeholder. | Reviewer Run Attempt with findings and human handoff. | Refactor. |
| QA automation | Executor `TaskKindQA`, prompt draft, empty worker dir | Placeholder. | QA Run Attempt executes configured checks and records evidence. | Refactor. |
| Security automation | `internal/security/policy.go`, legacy security drafts | Detached scaffold. | Security role plus enforced runner policy. | Refactor with security review. |
| Dashboard API | `internal/api/http`, `internal/telemetry/metrics`, `internal/dashboard` | Health/ready/metrics only; dashboard scaffold. | Read-only API for work, runs, approvals, PRs, workers, incidents. | Extend existing HTTP handler. |
| Optional tmux status | docs/plans for workspace live sessions | Design only. | Read-only live session status/attach helper, not durable authority. | Defer. |
| Structured logs | `internal/telemetry/logs`, service log file | Present. | All agency actions log structured records with correlation IDs. | Preserve and propagate. |
| Metrics | `internal/telemetry/metrics`, `/metrics` | Present. | Add agency counters for intake, dispatch, review, PR handoff. | Extend. |
| Recovery | `internal/runtime/recovery`, startup recovery | Present. | Recover interrupted runs and stale leases after restart. | Preserve and extend. |
| systemd deployment | `deploy/systemd`, install script | Present but not hardened. | Production-ready user/system unit with reviewed env and sandboxing. | Refactor separately. |
| Docker deployment | none | Missing. | Optional compose deployment if selected. | Defer. |
| Dry-run mode | scaffold config only | Missing in canonical runtime. | End-to-end dry-run for intake/dispatch/PR actions. | Add before GitHub writes. |
| Kill switch | scaffold config only | Missing in canonical runtime. | Runtime gate blocks worker launch and external mutation. | Add before real workers. |
| Human merge/deploy approval | project manifest, approvals, PR template | Partial policy substrate. | Enforced no autonomous merge/deploy. | Preserve invariant and add tests when PR manager exists. |

## Preservation Set

These modules have depth and should remain the foundation:

- `internal/app/lifecycle`
- `internal/app/bootstrap`
- `internal/store/sqlite`
- `internal/runtime/jobs`
- `internal/runtime/recovery`
- `internal/runtime/projections`
- `internal/executors/contract`
- `internal/executors/router`
- `internal/vcs/leases`
- `internal/vcs/worktrees`
- `internal/registry`
- `internal/core/projects`
- `internal/api/http`
- `internal/telemetry/logs`
- `internal/telemetry/metrics`

## Refactor Set

These modules contain useful intent but need locality and seam cleanup:

- `cmd/odin-os`
- `internal/executors/codex`
- `internal/workers`
- `internal/prompts`
- `internal/security`
- `internal/review`
- `prompts/`
- `deploy/systemd/*`

## Replacement Set

These modules should not become separate active seams:

- `internal/runner`
- historical `internal/workspace`
- `internal/adapters/github` for tracker behavior; the canonical GitHub tracker
  root is `internal/tracker`
- `internal/orchestrator`
- `src/*`
- `configs/*`

## Missing Target Modules

- Live mutation wiring behind approval gates for the existing GitHub tracker
  adapter.
- PR manager.
- Real Codex exec adapter.
- Prompt renderer implementation.
- Builder/QA/reviewer/security role runners.
- Agency dashboard API.
- Kill switch and dry-run enforcement.
- Optional tmux live status.
- Docker deployment.

## Notes On Terminology

Current source uses `tasks` and `runs`; target docs often use Work Items and Run Attempts. The migration should initially project Work Item and Run Attempt vocabulary over existing tables rather than renaming storage tables in the first slice.
