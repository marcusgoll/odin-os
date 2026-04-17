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
- CLI, API, and worker execution all resolve through shared control-plane orchestration, policy, runtime, and executor contracts.
- Executors are model-agnostic execution lanes and route through one common contract, including harness-driver-backed headless runners where they fit that contract.
- Tool, skill, and sub-agent loading is dynamic and scope-aware; Odin must not preload the full catalog into every task context.
- Mutating work is isolated through task-owned leased worktrees and branches.
- Self-heal is deterministic, bounded, and auditable; self-improvement is proposal-driven, replay-tested, promotion-gated, and reversible.

## Canonical Documents

- `docs/adr/0001-canonical-authority.md` defines the system's source-of-truth model, scope model, and governance rules.
- `docs/adr/0002-migration-policy.md` defines how legacy assets from `odin-orchestrator` are classified and moved into this repo.
- `docs/contracts/odin-operating-model.md` defines Odin's durable product objects and the control-plane versus execution-plane boundary.
- `docs/contracts/ubiquitous-language.md` freezes the canonical operating-model vocabulary and narrows older runtime terms.
- `docs/contracts/repo-layout.md` defines package and folder responsibilities.
- `docs/contracts/phase-exit-criteria.md` defines the acceptance gate for Phase 00 and the baseline gate every later phase must satisfy.
- `docs/contracts/skill-lifecycle.md` defines the canonical skill contract, CRUD rules, discovery model, and Codex maintenance workflow.

## Current Status

Phase 00 through Phase 15 are in place, and the Phase 17 alpha stabilization pass has closed the minimum trust blockers from the reality audit. Fresh runtimes now bootstrap into an honest ready state when a harness driver is configured, queued work can execute through harness-backed `codex_headless` and `claude_code_headless` lanes, runtime mutation is gated by transition and system-project policy checks, mutable work is forced through leased task-owned worktrees, `odin serve` runs bounded self-heal and queue execution loops, routing promotions require explicit promotion approval before activation, and service logs are newline-delimited JSON again. Full provider-backed execution and broader unattended orchestration remain deferred; see `docs/operations/alpha-readiness.md` for the current alpha operating envelope.

## Local Usage

To make `odin` available as a repeatable local command:

```bash
make build
make install-local
odin help
odin status --json
odin skills list --json
# replace YOUR_PROJECT_KEY with a non-system project key from config/projects.yaml
odin task run --project YOUR_PROJECT_KEY --title "smoke"
odin repl
```

This installs a symlink at `~/.local/bin/odin` pointing to this repo's built binary. Remove it with `make uninstall-local`.

## Skill maintenance

Skills are canonical registry assets under `registry/skills/*.md`. The recommended maintenance path is the repo-owned CLI:

```bash
odin skills list --json
odin skills get triage-skill --json
odin skills create --spec /tmp/echo-skill.json --json
odin skills invoke echo-skill --input '{"message":"hello"}' --json
odin skills update echo-skill --spec /tmp/echo-skill-v2.json --json
odin skills delete echo-skill --json
```

Use that path for lifecycle operations so authored files, validation, runtime discovery, and invocation stay aligned.

Command-backed skills execute through the restricted wrapper. The wrapper runs handlers from the repo root, strips inherited environment variables down to the allowlisted execution context, and records `restricted_command_v1` on invoke. Handlers that resolve outside `scripts/skills/` are rejected.

Skill invocation now enforces the declared permission model:

- `repo.read` and `runtime.read` are allowed in global scope
- mutating permissions require project-backed scope
- isolated mutations require a matching `limited_action` allowlist
- governance, destructive, and system-project mutations can require explicit approval before the handler runs

Typical gated workflow:

```bash
odin project select alpha-cli
odin transition set limited_action allow=docs_audit_note confirm because "skill maintenance"
odin skills invoke isolated-skill --input '{"message":"hello"}' --json
```
