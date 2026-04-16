# Odin OS Browser-Human Cutover Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build an `odin-os`-owned browser/Huginn capability and a bounded Plaid Transfer application workflow that no longer depends on the legacy `odin-orchestrator-main` browser runtime.

**Architecture:** Copy the required browser runtime into `odin-os`, expose it through the existing driver-command invocation model, add two bounded tools (`huginn_browser_session` and `plaid_transfer_application`), and prove the runtime never shells back into the legacy browser library. Keep Chromium as the only supported engine in this phase.

**Tech Stack:** Go, shell driver scripts, Node.js Playwright runtime, systemd user service, `odin-os` tool catalog and driver invocation pattern.

---

### Task 1: Add odin-os-owned browser runtime assets

**Files:**
- Create: `scripts/browser/browser-access.sh`
- Create: `scripts/browser/odin-huginn-server.js`
- Create: `scripts/browser/huginn-captcha.js`
- Test: `scripts/tests/browser-runtime-smoke.sh`
- Reference only: `/home/orchestrator/odin-orchestrator-main/scripts/odin/lib/browser-access.sh`
- Reference only: `/home/orchestrator/odin-orchestrator-main/scripts/odin/lib/odin-huginn-server.js`
- Reference only: `/home/orchestrator/odin-orchestrator-main/scripts/odin/lib/huginn-captcha.js`

**Step 1: Write the failing smoke contract**

Write `scripts/tests/browser-runtime-smoke.sh` so it fails if:

- the repo-local browser runtime files do not exist
- `browser_server_start` cannot launch Chromium
- `browser_snapshot` against `https://example.com` does not return `Example Domain`

**Step 2: Run smoke to verify it fails**

Run: `bash scripts/tests/browser-runtime-smoke.sh`
Expected: FAIL because the repo-local browser runtime files do not exist yet.

**Step 3: Copy and trim the runtime into odin-os**

Create the repo-local browser files by copying the minimum required runtime from the legacy repo and changing the runtime assumptions so they are odin-os-owned:

- default paths must point inside `odin-os`
- default engine must be Chromium
- no runtime imports or sourcing from `odin-orchestrator-main`

**Step 4: Run smoke to verify it passes**

Run: `bash scripts/tests/browser-runtime-smoke.sh`
Expected: PASS with Chromium launch, snapshot, and stop working end to end.

**Step 5: Commit**

```bash
git add scripts/browser scripts/tests/browser-runtime-smoke.sh
git commit -m "feat: vendor odin-os browser runtime"
```

### Task 2: Add a generic browser driver contract in odin-os

**Files:**
- Create: `internal/adapters/browserhuman/driver.go`
- Create: `internal/adapters/browserhuman/driver_test.go`
- Create: `internal/tools/invocation/service.go`
- Create: `internal/tools/invocation/service_test.go`
- Reference only: `/home/orchestrator/odin-os/internal/adapters/web/huginn_driver.go`

**Step 1: Write the failing driver tests**

Add tests for:

- missing driver command fails closed
- empty tool key defaults correctly when allowed
- mismatched response tool key fails
- non-completed response status fails
- structured artifacts are preserved

**Step 2: Run the driver tests to verify failure**

Run: `go test ./internal/adapters/browserhuman ./internal/tools/invocation -count=1`
Expected: FAIL because the packages do not exist yet.

**Step 3: Implement the minimal generic driver**

Create a generic browser driver contract that:

- reads one configured command from env
- sends JSON request on stdin
- reads one JSON response from stdout
- validates `tool_key`, `status`, and `artifacts`

**Step 4: Re-run the driver tests**

Run: `go test ./internal/adapters/browserhuman ./internal/tools/invocation -count=1`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/adapters/browserhuman internal/tools/invocation
git commit -m "feat: add generic browser driver contract"
```

### Task 3: Add repo-local browser driver scripts

**Files:**
- Create: `scripts/drivers/huginn-browser-session.sh`
- Create: `scripts/drivers/plaid-transfer-application.sh`
- Test: `tests/integration/browser_driver_scripts_test.go`

**Step 1: Write failing integration tests**

Create integration tests that stub the repo-local browser shell library and prove:

- `huginn-browser-session.sh` uses the repo-local `scripts/browser/browser-access.sh`
- `plaid-transfer-application.sh` uses the repo-local `scripts/browser/browser-access.sh`
- neither script resolves a legacy repo path
- both scripts emit one valid structured JSON result

**Step 2: Run the integration tests to verify failure**

Run: `go test ./tests/integration -run 'TestHuginnBrowserSessionScript|TestPlaidTransferApplicationScript' -count=1`
Expected: FAIL because the scripts do not exist yet.

**Step 3: Implement the driver scripts**

Implement:

- `huginn-browser-session.sh` for health, launch, snapshot, screenshot, and stop
- `plaid-transfer-application.sh` for bounded Plaid dashboard navigation and state detection

Return explicit states such as:

- `ready_for_login`
- `blocked_on_mfa`
- `submitted_for_review`
- `already_enabled`

**Step 4: Re-run the integration tests**

Run: `go test ./tests/integration -run 'TestHuginnBrowserSessionScript|TestPlaidTransferApplicationScript' -count=1`
Expected: PASS.

**Step 5: Commit**

```bash
git add scripts/drivers tests/integration/browser_driver_scripts_test.go
git commit -m "feat: add odin-os browser workflow drivers"
```

### Task 4: Register odin-os browser tools in the catalog

**Files:**
- Modify: `internal/tools/catalog/builtin.go`
- Modify: `internal/tools/catalog/builtin_test.go`
- Create: `docs/contracts/browser-human-tools.md`

**Step 1: Write failing catalog tests**

Add tests that require:

- `huginn_browser_session` exists
- `plaid_transfer_application` exists
- both tools invoke the generic browser driver path
- follow-on options are sensible and bounded

**Step 2: Run the catalog tests to verify failure**

Run: `go test ./internal/tools/catalog -count=1`
Expected: FAIL because the new tools are not registered yet.

**Step 3: Implement the minimal catalog wiring**

Register the two browser tools with:

- summary
- tags
- schemas
- invocation handlers using `internal/tools/invocation`

Document the request/response contract in `docs/contracts/browser-human-tools.md`.

**Step 4: Re-run the catalog tests**

Run: `go test ./internal/tools/catalog -count=1`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/tools/catalog docs/contracts/browser-human-tools.md
git commit -m "feat: register odin-os browser tools"
```

