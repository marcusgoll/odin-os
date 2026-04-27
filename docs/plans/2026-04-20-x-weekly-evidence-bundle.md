# X Weekly Evidence Bundle Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a weekly X evidence bundle tool that captures several explicit X post URLs through the existing single-post evidence path, records one `social_evidence` entry per post, and returns a compact weekly summary.

**Architecture:** Reuse the current `huginn_x_post_visible_evidence` extraction path and extend the generic tool-result contract so one tool invocation can request multiple memory writes. Implement the weekly bundle as a builtin orchestration layer rather than a second browser driver.

**Tech Stack:** Go, existing Huginn/browser driver stack, existing `/tool run` shell path, SQLite-backed knowledge memory, Go test, Make build

---

### Task 1: Write failing tests for the weekly batch tool and multi-memory recording

**Files:**
- Modify: `internal/tools/catalog/builtin_test.go`
- Modify: `internal/cli/repl/shell_test.go`

**Step 1: Add builtin tests for the weekly bundle tool**

Add tests proving:

- `huginn_x_weekly_evidence_bundle` exists
- it requires `target_urls`
- it rejects invalid or empty URL input
- it returns a compact summary with counts
- it requests multiple `social_evidence` memory records

**Step 2: Add shell tests for multi-memory recording**

Add tests proving:

- `/tool run huginn_x_weekly_evidence_bundle target_urls=...` records one workflow-scoped `social_evidence` entry per URL
- each recorded entry includes `channel=x` and `evidence_kind=x_post_visible`
- batch-specific fields such as `bundle_label` and `bundle_position` are present

**Step 3: Run focused tests and confirm failure**

Run:

```bash
go test ./internal/tools/catalog ./internal/cli/repl -run 'WeeklyEvidenceBundle|MemoryRecords' -count=1
```

Expected:

- FAIL because the weekly bundle tool and multi-memory shell path do not yet exist

### Task 2: Extend the generic tool-result memory contract

**Files:**
- Modify: `internal/tools/catalog/types.go`
- Modify: `internal/cli/repl/shell.go`

**Step 1: Replace the single optional memory record with a list**

Change the structured tool result to use:

- `MemoryRecords []MemoryRecord`

**Step 2: Update shell recording logic**

Make `/tool run` record each requested memory entry in order after a successful tool invocation.

**Step 3: Keep single-record tools working**

Update the current single-post X evidence tool to emit a one-element list rather than a single pointer.

**Step 4: Run focused tests and make them pass**

Run:

```bash
go test ./internal/tools/catalog ./internal/cli/repl -run 'MemoryRecords|VisibleEvidence|ToolRun' -count=1
```

Expected:

- PASS

### Task 3: Add the weekly X evidence bundle tool

**Files:**
- Modify: `internal/tools/catalog/builtin.go`
- Modify: `internal/tools/catalog/builtin_test.go`

**Step 1: Add input parsing helpers**

Add small helpers to:

- parse comma-separated `target_urls`
- trim whitespace
- dedupe normalized URLs while preserving order

**Step 2: Add the builtin tool**

Add `huginn_x_weekly_evidence_bundle` to the builtin catalog.

It should:

- require `target_urls`
- validate each URL through the existing X post evidence path
- call the existing single-post invocation path for each URL
- collect per-post memory records
- return compact batch facts and per-post status artifacts

**Step 3: Add batch labeling fields**

Each generated memory record should include:

- `bundle_label`
- `bundle_position`

**Step 4: Run focused builtin tests and make them pass**

Run:

```bash
go test ./internal/tools/catalog -run 'WeeklyEvidenceBundle' -count=1
```

Expected:

- PASS

### Task 4: Verify the bundle flows into existing analytics memory

**Files:**
- Modify: `internal/cli/repl/shell_test.go`

**Step 1: Add a shell regression proving analytics reuse**

Add a test that:

- runs the weekly bundle tool
- then triggers the existing Marcus analytics path
- confirms the existing `Recent X Visible Evidence:` section contains bundled evidence entries without any new prompt logic

**Step 2: Run focused shell tests and make them pass**

Run:

```bash
go test ./internal/cli/repl -run 'WeeklyEvidenceBundle|RecentXVisibleEvidence|AnalyticsSkill' -count=1
```

Expected:

- PASS

### Task 5: Update docs and prove the real odin path

**Files:**
- Modify: `docs/contracts/marcus-social-copilot.md`
- Modify: `docs/contracts/live-driver-tools.md`
- Modify: `memory/users/marcus-social-copilot.md`

**Step 1: Update live docs**

Document that:

- weekly X evidence bundles are live for explicit post URLs
- the bundle reuses the single-post X evidence path
- LinkedIn evidence remains manual
- automatic URL seeding from published outcomes is future work

**Step 2: Run targeted package verification**

Run:

```bash
go test ./internal/tools/catalog ./internal/cli/repl ./internal/cli/commands ./internal/memory/knowledge -count=1
make build
```

Expected:

- PASS
- `bin/odin` rebuilt successfully

**Step 3: Prove the real `odin` shell path**

Run a real shell session with fixture X evidence drivers and prove:

- `/tool run huginn_x_weekly_evidence_bundle target_urls=...` returns a compact batch summary
- one workflow-scoped `social_evidence` record is created per URL
- the existing Marcus analytics prompt includes bundled evidence through `Recent X Visible Evidence:`

**Step 4: Report completion without overclaiming**

The final report must explicitly include:

- Existing state found
- Reused components
- New components added
- Why new components were necessary
- Real `odin` command E2E checks performed
