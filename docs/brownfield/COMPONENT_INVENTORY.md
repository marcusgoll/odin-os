---
title: Odin OS Brownfield Component Inventory
status: draft
date: 2026-04-30
---

# Odin OS Brownfield Component Inventory

Classification values:

- Keep: valuable current asset; preserve and extend.
- Refactor: valuable but needs narrowing, relocation, or cleanup.
- Replace: concept may stay, implementation or location should be superseded.
- Remove: local artifact or duplicate that should not become canonical.

## Go Modules And Packages

| Asset | Role | Evidence | Classification | Notes |
| --- | --- | --- | --- | --- |
| `go.mod` | Go module `odin-os`; uses `gopkg.in/yaml.v3` and `modernc.org/sqlite`. | `go.mod` | Keep | Lightweight dependency set is reasonable for YAML and SQLite. |
| `cmd/odin` | Established binary entrypoint. | `cmd/odin/main.go`, `internal/app/lifecycle/run.go` | Keep | Main canonical operator surface today. |
| `cmd/odin-os` | Uncommitted duplicate entrypoint. | `cmd/odin-os/main.go` | Refactor | Decide whether to keep a service binary or remove. |
| `internal/app` | Bootstrap, lifecycle, config, backup. | `internal/app/*` | Keep | Real composition layer. |
| `internal/cli` | Commands, REPL, scope, session state, rendering. | `internal/cli/*` | Keep | Needs top-level help consistency. |
| `internal/core` | Projects/profile today; placeholders for policy/intake/scheduler. | `internal/core/*` | Keep/refactor | Right domain center; many subdirs still placeholders. |
| `internal/runtime` | Jobs, runs, events, health, recovery, projections, actions, checkpoints. | `internal/runtime/*` | Keep | Core working runtime. |
| `internal/store/sqlite` | Runtime persistence and migrations. | `internal/store/sqlite/*` | Keep/refactor | Deep but large; split by domain only after tests. |
| `internal/registry` | Markdown/frontmatter registry loader/parser/validator/compiler. | `internal/registry/*` | Keep | Good authored-asset seam. |
| `internal/executors` | Executor contract, router, adapters. | `internal/executors/*` | Keep/refactor | Keep as canonical runner seam. |
| `internal/vcs` | Branch, Git, lease, worktree logic. | `internal/vcs/*` | Keep | Real isolation substrate. |
| `internal/tools` | Tool catalog, budget, broker. | `internal/tools/*` | Keep | Useful for scoped capabilities. |
| `internal/learning` | Proposal/evaluation/promotion/replay. | `internal/learning/*` | Keep | Proposal-driven self-improvement substrate. |
| `internal/memory` | User and knowledge memory services. | `internal/memory/*` | Keep | Knowledge source work is meaningful but not fully surfaced by CLI. |
| `internal/api/http` | Operational HTTP handler. | `internal/api/http/operational.go` | Keep | Readiness/metrics only today. |
| `internal/adapters` | Placeholder external adapters. | `internal/adapters/doc.go`, `.gitkeep` dirs | Refactor | Do not add GitHub tracker behavior here; `internal/adapters/github` is reserved empty unless a later ADR assigns a non-tracker responsibility. |
| `internal/workers` | Worker roles; planner implemented. | `internal/workers/*` | Refactor | Keep planner; implement other roles only through runtime contracts. |
| `internal/runner` | Uncommitted duplicate runner seam. | `internal/runner/*` | Remove/replace | Merge into `internal/executors` if useful. |
| `internal/tracker` | Canonical GitHub issue/PR tracker seam. | `internal/tracker/*` | Keep/refactor | Owns tracker contract, GitHub REST adapter, and intake reconciliation; future GitHub tracker work belongs here. |
| `internal/orchestrator` | Uncommitted agency orchestrator placeholder. | `internal/orchestrator/service.go` | Replace | Too generic; real orchestration should compose existing runtime services. |
| `internal/config`, `internal/logging`, `internal/db`, `internal/dashboard`, `internal/security`, `internal/utils`, `internal/prompts`, `internal/review`, `internal/agents` | Uncommitted scaffold packages. | directories under `internal/` | Remove/refactor | Promote only if they replace a specific missing package and do not duplicate existing seams. |
| Historical `internal/workspace` | Removed duplicate workspace scaffold. | Removed `internal/workspace/manager.go` | Remove | Keep absent; use `internal/vcs/leases` and `internal/vcs/worktrees` for worktree lease behavior. |

## Registry Assets

