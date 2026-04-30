---
title: Odin OS Codex Runner Consolidation
status: draft
date: 2026-04-30
---

# Odin OS Codex Runner Consolidation

## Current Codex Invocation Inventory

| Path | Current behavior | Classification | Consolidation action |
| --- | --- | --- | --- |
| `internal/executors/codex/adapter.go` | Deterministic `codex_headless` executor; does not launch Codex CLI. | Keep as alpha executor. | Preserve behind `internal/executors/contract`. |
| `internal/runner/codexexec/adapter.go` | Compatibility shim plus stable `AgentRunner` for explicit `codex exec` command construction. | Consolidated in this slice. | Use for runner-facing compatibility while future work moves real execution into `internal/executors`. |
| `internal/runner/appserver/adapter.go` | Placeholder app-server runner returning `runner.ErrNotImplemented`. | Experimental. | Do not implement until Codex app-server ownership is decided. |
| `src/runner/codex-exec` and TypeScript scaffold references | Uncommitted TypeScript scaffold. | Duplicate runtime direction. | Remove or archive in a cleanup ticket; do not wire into Odin runtime. |
| `config/agency.example.yaml`, `configs/*.yaml` | Example references to `codex_exec.command`. | Reference-only until config root is reconciled. | Merge useful settings into canonical `config/` only after config decision. |
| Shell scripts under `scripts/` | No active Codex launch scripts found. | No Codex runner path. | Keep scripts thin; do not add shell Codex launches. |
| `.github/workflows/ci.yml` | Runs Go/CI checks; no Codex launch. | No Codex runner path. | No action. |
| Tmux/session design docs | Describe future/adopted live sessions; no active launch implementation. | Design-only. | Keep tmux as liveness/attach surface, not runner authority. |

## Canonical Interface

`internal/runner/runner.go` defines `AgentRunner` for compatibility with agency-style role execution:

```go
type AgentRunner interface {
	Run(ctx context.Context, request Request) (Result, error)
}
```

The current stable Codex CLI runner is `internal/runner/codexexec.NewAgentRunner(...)`.

Important boundaries:

- It builds `codex exec` using explicit args.
- It does not concatenate shell strings.
- It rejects `danger-full-access`.
- It supports dry-run without invoking Codex.
- It carries explicit timeout into the command executor.
- It redacts configured secrets and common token assignments from returned summaries.

## Remaining Cleanup Issues

1. Move real Codex CLI execution under `internal/executors` once executor security policy is complete.
2. Wire canonical config from `config/`, not `configs/`, into the runner.
3. Decide whether `internal/runner` remains as a compatibility facade or is fully collapsed into `internal/executors`.
4. Remove the TypeScript runner scaffold after explicit cleanup approval.
5. Keep app-server deferred until `codex exec` is proven and app-server protocol details can remain outside durable Work Item state.
6. Add structured log redaction before runner output is written to service logs.
7. Add integration proof only after an operator-approved environment has Codex CLI available and a safe disposable worktree.
