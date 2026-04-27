# Robinhood Live Transfer Submit Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Reconcile the proven finance prepare/approval seam into the main `odin-os` checkout, then add a real Robinhood transfer driver so Marcus can run principal-attended Family-Ops transfers through the real `odin` shell/top-level command path.

**Domain Source of Truth:** `CONTEXT.md`, `docs/adr/0001-canonical-authority.md`, `docs/adr/0002-migration-policy.md`, `docs/contracts/live-driver-tools.md`, `docs/plans/2026-04-20-robinhood-transfer-review-api.md`, `docs/plans/2026-04-20-robinhood-transfer-review-api-design.md`

**Context:** Family-Ops Finance workflow with a boundary crossing into Workspace Integration / Browser Control

**Owns / Does Not Own:** This slice owns Family-Ops Robinhood transfer workflow semantics, finance runtime orchestration, finance operator surfaces, transfer run evidence, and continuity-to-status mapping. It does not own Robinhood credential/bootstrap capability, generic browser-control taxonomy, or downstream settlement/reconciliation beyond confirmed Robinhood request acceptance.

**Invariants:**
- A material change to amount, **Transfer Direction**, **Funding Account**, or **Robinhood Account** creates a new **Transfer Intent**.
- Unchanged stale execution context does not create a new **Transfer Intent**; it forces a fresh prepare/evidence/approval cycle.
- `submitted` means confirmed Robinhood acceptance, not merely a submit click attempt.
- If approved continuity becomes unusable, the historical **Approval Request** stays `approved`, the governing **Work Item** becomes `blocked` with `blocked_reason=stale_context`, the old approved wake packet is sealed immediately, and no replacement active wake packet is created until fresh prepare.
- A transfer **Run Attempt** exposes exactly one primary canonical driver-evidence state; `prior_session_state` is optional, same-run only, artifact-local only, and v1-closed to recovered `session_expired`.
- Live auth/MFA/login remain principal-attended operator checkpoints; this slice must not automate hidden credential handling.

**Architecture:** First port the already-proven finance prepare/resolve slice from the isolated finance worktree into the canonical main checkout, but align it immediately to the now-locked glossary rather than copying older wording verbatim. Then add a dedicated Robinhood transfer driver adapter and deterministic script harness on the shared browser-control stack, persist driver evidence as run-local artifacts, and wire submit continuation to either confirmed submission or the locked stale-context path. Keep the shell/top-level `odin` surface canonical and defer broader HTTP transfer APIs until the live CLI workflow is stable in main.

**Tech Stack:** Go, SQLite, REPL shell, top-level `odin` lifecycle runner, runtime checkpoints/wake packets, shared web-driver adapter pattern, Bash driver scripts, deterministic integration tests, real `./bin/odin` E2E checks

---

## Context Mapping

- `Context:` Family-Ops Finance / Robinhood transfer workflow
- `Owns:` transfer prepare orchestration, approval pause/resolution, submit continuation, transfer run evidence, finance-specific shell/top-level receipts, stale-context classification
- `Depends on:` `internal/runtime/checkpoints`, `internal/runtime/runs`, `internal/store/sqlite`, `internal/adapters/web`, `internal/tools/invocation`, shared browser helper scripts, existing project registry enrollment for `family-ops`
- `Does not own:` workspace browser bootstrap/readiness, generic Browser Control tool naming, generic auth/session lifecycle outside finance execution evidence, settlement/reconciliation after Robinhood acceptance
- `Boundary crossings:` finance runtime -> invocation service -> Robinhood transfer driver -> shell scripts/browser helper stack; finance runtime -> checkpoints/wake packets; finance runtime -> run detail rendering for `/runs show`

## Current State

- `CONTEXT.md` now locks the finance glossary, authority, continuity, wake-packet, blocked-reason, and run-evidence rules for live Robinhood transfers.
- The canonical main checkout does **not** currently contain `internal/runtime/transfers` or `internal/runtime/approvals`.
- The isolated worktree `.worktrees/codex-finance-approval-surfaces/` does contain a proven first slice:
  - `/transfer prepare`
  - `/approvals resolve`
  - `odin approvals resolve`
  - safe-failure submit continuation that ends in `fresh prepare required`
- Main already has the generic browser-control adapter seam:
  - `internal/adapters/web/driver_common.go`
  - `internal/adapters/web/huginn_driver.go`
  - `internal/tools/invocation/service.go`
  - `docs/contracts/live-driver-tools.md`
