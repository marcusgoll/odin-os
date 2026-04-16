---
title: Odin OS
phase: "17"
status: active
updated: 2026-04-16
---

# Odin OS

Odin OS is Marcus's persistent workspace operating system: a user-adaptive control plane that owns durable workspace state, initiatives, companions, policy, memory, and governed work. Odin dispatches short-lived workers through shared execution contracts, but it is not a wrapper around Codex, Claude, or any single executor.

This repository is the runtime root. `odin-orchestrator` is a migration source only. Odin remains Go-first and CLI-first today, with SQLite as its initial runtime authority, Markdown with frontmatter as its canonical authored format, and Git-governed project execution as a baseline requirement. Managed projects are one initiative kind inside the workspace operating model, not a second architecture.

## Architecture Summary

- Odin's control plane owns durable workspace, initiative, companion, policy, memory, and work-item state plus the follow-through needed to keep work from being dropped.
- Runtime authority lives in SQLite at `data/odin.db`.
- Authored assets live in-repo as Markdown with frontmatter under `registry/`, `prompts/`, and `memory/`.
- CLI and API surfaces consume the same control-plane services; workers run in the execution plane through shared orchestration, policy, runtime, and executor contracts.
- Executors are model-agnostic execution lanes and route through one common contract, including plan-backed headless runners where they fit that contract.
- Tool, skill, and worker loading is dynamic and bounded by the resolved control context; Odin must not preload the full catalog into every run.
- Mutating work is isolated through leased worktrees and branches.
- Self-heal is deterministic, bounded, and auditable; self-improvement is proposal-driven, replay-tested, promotion-gated, and reversible.

## Canonical Documents

- `docs/adr/0001-canonical-authority.md` defines the system's source-of-truth model, scope model, and governance rules.
- `docs/adr/0002-migration-policy.md` defines how legacy assets from `odin-orchestrator` are classified and moved into this repo.
- `docs/contracts/odin-operating-model.md` defines Odin's durable product objects and the control-plane versus execution-plane boundary.
- `docs/contracts/ubiquitous-language.md` freezes the canonical operating-model vocabulary and narrows older runtime terms.
- `docs/contracts/repo-layout.md` defines package and folder responsibilities.
- `docs/contracts/phase-exit-criteria.md` defines the acceptance gate for Phase 00 and the baseline gate every later phase must satisfy.

## Current Status

Phase 00 through Phase 15 are in place, and the Phase 17 alpha stabilization pass has closed the minimum trust blockers from the reality audit. Fresh runtimes now bootstrap into an honest ready state, queued work can execute through one live `codex_headless` lane, runtime mutation is gated by transition and system-project policy checks, mutable work is forced through leased worktrees, `odin serve` runs bounded self-heal and queue execution loops, routing promotions require explicit promotion approval before activation, and service logs are newline-delimited JSON again. Full provider-backed execution and broader unattended orchestration remain deferred; see `docs/operations/alpha-readiness.md` for the current alpha operating envelope.

## Local Usage

To make `odin` available as a repeatable local command:

```bash
make build
make install-local
odin
```

This installs a symlink at `~/.local/bin/odin` pointing to this repo's built binary. Remove it with `make uninstall-local`.
