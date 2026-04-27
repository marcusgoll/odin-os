# Social Draft Resolution Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a narrow `/memory resolve` operator flow so Marcus can resolve pending `social_draft` memories into approved or rejected outcomes inside `odin-os`.

**Architecture:** Extend the existing `/memory` command rather than adding a new social command family. Update the selected `social_draft` record in place to clear the pending queue, and record a matching `social_outcome` when the draft already carries valid `channel` and `content_kind` metadata.

**Tech Stack:** Go, interactive CLI shell, SQLite store, runtime events, integration tests with the compiled `odin` binary.

---

### Task 1: Add failing CLI tests for social draft resolution

**Files:**
- Modify: `internal/cli/repl/shell_test.go`
- Modify: `tests/integration/social_workflow_test.go`

**Step 1: Write the failing shell test**

Add a test that:
- records a pending `social_draft`
- runs `/memory resolve <id> result=approved`
- expects the draft to leave the pending queue
- expects a `social_outcome` with `result=approved`

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/repl -run TestShellMemoryResolveApprovedDraftRecordsOutcome`
Expected: FAIL because `/memory resolve` does not exist yet.

**Step 3: Write the failing integration test**

Add a compiled-binary integration test that:
- drafts through the Marcus workflow
- resolves the returned draft id
- verifies the pending queue is empty
- verifies `social_outcome` appears

**Step 4: Run test to verify it fails**

Run: `go test ./tests/integration -run TestMarcusSocialDraftResolveCLIIntegration`
Expected: FAIL because the CLI subcommand does not exist yet.

### Task 2: Add the minimal store mutation needed by `/memory resolve`

**Files:**
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/store/sqlite/store_test.go`
- Modify: `internal/runtime/events/events.go`

**Step 1: Write the failing store test**

Add a test that updates one memory summary’s `details_json` and `updated_at`, then re-reads it and checks that a memory update event was emitted.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/store/sqlite -run TestUpdateMemorySummaryDetails`
Expected: FAIL because the update method and event do not exist yet.

**Step 3: Write minimal implementation**

Add:
- a narrow update params struct
- a store method to update `summary` and/or `details_json`
- a `memory.summary_updated` event payload

**Step 4: Run test to verify it passes**

Run: `go test ./internal/store/sqlite -run TestUpdateMemorySummaryDetails`
Expected: PASS

### Task 3: Implement `/memory resolve` on top of existing memory structures

**Files:**
- Modify: `internal/cli/commands/help.go`
- Modify: `internal/cli/repl/shell.go`

**Step 1: Add parser and validation**

Implement `/memory resolve <id> result=approved|rejected [reason=<text>]`.

Rules:
- the target memory must be visible in the current scope
- the target memory must be `social_draft`
- the target draft must currently have `approval=pending`
- resolving updates the draft’s `approval` field to `approved` or `rejected`
- if the resolved draft carries valid `channel` and `content_kind`, record a matching `social_outcome`

**Step 2: Run targeted CLI tests**

Run: `go test ./internal/cli/repl -run 'TestShellMemoryResolve(ApprovedDraftRecordsOutcome|RejectsAlreadyResolvedDraft)'`
Expected: PASS

### Task 4: Verify the full Marcus workflow through the real binary

**Files:**
- Modify: `docs/contracts/marcus-social-copilot.md`

**Step 1: Update contract wording**

Replace manual outcome logging language with approval-queue resolution through `/memory resolve`.

**Step 2: Run focused verification**

Run:
- `go test ./internal/store/sqlite ./internal/cli/repl`
- `go test ./tests/integration -run 'TestMarcusSocial(DraftAskCLIIntegrationAutoRecordsPendingDraft|DraftResolveCLIIntegration|AnalyticsRetrospectiveCLIIntegration)'`
- `go build -o ./bin/odin ./cmd/odin`

**Step 3: Run real odin E2E flow**

Run a real interactive session that:
- selects `marcus-social-growth-workflow`
- selects `marcus-x-drafting-assistant`
- drafts one X post
- resolves the draft with `/memory resolve`
- confirms `/memory list type=social_draft field.approval=pending` is empty
- confirms `/memory list type=social_outcome field.result=approved` shows the resolved item
