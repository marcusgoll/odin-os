---
title: Odin OS Threat Model
status: draft
date: 2026-04-30
---

# Odin OS Threat Model

## Scope

This model covers the current brownfield Odin OS repository after the initial
tracker, workspace, prompt, orchestration, PR/review, dashboard, tmux, and
deployment refactor slices. It is focused on the 24/7 software-development
orchestrator risk surface, not on a greenfield target.

## Security Objectives

- Do not expose production secrets to Codex workers, prompts, logs, issue text,
  artifacts, or external providers.
- Do not run workers as root.
- Do not allow Codex `danger-full-access` or equivalent sandbox bypass modes.
- Do not execute untrusted issue, prompt, tracker, or PR text as shell commands.
- Do not mutate the default branch directly.
- Do not autonomously merge pull requests or deploy to production.
- Keep mutable work isolated to task-owned branches and worktrees.
- Keep SQLite runtime state authoritative; GitHub and HTTP surfaces are adapters.
- Require authentication for administrative dashboard actions.
- Keep deployment human-approved and rollback-proven.

## Protected Assets

| Asset | Examples | Primary risk |
| --- | --- | --- |
| Source repositories | Odin OS and managed project Git roots | Direct branch mutation, unsafe worktree cleanup, malicious code execution |
| Runtime state | SQLite DB, leases, runs, events, approvals | State corruption, unapproved dispatch, stale recovery |
| Secrets | `GITHUB_TOKEN`, `ODIN_ADMIN_TOKEN`, OpenAI/Codex tokens, Google OAuth values, browser tokens | Prompt/log leakage, worker exfiltration, over-scoped API writes |
| Operator controls | CLI, REPL, dashboard admin endpoints, systemd service | Unauthorized kill switch, pause/resume, dispatch, or deploy |
| External adapters | GitHub, Codex CLI, browser/Huginn, Google, n8n scripts | Injection through external data or credentials |
| Deployment | systemd units, Docker image, env files, release directories | Root execution, secret baking, unsafe restart or rollback |

## Actors

| Actor | Trust level | Notes |
| --- | --- | --- |
| Human operator | trusted | Must approve merge and production deploy. |
| Odin daemon | trusted but constrained | Should run as non-root with limited env and filesystem access. |
| Codex worker | semi-trusted | Must receive task context, not production secrets; must be sandboxed. |
| GitHub issue author | untrusted | Issue title/body/labels are prompt-injection inputs. |
| Pull request author | untrusted unless explicitly trusted | CI must not expose secrets to forked or untrusted code. |
| Browser/session drivers | semi-trusted | Often hold live web session state or tokens. |
| Legacy migration sources | untrusted/reference-only | Must not be treated as current runtime authority. |

## Trust Boundaries

1. GitHub API to tracker intake: external issue text enters Odin state.
2. Runtime state to prompt renderer: persisted titles, bodies, and metadata enter
   worker prompts.
3. Prompt renderer to executor: untrusted natural-language text crosses into a
   subprocess-capable worker lane.
4. Executor to shell driver: Go code launches scripts and passes environment.
5. Dashboard HTTP to admin actions: network clients can request kill switch or
   issue state changes.
6. CLI/operator scripts to deployment: shell wrappers mutate systemd files,
   Docker state, or service runtime.
7. Worktree cleanup to filesystem: leases authorize deletion-like git worktree
   removal.

## Current Controls

- `internal/api/http/operational.go` protects admin actions with constant-time
  token comparison and disables admin actions when `ODIN_ADMIN_TOKEN` is absent.
- `internal/vcs/worktrees/manager.go` validates cleanup paths stay under the
  configured worktree root and rejects cleanup of the root itself.
- `internal/vcs/git/adapter.go` uses `exec.CommandContext` with explicit git
  arguments rather than concatenated shell commands.
- `internal/tracker/github/client.go` uses GitHub tokens only for API requests,
  supports dry-run no-write behavior, and redacts token-like strings in adapter
  errors.
- `internal/prompts/renderer.go` rejects path traversal in template names and
  blocks implementation prompts that lack brownfield guardrails.
- `deploy/systemd/odin-os.service` is a user unit with hardening options.
- `deploy/docker/Dockerfile` and `deploy/docker/docker-compose.yml` run the
  container as non-root and avoid baking secrets into the image.
- `.github/workflows/ci.yml` uses `pull_request`, not `pull_request_target`, and
  does not reference repository secrets.

## Current Blockers

The following issues must be fixed before Odin OS can safely run real
unattended Codex implementation workers:

1. Codex `danger-full-access` is now blocked in the live driver and Go runner;
   keep any parallel worker launch path from reintroducing sandbox bypass flags.
2. Prompt-command extraction is now blocked in the live Codex driver; keep any
   parallel runner from treating issue or prompt text as shell.
3. Canonical Codex execution inherits the full daemon environment when launching
   the driver.
4. Structured logging writes sensitive field values verbatim.
5. HTTP capability invocation is not authenticated at the route level and can
   pass with empty caller identity for unpermissioned capabilities.
6. GitHub issue text and persisted intake metadata can enter prompts without a
   strict untrusted-data envelope.

## Non-Goals

- This review does not approve autonomous merge or autonomous deploy.
- This review does not certify live production deployment.
- This review does not implement a new runner, tracker, dashboard, or
  orchestrator loop.
- This review does not remove legacy scripts or compatibility paths.