| Asset | Role | Classification | Notes |
| --- | --- | --- | --- |
| `registry/agents/triage-agent.md` | Intake triage agent. | Keep | Active registry agent. |
| `registry/skills/triage-skill.md` | Intake/planning skill. | Keep | Active registry skill. |
| `registry/commands/status.md` | Command registry asset. | Keep | Keep aligned with CLI. |
| `registry/workflows/project-intake.md` | Project intake workflow. | Keep | Current general workflow. |
| `registry/workflows/flica-schedule.md` | FLICA schedule preflight workflow. | Keep | Operator-invoked; defers airline semantics to PBS. |
| `registry/workflows/flica-seniority-bid.md` | Seniority bid workflow. | Keep | Active; depends on `/tradeboard`. |
| `registry/workflows/flica-fcfs-bid.md` | FCFS bid workflow. | Keep | Active; depends on `/tradeboard`. |
| `registry/workflows/flica-tradeboard.md` | TradeBoard workflow. | Keep | Active; composed workflow. |
| `registry/workflows/flica-tradeboard-split-post.md` | Split post workflow. | Keep | Valuable proven operational knowledge. |
| `registry/workflows/flica-annual-vacation.md` | Vacation workflow. | Refactor | Draft and blocked for live writes until operator surface exists. |

## Skills Inventory

| Skill | Path | Classification | Notes |
| --- | --- | --- | --- |
| Triage Skill | `registry/skills/triage-skill.md` | Keep | Only active in-repo skill. |
| Legacy skills | `docs/migration/legacy-inventory.md` | Refactor/reference-only | Inventory includes legacy skills such as GitHub auth boundaries; do not copy wholesale. |
| Global Codex skills | outside repo | Reference only | Not Odin runtime assets. |

## Agents Inventory

| Agent | Path | Classification | Notes |
| --- | --- | --- | --- |
| Triage Agent | `registry/agents/triage-agent.md` | Keep | Only active in-repo agent definition. |
| Planner worker | `internal/workers/planner` | Keep | Implemented with tests. |
| Builder/QA/research/reviewer worker dirs | `internal/workers/*` | Refactor | Placeholder dirs exist; implementation should follow runtime service seams. |
| Scaffold roles | `internal/agents/roles.go` | Remove/refactor | Uncommitted duplicate role model. |

## Shims And Scripts Inventory

| Asset | Role | Classification | Notes |
| --- | --- | --- | --- |
| `scripts/dev/install-local.sh` | Symlink `bin/odin` into local bin. | Keep | Thin wrapper. |
| `scripts/dev/uninstall-local.sh` | Remove symlink. | Keep | Thin wrapper. |
| `scripts/dev/install-systemd-service.sh` | Install user systemd service. | Refactor | Useful, but service hardening needs review. |
| `scripts/dev/backup-odin.sh` | Wrapper around `go run ./cmd/odin backup`. | Keep | Thin. |
| `scripts/dev/restore-odin.sh` | Wrapper around `go run ./cmd/odin restore`. | Keep | Thin. |
| `scripts/dev/verify-backup.sh` | Wrapper around `go run ./cmd/odin verify-backup`. | Keep | Thin. |
| `scripts/ci/verify-pr-template.sh` | PR body contract validator. | Keep | Enforces proven/unproven and real Odin proof. |
| `scripts/tests/*` | Shell tests for Makefile and PR validator. | Keep | Good CI self-checks. |
| `scripts/migrate/extract-odin-orchestrator.go` | Legacy migration extractor entrypoint. | Keep | Supports ADR 0002. |
| REPL slash commands | `internal/cli/repl/*` | Keep/refactor | Useful operator shell; top-level commands remain proof authority. |
| Legacy shims in migration docs | `docs/migration/legacy-inventory.md` | Reference only | Do not revive without explicit rewrite. |

## Runner / Executor Inventory

| Executor | Path | Current behavior | Classification |
| --- | --- | --- | --- |
| `codex_headless` | `internal/executors/codex/adapter.go` | Deterministic local alpha executor; completes tasks without real Codex. | Keep/refactor |
| `claude_code_headless` | `internal/executors/claude_code/adapter.go` | Static capabilities; run not implemented. | Refactor |
| `gemini_cli_headless` | `internal/executors/gemini_cli/adapter.go` | Static capabilities; run not implemented. | Refactor |
| `openai_api` | `internal/executors/openai_api/adapter.go` | Static capabilities; run not implemented. | Refactor |
| `anthropic_api` | `internal/executors/anthropic_api/adapter.go` | Static capabilities; run not implemented. | Refactor |
| `google_api` | `internal/executors/google_api/adapter.go` | Static capabilities; run not implemented. | Refactor |
| `xai_api` | `internal/executors/xai_api/adapter.go` | Static capabilities; run not implemented. | Refactor |
| `openrouter_api` | `internal/executors/openrouter_api/adapter.go` | Static broker capabilities; run not implemented. | Refactor |
| `codexexec` | `internal/runner/codexexec/adapter.go` | Uncommitted placeholder. | Replace into `internal/executors` |
| `appserver` | `internal/runner/appserver/adapter.go` | Uncommitted placeholder. | Replace into `internal/executors` later |

## State / Database Inventory

