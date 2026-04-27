# Robinhood Transfer Review API Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build an Odin-owned Robinhood browser transfer workflow that prepares a transfer up to the review screen, requests explicit approval, and only performs the final submit after the operator resolves that approval through the real `odin` command path.

**Architecture:** Reuse Odin’s existing tasks, runs, approvals, transcripts, memory summaries, and wake packets as the durable workflow model. Add a Robinhood transfer runtime service, a deterministic browser driver with `prepare` and `submit` modes, a minimal `/transfer prepare` shell command, approval resolution parity across REPL/top-level/API, and HTTP handlers mounted under `odin serve`.

**Tech Stack:** Go, Bash, Node.js, jq, SQLite, repo-local Huginn browser helpers, REPL shell, `odin serve` HTTP surface, real `odin` E2E verification.

---

### Task 1: Add failing tests for approval resolution entrypoints

**Files:**
- Modify: `internal/cli/repl/shell_test.go`
- Modify: `internal/app/lifecycle/run_test.go`
- Modify: `internal/cli/commands/help.go`
- Modify: `internal/cli/commands/commands_test.go`

**Step 1: Write the failing tests**

Add tests that expect:

- `/approvals resolve 17 approve because final confirmation` to be accepted by the REPL
- `odin approvals resolve 17 approve final confirmation` to be accepted by the top-level lifecycle runner
- help text to mention the new approval resolution syntax

Example assertions:

```go
if err := shell.HandleLine(ctx, "/approvals resolve 17 approve because final confirmation", &output); err != nil {
    t.Fatalf("HandleLine(/approvals resolve) error = %v", err)
}
if !strings.Contains(output.String(), "approval=17") {
    t.Fatalf("output = %q, want approval id", output.String())
}
```

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/cli/repl ./internal/app/lifecycle ./internal/cli/commands -run 'TestShellApprovalsResolve|TestRunApprovalsResolveCommand|TestInteractiveHelpIncludesApprovalsResolve'
```

Expected: FAIL because the shell only lists approvals and the top-level command path does not support `approvals resolve`.

**Step 3: Write minimal implementation**

Add parsing/help hooks only far enough to make the tests compile and fail for the real missing service behavior.

**Step 4: Run test to verify the compile path works**

Run the same command again.

Expected: still FAIL, but now on the missing runtime approval-resolution behavior instead of missing parser hooks.

**Step 5: Commit**

```bash
git add internal/cli/repl/shell_test.go internal/app/lifecycle/run_test.go internal/cli/commands/help.go internal/cli/commands/commands_test.go
git commit -m "test: lock approval resolution entrypoints"
```

### Task 2: Add failing tests for the transfer runtime prepare flow

**Files:**
- Create: `internal/runtime/transfers/service_test.go`
- Modify: `internal/runtime/checkpoints/service_test.go`

**Step 1: Write the failing test**

Create runtime transfer tests that expect `PrepareRobinhoodTransfer(...)` to:

- create a task + run
- invoke a driver result shaped like `review_ready`
- record a pending approval
- compact an `approval_wait` wake packet
- persist transcript + episode memory

Example shape:

```go
result, err := service.PrepareRobinhoodTransfer(ctx, PrepareParams{
    ProjectKey: "family-ops",
    Direction: "deposit",
    AmountUSD: "25.00",
    SourceAccount: "checking",
    DestinationAccount: "brokerage",
})
if err != nil {
    t.Fatalf("PrepareRobinhoodTransfer() error = %v", err)
}
if result.ApprovalID == 0 {
    t.Fatalf("ApprovalID = 0, want pending approval")
}
```

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/runtime/transfers ./internal/runtime/checkpoints -run 'TestPrepareRobinhoodTransferRequestsApprovalAndWritesWakePacket'
```

Expected: FAIL because no transfer runtime service exists yet.

**Step 3: Write minimal implementation**

Create the service skeleton and the minimal types/signatures needed for the test to compile.

**Step 4: Run test to verify it still fails for real behavior**

Run the same command again.

