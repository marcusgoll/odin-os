---
title: Odin OS Brownfield Audit
status: draft
date: 2026-04-30
---

# Odin OS Brownfield Audit

## Scope

This audit inspects the current Odin OS repository as a brownfield Go orchestration system. It does not propose a greenfield replacement and does not treat messy or partial implementation as worthless. Working behavior is separated from incomplete or conflicting implementation.

## Current Architecture Summary

Odin OS is currently a Go-first, CLI-first governed control plane. The committed architecture is anchored by:

- `cmd/odin/main.go`: main binary entrypoint that resolves the repo root and delegates to `internal/app/lifecycle.Run`.
- `internal/app/lifecycle/run.go`: command dispatcher, interactive shell entry, `doctor`, `healthcheck`, `serve`, backup/restore, `profile`, and `work`.
- `internal/app/bootstrap/bootstrap.go`: runtime bootstrap, SQLite migration, registry load, project manifest registration, executor catalog creation, and readiness seeding.
- `internal/store/sqlite`: canonical runtime state and migrations.
- `registry/`: authored Markdown registry assets for agents, skills, workflows, and commands.
- `config/`: authored runtime, project, executor, model, policy, and telemetry config.
- `internal/executors`: shared executor contract, router, and current adapter inventory.
- `internal/runtime`: jobs, runs, events, projections, health, recovery, actions, and checkpoints.
- `internal/vcs`: Git branch, worktree, lease, and adapter logic.
- `internal/cli`: REPL, command adapters, scope handling, state cache, and rendering.
- `internal/api/http`: operational HTTP endpoints for health, readiness, and metrics.

The repo also contains uncommitted scaffold material from recent agency-orchestrator experiments:

- `cmd/odin-os/`
- `internal/{agents,config,dashboard,db,logging,orchestrator,prompts,review,runner,security,tracker,utils,workspace}/`
- `configs/`
- TypeScript files under `src/`, `package.json`, `package-lock.json`, `tsconfig.json`, and `eslint.config.js`

Those uncommitted assets are not yet proven as canonical Odin architecture. They should be audited and either promoted deliberately or removed.

The worktree also contains other dirty implementation files unrelated to this brownfield audit, including `go.mod`, `go.sum`, several `internal/memory/knowledge/*` files, untracked PDF testdata, and an untracked root file named `--help`. This audit does not classify those as agency architecture except as active worktree risk.

## Current Directory Map

Top-level directories:

| Path | Observed role | Classification |
| --- | --- | --- |
| `cmd/` | Binary entrypoints. `cmd/odin` is established; `cmd/odin-os` is uncommitted duplicate entrypoint. | Keep `cmd/odin`; refactor/decide `cmd/odin-os` |
| `internal/` | Main Go implementation. Contains mature packages and uncommitted scaffold packages. | Keep core packages; remove or promote scaffold packages deliberately |
| `registry/` | Canonical authored agent, skill, workflow, and command definitions. | Keep |
| `prompts/` | Prompt assets. Mostly `.gitkeep`; uncommitted agency prompts exist. | Refactor |
| `memory/` | Authored durable memory roots. Currently mostly placeholders. | Keep |
| `config/` | Active Odin config. Used by bootstrap and lifecycle. | Keep |
| `configs/` | Uncommitted agency config examples, not wired into current bootstrap. | Replace or remove |
| `data/` | Runtime SQLite data location. Ignored database files. | Keep local runtime output |
| `runs/` | Logs, summaries, artifacts output roots. | Keep local runtime output |
| `state/` | Cache, compiled registry, snapshot output roots. | Keep local runtime output |
| `docs/` | ADRs, contracts, operations, migration notes, plans, and audits. | Keep |
| `scripts/` | CI, development, backup, install, and migration helpers. | Keep/refactor thin wrappers |
| `deploy/` | systemd deployment files. | Keep/refactor |
| `.github/` | GitHub Actions CI and PR template. | Keep |
| `.worktrees/` | Local git worktrees. | Keep as local operational state |
| `src/`, `tests/agency-scaffold.test.ts`, `node_modules/` | Uncommitted TypeScript scaffold and dependencies. | Remove |

