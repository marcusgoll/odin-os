# Robinhood Transfer Review API Design

**Date:** 2026-04-20

## Goal

Add an Odin-owned Robinhood transfer workflow that uses a headed Huginn browser session to prepare a transfer up to the review screen, stops before the final submit click, requests explicit operator approval, and only performs the final submit after that approval is resolved through Odin.

## Current State

- Plaid Transfer production is externally blocked for the current Plaid org by product entitlement. The most recent Odin evidence shows `Transfer` gated behind `Upgrade plan`, not an accessible submission flow.
- The approved replacement direction is a Robinhood browser workflow with a final human confirmation before the last click.
- This must remain an `odin-os` feature. Family-Ops continues to use Odin only through the real `odin` CLI.

## What Already Exists

- Browser automation and trusted headed-session reuse:
  - `scripts/browser/browser-access.sh`
  - `scripts/browser/browser-auth.sh`
  - `scripts/browser/chrome-cdp-start.sh`
  - `scripts/browser/odin-huginn-server.js`
- Existing browser-driver pattern and adapters:
  - `scripts/drivers/plaid-transfer-application.sh`
  - `scripts/drivers/plaid-support-case.sh`
  - `internal/adapters/web/driver_common.go`
  - `internal/adapters/web/huginn_driver.go`
  - `internal/adapters/web/visual_driver.go`
  - `internal/tools/invocation/service.go`
- Durable runtime primitives already in use:
  - tasks/runs in `internal/store/sqlite`
  - approvals via `RequestApproval` / `ResolveApproval`
  - wake packets and resume state in `internal/runtime/checkpoints`
  - persisted transcripts and memory summaries recorded by run flows
- Real operator entrypoint:
  - REPL shell in `internal/cli/repl/shell.go`
  - existing run inspection in `/runs show`
  - existing pending approval listing in `/approvals`
- Existing API surface:
  - operational-only HTTP handler in `internal/api/http/operational.go`
  - `odin serve` already mounts the operational handler in `internal/app/lifecycle/run.go`

## What Is Partial

- Approval resolution exists below the shell:
  - `internal/store/sqlite/store.go` supports `ResolveApproval`
  - `scripts/ops/odin-n8n-ssh-dispatch.sh` already assumes a top-level `odin approvals resolve ...` command
- The REPL only lists approvals. It does not let the operator approve or deny them.
- Checkpoints already model `approval_wait`, but there is no workflow that uses approval wait to resume a real browser-side submit step.
- Driver adapters already decode deterministic JSON results, but there is no Robinhood money-movement driver yet.

## Gaps

1. No Robinhood transfer driver that can:
   - navigate to the transfer flow
   - fill fields to the review screen
   - capture review evidence
   - stop before submit
   - resume later for the final click
2. No Odin transfer orchestration service that:
   - prepares the transfer
   - records transcripts/memory
   - requests approval
   - stores a wake packet for resume
3. No operator-facing approval resolution path in the REPL.
4. No working top-level `odin approvals resolve ...` path even though repo-owned scripts already expect it.
5. No HTTP API for transfer preparation, transfer status, or approval resolution.

## Reuse Plan

- Reuse tasks, runs, approvals, transcripts, memory summaries, and wake packets as the durable workflow backbone.
- Reuse the existing browser helper stack instead of creating a second headed-browser subsystem.
- Reuse the existing web driver adapter pattern and `internal/tools/invocation/service.go` for deterministic script invocation.
- Reuse the existing approval table and events instead of creating a second transfer-specific approval store.
- Reuse `odin serve` and the current HTTP mux wiring by extending the existing HTTP surface rather than standing up another server.

## Recommended Approach

### 1. Keep the workflow durable without adding a new v1 intent table

For v1, the transfer intent should be represented by:

- the task key
- the latest run
- the approval row
- the latest approval-wait wake packet
- transcript and memory artifacts

This keeps the feature aligned with existing Odin runtime structures. A dedicated `transfer_intents` table is not justified until packet-backed status views prove insufficient.

### 2. Add a first-class Robinhood transfer orchestration service

Create `internal/runtime/transfers/service.go` as the Odin-owned orchestration layer. It should:

- accept a structured Robinhood transfer intent
- create or reuse a durable task/run record
- invoke a deterministic Robinhood browser driver in `prepare` mode
- record transcript + memory summary
- request approval when the driver reaches the review state
- write an `approval_wait` wake packet containing the facts needed to resume submit safely
- fail safe if the review page is not reached

The same service should also support `ResumeApproved` for the final click after approval.

### 3. Add a deterministic Robinhood driver with two explicit modes

Add a Robinhood driver script and adapter with:

- `mode=prepare`
  - authenticate via trusted headed session if needed
  - navigate to Robinhood transfer flow
  - populate the requested transfer fields
  - stop on the review screen
  - capture screenshot + current URL + summary facts
  - never click final submit
- `mode=submit`
  - require previously approved wake-packet facts
  - reattach to the same trusted browser state
  - verify the session is still on the expected review state
  - click final submit only after the approval has already been resolved
  - capture confirmation evidence

If resume cannot safely reattach to the prepared review state, the driver must fail safe and require a fresh prepare step instead of guessing.

### 4. Add the minimal operator surface

#### REPL

Add:

- `/transfer prepare key=value...`
- `/approvals resolve <approval-id> <approve|deny> because <reason...>`

Keep inspection on existing surfaces:

- `/approvals`
- `/runs`
- `/runs show <id>`

This avoids inventing a second status UI when the shell already has run detail and pending-approval inspection.

#### Top-level command