- Main `internal/runtime/runs/service.go` currently renders transcripts, memory summaries, and delegation artifacts, but has no generic run-artifact persistence layer for transfer driver evidence.
- Main `internal/app/lifecycle/run.go` still lacks a finance machine command branch.
- Main `internal/api/http/operational.go` is still only health/metrics and is intentionally out of scope for this slice.

## What Already Exists

- Locked finance domain contract in `CONTEXT.md`
- Shared SQLite runtime authority from ADR 0001
- Existing migration discipline from ADR 0002
- Existing checkpoints/wake-packet machinery
- Existing `/runs show <id|active>` run-detail surface
- Existing project registry enrollment for `family-ops`
- Existing shared web-driver invocation contract and Bash driver pattern
- Proven isolated finance tests and code in `.worktrees/codex-finance-approval-surfaces/`

## Gaps

- The proven finance prepare/resolve slice is not reconciled into the main checkout.
- No run-local artifact persistence exists for finance driver evidence such as `session_state` and `prior_session_state`.
- No Robinhood transfer driver adapter or script exists in the main checkout.
- Prepare currently has no real driver-backed review evidence in main.
- Submit continuation currently has no real submit path in main and no continuity verification logic.
- Principal-attended operator identity is locked in the domain model, but the current repo still lacks authenticated operator identity primitives; this slice can preserve the operational boundary but cannot truthfully claim cryptographic actor enforcement.

## Reuse Plan

- Reuse the isolated finance worktree implementation as the seed for:
  - `internal/runtime/transfers/service.go`
  - `internal/runtime/approvals/service.go`
  - shell/top-level finance command receipts
- Reuse shared browser-control adapter patterns from `internal/adapters/web` and `internal/tools/invocation`.
- Reuse SQLite artifact patterns already present in delegation artifacts instead of inventing an unrelated evidence store shape.
- Reuse existing `/runs show` instead of inventing a new finance evidence surface.
- Reuse existing `family-ops` registry enrollment in `config/projects.yaml`.
- Reuse deterministic script-driven integration testing before any operator-attended live Robinhood proof.

## New Additions

- Main-checkout finance runtime packages and command parity
- A small generic run-artifact persistence/read layer for run-local driver evidence
- A Robinhood transfer driver adapter plus deterministic driver script
- Driver-backed prepare-mode evidence persistence
- Submit-continuation classification logic for:
  - confirmed submit
  - `session_expired`
  - `resume_verification_failed`
- An operator runbook for attended live Robinhood proof once the deterministic path is green

## Why New Additions Are Necessary

- The isolated finance slice must land in main before live-driver work can be canonical.
- The locked domain contract forbids copying low-level driver evidence into transcript/memory as another structured field, so run-local artifact persistence is now required.
- Real Marcus transfers require a real Robinhood driver path, not the current safe-failure placeholder.
- The stale-context rules are too specific to leave as ad hoc shell behavior; they must be encoded in runtime orchestration and persisted evidence.

## Real odin E2E Verification

This planning slice does not run `odin`, but the implementation must finish with:

- deterministic fake-driver shell proof:
  - `/project family-ops`
  - `/transfer prepare ...`
  - `/runs show <prepare-run-id>`
  - `/approvals resolve <approval-id> approve because ...`
  - `/runs show <submit-run-id>`
- top-level parity proof:
  - `./bin/odin approvals resolve <approval-id> approve <reason...>`
- operator-attended real-driver proof in stages:
  - read-only prepare path against a real headed Robinhood session
  - then smallest-amount attended submit only after prepare evidence and continuity checks are stable

## Remaining Risks

- Main is dirty; this plan should execute in a fresh worktree and cherry-pick or port only the relevant finance slice.
- The old worktree implementation uses some pre-glossary wording and packet behavior that now needs alignment, not blind copying.
- The repo still lacks authenticated operator identity, so principal-attended enforcement remains partly operational in this slice.
- HTTP transfer prepare/status endpoints are intentionally deferred; this slice is CLI-first and shell-first.

## Best operating rule going forward

Reconcile the proven finance workflow into the canonical checkout first, then add the real Robinhood driver and continuity classification on top of that shared runtime seam. Do not build live-driver behavior directly onto stale design-doc assumptions or worktree-only code.

## Implementation Tasks

