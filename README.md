---
title: Odin OS
phase: "17"
status: active
updated: 2026-04-09
---

# Odin OS

Odin OS is the canonical future runtime for Odin: a Go-first, CLI-first orchestration system with SQLite as its initial runtime authority, Markdown with frontmatter as its canonical authored format, and Git-governed project execution as a baseline requirement.

This repository is the runtime root. `odin-orchestrator` is a migration source only. The system is designed to operate across explicit scopes: global control, the reserved `odin-core` system project, managed local or GitHub-backed projects, and new-project setup flows. GitHub is optional, but Git is mandatory for any managed project.

## Architecture Summary

- Runtime authority lives in SQLite at `data/odin.db`.
- Authored assets live in-repo as Markdown with frontmatter under `registry/`, `prompts/`, and `memory/`.
- CLI, API, and worker execution all resolve through shared orchestration, policy, runtime, and executor contracts.
- Executors are model-agnostic and route through a common contract, including plan-backed headless runners where they fit that contract.
- Tool, skill, and sub-agent loading is dynamic and scope-aware; Odin must not preload the full catalog into every task context.
- Mutating work is isolated through task-owned worktrees and branches.
- Self-heal is deterministic, bounded, and auditable; self-improvement is proposal-driven, replay-tested, promotion-gated, and reversible.

## Canonical Documents

- `docs/adr/0001-canonical-authority.md` defines the system's source-of-truth model, scope model, and governance rules.
- `docs/adr/0002-migration-policy.md` defines how legacy assets from `odin-orchestrator` are classified and moved into this repo.
- `docs/contracts/repo-layout.md` defines package and folder responsibilities.
- `docs/contracts/phase-exit-criteria.md` defines the acceptance gate for Phase 00 and the baseline gate every later phase must satisfy.

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