Committed Go package families:

- `internal/app`: lifecycle, config, bootstrap, backup.
- `internal/cli`: command, REPL, scope, session state, rendering, TUI placeholder.
- `internal/core`: project governance, profile, policy/intake/scheduler placeholders.
- `internal/runtime`: jobs, runs, health, recovery, projections, events, actions, checkpoints.
- `internal/store/sqlite`: migrations and runtime data access.
- `internal/registry`: Markdown/frontmatter loading, parsing, validation, compiling, watching placeholder.
- `internal/executors`: contract, router, Codex deterministic lane, static provider adapters.
- `internal/vcs`: branch naming, Git adapter, lease manager, worktree manager.
- `internal/tools`: catalog, budgets, broker.
- `internal/learning`: proposals, evaluation, promotion, replay.
- `internal/memory`: user memory and knowledge source runtime.
- `internal/api/http`: operational HTTP handler.
- `internal/migration/extractor`: legacy migration inventory tooling.

## Existing Components Inventory

See `docs/brownfield/COMPONENT_INVENTORY.md` for the detailed keep/refactor/replace/remove table.

High-value existing modules:

- `internal/store/sqlite`: deep module with transactional state, events, approvals, leases, recovery, learning, actions, knowledge, and memory summaries.
- `internal/app/lifecycle`: real composition layer and command dispatcher.
- `internal/runtime/jobs`: queued task execution through routing, transitions, worktree leases, and executor contract.
- `internal/runtime/recovery`: monitor, diagnosis, playbook, startup recovery, and self-heal execution.
- `internal/vcs/leases` plus `internal/vcs/worktrees`: work isolation substrate.
- `internal/executors/contract` and `internal/executors/router`: model-agnostic executor seam.
- `registry/`: authored assets that already match the Markdown/frontmatter authority model.

## Existing Skills Inventory

Current committed Odin registry skill:

- `registry/skills/triage-skill.md`: active intake/planning skill. Keep.

Global Codex skills outside this repo are not Odin runtime assets and were not copied into the audit. Legacy skill inventory exists in:

- `docs/migration/legacy-inventory.md`
- `docs/migration/duplicate-report.md`

Those migration docs classify old skills for rewrite, archive, or reference-only use. They should be treated as migration evidence, not active Odin runtime behavior.

## Existing Agents Inventory

Current committed Odin registry agent:

- `registry/agents/triage-agent.md`: active intake triage definition. Keep.

Current uncommitted Go role constants:

- `internal/agents/roles.go`: scaffold role names. Refactor or remove unless promoted into the existing registry/worker model.

Current worker packages:

- `internal/workers/planner`: implemented planner worker with tests.
- `internal/workers/{builder,qa,research,reviewer}`: placeholder directories. Refactor as roles are implemented.

## Existing Shims Inventory

No production shim subsystem is currently implemented as a first-class Go package.

Observed shim-like assets:

- `scripts/dev/*.sh`: thin shell wrappers around `go run ./cmd/odin ...` or local symlink/systemd installation. Keep, but keep thin.
- `cmd/odin-os`: uncommitted alias-like second binary delegating to `internal/app/lifecycle`. Refactor/decide.
- REPL aliases and command surfaces in `internal/cli/repl`: useful operator surface, but docs say top-level commands must remain proof authority.
- Migration references to old shims in `docs/migration/legacy-inventory.md`: reference-only until rewritten.

## Existing Runner / Executor Inventory

Current implemented executor seam:

- `internal/executors/contract/types.go`: portable executor interface and task spec.
- `internal/executors/router`: config loading, catalog, selection, routing promotion overlay.
- `config/executors.yaml`: configured executor inventory and route preferences.
- `config/models.yaml`: model metadata.

Current executor adapters:

