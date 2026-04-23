---
title: Verification Model
status: active
date: 2026-04-17
---

# Verification Model

Odin proves behavior in layers, but operator-visible truth comes from the real runtime interface.

Passing internal tests is necessary and often fast feedback. It is not sufficient proof for user-visible CLI behavior, long-running orchestration, backup and recovery flows, governance enforcement, or any change whose correctness depends on real command wiring.

## Core rules

- Prefer TDD where practical. Start with a failing test or reproducer before the implementation change.
- Use the cheapest test that can prove the specific claim, then stop adding lower-value duplication.
- Treat internal-only confidence as provisional when the operator experiences the feature through `odin`.
- For user-visible or orchestration behavior, Definition of Done requires real command execution through the repo-owned `odin` binary.
- Verification output must separate what is proven from what is still unproven.

## Proof layers

### Unit

Unit tests cover package-local logic with tight scope and fast feedback.

Use unit tests for:

- parsing
- validation
- routing decisions
- policy evaluation
- state transitions
- pure transforms
- error mapping

Unit tests do not prove command wiring, process execution, or runtime side effects.

### Contract

Contract tests protect authored and machine-readable boundaries from drift.

Use contract tests for:

- config and registry formats
- Markdown frontmatter contracts
- JSON or YAML payload shapes
- driver stdin and stdout envelopes
- projection and event shapes
- durable file layout expectations

Contract tests prove compatibility of the boundary. They do not prove that an operator flow works through `odin`.

### Integration

Integration tests exercise multiple owned packages together with real infrastructure where practical, usually through temp runtime roots, temp Git repos, SQLite, real file IO, or subprocesses.

Use integration tests for:

- bootstrap and runtime startup
- health and projection freshness
- governance and transition enforcement
- worktree and lease behavior
- executor routing and task execution services
- backup services
- recovery services

Integration tests prove that Odin internals compose correctly. They still do not replace command-level proof when the feature is consumed through the CLI.

### End-to-end

End-to-end verification runs the real repo-owned `odin` command path and inspects the actual operator-visible result.

Use E2E verification for:

- interactive shell behavior
- operational commands such as `doctor`, `healthcheck`, `backup`, `restore`, and `verify-backup`
- long-running orchestration through `odin serve`
- any future command that creates, mutates, or reports user-visible runtime state
- install and invocation flows where symlinked or built binaries matter

E2E tests and command checks must assert:

- exit status
- stdout or stderr contract where visible
- durable runtime side effects
- observable logs, projections, or stored state when relevant

## Command-level E2E requirements

Real `odin` execution is required whenever a change affects at least one of these:

- command output a human or machine consumes directly
- CLI argument parsing or dispatch
- REPL interaction or shell state
- service-loop orchestration
- task execution, recovery, or wake-packet behavior
- backup, restore, or verification flows
- project governance, approvals, or transition enforcement
- installation or repo-root resolution behavior

The minimum command-level proof is:

1. build or use the repo-owned `odin` binary
2. execute the real command against a controlled runtime root
   If the command mutates repo-authored config or registry content, use a copied repo root as well; `ODIN_ROOT` only isolates runtime state.
3. assert the visible result and the relevant runtime side effects

Examples:

- `odin doctor --json` must prove structured output and honest degraded or healthy reporting
- `odin healthcheck` must prove ready or not-ready exit behavior
- `odin` with scripted stdin must prove interactive shell behavior
- `odin serve` must prove startup, shutdown, and orchestration side effects
- `odin backup`, `odin verify-backup`, and `odin restore` must prove archive creation, verification, and usable restored state

If a feature is only real when invoked through `odin`, an internal service test alone does not close the work.

## Failure-path requirements

Every new or changed user-visible flow needs at least one explicit failure-path check at the highest meaningful layer.

Prefer real command failure checks when the failure is operator-visible. Examples:

- invalid CLI usage or unsupported arguments
- degraded runtime health
- denied mutation because governance policy blocks it
- missing backup archive or invalid backup contents
- unavailable executor or unhealthy driver
- interrupted `serve` session that must leave recoverable state
- invalid authored contract detected during startup or command execution

If the failure is purely internal and cannot surface directly through the runtime interface, integration or unit coverage is sufficient.

## Regression expectations

Odin uses a practical solo-builder pyramid:

- many unit tests
- targeted contract tests
- focused integration tests
- a small number of high-value command-level E2E checks

Regression policy:

- Every bug fix starts with a failing reproducer where practical.
- If the bug was operator-visible, the regression suite must include a command-level reproducer or an acceptance test that builds and runs `odin`.
- If a contract changes, the contract document and its contract tests change in the same work.
- If a user-visible runtime flow changes, update or extend the nearest existing E2E rather than creating redundant parallel suites.
- `make test`, `make build`, and `make test-alpha` remain the baseline repo gates. Targeted real-command checks are added on top when a change touches an operator flow.

## Definition of Done

Work is done only when all applicable items below are true:

- unit coverage exists for the logic that changed
- contract coverage exists for any authored or machine boundary that changed
- integration coverage exists for the owned services or runtime composition that changed
- command-level E2E proof exists for every changed user-visible or orchestration-facing behavior
- at least one relevant failure path is covered
- docs are updated when the operator contract changed
- verification results clearly separate proven behavior from unproven behavior

The following are not sufficient for done on their own:

- passing package tests
- passing mocked service tests
- manually reasoning that command wiring should work
- proving only happy paths for an operator-visible flow

## Verification report format

Every implementation summary, PR description, or ship note should end with a short proof report.

Use this structure:

### Proven

- behaviors directly demonstrated by tests or real `odin` commands

### Unproven

- behaviors not exercised yet
- external dependencies intentionally stubbed or deferred
- risks that still rely on reasoning rather than evidence

### Commands run

- exact test commands
- exact `odin` commands used for runtime proof

If the real `odin` path could not be exercised for a user-visible change, the work is not done. It is at best partially verified.