### Task 5: Add Chromium-only preflight and host diagnostics

**Files:**
- Create: `scripts/ops/browser-preflight.sh`
- Create: `scripts/tests/browser-preflight-test.sh`
- Create: `docs/operations/browser-human.md`

**Step 1: Write the failing preflight test**

The preflight script must fail if:

- Chromium is missing
- required runtime libs are missing
- Firefox or WebKit are requested

**Step 2: Run the test to verify failure**

Run: `bash scripts/tests/browser-preflight-test.sh`
Expected: FAIL because the preflight script does not exist yet.

**Step 3: Implement the preflight**

The script should:

- check Chromium browser binary availability
- check required host libraries
- print one clear readiness summary
- explicitly reject unsupported engines in this phase

Document usage in `docs/operations/browser-human.md`.

**Step 4: Re-run the test**

Run: `bash scripts/tests/browser-preflight-test.sh`
Expected: PASS.

**Step 5: Commit**

```bash
git add scripts/ops/browser-preflight.sh scripts/tests/browser-preflight-test.sh docs/operations/browser-human.md
git commit -m "feat: add browser preflight and operations guide"
```

### Task 6: Add audited Plaid workflow artifacts and smoke proof

**Files:**
- Modify: `scripts/drivers/plaid-transfer-application.sh`
- Modify: `tests/integration/browser_driver_scripts_test.go`
- Create: `docs/operations/plaid-transfer-application.md`

**Step 1: Write the failing Plaid-state test**

Add tests that require the Plaid workflow to emit:

- a screenshot artifact path when available
- the detected workflow state
- a summary that distinguishes `login`, `mfa`, `review`, and `enabled`

**Step 2: Run the Plaid-state test to verify failure**

Run: `go test ./tests/integration -run TestPlaidTransferApplicationArtifacts -count=1`
Expected: FAIL because artifact details are incomplete.

**Step 3: Implement the artifact and state output**

Extend the Plaid workflow driver so it returns:

- `session_state`
- `current_url`
- `screenshot_path`
- `evidence`
- `next_action`

Document the operator flow in `docs/operations/plaid-transfer-application.md`.

**Step 4: Re-run the Plaid-state test**

Run: `go test ./tests/integration -run TestPlaidTransferApplicationArtifacts -count=1`
Expected: PASS.

**Step 5: Commit**

```bash
git add scripts/drivers/plaid-transfer-application.sh tests/integration/browser_driver_scripts_test.go docs/operations/plaid-transfer-application.md
git commit -m "feat: add audited Plaid browser workflow output"
```

### Task 7: Run final verification and smoke the odin-os-native browser lane

**Files:**
- Verify only

**Step 1: Run focused Go verification**

Run:

```bash
go test ./internal/tools/catalog ./internal/adapters/browserhuman ./internal/tools/invocation ./tests/integration -count=1
```

Expected: PASS.

**Step 2: Run shell/browser verification**

Run:

```bash
bash scripts/tests/browser-runtime-smoke.sh
bash scripts/tests/browser-preflight-test.sh
```

Expected: PASS.

**Step 3: Run one real host smoke**

Run:

```bash
export ODIN_BROWSER_HUMAN_DRIVER="/home/orchestrator/.config/superpowers/worktrees/odin-os/phase-36-generic-huginn-browser/scripts/drivers/huginn-browser-session.sh"
export ODIN_PLAID_BROWSER_DRIVER="/home/orchestrator/.config/superpowers/worktrees/odin-os/phase-36-generic-huginn-browser/scripts/drivers/plaid-transfer-application.sh"
```

Then prove:

- browser preflight passes
- generic browser session can snapshot `https://example.com`
- Plaid workflow reaches a real bounded state without using the legacy browser path

**Step 4: Inspect for legacy leakage**

Run:

```bash
rg -n "odin-orchestrator-main|scripts/odin/lib/browser-access.sh|ODIN_BROWSER_ACCESS_LIB_PATH" .
```

Expected: only reference-only docs or migration notes remain; no runtime path depends on the legacy browser library.

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: cut odin-os browser workflows over from legacy huginn"
```