Expected: FAIL on missing orchestration behavior.

**Step 5: Commit**

```bash
git add internal/runtime/transfers/service_test.go internal/runtime/checkpoints/service_test.go internal/runtime/transfers/service.go
git commit -m "test: lock robinhood transfer prepare orchestration"
```

### Task 3: Implement the transfer runtime prepare service

**Files:**
- Create: `internal/runtime/transfers/service.go`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/runtime/checkpoints/service.go`

**Step 1: Keep the prepare test red**

Run:

```bash
go test ./internal/runtime/transfers -run 'TestPrepareRobinhoodTransferRequestsApprovalAndWritesWakePacket'
```

Expected: FAIL.

**Step 2: Write minimal implementation**

Implement `PrepareRobinhoodTransfer` so it:

- resolves the target project
- creates a task title like `Prepare Robinhood transfer review`
- starts a run with executor `robinhood_transfer`
- calls the Robinhood driver in `prepare` mode
- records transcript + episode memory like the jobs service
- requests approval
- compacts a wake packet using `TriggerApprovalWait`

Example shape:

```go
compaction, err := checkpoints.Service{Store: service.Store}.Compact(ctx, checkpoints.CompactParams{
    TaskID: task.ID,
    RunID:  &run.ID,
    Trigger: checkpoints.TriggerApprovalWait,
    Objective: "Resume Robinhood transfer submit after approval",
    BlockingReason: "awaiting operator approval before final submit",
    NextSteps: []string{"review evidence", "approve or deny final submit"},
})
```

**Step 3: Run the test to verify it passes**

Run:

```bash
go test ./internal/runtime/transfers -run 'TestPrepareRobinhoodTransferRequestsApprovalAndWritesWakePacket'
```

Expected: PASS

**Step 4: Run the focused storage/checkpoint suite**

Run:

```bash
go test ./internal/runtime/transfers ./internal/runtime/checkpoints ./internal/store/sqlite
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/runtime/transfers/service.go internal/store/sqlite/models.go internal/store/sqlite/store.go internal/runtime/checkpoints/service.go
git commit -m "feat: add transfer prepare runtime service"
```

### Task 4: Add failing tests for the Robinhood browser driver

**Files:**
- Create: `internal/adapters/web/robinhood_transfer_driver.go`
- Create: `internal/adapters/web/robinhood_transfer_driver_test.go`
- Create: `tests/integration/robinhood_transfer_flow_test.go`

**Step 1: Write the failing tests**

Add unit/integration coverage for:

- `mode=prepare` reaches a `review_ready` result and never claims submission
- `mode=submit` requires resume facts and returns `submitted` only after the explicit submit branch
- driver response `tool_key` and status validation follow the existing adapter pattern

Example request:

```go
request := web.RobinhoodTransferRequest{
    ToolKey: "robinhood_transfer_flow",
    Input: web.RobinhoodTransferInput{
        Mode: "prepare",
        Direction: "deposit",
        AmountUSD: "25.00",
        SourceAccount: "checking",
        DestinationAccount: "brokerage",
    },
}
```

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/adapters/web ./tests/integration -run 'TestRobinhoodTransferDriverInvoke|TestRobinhoodTransferFlowScriptPrepareStopsAtReview'
```

Expected: FAIL because the adapter and script do not exist.

**Step 3: Write minimal implementation**

Add the request/response structs and a driver skeleton that compiles.

**Step 4: Run test to verify it still fails on real behavior**

Run the same command again.

Expected: FAIL on missing driver/script behavior.

**Step 5: Commit**

```bash
git add internal/adapters/web/robinhood_transfer_driver.go internal/adapters/web/robinhood_transfer_driver_test.go tests/integration/robinhood_transfer_flow_test.go
git commit -m "test: lock robinhood transfer driver contract"
```

### Task 5: Implement the Robinhood driver and invocation hook

**Files:**
- Create: `scripts/drivers/robinhood-transfer-flow.sh`
- Create: `internal/adapters/web/robinhood_transfer_driver.go`
- Modify: `internal/adapters/web/driver_common.go`
- Modify: `internal/tools/invocation/service.go`
- Modify: `internal/tools/invocation/service_test.go`