| Area | Tables / code | Classification | Notes |
| --- | --- | --- | --- |
| Runtime projects/tasks/runs | `0001_runtime.sql`, `store.go` | Keep | Storage terms lag canonical Work Item / Run Attempt language. |
| Approvals | `approvals`, action-bound approval columns | Keep/refactor | Needs broader operator resolution surfaces. |
| Events | `events`, `internal/runtime/events` | Keep | Append-only audit model. |
| Recovery | `incidents`, `recoveries`, `internal/runtime/recovery` | Keep | Working self-heal substrate. |
| Context packets | `context_packets`, `internal/runtime/checkpoints` | Keep | Wake/handoff model. |
| Worktree leases | `worktree_leases`, `internal/vcs/leases` | Keep | Critical for mutating execution isolation. |
| Projection freshness | `projection_freshness` | Keep | Powers health/readiness truth. |
| Project transitions | `project_transitions`, reports | Keep | Guards migration/cutover states. |
| Learning | proposals/evaluations/promotions | Keep | Runtime overlays, not file rewrites. |
| Action evidence | actions/payloads/evidence | Keep | Valuable general action substrate. |
| Knowledge sources | knowledge tables and FTS | Keep/refactor | Needs CLI surface completion. |
| Memory summaries | transcripts/summaries | Keep/refactor | Useful but needs command/operator surfaces. |

## Config / Secrets Inventory

| Asset | Classification | Notes |
| --- | --- | --- |
| `config/odin.yaml` | Keep | Active app config. |
| `config/projects.yaml` | Keep | Canonical project manifest. |
| `config/executors.yaml` | Keep | Active executor config. |
| `config/models.yaml` | Keep | Model metadata. |
| `config/media-stack.yaml` | Keep | Active media-stack operations config. |
| `config/telemetry.yaml` | Keep/refactor | Health config exists but not all values are wired through current app config. |
| `config/policies.yaml` | Refactor | Placeholder. |
| `config/agency.example.yaml` | Refactor | Tracked agency example in the canonical `config/` root; overlaps `configs/*.yaml` runner/GitHub-token examples. |
| `configs/default.yaml` | Remove/refactor | Duplicate agency example root; useful fields are `runtime.log_dir` and `runners.codex_exec.sandbox_mode`. |
| `configs/development.yaml` | Remove/refactor | Duplicate agency example root; useful field is `logging.level=debug`. |
| `configs/production.example.yaml` | Remove/refactor | Duplicate agency example root; useful production placeholders are `runtime.workspace_root`, `runtime.log_dir`, `runtime.kill_switch=true`, and runner sandbox mode. |
| `deploy/systemd/odin.env.example` | Keep/refactor | No secrets committed; token is empty placeholder. |

### Config root decision

`config/` is the only canonical repo-authored configuration root. Runtime loaders and tests read `config/odin.yaml`, `config/projects.yaml`, `config/projects.local.yaml`, `config/executors.yaml`, and other config files below `config/`; no runtime loader should be taught to scan `configs/`.

`configs/` is a tracked duplicate example root. Before removing it, preserve any useful agency example fields in `config/agency.example.yaml` or an operations doc, then remove `configs/` in a cleanup PR with reference checks.

Cleanup reference checks:

- `rg -n "configs/" .`
- `rg -n "config/agency.example.yaml|configs/(default|development|production.example).yaml" docs config configs`
- `go test ./internal/app/config ./internal/core/projects ./internal/app/bootstrap ./internal/app/lifecycle -count=1`
- `ODIN_ROOT="$(mktemp -d)" ./bin/odin doctor --json`

Rollback: restore the removed files from the cleanup commit or revert the cleanup commit; no database migration or runtime state rollback is needed because `configs/` is not loaded by the runtime.

## Deployment Inventory

| Asset | Classification | Notes |
| --- | --- | --- |
| `deploy/systemd/odin.service` | Refactor | Works conceptually; add hardening and align working directory for deployment target. |
| `deploy/systemd/odin.env.example` | Keep/refactor | Environment placeholders are clear. |
| Docker Compose | Missing | Not needed for v1 unless chosen later. |

## CI / Tests Inventory

| Asset | Classification | Notes |
| --- | --- | --- |
| `.github/workflows/ci.yml` | Keep | Runs `make ci` and PR validation. |
| `.github/pull_request_template.md` | Keep | Strong verification contract. |
| `Makefile` | Keep/refactor | Good common commands. Current worktree version also builds `odin-os`; decide if that stays. |
| `tests/integration/alpha_acceptance_test.go` | Keep | High-value broad E2E-style integration. |
| Package tests under `internal/**` | Keep | Good unit and integration coverage. |
| TypeScript tests | Remove | Uncommitted and contrary to Go-native direction. |

## Documentation Inventory

| Asset | Classification | Notes |
| --- | --- | --- |
| `CONTEXT.md` | Keep | Rich domain model and invariants. |
| `docs/adr/0001-canonical-authority.md` | Keep | Core authority ADR. |
| `docs/adr/0002-migration-policy.md` | Keep | Brownfield migration policy. |
| `docs/contracts/*` | Keep | Strong contracts for layout, verification, routing, worktrees, etc. |
| `docs/operations/*` | Keep | Homelab and alpha operations. |
| `docs/plans/*` | Keep/reference | Useful historical plans; must not be mistaken for implemented behavior. |
| `docs/ARCHITECTURE.md`, `docs/ROADMAP.md`, `docs/SECURITY.md` | Refactor | New agency docs are useful but should be reconciled with brownfield audit. |