- `internal/executors/codex/adapter.go`: working deterministic alpha lane named `codex_headless`; it does not launch real Codex CLI.
- `internal/executors/claude_code/adapter.go`: static metadata adapter; execution not implemented.
- `internal/executors/gemini_cli/adapter.go`: static metadata adapter; execution not implemented.
- `internal/executors/openai_api/adapter.go`: static metadata adapter; execution not implemented.
- `internal/executors/anthropic_api/adapter.go`: static metadata adapter; execution not implemented.
- `internal/executors/google_api/adapter.go`: static metadata adapter; execution not implemented.
- `internal/executors/xai_api/adapter.go`: static metadata adapter; execution not implemented.
- `internal/executors/openrouter_api/adapter.go`: static metadata adapter; execution not implemented.

Uncommitted runner scaffold:

- `internal/runner/codexexec/adapter.go`: placeholder future `codex exec` wrapper.
- `internal/runner/appserver/adapter.go`: placeholder future Codex app-server runner.

Recommended action: keep the existing `internal/executors` seam as the canonical runner seam. Do not promote `internal/runner` as a parallel abstraction without deleting or merging the duplicate.

## Existing State / Database Inventory

SQLite is the canonical runtime authority per `docs/adr/0001-canonical-authority.md`.

Migrations present:

- `0001_runtime.sql`: projects, tasks, runs, approvals, events, incidents, recoveries, registry versions, executor health, context packets.
- `0002_context_packets_envelope.sql`: wake/handoff envelope metadata.
- `0003_worktree_leases.sql`: mutable worktree lease tracking and uniqueness.
- `0004_projection_freshness.sql`: projection freshness state.
- `0005_project_transitions.sql`: project transition authority and reports.
- `0006_learning.sql`: proposals, evaluations, promotions.
- `0017_workspace_profile.sql`: workspace preference profile.
- `0018_actions.sql`: action records, immutable payloads, evidence events, action-bound approvals.
- `0019_knowledge_sources.sql`: artifacts, sources, extractions, chunks, FTS, related sources, restricted-use approvals.
- `0020_memory_summaries.sql`: conversation transcripts and memory summaries.

The store implementation is large but functional. It owns many real behaviors and should be deepened through smaller domain services over the existing store rather than replaced wholesale.

## Existing Config / Secrets Inventory

Active config:

- `config/odin.yaml`: runtime root and service HTTP address.
- `config/projects.yaml`: `odin-core` system project manifest and governance rules.
- `config/executors.yaml`: executor inventory and route preferences.
- `config/models.yaml`: model metadata.
- `config/telemetry.yaml`: logging, health, metrics, and doctor defaults.
- `config/policies.yaml`: placeholder.

Uncommitted config:

- `config/agency.example.yaml`
- `configs/default.yaml`
- `configs/development.yaml`
- `configs/production.example.yaml`

Secrets:

- No obvious raw secrets were found in config files.
- `deploy/systemd/odin.env.example` includes `ODIN_TRADEBOARD_API_TOKEN=` as an empty placeholder.
- GitHub token references use environment variable names such as `GITHUB_TOKEN`.

Risk: there are now two config roots, `config/` and `configs/`, with overlapping agency settings. That is a drift risk.

## Existing Deployment Inventory

Deployment assets:

- `deploy/systemd/odin.service`: runs `bin/odin serve`, working directory `%h/odin-os`.
- `deploy/systemd/odin.env.example`: runtime root, HTTP address, and tradeboard API env placeholders.
- `scripts/dev/install-systemd-service.sh`: installs user systemd unit and env file.
- `scripts/dev/install-local.sh`: symlinks `bin/odin` into `~/.local/bin`.
- `scripts/dev/backup-odin.sh`, `restore-odin.sh`, `verify-backup.sh`: thin wrappers around real Odin commands.

No Docker Compose deployment file exists in the current repo.

Known deployment gap: `odin.service` does not explicitly set `User=`, sandboxing, `NoNewPrivileges=`, or hardened systemd options. For a user unit this may be acceptable in development, but production hardening is incomplete.

