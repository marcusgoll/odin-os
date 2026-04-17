---
title: Odin OS
phase: "17"
status: active
updated: 2026-04-16
---

# Odin OS

Odin OS is the canonical future runtime for Odin: a Go-first, CLI-first workspace operating system with SQLite as its initial runtime authority, Markdown with frontmatter as its canonical authored format, and Git-governed managed project execution as a baseline requirement.

This repository is the runtime root. `odin-orchestrator` is a migration source only. The system is designed around a workspace-first semantic center: workspace, initiative, companion, managed project, work item, run attempt, control scope, and execution lane. GitHub is optional, but Git is mandatory for any managed project.

See `docs/contracts/ubiquitous-language.md` for the frozen vocabulary and `docs/contracts/workspace-context-map.md` for the bounded-context map.

## Architecture Summary

- Workspace is the top-level operating environment and the semantic root for all durable work.
- Initiatives are durable responsibility streams that can hold managed projects or non-project life and work streams.
- Companions are durable AI roles such as assistants, advisors, operators, and specialists.
- Managed projects are governed initiatives with Git-backed mutation rules and explicit project governance.
- Work items are the durable unit of governed work, and run attempts are the disposable execution records.
- Runtime authority lives in SQLite at `data/odin.db`.
- Authored assets live in-repo as Markdown with frontmatter under `registry/`, `prompts/`, and `memory/`.
- CLI, API, and worker execution all resolve through shared orchestration, policy, runtime, and executor contracts.
- Executors are model-agnostic and route through a common contract, including plan-backed headless runners where they fit that contract.
- Tool, skill, and companion loading is dynamic and control-scope-aware; Odin must not preload the full catalog into every work item context.
- Mutating work is isolated through work-item-owned worktrees and branches.
- Self-heal is deterministic, bounded, and auditable; self-improvement is proposal-driven, replay-tested, promotion-gated, and reversible.

## Canonical Documents

- `docs/contracts/ubiquitous-language.md` freezes the canonical workspace vocabulary.
- `docs/contracts/workspace-context-map.md` defines the target bounded-context dependency direction.
- `docs/adr/0001-canonical-authority.md` defines the system's source-of-truth model, control-scope model, and governance rules.
- `docs/adr/0002-migration-policy.md` defines how legacy assets from `odin-orchestrator` are classified and moved into this repo.
- `docs/contracts/repo-layout.md` defines package and folder responsibilities.
- `docs/contracts/phase-exit-criteria.md` defines the acceptance gate for Phase 00 and the baseline gate every later phase must satisfy.
- `docs/operations/workspace-bootstrap.md` explains fresh-runtime workspace bootstrap and legacy runtime repair.

## Current Status

Phase 00 through Phase 15 are in place, and the Phase 17 alpha stabilization pass has closed the minimum trust blockers from the reality audit. Fresh runtimes now bootstrap into an honest ready state, queued work can execute through one live `codex_headless` lane, runtime mutation is gated by transition and system-project policy checks, mutable work is forced through leased work-item-owned worktrees, `odin serve` runs bounded self-heal and queue execution loops, routing promotions require explicit promotion approval before activation, and service logs are newline-delimited JSON again. Full provider-backed execution and broader unattended orchestration remain deferred; see `docs/operations/alpha-readiness.md` for the current alpha operating envelope.

## Local Usage

To make `odin` available as a repeatable local invocation:

```bash
make build
make install-local
odin
```

This installs a symlink at `~/.local/bin/odin` pointing to this repo's built binary. Remove it with `make uninstall-local`.

## Workspace Migration Helper

If you need to repair an older runtime so existing projects and tasks are linked into the workspace model, run:

```bash
go run ./scripts/migrate/bootstrap_workspace -runtime-root /path/to/odin-root
```

The helper is additive and idempotent. It bootstraps the default workspace and companion, reconciles managed-project initiatives, and binds legacy tasks into the workspace model without renaming the underlying `tasks` table.
