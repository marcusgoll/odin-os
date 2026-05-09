---
title: ADR-0002 Worker Panic Retry Policy
status: accepted
date: 2026-05-09
---

# ADR-0002: Worker Panic Retry Policy

## Context

Odin OS now executes queued work through the shared runtime job service and the
executor contract. A panic inside an executor is contained by
`internal/runtime/jobs.runExecutorTask`, converted into a `worker panic in
executor "<key>"` error, and finalized by the normal run outcome path.

The current implementation retries only transient execution failures. The
transient classifier is intentionally narrow: context deadline exceeded, OS
timeouts, and temporary network errors. Panic-derived worker errors do not match
that class today.

The behavior is already characterized by tests:

- queued worker panics fail the task and run, write failure-analysis artifacts,
  recommend follow-up, and release worktree leases
- direct task worker panics follow the same terminal failure path
- transient executor failures retry with bounded backoff through the existing
  retry counter and max-attempts policy

## Decision

Panic-derived worker errors are terminal containment failures by default. Odin
must fail the run and work item, record the failure-analysis artifact, preserve
the failure for operator review, and release any active worktree lease. Odin must
not automatically retry a panic-derived worker error.

The reason is that a panic means Odin lost confidence in the worker boundary, not
that an external dependency briefly failed. Retrying the same panic
automatically can duplicate external side effects, hide a worker defect, or
stress a broken adapter loop.

Timeouts and temporary network errors remain retryable through the existing
transient-failure path. Admission failures such as worktree lease conflicts may
also retry later when the admission path explicitly classifies them as retryable.
Those paths are separate from panic containment.

## Future Policy Gate

A future executor may request policy-dependent panic retry only if a later ADR or
contract update defines all of the following:

- a stable typed failure code that distinguishes the panic class
- proof that the attempted work is idempotent or had no external side effects
- a bounded retry budget lower than or equal to the normal task max-attempts
  budget
- preservation of existing approval, transition, sandbox, and worktree policy
  gates
- tests proving the retry path cannot create duplicate executor entry

Until those conditions are documented and implemented, panic-derived worker
errors remain terminal.

## Consequences

Positive:

- Panic containment stays fail-closed and auditable.
- Operators see a durable failed run instead of silent retry loops.
- Existing approval and worktree safety gates remain unchanged.
- Worker defects are surfaced as defects to fix, not transient runtime noise.

Negative:

- Some recoverable adapter defects may require manual retry after a fix.
- A single panic can fail a task even if a later retry might have succeeded.

## Rejected Alternatives

### Retry panics as transient failures

Rejected because panics are not reliable evidence of a temporary dependency
outage. Automatic retry can duplicate work and obscure adapter defects.

### Block the task instead of failing it

Rejected because the run already started and produced a concrete execution
failure. Blocking is reserved for admission or approval states where execution
has not safely begun.

### Make panic retry executor-specific without an ADR

Rejected because executor-specific panic retry would fragment the shared runtime
contract and weaken operator predictability.
