---
title: Odin OS
phase: "17"
status: active
updated: 2026-04-17
---

# Odin OS

Odin OS is the canonical future runtime for Odin: a Go-first, CLI-first orchestration system with SQLite as its initial runtime authority, Markdown with frontmatter as its canonical authored format, and Git-governed project execution as a baseline requirement.

This repository is the runtime root. `odin-orchestrator` is a migration source only. The system is designed around a workspace-first semantic center that still operates across explicit scopes: global control, the reserved `odin-core` system project, managed local or GitHub-backed projects, and new-project setup flows. GitHub is optional, but Git is mandatory for any managed project.

See `docs/contracts/ubiquitous-language.md` for the frozen vocabulary, `docs/contracts/workspace-context-map.md` for the bounded-context map, `docs/contracts/follow-through-contract.md` for the workspace-owned operating profile and follow-through model, and `docs/contracts/companion-swarm-orchestration.md` for bounded companion delegation and swarm rules.

## Architecture Summary

- Workspace is the top-level operating environment and the semantic root for all durable work.
- Initiatives are durable responsibility streams that can hold managed projects or non-project life and work streams.
- Companions are durable AI roles such as assistants, advisors, operators, and specialists.
- Bounded swarms are supervised execution patterns behind companion-owned work, built from existing tasks, runs, approvals, and delegations.
- Managed projects are governed initiatives with Git-backed mutation rules and explicit project governance.
- Work items are the durable unit of governed work, and run attempts are the disposable execution records.
- Follow-up obligations are durable control-plane objects that materialize into work items when due.
- Runtime authority lives in SQLite at `data/odin.db`.
- Authored assets live in-repo as Markdown with frontmatter under `registry/`, `prompts/`, and `memory/`.
- CLI, API, and worker execution all resolve through shared orchestration, policy, runtime, and executor contracts.
- Executors are model-agnostic and route through a common contract, including plan-backed headless runners where they fit that contract.
- Tool, skill, and sub-agent loading is dynamic and scope-aware; Odin must not preload the full catalog into every task context.
- Mutating work is isolated through task-owned worktrees and branches.
- Self-heal is deterministic, bounded, and auditable; self-improvement is proposal-driven, replay-tested, promotion-gated, and reversible.
- The root command surface includes `odin initiative`, `odin companion`, `odin profile`, `odin followup`, and `odin agenda` alongside the broader lifecycle commands.
- Companion lifecycle commands are implemented today; companion execution and swarm inspection should continue to extend that same command family rather than introducing a second CLI.

## Canonical Documents

- `docs/adr/0001-canonical-authority.md` defines the system's source-of-truth model, scope model, and governance rules.
- `docs/adr/0002-migration-policy.md` defines how legacy assets from `odin-orchestrator` are classified and moved into this repo.
- `docs/contracts/repo-layout.md` defines package and folder responsibilities.
- `docs/contracts/phase-exit-criteria.md` defines the acceptance gate for Phase 00 and the baseline gate every later phase must satisfy.
- `docs/operations/workspace-bootstrap.md` explains fresh-runtime workspace bootstrap and legacy runtime repair.
- `docs/contracts/verification-model.md` defines how Odin proves behavior across unit, contract, integration, and real `odin` command execution.

## Current Status

Phase 00 through Phase 15 are in place, and the Phase 17 alpha stabilization pass has closed the minimum trust blockers from the reality audit. Fresh runtime roots now bootstrap into an honest not-ready state until a live `odin serve` process marks them `ready`, and they fail closed again when that daemon drains or stops. Queued work can execute through one live `codex_headless` lane, runtime mutation is gated by transition and system-project policy checks, mutable work is forced through leased task-owned worktrees, `odin serve` runs bounded self-heal and queue execution loops, routing promotions require explicit promotion approval before activation, and service logs are newline-delimited JSON again. Full provider-backed execution and broader unattended orchestration remain deferred; see `docs/operations/alpha-readiness.md` for the current alpha operating envelope.

## Local Usage

To make `odin` available as a repeatable local command:

```bash
make build
make install-local
odin
```

This installs a symlink at `~/.local/bin/odin` pointing to this repo's built binary. Remove it with `make uninstall-local`.

## Always-On Runtime

For the single-daemon control plane:

- run `odin serve` under a service manager such as `systemd`
- use `odin healthcheck` for fail-closed readiness checks
- use `odin doctor --json` for machine-readable health inspection
- use `docs/operations/always-on-cutover-checklist.md` before treating a runtime root as always-on

## Workspace Migration Helper

If you need to repair an older runtime so existing projects and tasks are linked into the workspace model, run:

```bash
go run ./scripts/migrate/bootstrap_workspace -runtime-root /path/to/odin-root
```

The helper is additive and idempotent. It bootstraps the default workspace and companion, reconciles managed-project initiatives, and binds legacy tasks into the workspace model without renaming the underlying `tasks` table.

## Contribution Workflow

Before opening a pull request:

- read `docs/contracts/verification-model.md`
- prefer `make ci` to mirror the local CI verification stack
- run additional targeted commands when the change needs narrower iteration than `make ci`
- if the change affects user-visible or orchestration-facing behavior, run the real repo-owned `odin` command path against a controlled `ODIN_ROOT`
- if the change affects the bounded media profile, run `make test-media` and the real `odin doctor --json` or `odin healthcheck` media probe path against a controlled `ODIN_ROOT`

Pull requests are expected to use the repo template and report:

- `Proven`
- `Unproven`
- `Commands Run`

On `pull_request` events, CI validates that the PR body includes those sections and, for operator-visible changes, includes real `odin` command evidence.