### Task 1: Reconcile The Proven Finance Prepare/Resolve Slice Into Main

**Domain Goal:** Make the shell/top-level Family-Ops transfer workflow real in the canonical checkout before layering live-driver complexity onto it.

**Domain Rules Enforced:**
- `/transfer prepare` and `/approvals resolve` are canonical Odin operator surfaces for this workflow.
- Approval resolution remains a compact receipt, while longer follow-up guidance stays on `/transfer prepare`.
- Approved-but-unusable continuity is not denial and is not silent success.

**Why this matters:**
- The current best proof lives only in an isolated worktree. Main cannot be the source of truth for real Marcus transfers until that slice is reconciled.

**Files:**
- Create: `internal/runtime/transfers/service.go`
- Create: `internal/runtime/transfers/service_test.go`
- Create: `internal/runtime/approvals/service.go`
- Create: `internal/runtime/approvals/service_test.go`
- Modify: `internal/cli/repl/shell.go`
- Modify: `internal/cli/repl/shell_test.go`
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/app/lifecycle/run_test.go`
- Modify: `internal/cli/commands/help.go`

**Step 1: Write the failing tests**

Seed the tests from the isolated finance worktree, but align the expectations to the locked glossary:

```go
func TestShellTransferPreparePrintsHandlesSummaryAndNext(t *testing.T) {
    if err := shell.HandleLine(ctx, "/transfer prepare direction=deposit amount_usd=25.00 source_account=checking destination_account=brokerage memo=test", &output); err != nil {
        t.Fatalf("HandleLine(/transfer prepare) error = %v", err)
    }
    for _, want := range []string{
        "task=robinhood-transfer-",
        "run=1",
        "approval=1",
        "summary=review prepared and awaiting approval",
        "/runs show 1",
        "/approvals resolve 1 <approve|deny> because <reason...>",
    } {
        if !strings.Contains(output.String(), want) {
            t.Fatalf("output = %q, want %q", output.String(), want)
        }
    }
}
```

```go
func TestRunApprovalsResolveApproveCommand(t *testing.T) {
    err := Run(ctx, repoRoot, []string{"approvals", "resolve", "1", "approve", "final", "confirmation"}, nil, &stdout)
    if err != nil {
        t.Fatalf("Run(approvals resolve) error = %v", err)
    }
}
```

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/runtime/transfers ./internal/runtime/approvals ./internal/cli/repl ./internal/app/lifecycle -run 'TestShellTransferPreparePrintsHandlesSummaryAndNext|TestShellApprovalsResolveApprove|TestRunApprovalsResolveApproveCommand|TestServicePrepareCreatesApprovalWaitTransfer|TestResolveApprovePreparedTransferFailsSafeAndRequiresFreshPrepare' -count=1
```

Expected: FAIL because the main checkout still lacks the finance runtime packages and top-level command branch.

**Step 3: Write minimal implementation**

Port the isolated finance worktree slice into main, but align the code immediately to the locked finance vocabulary. Use the worktree files as input, not as authority:

```go
switch args[0] {
case "approvals":
    return runApprovals(ctx, app, args[1:], stdout)
}
```

```go
type PrepareResult struct {
    Task     sqlite.Task
    Run      sqlite.Run
    Approval sqlite.Approval
    Summary  string
}
```

**Step 4: Run the focused tests**

Run:

```bash
go test ./internal/runtime/transfers ./internal/runtime/approvals ./internal/cli/repl ./internal/app/lifecycle ./internal/cli/commands -count=1
```

Expected: PASS with the same safe-failure continuation behavior that was already proven in isolation.

**Step 5: Commit**

```bash
git add internal/runtime/transfers/service.go internal/runtime/transfers/service_test.go internal/runtime/approvals/service.go internal/runtime/approvals/service_test.go internal/cli/repl/shell.go internal/cli/repl/shell_test.go internal/app/lifecycle/run.go internal/app/lifecycle/run_test.go internal/cli/commands/help.go
git commit -m "feat: reconcile finance transfer prepare and approval resolve"
```

### Task 2: Add Run-Local Artifact Persistence For Driver Evidence

**Domain Goal:** Give finance driver evidence one canonical home on the **Run Attempt** instead of leaking it into transcript or memory as another structured field.

**Domain Rules Enforced:**
- Browser-driver evidence belongs to **Run Attempt** execution evidence.
- `prior_session_state` stays local to run artifact transport and does not get copied into transcript, memory, or broader read models.

