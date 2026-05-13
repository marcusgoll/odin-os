---
title: Odin OS Security Checklist
status: draft
date: 2026-04-30
---

# Odin OS Security Checklist

Use this checklist before merging changes that touch runners, shims,
subprocesses, filesystems, GitHub tokens, secrets, dashboard controls,
deployment, or worker prompt flow.

## Blocker Checks

- [ ] The change does not introduce direct commits to `main`.
- [ ] The change does not introduce autonomous merge.
- [ ] The change does not introduce autonomous production deploy.
- [ ] Workers cannot run as root.
- [ ] Codex cannot run with `danger-full-access` or sandbox bypass flags.
- [ ] Untrusted issue, PR, prompt, or tracker text is never executed by a shell.
- [ ] Worker subprocesses receive an allowlisted environment, not `os.Environ`.
- [ ] Production secrets are not included in worker prompts, prompt metadata, or
      worker artifacts.
- [ ] Structured logs redact secret-looking keys and configured secret values.
- [ ] Dashboard/admin mutation endpoints require authentication.
- [ ] GitHub mutation code honors dry-run and uses least-privilege tokens.
- [ ] Filesystem cleanup is bounded by an approved runtime/worktree root.
- [ ] Deployment changes are human-approved and have rollback steps.

## Secrets And Tokens

- [ ] Env examples contain placeholders only.
- [ ] Real env files are ignored and documented as local-only.
- [ ] GitHub tokens are read from env only at the adapter boundary.
- [ ] GitHub token scopes are documented per operation.
- [ ] OpenAI/Codex tokens are not inherited by workers unless explicitly required
      by a safe lane.
- [ ] Browser, Google, and other live-session tokens are cached with `0600`
      permissions.
- [ ] Local credential env files such as `~/.odin-env` are owner-owned,
      non-symlink regular files with `0600` permissions and are parsed as data.
- [ ] Error messages and command summaries are redacted before persistence.

## Process Execution

- [ ] Go code uses `exec.CommandContext` with explicit args.
- [ ] Shell code does not use `bash -c` with untrusted text.
- [ ] Driver paths are validated as executable, trusted, and outside
      world-writable directories.
- [ ] Subprocesses have bounded timeouts and process-group cancellation where
      practical.
- [ ] Subprocesses run in task-owned worktrees or read-only roots appropriate to
      the task.
- [ ] No prompt instruction is treated as a security boundary.

## Filesystem And Git

- [ ] Worktree paths are deterministic and sanitized.
- [ ] Cleanup refuses paths outside the worktree root.
- [ ] Cleanup refuses the worktree root itself.
- [ ] Dirty worktree detection exists before cleanup or PR handoff.
- [ ] Git commands use explicit args, not shell-concatenated command strings.
- [ ] Default-branch mutation remains forbidden by project policy.

## Dashboard And HTTP

- [ ] Health/status endpoints do not expose secrets.
- [ ] Admin endpoints require `ODIN_ADMIN_TOKEN`.
- [ ] Token comparisons use constant-time comparison.
- [ ] Kill switch writes an auditable event and fails closed.
- [ ] Capability invocation routes are authenticated before they can invoke
      mutable or subprocess-backed capabilities.
- [ ] HTTP error responses avoid echoing secrets or raw driver stderr.

## GitHub And CI

- [ ] CI uses `pull_request`, not `pull_request_target`, unless a separate
      security review approves it.
- [ ] CI does not pass secrets to untrusted PR code.
- [ ] PR template requires proven/unproven evidence and command output.
- [ ] GitHub issue body/title are treated as untrusted prompt input.
- [ ] Tracker dry-run mode performs no API writes.
- [ ] Tracker errors redact token-like strings.

## Deployment

- [ ] systemd service runs as a user service or otherwise documents the non-root
      user.
- [ ] Docker image runs as non-root and does not bake secrets.
- [ ] Env files are mounted/provided at runtime only.
- [ ] Healthcheck exists and is exercised.
- [ ] Rollback path is documented and tested for live changes.
- [ ] Live restart/deploy requires human approval.