**Step 1: Keep the driver tests red**

Run:

```bash
go test ./internal/adapters/web ./tests/integration -run 'TestRobinhoodTransferDriverInvoke|TestRobinhoodTransferFlowScriptPrepareStopsAtReview'
```

Expected: FAIL.

**Step 2: Write minimal implementation**

Implement the driver using the same deterministic JSON contract as the other web drivers.

The script should:

- source `browser-access.sh` and `browser-auth.sh`
- start a trusted headed session
- support `prepare` and `submit`
- return explicit states such as `review_ready`, `submitted`, `session_expired`, `resume_verification_failed`

Example response shape:

```json
{
  "status": "completed",
  "tool_key": "robinhood_transfer_flow",
  "summary": "Robinhood transfer review is ready and awaiting approval.",
  "artifacts": {
    "session_state": "review_ready",
    "review_url": "https://robinhood.com/...",
    "screenshot_path": "/var/odin/browser-state/robinhood-transfer-review.png"
  }
}
```

**Step 3: Run the focused driver suite**

Run:

```bash
go test ./internal/adapters/web ./tests/integration -run 'TestRobinhoodTransferDriverInvoke|TestRobinhoodTransferFlowScriptPrepareStopsAtReview|TestRobinhoodTransferFlowScriptSubmitRequiresResumeFacts'
bash -n scripts/drivers/robinhood-transfer-flow.sh
```

Expected: PASS

**Step 4: Commit**

```bash
git add scripts/drivers/robinhood-transfer-flow.sh internal/adapters/web/robinhood_transfer_driver.go internal/tools/invocation/service.go internal/tools/invocation/service_test.go
git commit -m "feat: add robinhood transfer browser driver"
```

### Task 6: Add failing tests for `/transfer prepare` and HTTP prepare/status

**Files:**
- Modify: `internal/cli/repl/shell_test.go`
- Create: `internal/api/http/transfers_test.go`
- Modify: `internal/app/lifecycle/run_test.go`

**Step 1: Write the failing tests**

Add tests that expect:

- `/transfer prepare direction=deposit amount_usd=25.00 source_account=checking destination_account=brokerage`
- `POST /api/transfers/robinhood/prepare`
- `GET /api/transfers/tasks/{taskKey}`

Example REPL assertion:

```go
if err := shell.HandleLine(ctx, "/transfer prepare direction=deposit amount_usd=25.00 source_account=checking destination_account=brokerage", &output); err != nil {
    t.Fatalf("HandleLine(/transfer prepare) error = %v", err)
}
if !strings.Contains(output.String(), "approval_id=") {
    t.Fatalf("output = %q, want approval id", output.String())
}
```

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/cli/repl ./internal/api/http ./internal/app/lifecycle -run 'TestShellTransferPrepare|TestTransfersHTTPPrepare|TestTransfersHTTPStatus'
```

Expected: FAIL because the shell command and HTTP handlers do not exist.

**Step 3: Write minimal implementation**

Add only enough parser/handler scaffolding for the tests to compile.

**Step 4: Run test to verify it still fails for missing runtime behavior**

Run the same command again.

Expected: FAIL on real missing logic.

**Step 5: Commit**

```bash
git add internal/cli/repl/shell_test.go internal/api/http/transfers_test.go internal/app/lifecycle/run_test.go
git commit -m "test: lock transfer prepare operator and http surfaces"
```

### Task 7: Implement `/transfer prepare` and HTTP prepare/status

**Files:**
- Modify: `internal/cli/commands/help.go`
- Modify: `internal/cli/repl/shell.go`
- Create: `internal/api/http/transfers.go`
- Create: `internal/api/http/router.go`
- Modify: `internal/app/lifecycle/run.go`

**Step 1: Keep the shell/API tests red**

Run:

```bash
go test ./internal/cli/repl ./internal/api/http -run 'TestShellTransferPrepare|TestTransfersHTTPPrepare|TestTransfersHTTPStatus'
```

Expected: FAIL.

**Step 2: Write minimal implementation**

Implement:

- `/transfer prepare key=value...`
- `POST /api/transfers/robinhood/prepare`
- `GET /api/transfers/tasks/{taskKey}`
- a shared HTTP router that mounts both operational handlers and the new workflow handlers

The status view should be assembled from:

- task
- latest run
- latest wake packet
- pending approval if present

**Step 3: Run the focused suite**

Run:

```bash
go test ./internal/cli/repl ./internal/api/http ./internal/app/lifecycle -run 'TestShellTransferPrepare|TestTransfersHTTPPrepare|TestTransfersHTTPStatus|TestServeMountsTransferHandlers'
```

Expected: PASS

**Step 4: Commit**

```bash
git add internal/cli/commands/help.go internal/cli/repl/shell.go internal/api/http/transfers.go internal/api/http/router.go internal/app/lifecycle/run.go
git commit -m "feat: add transfer prepare shell and http surfaces"
```

### Task 8: Add failing tests for approval resolution with submit continuation

**Files:**
- Create: `internal/runtime/approvals/service_test.go`
- Modify: `internal/cli/repl/shell_test.go`
- Create: `internal/api/http/approvals_test.go`

**Step 1: Write the failing tests**

Add tests that expect:

- approval resolution updates the approval row
- approved Robinhood transfer wake packets trigger a submit continuation
- denied approvals do not trigger browser submit
- `/approvals resolve ...` and `POST /api/approvals/{id}/resolve` both return the submit result

Example assertion:

```go
result, err := approvalsService.Resolve(ctx, ResolveParams{
    ApprovalID: approval.ID,
    Action: "approve",
    DecisionBy: "operator",
    Reason: "final confirmation",
})
if err != nil {
    t.Fatalf("Resolve() error = %v", err)
}
if result.SubmitRun == nil {
    t.Fatalf("SubmitRun = nil, want continuation run")
}
```

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/runtime/approvals ./internal/cli/repl ./internal/api/http -run 'TestResolveApprovalResumesRobinhoodSubmit|TestShellApprovalsResolve|TestApprovalsHTTPResolve'
```

Expected: FAIL because no approval runtime service exists.

**Step 3: Write minimal implementation**

Add the approval service skeleton and wire tests to it.

**Step 4: Run test to verify it still fails on real continuation behavior**

Run the same command again.

Expected: FAIL on submit continuation behavior.

**Step 5: Commit**

```bash
git add internal/runtime/approvals/service_test.go internal/cli/repl/shell_test.go internal/api/http/approvals_test.go internal/runtime/approvals/service.go
git commit -m "test: lock approval continuation behavior"
```

### Task 9: Implement approval resolution parity and submit continuation

**Files:**
- Create: `internal/runtime/approvals/service.go`
- Modify: `internal/cli/repl/shell.go`
- Modify: `internal/app/lifecycle/run.go`
- Create: `internal/api/http/approvals.go`

**Step 1: Keep the approval tests red**

Run:

```bash
go test ./internal/runtime/approvals ./internal/cli/repl ./internal/api/http -run 'TestResolveApprovalResumesRobinhoodSubmit|TestShellApprovalsResolve|TestApprovalsHTTPResolve'
```

Expected: FAIL.

**Step 2: Write minimal implementation**

Implement a shared approval runtime service that:

- calls `ResolveApproval`
- loads the latest wake packet via `checkpoints.Service`
- detects resumable Robinhood transfer facts
- starts a new submit run
- invokes the Robinhood driver in `submit` mode
- records transcript + memory summary

Example decision shape:

```go
if state.Trigger == checkpoints.TriggerApprovalWait && state.RunContext != nil && state.RunContext.Facts["resume_action"] == "robinhood_transfer_submit" {
    return service.resumeRobinhoodSubmit(ctx, approval, state)
}
```

**Step 3: Run the focused approval suite**

Run:

