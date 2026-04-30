---
title: Odin OS Agency Orchestrator Security
status: draft
date: 2026-04-30
---

# Odin OS Agency Orchestrator Security

## Non-Negotiable Rules

- No direct commits to `main` or any default branch.
- No autonomous merge.
- No autonomous production deploy.
- No production secrets exposed to Codex workers.
- Every mutating worker gets one issue-derived Work Item and one worktree.
- Human review is required before merge or production deployment.
- All work must be resumable after server restart.
- Logs must be structured.
- Dry-run mode must exist before unattended dispatch.
- Kill switch must exist before unattended dispatch.
- Workers must not run as root.
- Codex must not run in danger-full-access mode.

## Authority Boundary

Odin SQLite runtime state is the authority for dispatch, retries, approvals, Work Item state, Run Attempt state, Delivery Gate evidence, and worktree leases.

GitHub Issues, labels, comments, and pull requests are intake and projection surfaces. They must not outrank Odin runtime state.

Codex workers are executor lanes. They must not decide merge, production deployment, or approval state.

## Process And User Policy

The daemon and workers must run as a non-root user.

Before launching a worker, Odin must record:

- Run Attempt id
- project key
- worktree path
- task branch
- executor key
- effective uid/gid when available
- sandbox and approval policy
- command argv

Worker launch must fail closed when:

- effective uid is root
- requested Codex sandbox mode is danger-full-access
- requested approval policy bypasses Odin approval boundaries
- worktree path is missing or points at the canonical repo root

## Codex Policy

The v1 runner uses `codex exec` through a Go subprocess adapter.

Allowed:

- workspace-scoped execution
- non-root worker user
- explicit worktree cwd
- structured stdout/stderr capture
- timeout and context cancellation

Forbidden:

- danger-full-access mode
- production secret injection
- default-branch mutation
- merge commands
- production deployment commands
- hidden credential store mutation

The future app-server runner must use authenticated local transports only and remain behind the executor interface.

## Secret Handling

Workers may receive only scoped development credentials required for the current managed project and Work Item.

Forbidden in worker context:

- production environment files
- production deployment credentials
- customer data exports
- broad cloud credentials
- unrestricted GitHub tokens
- host-level Odin secrets unrelated to the current Run Attempt

Structured logs, events, summaries, and artifacts must redact configured secret patterns before persistence.

## Git And Workspace Policy

All mutating work must happen on task-owned branches in mutable worktrees outside the canonical repo root.

Workers must not:

- commit directly to default branches
- mutate protected branches
- merge pull requests
- force push unless project policy explicitly permits it and a human operator approves it
- run destructive cleanup against the canonical repo root

Worktree cleanup must preserve evidence and must never remove a canonical project repository.

## Command Policy

Each managed project declares allowed command policy through Odin project governance and executor configuration.

Commands that require denial or explicit approval:

- production deploy commands
- secret inspection commands
- destructive Git commands
- host-level package or service mutation outside the project worktree
- credential store mutation
- broad network exfiltration or upload commands
- commands requiring root privileges

## Network Policy

Default network access should be limited to:

- GitHub APIs needed for issue and PR workflows
- package registries needed by the project
- approved test endpoints
- OpenAI/Codex endpoints used by the configured executor lane

Network destinations used by workers should be logged with host, protocol, Run Attempt, and Work Item identifiers.

## Dry-Run Mode

Dry-run mode may:

- poll intake sources
- classify issues
- render scheduler decisions
- render prompt previews
- report blocked reasons

Dry-run mode must not:

- create branches
- create worktrees
- start Codex workers
- create or update pull requests
- mutate GitHub labels or comments
- advance Delivery Gates as completed

Any exception must be explicitly configured as a test fixture and visible in structured output.

## Kill Switch

Kill switch inputs:

- environment variable
- Odin config value
- sentinel file in the runtime root
- future operator command state

When active, Odin must:

- stop new agency dispatch
- stop intake mutation
- surface kill-switch state in status output
- drain or interrupt active workers according to configured policy
- record an event for the control state change

## Human Review Handoff

Human Review Handoff is not approval to merge or deploy.

A handoff may include:

- linked issue
- linked pull request
- implementation summary
- QA artifacts
- reviewer findings
- security findings
- remaining risks
- recommended human decision

Only a human operator may approve merge or production deployment.

## Logging And Audit

Every important runtime mutation must append an Odin event.

Structured logs must include:

- project key
- Work Item id
- Run Attempt id
- executor key
- role
- event type
- timestamp
- outcome
- worker uid/gid when available
- worktree path

Logs must not include raw secrets, production credentials, or unnecessary prompt context that contains sensitive data.

## Restart Safety

After restart, Odin must be able to reconstruct:

- claimed Work Items
- active or interrupted Run Attempts
- leased worktrees
- retry queue state
- pending handoffs
- current Delivery Gates
- active or interrupted worker processes

Startup recovery must record what it interrupted, resumed, abandoned, or escalated.

## Dashboard And API Policy

The Go dashboard/API is an operator visibility surface over Odin projections. It must not become a separate control plane.

V1 dashboard/API endpoints should be read-only unless a specific action is backed by the same approval and event path as the CLI.

## App-Server Runner Boundary

The future Codex app-server runner may persist thread, turn, and streamed event metadata as executor evidence.

It must not:

- replace Work Item state
- replace Run Attempt state
- bypass Odin approvals
- bypass worktree leases
- expose unauthenticated remote app-server transports
- leak app-server experimental protocol details into stable Odin domain terms
