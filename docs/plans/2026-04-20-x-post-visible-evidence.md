# X Post Visible Evidence Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a read-only X post evidence tool backed by Huginn visible-page capture, auto-record the resulting evidence into workflow-scoped memory, and surface that evidence in the Marcus analytics prompt.

**Architecture:** Reuse the existing Huginn driver stack by adding one dedicated X post evidence driver, adapter, invocation method, and builtin tool. Extend the generic tool-result structure so tools can request memory recording, then wire the Marcus analytics retrospective to include recent `social_evidence` entries.

**Tech Stack:** Go, existing Huginn/browser shell drivers, existing `/tool run` path, SQLite-backed knowledge memory, Go test, Make build

---

### Task 1: Write failing tests for the new X evidence tool and memory path

**Files:**
- Modify: `internal/adapters/web/`
- Modify: `internal/tools/invocation/service_test.go`
- Modify: `internal/tools/catalog/builtin_test.go`
- Modify: `internal/cli/repl/shell_test.go`

**Step 1: Add adapter tests for the new driver contract**

Create tests for a new web adapter file that verify:

- the driver reads `ODIN_HUGINN_X_POST_DRIVER`
- request JSON contains the explicit `target_url`
- completed responses decode artifacts correctly
- missing config fails closed
- non-completed driver status fails closed

**Step 2: Add invocation service tests**

Add a new service test proving the invocation layer exposes the new driver result and raw output.

**Step 3: Add builtin tool tests**

Add a tool catalog test proving:

- `huginn_x_post_visible_evidence` exists
- it requires `target_url`
- it returns expected screenshot/evidence artifacts from a fixture driver

**Step 4: Add shell tests for memory recording and analytics consumption**

Add shell tests that verify:

- `/tool run huginn_x_post_visible_evidence target_url=...` records workflow-scoped `social_evidence`
- the recorded memory includes `channel=x` and `evidence_kind=x_post_visible`
- the Marcus analytics prompt includes a `Recent X Visible Evidence:` section when recent `social_evidence` exists

**Step 5: Run focused tests and confirm failure**

Run:

```bash
go test ./internal/adapters/web ./internal/tools/invocation ./internal/tools/catalog ./internal/cli/repl -run 'XPost|VisibleEvidence|RecentXVisibleEvidence' -count=1
```

Expected:

- FAIL because the new driver/tool/memory path does not yet exist

### Task 2: Implement the dedicated X post evidence driver path

**Files:**
- Create: `internal/adapters/web/x_post_driver.go`
- Create: `internal/adapters/web/x_post_driver_test.go`
- Modify: `internal/tools/invocation/service.go`
- Modify: `internal/tools/invocation/service_test.go`
- Create: `scripts/drivers/huginn-x-post-evidence.sh`

**Step 1: Add the new adapter types**

Create a new adapter with:

- env var: `ODIN_HUGINN_X_POST_DRIVER`
- tool key: `huginn_x_post_visible_evidence`
- request input fields:
  - `target_url`
  - `label`
  - `screenshot_path`
  - `wait_ms`
  - `headless`

Reuse the same `invokeDriverCommand(...)` path as the existing Huginn drivers.

**Step 2: Add the invocation service method**

Add `HuginnXPostVisibleEvidence(...)` to the invocation service.

**Step 3: Add the shell driver script**

Create `scripts/drivers/huginn-x-post-evidence.sh` that:

- validates the target host is an allowed X host
- starts the existing Huginn browser server
- captures screenshot and visible-page snapshot
- writes full snapshot text to a file artifact
- uses `browser_evaluate` for best-effort visible field extraction
- returns structured JSON with:
  - target URL
  - final URL
  - screenshot path
  - snapshot path
  - snapshot excerpt
  - extracted visible fields when present

Keep it read-only.

**Step 4: Run focused driver/invocation tests and make them pass**

Run:

```bash
go test ./internal/adapters/web ./internal/tools/invocation -run 'XPost|VisibleEvidence' -count=1
```

Expected:

- PASS

### Task 3: Add the builtin tool and generic tool-memory recording

**Files:**
- Modify: `internal/tools/catalog/types.go`
- Modify: `internal/tools/catalog/builtin.go`
- Modify: `internal/tools/catalog/builtin_test.go`
- Modify: `internal/cli/repl/shell.go`
- Modify: `internal/cli/repl/shell_test.go`

**Step 1: Extend tool results with optional memory recording**

Add a small optional memory-record payload to the structured tool result type.

This payload should include:

- memory type
- summary
- fields

**Step 2: Add the builtin tool**

Add `huginn_x_post_visible_evidence` to the builtin tool catalog.

It should:

- require `target_url`
- call the new invocation service method
- return structured artifacts
- request workflow-scoped memory recording as `social_evidence`

**Step 3: Record tool-requested memory in the shell**

Extend `/tool run` so that after a successful tool invocation:

- if the result requests memory recording
- and a valid memory scope exists
- the shell records the memory and reports that fact

Do not add a new command.

**Step 4: Run focused tool/shell tests and make them pass**

Run:

```bash
go test ./internal/tools/catalog ./internal/cli/repl -run 'XPost|VisibleEvidence|ToolRun' -count=1
```

Expected:

- PASS

### Task 4: Surface recent X visible evidence in the Marcus analytics prompt

**Files:**
- Modify: `internal/cli/repl/shell.go`
- Modify: `internal/cli/repl/shell_test.go`

**Step 1: Add recent `social_evidence` retrieval**

In `socialRetrospectivePromptContext(...)`, fetch recent `social_evidence` entries for the current workflow scope.

Keep the window bounded to the same 7-day window as the weekly retrospective.

**Step 2: Add a new evidence section**

Append:

- `Recent X Visible Evidence:`

Render recent evidence entries using existing retrospective line formatting where possible.

**Step 3: Keep comparison/carry-forward behavior intact**

Do not try to compare evidence across weeks in this slice.

**Step 4: Run focused analytics prompt tests and make them pass**

Run:

```bash
go test ./internal/cli/repl -run 'RecentXVisibleEvidence|AnalyticsSkill' -count=1
```

Expected:

- PASS

### Task 5: Update docs and prove the real odin path

**Files:**
- Modify: `docs/contracts/marcus-social-copilot.md`
- Modify: `memory/users/marcus-social-copilot.md`
- Modify: `docs/contracts/live-driver-tools.md`

**Step 1: Update live docs**

Document that:

- X read-only explicit-post evidence capture is live
- LinkedIn remains manual
- the slice uses visible-page evidence only
- unofficial API harvesting is not part of the implementation

**Step 2: Run targeted package verification**

Run:

```bash
go test ./internal/adapters/web ./internal/tools/invocation ./internal/tools/catalog ./internal/cli/repl ./internal/cli/commands ./internal/memory/knowledge -count=1
make build
```

Expected:

- PASS
- `bin/odin` rebuilt successfully

**Step 3: Prove the real `odin` shell path**

Run a real shell session with a fixture X post driver configured through `ODIN_HUGINN_X_POST_DRIVER` and prove:

- `/tool run huginn_x_post_visible_evidence target_url=...` returns screenshot/evidence artifacts
- workflow-scoped `social_evidence` is recorded
- the Marcus analytics prompt includes `Recent X Visible Evidence:`

**Step 4: Report completion without overclaiming**

The final report must explicitly include:

- Existing state found
- Reused components
- New components added
- Why new components were necessary
- Real `odin` command E2E checks performed