## Existing CI / CD Inventory

CI assets:

- `.github/workflows/ci.yml`: runs `make ci` on push and pull request, then validates PR body on pull requests.
- `.github/pull_request_template.md`: requires Summary, Verification Contract, Proven, Unproven, Commands Run.
- `scripts/ci/verify-pr-template.sh`: checks required headings, non-placeholder bullets, and real `odin` proof when user-visible behavior is changed.
- `scripts/tests/make-ci-target-test.sh`: verifies `make ci` includes expected commands.
- `scripts/tests/verify-pr-template-test.sh`: tests the PR-body validator.

Gap: CI does not currently detect untracked TypeScript scaffold drift or duplicate config roots because those files are untracked in this working tree.

## Existing Tests And Coverage Gaps

Working tests:

- `go test ./...` passes in the current worktree.
- `go vet ./...` passes in the current worktree.
- `tests/integration/alpha_acceptance_test.go` exercises broad runtime behavior.
- Package tests cover bootstrap, lifecycle, CLI, scope, registry, projects, executors, routing, jobs, recovery, projections, actions, knowledge, learning, VCS, backup, and telemetry.

Coverage gaps:

- Real `codex exec` is not tested because it is not implemented.
- GitHub Issues intake is not implemented or tested.
- Draft PR creation is not implemented or tested.
- `odin workspace ...` is documented but not implemented.
- `odin knowledge ...`, `odin brief ceo`, and full delivery-gate behavior are documented/planned but not all implemented.
- Provider API adapters are static metadata adapters; execution paths are not tested.
- Existing `go test ./...` currently includes `odin-os/node_modules/flatted/golang/pkg/flatted` because untracked `node_modules/` exists locally. That is a local hygiene issue.

## Known Working Flows

Proven in this audit:

- `go test ./...` passes.
- `go vet ./...` passes.
- `go build -o /tmp/odin-os-audit-odin ./cmd/odin` succeeds.
- Fresh-root `odin doctor --json` reports healthy checks.
- Fresh-root `odin healthcheck` returns `ready`.
- Fresh-root `odin work status` returns counts and explicitly marks `dispatch=not_implemented intake=not_implemented`.
- `odin serve` starts on an ephemeral HTTP address and prints `serving on 127.0.0.1:<port>` before timeout termination.

Known from code/tests:

- Bootstrap creates data/state directories, migrates SQLite, loads registry/config, seeds readiness state.
- The deterministic `codex_headless` lane can complete queued tasks through the executor contract.
- Runtime job execution enforces transition authority, system-project approval gating, and mutable worktree policy.
- Worktree leases prevent conflicting active mutable leases.
- Recovery can detect health/projection/source/queue/run issues and execute bounded playbooks.
- Backup/restore/verify commands exist and have tests.
- Operational HTTP exposes `/healthz`, `/readyz`, and `/metrics`.

## Broken Or Incomplete Flows

- `./bin/odin --help` and `./bin/odin help` return unknown command. Only subcommands such as `work help` expose usage.
- `odin serve` default port `127.0.0.1:9443` can collide with an existing process; ephemeral `ODIN_HTTP_ADDR=127.0.0.1:0` works.
- `odin work status` exists, but dispatch and intake are explicitly not implemented.
- `odin workspace ...` is planned in docs but absent in current command dispatcher.
- Real `codex exec` runner is absent; `codex_headless` is deterministic local alpha behavior.
- Codex app-server runner is absent and should remain phase two.
- GitHub Issues intake adapter is absent except for an uncommitted placeholder under `internal/tracker/github`.
- Pull request creation/update is absent.
- `config/policies.yaml` is placeholder-only.
- The active registry contains no delivery profile workflow tagged `delivery_profile`, so `odin work profiles` currently has no profiles unless uncommitted or future registry files add them.

## Duplicate Or Conflicting Implementations

