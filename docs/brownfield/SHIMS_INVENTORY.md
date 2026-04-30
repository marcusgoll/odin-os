---
title: Odin OS Shims Inventory
status: draft
date: 2026-04-30
---

# Odin OS Shims Inventory

## Scope

The active checkout has no root `shims/` directory and no first-class Go shim package. This inventory treats thin shell wrappers, duplicate adapter seams, and migration tools as shim-like assets when they translate between Odin and another interface.

## Shell Shims And Scripts

| Path | Purpose | Upstream / downstream interface | Runtime dependencies | Risks | Typed Go adapter? | Recommendation |
| --- | --- | --- | --- | --- | --- | --- |
| `scripts/dev/backup-odin.sh` | Convenience wrapper for backups. | Operator shell -> `go run ./cmd/odin backup <archive>`. | Bash, Go toolchain, repo root. | Thin and safe, but slower than built binary; command path differs from installed `odin`. | No. Keep as dev wrapper. | Keep. |
| `scripts/dev/restore-odin.sh` | Convenience wrapper for restore. | Operator shell -> `go run ./cmd/odin restore <archive> <destination>`. | Bash, Go toolchain, filesystem write access. | Restore mutates filesystem; must remain explicit and operator-invoked. | No. Core restore stays in Go command. | Keep. |
| `scripts/dev/verify-backup.sh` | Convenience wrapper for backup verification. | Operator shell -> `go run ./cmd/odin verify-backup <archive>`. | Bash, Go toolchain. | Low. | No. | Keep. |
| `scripts/dev/install-local.sh` | Symlink built `bin/odin` into local bin. | Operator shell -> filesystem symlink. | Bash, `ln`, `~/.local/bin`, built binary. | Can point at stale binary if not rebuilt. | No. | Keep/refactor. Add docs to build first. |
| `scripts/dev/uninstall-local.sh` | Remove local `odin` symlink. | Operator shell -> filesystem remove. | Bash, `rm`. | Low; only removes configured symlink path. | No. | Keep. |
| `scripts/dev/install-systemd-service.sh` | Install and start user systemd service. | Operator shell -> copy service/env -> `systemctl --user enable --now`. | Bash, systemd user manager, `deploy/systemd/*`. | Starts long-running daemon; service hardening remains incomplete; env file may contain secrets later. | No. Deployment behavior should remain script plus systemd unit until Go install command is justified. | Refactor with security review before production use. |
| `scripts/ci/verify-pr-template.sh` | Validate PR body contract. | CI/local shell -> Markdown body file checks. | Bash, grep, awk. | Shell parser can drift from template. | Maybe later if PR validation becomes part of Odin CLI; not needed now. | Keep. |
| `scripts/tests/make-ci-target-test.sh` | Characterize Makefile CI target. | Shell test -> `make -n ci` output. | Bash, make, grep. | Brittle to harmless command reordering, but valuable. | No. | Keep. |
| `scripts/tests/verify-pr-template-test.sh` | Characterize PR template validator. | Shell test -> temp PR body files -> `verify-pr-template.sh`. | Bash, temp filesystem. | Low. | No. | Keep. |
| `scripts/migrate/extract-odin-orchestrator.go` | Legacy migration extractor CLI. | Operator/dev -> Go migration extractor -> docs/state output. | Go toolchain, `/home/orchestrator/odin-orchestrator` by default. | Reads phased-out repo; generated drafts may be mistaken for active assets. | Already Go. Keep as migration tool, not runtime adapter. | Keep/refactor. Make generated-output status explicit in docs. |

## Duplicate Or Shim-Like Go Seams

| Path | Purpose | Upstream / downstream interface | Runtime dependencies | Risks | Typed Go adapter? | Recommendation |
| --- | --- | --- | --- | --- | --- | --- |
| `internal/runner/runner.go` | Uncommitted generic runner interface for one role attempt in one worktree. | Agency service -> runner adapters. | Go context only. | Duplicates `internal/executors/contract`, splitting policy, logging, cancellation, resume, and security. | It should not become a separate adapter. Merge useful fields into `internal/executors/contract`. | Replace/remove. |
| `internal/runner/codexexec/adapter.go` | Placeholder wrapper around `codex exec`. | `internal/runner.Runner` -> future Codex CLI subprocess. | Codex CLI, process execution, worktree, sandbox policy. | Real execution absent; if implemented here it bypasses canonical executor routing. | Yes, but inside `internal/executors`, not `internal/runner`. | Replace into `internal/executors/codex_exec` after security review. |
| `internal/runner/appserver/adapter.go` | Placeholder Codex app-server runner. | `internal/runner.Runner` -> future app-server protocol. | Codex app-server, protocol glue, process/network lifecycle. | Experimental surface; duplicates executor seam. | Yes, later behind `internal/executors/contract`. | Replace later; keep phase-two only. |
| `internal/tracker/tracker.go` and `internal/tracker/github/client.go` | Placeholder GitHub issue tracker adapter. | Agency service -> GitHub Issues. | GitHub API/token later. | Duplicates intended `internal/adapters`/intake seam; not implemented. | Yes. One typed GitHub intake adapter is needed, but package root must be chosen first. | Replace/refactor into one canonical GitHub intake seam. |
| `internal/workspace/manager.go` | Placeholder workspace lease manager. | Agency service -> git worktree lease. | Filesystem/Git later. | Duplicates established `internal/vcs/worktrees` and `internal/vcs/leases`. | No separate adapter. Use existing VCS packages. | Replace/remove. |
| `internal/security/policy.go` | Placeholder worker policy checks. | Runner launch -> UID/sandbox validation. | OS UID and runner config later. | Useful checks are detached from canonical executor path. | Should become policy enforcement in executor launch path. | Refactor into canonical executor security review path. |
| `internal/prompts/renderer.go` | Placeholder prompt renderer interface. | Worker/runner -> prompt templates. | Prompt files later. | Useful seam but not wired; overlaps `src/prompts`. | Yes, if prompt assets remain repo-authored. | Refactor and connect to canonical prompt location. |
| `src/agents/index.ts`, `src/prompts/index.ts`, `package.json`, `tsconfig.json` | TypeScript agency scaffold. | Node/Vitest scaffold -> separate orchestration model. | Node 20, npm packages. | Conflicts with Go-native Odin-OS target and introduces parallel runtime. | No. | Remove after explicit cleanup approval. |
| `config/agency.example.yaml`, `configs/*.yaml` | Agency config examples. | Operator config -> scaffold runner settings. | YAML loaders later. | Duplicates active `config/` root and can mislead operators. | No separate adapter. Merge useful examples into `config/*.example.yaml` if needed. | Replace/remove. |

## Codex / Worker Launch Scripts

No active shell script launches Codex, tmux, or worker agents in this checkout. The only Codex-specific current assets are configuration/prompt placeholders and unimplemented Go runner stubs. Real `codex exec` must be implemented behind `internal/executors/contract` with security review.

## Shim Consolidation Recommendations

1. Keep shell scripts thin and operator-invoked.
2. Do not add new shims for Codex, GitHub, worktrees, or prompts until the existing executor/VCS/registry seams are exhausted.
3. Collapse `internal/runner` into `internal/executors`.
4. Collapse workspace management into `internal/vcs`.
5. Pick one GitHub intake adapter root before adding real GitHub API code.
6. Treat every subprocess, filesystem mutation, GitHub token, and worker-sandbox change as security-reviewed.