**Why this matters:**
- The glossary now explicitly forbids using transcript/memory as a second structured storage home for `prior_session_state`.

**Files:**
- Create: `internal/store/sqlite/migrations/0011_run_artifacts.sql`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/store/sqlite/store_test.go`
- Modify: `internal/runtime/runs/service.go`
- Modify: `internal/runtime/runs/service_test.go`
- Modify: `internal/cli/repl/shell.go`
- Modify: `internal/cli/repl/shell_test.go`

**Step 1: Write the failing tests**

Add store and run-detail tests that expect a run-local artifact row:

```go
func TestRunArtifactsRecordAndListByRun(t *testing.T) {
    artifact, err := store.RecordRunArtifact(ctx, sqlite.RecordRunArtifactParams{
        RunID:        run.ID,
        ArtifactType: "driver_result",
        Summary:      "Robinhood review ready",
        DetailsJSON:  `{"session_state":"review_ready"}`,
    })
    if err != nil {
        t.Fatalf("RecordRunArtifact() error = %v", err)
    }
    artifacts, err := store.ListRunArtifacts(ctx, sqlite.ListRunArtifactsParams{RunID: run.ID})
    if err != nil {
        t.Fatalf("ListRunArtifacts() error = %v", err)
    }
    if len(artifacts) != 1 || artifacts[0].ID != artifact.ID {
        t.Fatalf("artifacts = %+v, want recorded run artifact", artifacts)
    }
}
```

Add a shell test that expects `/runs show <id>` to render driver-result artifact rows.

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/store/sqlite ./internal/runtime/runs ./internal/cli/repl -run 'TestRunArtifactsRecordAndListByRun|TestRunsDetailIncludesRunArtifacts|TestShellRunsShowIncludesRunArtifacts' -count=1
```

Expected: FAIL because no run-artifact storage or rendering exists yet.

**Step 3: Write minimal implementation**

Model the new store after the existing delegation-artifact pattern:

```go
type RunArtifact struct {
    ID           int64
    RunID        int64
    ArtifactType string
    Summary      string
    DetailsJSON  string
    CreatedAt    time.Time
}
```

```sql
CREATE TABLE IF NOT EXISTS run_artifacts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    artifact_type TEXT NOT NULL,
    summary TEXT NOT NULL,
    details_json TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

**Step 4: Run the focused tests**

Run:

```bash
go test ./internal/store/sqlite ./internal/runtime/runs ./internal/cli/repl -count=1
```

Expected: PASS with `/runs show` now able to surface run-local driver evidence.

**Step 5: Commit**

```bash
git add internal/store/sqlite/migrations/0011_run_artifacts.sql internal/store/sqlite/models.go internal/store/sqlite/store.go internal/store/sqlite/store_test.go internal/runtime/runs/service.go internal/runtime/runs/service_test.go internal/cli/repl/shell.go internal/cli/repl/shell_test.go
git commit -m "feat: add run artifact persistence for driver evidence"
```

### Task 3: Add A Robinhood Transfer Driver Adapter And Deterministic Script Harness

**Domain Goal:** Put the Robinhood transfer flow on the same shared browser-control adapter pattern as the other live drivers without inventing a finance-only driver subsystem.

**Domain Rules Enforced:**
- Finance consumes workspace browser capability; it does not own a separate browser platform.
- Driver evidence states stay explicit and deterministic: `review_ready`, `submitted`, `session_expired`, `resume_verification_failed`.

**Why this matters:**
- The current finance seam has no real driver. Without a deterministic driver harness, live-driver work will be impossible to test safely.

**Files:**
- Create: `internal/adapters/web/robinhood_transfer_driver.go`
- Create: `internal/adapters/web/robinhood_transfer_driver_test.go`
- Modify: `internal/tools/invocation/service.go`
- Modify: `internal/tools/invocation/service_test.go`
- Create: `scripts/drivers/robinhood-transfer-flow.sh`
- Create: `tests/integration/robinhood_transfer_flow_test.go`
- Modify: `docs/contracts/live-driver-tools.md`

**Step 1: Write the failing tests**

Add unit and integration coverage for `prepare` and `submit` modes:

```go
func TestRobinhoodTransferDriverPrepareReturnsReviewReady(t *testing.T) {
    response, err := NewRobinhoodTransferDriver().Invoke(ctx, RobinhoodTransferRequest{
        ToolKey: "robinhood_transfer_flow",
        Input: RobinhoodTransferInput{
            Mode:               "prepare",
            Direction:          "deposit",
            AmountUSD:          "25.00",
            SourceAccount:      "checking",
            DestinationAccount: "brokerage",
        },
    })
    if err != nil {
        t.Fatalf("Invoke() error = %v", err)
    }
    if got := stringValue(response.Artifacts, "session_state"); got != "review_ready" {
        t.Fatalf("artifacts.session_state = %q, want review_ready", got)
    }
}
```

```go
func TestRobinhoodTransferDriverSubmitCanReturnResumeVerificationFailedWithPriorSessionState(t *testing.T) {
    // fixture-driven driver result should expose session_state=resume_verification_failed
    // and prior_session_state=session_expired
}
```

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/adapters/web ./internal/tools/invocation ./tests/integration -run 'TestRobinhoodTransferDriver|TestRobinhoodTransferFlowScript' -count=1
```