Add:

- `odin approvals resolve <approval-id> <approve|deny> <reason...>`

Reason: repo-owned scripts already assume this command exists. Making it real is cheaper and cleaner than inventing a second non-CLI resolver path.

### 5. Add an internal HTTP API that mirrors the same workflow

Extend the `odin serve` HTTP surface with:

- `POST /api/transfers/robinhood/prepare`
- `GET /api/transfers/tasks/{taskKey}`
- `POST /api/approvals/{approvalID}/resolve`

The HTTP handlers should call the same runtime services used by the shell and top-level command paths. The HTTP API is the contract; the REPL is the operator UX.

## Proposed Workflow

1. Operator enters the real Odin shell and selects `family-ops`.
2. Operator runs `/transfer prepare ...`.
3. Odin:
   - creates a task/run
   - runs the Robinhood driver in `prepare` mode
   - records transcript + memory
   - requests approval
   - writes an `approval_wait` wake packet with resume facts
4. Operator reviews evidence with `/runs show <run-id>` and `/approvals`.
5. Operator resolves approval with `/approvals resolve <id> approve because ...`.
6. Odin:
   - resolves the approval row
   - loads the wake packet
   - runs the Robinhood driver in `submit` mode
   - records the final submit result as a new run
7. Operator inspects the submit outcome with `/runs show <submit-run-id>`.

## API Contract

### `POST /api/transfers/robinhood/prepare`

Example body:

```json
{
  "project_key": "family-ops",
  "direction": "deposit",
  "amount_usd": "25.00",
  "source_account": "checking",
  "destination_account": "brokerage",
  "memo": "household auto-transfer test"
}
```

Example response:

```json
{
  "task_key": "robinhood-transfer-20260420-001",
  "run_id": 201,
  "approval_id": 17,
  "status": "pending_approval",
  "summary": "Robinhood transfer is prepared at the review screen and awaiting final approval.",
  "artifacts": {
    "review_url": "https://robinhood.com/...",
    "screenshot_path": "/var/odin/browser-state/robinhood-transfer-review.png"
  }
}
```

### `GET /api/transfers/tasks/{taskKey}`

Returns a packet-backed status view assembled from the task, latest run, latest wake packet, and any pending approval.

### `POST /api/approvals/{approvalID}/resolve`

Example body:

```json
{
  "action": "approve",
  "decision_by": "operator",
  "reason": "final confirmation before submit"
}
```

For resumable transfer approvals, the resolver should synchronously trigger the submit continuation and return the resulting run detail.

## New Additions

- `internal/runtime/transfers/service.go`
- `internal/runtime/transfers/service_test.go`
- `internal/runtime/approvals/service.go`
- `internal/runtime/approvals/service_test.go`
- `internal/api/http/transfers.go`
- `internal/api/http/transfers_test.go`
- `internal/api/http/approvals.go`
- `internal/api/http/approvals_test.go`
- `internal/adapters/web/robinhood_transfer_driver.go`
- `internal/adapters/web/robinhood_transfer_driver_test.go`
- `scripts/drivers/robinhood-transfer-flow.sh`
- `tests/integration/robinhood_transfer_flow_test.go`
- shell command support for `/transfer prepare` and `/approvals resolve`
- top-level `odin approvals resolve ...`

## Why New Additions Are Necessary

- Plaid Transfer production is externally blocked.
- Odin already has browser primitives, but it does not yet have a first-class money-movement workflow that pauses at review and resumes only after explicit approval.
- The approval system already exists, but the operator entrypoint to resolve approvals is incomplete.
- The HTTP API currently exposes only operational health, not workflow actions.

## Safety Rules

- The Robinhood driver must run headed, not headless.
- `prepare` must never click final submit.
- `submit` must require an already-approved approval record and a resumable wake packet.
- If the review state cannot be safely verified, Odin must stop and require a new prepare step.
- Approval denial must not click anything and must seal the wake packet as denied/stale.
- The workflow must keep evidence:
  - review screenshot
  - final confirmation screenshot if submit succeeds
  - URLs and summary facts in transcript/memory

## Real odin E2E Verification

Implementation is not complete until all of these pass:

1. Start the real service:

```bash
./bin/odin serve
```

2. Verify HTTP prepare:

```bash
curl -s -X POST http://127.0.0.1:8080/api/transfers/robinhood/prepare \
  -H 'Content-Type: application/json' \
  -d '{"project_key":"family-ops","direction":"deposit","amount_usd":"25.00","source_account":"checking","destination_account":"brokerage","memo":"test"}'
```

3. Verify the real operator shell:

```text
/project family-ops
/transfer prepare direction=deposit amount_usd=25.00 source_account=checking destination_account=brokerage memo=test
/approvals
/runs show active
/approvals resolve <approval-id> approve because final confirmation before submit
/runs show active
```

4. Verify the top-level resolver path expected by repo scripts:

```bash
./bin/odin approvals resolve <approval-id> approve final confirmation before submit
```

## Remaining Risks

- Robinhood selectors and review/submit layout are not yet audited, so the first driver iteration must be read-only and TDD-heavy until the real review state is stable.
- Approval resolution is currently only partial in the repo, so shell/top-level/API parity must be implemented together.
- If the review page cannot be safely resumed, submit must fail safe and require a new prepare step.

## Best operating rule going forward

Reuse tasks, runs, approvals, transcripts, memory summaries, and wake packets as the durable transfer workflow model. Add only the missing command, API, and Robinhood-driver surfaces needed to make “prepare now, approve later, submit only after approval” a real Odin-owned workflow.