- `cmd/odin` vs uncommitted `cmd/odin-os`: both delegate to lifecycle. Decide whether there is one binary with `serve` mode or a second service binary.
- `internal/executors/*` vs uncommitted `internal/runner/*`: both describe execution lanes. Keep one canonical seam.
- `internal/adapters/github` placeholder directory vs uncommitted `internal/tracker/github`: both could become GitHub integration roots. Decide whether GitHub is an adapter under `internal/adapters` or an intake adapter under `internal/core/intake/github`.
- `config/` vs uncommitted `configs/`: duplicate config root.
- Go-native scaffold vs uncommitted TypeScript scaffold in `src/`: TypeScript is contrary to the current Go-native decision and should be removed unless explicitly archived as reference-only.
- Canonical Work Item / Run Attempt language vs storage names `tasks` and `runs`: this is a known compatibility naming conflict in `CONTEXT.md`.

## Security Risks

See `docs/brownfield/RISK_REGISTER.md` for the detailed register.

Highest risks:

- Real worker execution is not yet hardened because real `codex exec` does not exist.
- No enforced no-root/no-danger-full-access policy exists in the canonical executor path.
- systemd service lacks explicit hardening settings.
- Duplicate scaffold and config roots can lead future agents to implement into the wrong seam.
- Default `odin serve` port collision can hide service-state truth.
- GitHub integration is not implemented, so token scoping and mutation boundaries are only documented.

## Refactor Opportunities

1. Collapse duplicate scaffold seams before feature work.
2. Keep `internal/executors` as the only runner seam and add real `codex_exec` there.
3. Move agency-specific placeholder config into `config/` only after contract decisions; remove `configs/`.
4. Add a top-level usage/help command through `internal/app/lifecycle`.
5. Split `internal/store/sqlite/store.go` by domain area while preserving one SQLite store package and transaction model.
6. Add a read-only GitHub intake adapter under one chosen package root.
7. Promote delivery profiles into registry workflows instead of hardcoded route logic.
8. Add systemd hardening before long-running unattended use.
9. Add CI hygiene checks for generated/untracked scaffold classes.
10. Add command-level tests for every newly surfaced operator flow.

## Recommended Target Architecture

Do not replace Odin. Deepen it.

Target shape:

- One canonical binary surface: keep `cmd/odin` unless there is a strong deployment need for `cmd/odin-os`.
- One lifecycle composition root: `internal/app/lifecycle`.
- One runtime authority: `internal/store/sqlite` with smaller domain services layered above it.
- One executor seam: `internal/executors/contract` and `internal/executors/router`.
- One GitHub intake seam: standardize under `internal/core/intake/github` or `internal/adapters/github`, not both.
- One operator proof path: top-level `odin ...` commands with REPL aliases as thin adapters only.
- One authored asset model: registry Markdown/frontmatter, prompts under `prompts/`, memory under `memory/`, config under `config/`.
- One work isolation model: `internal/vcs` branches, worktrees, and leases.
- One deployment path for v1: `bin/odin serve` under systemd with hardened service options.

## Migration Strategy

See `docs/brownfield/MIGRATION_PLAN.md`.

Short version:

1. Freeze greenfield scaffold expansion.
2. Delete or archive uncommitted TypeScript artifacts.
3. Decide the binary and runner seam.
4. Add missing contracts before adding code.
5. Implement the smallest vertical slices through existing services.
6. Prove every operator-visible feature through real `odin` commands.

## Commands Run

```bash
git status --short --branch
go list ./...
go test ./...
go vet ./...
go build -o /tmp/odin-os-audit-odin ./cmd/odin
ODIN_ROOT="$(mktemp -d)" /tmp/odin-os-audit-odin doctor --json
ODIN_ROOT="$(mktemp -d)" /tmp/odin-os-audit-odin healthcheck
ODIN_ROOT="$(mktemp -d)" /tmp/odin-os-audit-odin work status
ODIN_ROOT="$(mktemp -d)" ODIN_HTTP_ADDR=127.0.0.1:0 timeout 1 /tmp/odin-os-audit-odin serve
```