Expected: FAIL because no Robinhood transfer adapter or script exists yet.

**Step 3: Write minimal implementation**

Follow the existing adapter pattern:

```go
type RobinhoodTransferInput struct {
    Mode               string `json:"mode"`
    Direction          string `json:"direction"`
    AmountUSD          string `json:"amount_usd"`
    SourceAccount      string `json:"source_account"`
    DestinationAccount string `json:"destination_account"`
    Memo               string `json:"memo,omitempty"`
    ResumeFacts        map[string]string `json:"resume_facts,omitempty"`
}
```

Use a dedicated driver env var for implementation wiring, consistent with existing live-driver naming:

```bash
export ODIN_HUGINN_ROBINHOOD_TRANSFER_DRIVER="$PWD/scripts/drivers/robinhood-transfer-flow.sh"
```

**Step 4: Run the focused tests**

Run:

```bash
go test ./internal/adapters/web ./internal/tools/invocation ./tests/integration -count=1
```

Expected: PASS with deterministic prepare/submit fixtures.

**Step 5: Commit**

```bash
git add internal/adapters/web/robinhood_transfer_driver.go internal/adapters/web/robinhood_transfer_driver_test.go internal/tools/invocation/service.go internal/tools/invocation/service_test.go scripts/drivers/robinhood-transfer-flow.sh tests/integration/robinhood_transfer_flow_test.go docs/contracts/live-driver-tools.md
git commit -m "feat: add robinhood transfer driver harness"
```

### Task 4: Wire `/transfer prepare` To The Real Driver And Persist Review Evidence

**Domain Goal:** Make prepare produce real review-ready evidence instead of a synthetic blocked task with no driver-backed review artifact.

**Domain Rules Enforced:**
- `pending_approval` means fresh review evidence exists and approval is truly pending.
- Prepare receipts stay compact; artifacts belong on run-local evidence and `/runs show`.
- Auth/MFA during prepare are explicit operator checkpoints, not hidden automation.

**Why this matters:**
- Marcus cannot safely approve a transfer later if Odin never persisted the real review evidence that approval is supposed to authorize.

**Files:**
- Modify: `internal/runtime/transfers/service.go`
- Modify: `internal/runtime/transfers/service_test.go`
- Modify: `internal/runtime/runs/service_test.go`
- Modify: `tests/integration/robinhood_transfer_flow_test.go`

**Step 1: Write the failing tests**

Add a runtime test that expects prepare to call the driver and persist run-local artifact evidence:

```go
func TestPreparePersistsReviewReadyDriverArtifact(t *testing.T) {
    result, err := service.Prepare(ctx, params)
    if err != nil {
        t.Fatalf("Prepare() error = %v", err)
    }
    artifacts := listRunArtifacts(t, ctx, store, result.Run.ID)
    if got := artifactStringValue(artifacts[0], "session_state"); got != "review_ready" {
        t.Fatalf("session_state = %q, want review_ready", got)
    }
}
```

Require the checkpoint/wake state to use the canonical blocked reason:

```go
if latestWake.Trigger != "approval_wait" {
    t.Fatalf("Trigger = %q, want approval_wait", latestWake.Trigger)
}
if blockingReason(latestWake) != "approval_required" {
    t.Fatalf("blocking_reason = %q, want approval_required", blockingReason(latestWake))
}
```

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/runtime/transfers ./internal/runtime/runs ./tests/integration -run 'TestPreparePersistsReviewReadyDriverArtifact|TestPrepareUsesApprovalRequiredBlockingReason|TestRobinhoodTransferFlowScriptPrepare' -count=1
```

Expected: FAIL because prepare still uses synthetic completion and does not invoke the real driver.

**Step 3: Write minimal implementation**

Route prepare through the invocation service and persist the driver result on the run:

```go
driverResult, err := service.Invocation.RobinhoodTransfer(ctx, request)
if err != nil {
    return PrepareResult{}, err
}
_, err = service.Store.RecordRunArtifact(ctx, sqlite.RecordRunArtifactParams{
    RunID:        run.ID,
    ArtifactType: "driver_result",
    Summary:      driverResult.Summary,
    DetailsJSON:  driverResult.RawOutput,
})
```

**Step 4: Run the focused tests**

Run:

```bash
go test ./internal/runtime/transfers ./internal/runtime/runs ./tests/integration -count=1
```

Expected: PASS with real `review_ready` evidence persisted on the prepare run.

**Step 5: Commit**

```bash
git add internal/runtime/transfers/service.go internal/runtime/transfers/service_test.go internal/runtime/runs/service_test.go tests/integration/robinhood_transfer_flow_test.go
git commit -m "feat: wire transfer prepare to robinhood driver"
```

### Task 5: Wire Approved Submit Continuation To Real Driver Classification

**Domain Goal:** Replace the safe-failure placeholder with real submit continuation that either confirms submission or maps to the locked stale-context path.

**Domain Rules Enforced:**
- `submitted` requires confirmed Robinhood acceptance.
- `session_expired` and `resume_verification_failed` stay run-level evidence, but both converge to the same downstream finance consequence when approved execution context is unusable.
- Approved-but-unusable continuity keeps the old approval historically `approved`.
- No replacement active wake packet is created when approved continuity becomes unusable.

**Why this matters:**
- This is the real Marcus-transfer gap. Approval already exists; the missing step is truthful submit continuation with correct continuity semantics.

**Files:**
- Modify: `internal/runtime/approvals/service.go`
- Modify: `internal/runtime/approvals/service_test.go`
- Modify: `internal/runtime/transfers/service_test.go`
- Modify: `internal/runtime/checkpoints/service.go`
- Modify: `internal/runtime/checkpoints/service_test.go`
- Modify: `internal/cli/repl/shell_test.go`
- Modify: `internal/app/lifecycle/run_test.go`

**Step 1: Write the failing tests**

Add three focused approval-continuation cases:

```go
func TestResolveApproveSubmittedMarksTaskCompleted(t *testing.T) {}
func TestResolveApproveSessionExpiredBlocksTaskWithStaleContextAndSealsOldWake(t *testing.T) {}
func TestResolveApproveResumeVerificationFailedPersistsPriorSessionStateAndNoActiveReplacementWake(t *testing.T) {}
```

The failing assertions should include:

- `submitted` path:
  - submit run `status=completed`
  - task `status=completed`
  - driver artifact `session_state=submitted`
- unusable continuity paths:
  - submit run `status=failed`
  - task `status=blocked`
  - task `blocked_reason=stale_context`
  - old approval-linked wake packet sealed immediately
  - no replacement active wake packet exists
  - artifact `session_state=session_expired` or `resume_verification_failed`
  - optional `prior_session_state=session_expired` only on the latter case

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/runtime/approvals ./internal/runtime/checkpoints ./internal/cli/repl ./internal/app/lifecycle -run 'TestResolveApproveSubmittedMarksTaskCompleted|TestResolveApproveSessionExpiredBlocksTaskWithStaleContextAndSealsOldWake|TestResolveApproveResumeVerificationFailedPersistsPriorSessionStateAndNoActiveReplacementWake|TestShellApprovalsResolveApprove|TestRunApprovalsResolveApproveCommand' -count=1
```

Expected: FAIL because the main path is still either missing or only safe-fails through the placeholder branch.

**Step 3: Write minimal implementation**

Call the Robinhood driver in `submit` mode, then classify by primary driver state:

```go
switch sessionState {
case "submitted":
    finishSubmitRunCompleted(...)
case "session_expired":
    blockTaskWithStaleContext(...)
case "resume_verification_failed":
    blockTaskWithStaleContext(...)
default:
    return fmt.Errorf("unsupported robinhood transfer submit session_state %q", sessionState)
}
```

