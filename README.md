---
title: Odin OS
phase: "13"
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

Phase 00 through Phase 13 are in place. The repository now includes the authority docs, the target directory scaffold, the Markdown registry system, the baseline SQLite runtime store, project governance, the interactive Odin shell, the model-agnostic executor architecture, the dynamic tool broker, structured context compaction with append-only wake packets, a task-owned Git worktree plus lease model for isolated multi-agent mutation, first-class observability surfaces for logs, health, metrics, incidents, recoveries, and operator projections, deterministic self-heal playbooks with bounded retries and escalation, a migration extractor that inventories `odin-orchestrator`, flags likely duplicates and backups, and can emit review-only draft registry assets under `state/migration/`, and an explicit project transition ladder with read-only shadow and compare modes, limited-action gating, and auditable cutover state. Broader orchestration and live provider execution remain for later phases.
