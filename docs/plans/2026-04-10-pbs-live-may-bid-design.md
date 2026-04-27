---
title: PBS Live May Bid Design
status: proposed
date: 2026-04-10
---

# PBS Live May Bid Design

## Goal

Make `odin-os` run a real May PBS bid workflow end-to-end:

1. collect live off-dates from Google Calendar
2. validate the live browser session through Huginn
3. generate a PBS preview from structured inputs
4. stop for explicit approval
5. resume and submit the bid into PBS
6. record verification and rollback metadata in Odin runtime state

## Current branch reality

This `odin-os` checkout now has the live driver contract and repo-local driver scripts:

- `ODIN_GOOGLE_CALENDAR_DRIVER` via `scripts/drivers/google-calendar-off-dates.sh`
- `ODIN_HUGINN_DRIVER` via `scripts/drivers/huginn-pbs-session.sh`

But the branch still does not have the rest of the May-bid stack:

- no bounded `pbs_submit_may_bid` runtime action
- no action-aware task persistence in the SQLite task model
- no approval service that requeues/resumes the specialized action
- no `ODIN_PBS_BID_BRIDGE` integration in `odin-os`
- no structured PBS bridge in `/home/orchestrator/pbs`
- no current-branch machine CLI path for creating/running/approving this workflow

So the missing work is no longer “tool access.” It is workflow orchestration and bridge integration.

## Approaches

### 1. Legacy file bridge

Write Google/Huginn results into `/var/odin/pbs-bid-overrides.json`, then let existing PBS autopilot read the file.

Pros:
- shortest path
- minimal code in `odin-os`

Cons:
- preserves hidden shared state
- weak auditability
- not a credible v2 Odin workflow

### 2. Odin-orchestrated bounded workflow with PBS bridge

`odin-os` owns the durable task, approval, resume, evidence, and audit trail. `pbs` exposes a structured bridge that accepts preview/submit JSON and performs the real PBS write.

Pros:
- matches the intended v2 architecture
- reuses proven PBS submit logic
- keeps approval and recovery state in Odin

Cons:
- requires coordinated changes in `odin-os` and `pbs`
- needs explicit action plumbing in the current branch

### 3. Full native Odin rewrite

Move recommendation and PBS submission logic fully into `odin-os`.

Pros:
- one system long term

Cons:
- duplicates mature PBS logic
- highest risk for the first live write
- unnecessary for the current goal

## Recommendation

Use approach 2.

`odin-os` should own orchestration and approvals. `pbs` should remain the bid engine and submit writer. The live drivers already prove the external tool boundary. The remaining work is to make the workflow durable and bounded.

## Target architecture

### 1. Baseline

The implementation should either:

- rebase this branch onto the already-merged harness CLI baseline on `main`, or
- port the equivalent operational command surface into this branch before starting May-bid work

Without that, the workflow can run internally, but the harness will not have a clean machine CLI to create tasks, inspect approvals, approve them, and observe final state.

### 2. Odin responsibilities

`odin-os` will:

- define a bounded action key: `pbs_submit_may_bid`
- allowlist that action for project `pbs`
- create tasks that persist `action_key`
- detect the action in the job runner
- invoke:
  - `google_calendar_off_dates`
  - `huginn_pbs_session`
  - `ODIN_PBS_BID_BRIDGE preview`
- request an approval and persist a resume payload
- on approval, requeue and resume the same workflow
- call `ODIN_PBS_BID_BRIDGE submit`
- record:
  - preview details
  - submit result
  - verification result
  - rollback metadata

### 3. PBS responsibilities

`pbs` will expose a structured bridge command that:

- accepts preview requests with explicit `off_dates`
- generates a recommendation without reading `/var/odin/pbs-bid-overrides.json`
- returns a preview payload that Odin can persist unchanged
- accepts submit requests anchored to the preview payload
- validates the active PBS session before mutation
- diffs live bids vs desired bids
- submits supported changes
- re-reads and verifies the final PBS state
- returns snapshot and rollback handles

### 4. Approval and resume model

The workflow is intentionally two-stage:

- run 1:
  - collect live evidence
  - build preview
  - create approval
  - compact checkpoint with a resume payload
  - finish run as `awaiting_approval`
- run 2:
  - approval service resolves the pending approval
  - task is requeued
  - job runner reloads the resume payload
  - PBS bridge `submit` executes
  - run finishes as `completed` or `failed`

No submit should occur without an Odin approval row and persisted resume payload.

### 5. Bounded action policy

`pbs_submit_may_bid` should be treated as a bounded limited action, not as a generic free-form task. That requires:

- manifest support for project policy `limited_actions`
- validation of known bounded action keys
- explicit allowlisting in `config/projects.yaml`
- transition-state authorization that only permits the action when `pbs` is in `limited_action` state with `pbs_submit_may_bid` allowed

### 6. Operator flow

The real operator flow should be:

1. `odin task run --project pbs --action pbs_submit_may_bid --title "Prepare and submit May bid" --json`
2. inspect the returned preview and pending approval
3. `odin approvals approve --id <approval_id> --by operator --reason "approved for live submit"`
4. run `odin serve` or let the service loop resume the queued task
5. inspect final run metadata for verification and rollback details

## Error handling

Fail closed on:

- missing or failed live drivers
- missing or stale Huginn/PBS session
- missing action allowlist
- missing preview payload
- session mismatch between preview and submit
- bridge response that requires unsupported `new_line` creation
- failed post-submit verification

If preview succeeds but checkpoint persistence fails, Odin should reject the pending approval immediately so no orphan approval remains.

## Testing strategy

### odin-os

- adapter tests for live driver JSON contracts
- invocation tests
- builtin tool tests
- new action unit tests for:
  - preview path
  - approval wait path
  - approval rejection path
  - resumed submit path
  - checkpoint persistence failure
- job service tests for action dispatch
- lifecycle CLI tests for task create/run/approval flows
- integration acceptance covering the full bounded workflow with fixture drivers and bridge

### pbs

- bridge tests for:
  - preview from structured off-dates
  - submit from prior preview payload
  - session mismatch failure
  - unsupported new-line creation failure
  - CLI exit behavior on structured failures

### live validation

One real run in project `pbs` with:

- real Google Calendar driver
- real Huginn session driver
- real PBS bridge
- real Odin approval
- real submit

Success means the final run records verification and rollback metadata, and the submitted bid is visible in PBS.