```bash
go test ./internal/runtime/approvals ./internal/cli/repl ./internal/api/http
```

Expected: PASS

**Step 4: Commit**

```bash
git add internal/runtime/approvals/service.go internal/cli/repl/shell.go internal/app/lifecycle/run.go internal/api/http/approvals.go
git commit -m "feat: resolve approvals through submit continuation"
```

### Task 10: Run focused verification and build

**Files:**
- Modify: none unless verification exposes another bug

**Step 1: Run the focused suite**

Run:

```bash
go test ./internal/runtime/transfers ./internal/runtime/approvals ./internal/cli/repl ./internal/api/http ./internal/adapters/web ./internal/tools/invocation ./tests/integration -run 'TestPrepareRobinhoodTransferRequestsApprovalAndWritesWakePacket|TestRobinhoodTransferDriverInvoke|TestRobinhoodTransferFlowScriptPrepareStopsAtReview|TestShellTransferPrepare|TestTransfersHTTPPrepare|TestResolveApprovalResumesRobinhoodSubmit|TestShellApprovalsResolve|TestApprovalsHTTPResolve'
```

Expected: PASS

**Step 2: Run syntax/build verification**

Run:

```bash
bash -n scripts/drivers/robinhood-transfer-flow.sh scripts/browser/browser-access.sh scripts/browser/browser-auth.sh
node --check scripts/browser/odin-huginn-server.js
go build -o ./bin/odin ./cmd/odin
```

Expected: PASS

**Step 3: Commit**

```bash
git add .
git commit -m "chore: verify robinhood transfer review workflow"
```

### Task 11: Verify the real `odin serve` HTTP path

**Files:**
- Modify: none unless live verification exposes a bug

**Step 1: Start the real service**

Run:

```bash
ODIN_DIR=/var/odin ODIN_ROOT=/var/odin ./bin/odin serve
```

Expected: service starts and binds the configured HTTP address.

**Step 2: Prepare a transfer through the real HTTP API**

Run:

```bash
curl -s -X POST http://127.0.0.1:8080/api/transfers/robinhood/prepare \
  -H 'Content-Type: application/json' \
  -d '{"project_key":"family-ops","direction":"deposit","amount_usd":"25.00","source_account":"checking","destination_account":"brokerage","memo":"test"}'
```

Expected: response includes `task_key`, `run_id`, `approval_id`, and `status=pending_approval`.

**Step 3: Inspect status through the real HTTP API**

Run:

```bash
curl -s http://127.0.0.1:8080/api/transfers/tasks/<task-key>
```

Expected: response includes the latest run summary and the pending approval.

### Task 12: Verify the real `odin` shell path

**Files:**
- Modify: none unless live verification exposes a bug

**Step 1: Run the operator shell**

Run:

```bash
ODIN_DIR=/var/odin ODIN_ROOT=/var/odin ./bin/odin
```

Then in the shell:

```text
/project family-ops
/transfer prepare direction=deposit amount_usd=25.00 source_account=checking destination_account=brokerage memo=test
/approvals
/runs show active
```

Expected: the prepare run stops at review, records evidence, and creates a pending approval.

**Step 2: Approve and submit through the real shell**

In the same shell:

```text
/approvals resolve <approval-id> approve because final confirmation before submit
/runs show active
```

Expected: the approval resolves, a submit continuation runs, and the final run records either submission confirmation or a safe failure requiring a fresh prepare.

**Step 3: Verify the top-level compatibility path**

Run:

```bash
ODIN_DIR=/var/odin ODIN_ROOT=/var/odin ./bin/odin approvals resolve <approval-id> approve final confirmation before submit
```

Expected: same submit continuation behavior as the REPL path.

Plan complete and saved to `docs/plans/2026-04-20-robinhood-transfer-review-api.md`. Two execution options:

**1. Subagent-Driven (this session)** - I dispatch fresh subagent per task, review between tasks, fast iteration

**2. Parallel Session (separate)** - Open new session with executing-plans, batch execution with checkpoints

**Which approach?**
