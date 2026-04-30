---
title: Odin OS Brownfield Risk Register
status: draft
date: 2026-04-30
---

# Odin OS Brownfield Risk Register

| ID | Risk | Severity | Evidence | Impact | Recommendation |
| --- | --- | --- | --- | --- | --- |
| R01 | Dirty worktree contains uncommitted scaffold and unrelated modifications. | High | `git status --short --branch` shows modified `.gitignore`, `Makefile`, `README.md`, `go.mod`, `go.sum`, knowledge-package files, integration test, plus untracked TS, Go scaffold, PDF testdata, and a root `--help` file. | Future agents may build on accidental artifacts. | Freeze implementation, classify changes, remove or promote intentionally. |
| R02 | TypeScript scaffold conflicts with Go-native Odin direction. | High | `src/`, `package.json`, `package-lock.json`, `tsconfig.json`, `eslint.config.js`, `tests/agency-scaffold.test.ts`. | Wrong runtime language and duplicate architecture may persist. | Remove or archive as reference-only before agency implementation. |
| R03 | Duplicate runner seams. | High | Established `internal/executors/*`; uncommitted `internal/runner/*`. | Splits executor policy, logs, and safety checks. | Keep `internal/executors` as canonical; move any useful runner code there. |
| R04 | Real `codex exec` runner is absent. | High | `internal/executors/codex/adapter.go` is deterministic alpha; `internal/runner/codexexec` is placeholder. | Agency cannot do real AI implementation work yet. | Implement `codex_exec` behind `internal/executors/contract` with explicit security policy. |
| R05 | Worker security policy is not enforced in canonical executor path. | High | No canonical check in `internal/executors/codex` for root or danger-full-access; uncommitted `internal/security` is separate. | Real workers could launch unsafe modes if added naively. | Add policy checks to execution path before real subprocess launch. |
| R06 | GitHub intake and PR integration are not implemented. | High | Only uncommitted `internal/tracker/github` placeholder; docs identify missing intake/PR manager. | 24/7 issue-to-PR loop cannot run. | Implement read-only intake first; PR creation later; keep GitHub projection-only. |
| R07 | Default `odin serve` port collides easily. | Medium | `ODIN_HTTP_ADDR` default is `127.0.0.1:9443`; audit observed bind error on default port. | Operators may misread service health if another process owns the port. | Improve error message and document/allow ephemeral or configured ports. |
| R08 | Top-level help is missing. | Medium | `./bin/odin --help` and `./bin/odin help` return unknown command. | Operator discovery is poor. | Add top-level usage output through lifecycle dispatch. |
| R09 | Config root duplication. | Medium | Active `config/`; uncommitted `configs/`. | Operators and agents may edit wrong files. | Keep `config/`; remove or merge examples into docs or `config/*.example.yaml`. |
| R10 | systemd service lacks hardening. | Medium | `deploy/systemd/odin.service` has no `NoNewPrivileges`, restricted paths, or explicit sandboxing. | Long-running daemon has broader host access than necessary. | Add hardening after confirming deployment mode. |
| R11 | `go test ./...` includes untracked `node_modules` Go package. | Medium | Audit output includes `odin-os/node_modules/flatted/golang/pkg/flatted`. | Local dependencies can pollute Go package graph. | Remove `node_modules`; ensure ignored generated dirs are absent in clean worktrees. |
| R12 | Storage names conflict with domain names. | Medium | `CONTEXT.md` says Work Item / Run Attempt; tables and many surfaces still say `tasks` / `runs`. | Confusing operator model and docs drift. | Keep storage compatibility; render canonical names at operator surfaces. |
| R13 | `internal/store/sqlite/store.go` is very large. | Medium | Store has thousands of lines and many domain methods. | Changes have wide review burden and lower locality. | Split by domain files inside same package, preserving transaction model. |
| R14 | Delivery profiles absent from active registry. | Medium | `odin work status` reports `delivery_profiles=0`; registry workflows lack `delivery_profile` tag. | Delivery workflow cannot select governed profiles yet. | Add one minimal delivery profile registry entry before scheduler work. |
| R15 | Plans can be mistaken for implemented behavior. | Medium | Many `docs/plans/*` describe future commands such as `odin workspace`, `odin knowledge`, `odin brief ceo`. | Agents may claim features exist from docs alone. | Audits and PRs must separate implemented, planned, and uncommitted states. |
| R16 | Existing provider adapters report capabilities but do not execute. | Medium | Static executors return `ErrNotImplemented`; only `codex_headless` runs. | Router may select paths that cannot run if config changes. | Mark non-live adapters unavailable until implemented or gate selection. |
| R17 | Policy config is placeholder. | Medium | `config/policies.yaml` contains Phase 01 placeholder comments. | Security expectations live in code/docs, not active config. | Either remove placeholder or wire a minimal policy config contract. |
| R18 | Agency docs may drift from brownfield reality. | Medium | `docs/ARCHITECTURE.md`, `docs/ROADMAP.md`, `docs/SECURITY.md` are new and aspirational. | Implementation may follow docs over working seams. | Reconcile docs after this audit; reference existing modules explicitly. |
| R19 | `.worktrees/` contains many local worktrees. | Low | `find .worktrees` shows many active worktree dirs. | Audits can accidentally scan unrelated branches or stale local state. | Keep `.worktrees/` ignored/local; avoid repo-wide scans into it unless intentional. |
| R20 | GitHub token and tradeboard token are env-based but not fully scoped in code. | Medium | Config uses `GITHUB_TOKEN`; `odin.env.example` has `ODIN_TRADEBOARD_API_TOKEN=`. | Future integrations may over-scope tokens. | Add minimal-token docs and startup validation before integrations. |
| R21 | Root file named `--help` can confuse scripts and cleanup. | Low | `git status --short` reports `?? --help`. | Shell commands may treat it as an option unless paths are escaped. | Remove intentionally in a cleanup ticket with `rm -- --help` after confirming it is generated junk. |

## Security-Specific Notes

Current hard rules from docs and context remain valid:

- No direct commits to default branch.
- No autonomous merge.
- No autonomous production deploy.
- Workers must not receive production secrets.
- Mutating work must use task-owned worktrees and branches.
- SQLite remains runtime authority.

The implementation already enforces some branch/worktree and transition rules in `internal/runtime/jobs/service.go`, but real external worker launch is not present. Do not assume worker sandboxing exists until `codex exec` launch code proves it.

## Highest Priority Risk Reductions

1. Remove uncommitted TypeScript scaffold and duplicate Go runner/config scaffolds.
2. Add top-level help and command discovery.
3. Add a minimal delivery profile registry entry.
4. Implement real `codex_exec` only after policy checks are in the canonical executor path.
5. Harden systemd before any 24/7 unattended deployment.