Ensure the stale-context branch seals the old approval packet immediately and records no replacement active wake packet.

**Step 4: Run the focused tests**

Run:

```bash
go test ./internal/runtime/approvals ./internal/runtime/checkpoints ./internal/cli/repl ./internal/app/lifecycle -count=1
```

Expected: PASS with submit continuation now matching the locked finance lifecycle.

**Step 5: Commit**

```bash
git add internal/runtime/approvals/service.go internal/runtime/approvals/service_test.go internal/runtime/transfers/service_test.go internal/runtime/checkpoints/service.go internal/runtime/checkpoints/service_test.go internal/cli/repl/shell_test.go internal/app/lifecycle/run_test.go
git commit -m "feat: add robinhood submit continuation classification"
```

### Task 6: Add Operator Runbook And Real `odin` Proof Sequence

**Domain Goal:** Prove the live CLI workflow through the real operator surface while keeping the principal-attended Robinhood boundary explicit.

**Domain Rules Enforced:**
- Real transfer work must go through repo-owned Odin command paths.
- Live auth/MFA remain explicit operator checkpoints.
- Proven behavior and unproven live behavior must be reported separately.

**Why this matters:**
- Internal tests are not enough here. The repo needs an executable proof sequence for Marcus and a documented line between deterministic proof and attended live Robinhood use.

**Files:**
- Create: `docs/operations/marcus-robinhood-live-transfer-runbook.md`
- Modify: `docs/contracts/live-driver-tools.md`
- Modify: `tests/integration/robinhood_transfer_flow_test.go`

**Step 1: Write the failing or missing proof harness**

Add a deterministic integration test that exercises the full command path in a fresh runtime root:

```bash
printf '/project family-ops\n/transfer prepare direction=deposit amount_usd=25.00 source_account=checking destination_account=brokerage memo=test\n/runs show active\n/approvals resolve 1 approve because final confirmation\n/runs show active\n/quit\n'
```

Assert:
- prepare run shows `session_state=review_ready`
- approve path returns `approval=<id> status=resolved result=approved run=<id>`
- submit run shows either `session_state=submitted` or the correct stale-context evidence, depending on fixture

**Step 2: Run test to verify it fails or is missing**

Run:

```bash
go test ./tests/integration -run 'TestRobinhoodTransferShellFlowDeterministic' -count=1
```

Expected: FAIL or be absent before the proof harness is added.

**Step 3: Write minimal implementation**

Document and script the proof boundary:

- deterministic fake-driver proof in CI/local tests
- attended real-session prepare-only smoke
- attended smallest-amount real submit only after prepare and stale-context classification are stable

Include exact live commands in the runbook:

```bash
./bin/odin
/project family-ops
/transfer prepare direction=deposit amount_usd=1.00 source_account=checking destination_account=brokerage memo=attended-smoke
/runs show <prepare-run-id>
/approvals resolve <approval-id> approve because attended live confirmation
/runs show <submit-run-id>
```

**Step 4: Run the focused proof**

Run:

```bash
go test ./tests/integration -run 'TestRobinhoodTransferShellFlowDeterministic' -count=1
go build -o ./bin/odin ./cmd/odin
```

Expected: deterministic proof PASS; binary build PASS. Any real Robinhood proof must be reported separately as operator-attended and may remain unrun in the implementation session.

**Step 5: Commit**

```bash
git add docs/operations/marcus-robinhood-live-transfer-runbook.md docs/contracts/live-driver-tools.md tests/integration/robinhood_transfer_flow_test.go
git commit -m "docs: add robinhood transfer live proof runbook"
```

## Review Checklist

- domain naming matches `CONTEXT.md`
- finance run evidence stays on run-local artifacts, not copied into transcript or memory as another structured field
- approved-but-unusable continuity preserves historical approval and uses `blocked_reason=stale_context`
- no replacement active wake packet is created on stale approved continuity loss
- `submitted` is used only for confirmed Robinhood acceptance
- reused repo structures are explicit: checkpoints, runs, shell, lifecycle, shared web adapters, invocation service
- ADR 0001 canonical SQLite authority is honored
- ADR 0002 migration discipline is honored by porting only the relevant isolated finance slice
- HTTP finance endpoints are explicitly deferred, not half-implemented
- principal-attended enforcement gaps are documented honestly rather than papered over
