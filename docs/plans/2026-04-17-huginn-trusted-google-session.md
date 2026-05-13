# Huginn Trusted Google Session Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Reuse a trusted headed Chrome session for Google-backed Plaid login in `odin-os` so Huginn can save and resume browser state instead of starting a fresh browser flow each run.

**Architecture:** Port a minimal Chrome CDP starter plus a Huginn `/connect` path from `odin-orchestrator` into `odin-os`. Add a small browser helper that starts or reuses trusted Chrome and connects the repo-local Huginn server to it, then make the Plaid driver prefer that trusted session when Google auth is involved.

**Tech Stack:** Bash, Node.js, jq, repo-local Huginn server, Go integration tests, real `odin` CLI verification.

---

### Task 1: Add failing tests for trusted-session browser connection

**Files:**
- Modify: `tests/integration/live_driver_scripts_test.go`

**Step 1: Write the failing test**

Add a fixture-backed test for the real `scripts/browser/browser-access.sh` helper that:

- stubs a Chrome CDP starter library
- uses a local HTTP stub for the Huginn server
- calls the new trusted-session helper
- expects `/connect` to be used instead of `/launch`

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./tests/integration -run 'TestBrowserAccessTrustedSessionConnectsToChromeCDP'
```

Expected: FAIL because `browser-access.sh` does not yet have a trusted-session helper.

**Step 3: Write minimal implementation**

Add the smallest helper and override hook needed to let the real browser helper connect Huginn to a trusted Chrome CDP endpoint.

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./tests/integration -run 'TestBrowserAccessTrustedSessionConnectsToChromeCDP'
```

Expected: PASS

### Task 2: Add failing test for Plaid trusted-session preference

**Files:**
- Modify: `tests/integration/plaid_browser_reuse_test.go`

**Step 1: Write the failing test**

Add a fixture-backed Plaid driver regression that:

- provides Google credentials
- exposes both `browser_trusted_session_start` and `browser_server_start`
- expects the trusted-session helper to be used first

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./tests/integration -run 'TestPlaidTransferApplicationScriptPrefersTrustedGoogleSessionWhenAvailable'
```

Expected: FAIL because the driver still uses the regular launch path.

**Step 3: Write minimal implementation**

Update the Plaid driver to use the trusted-session helper when available, with fallback to the current headed launch.

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./tests/integration -run 'TestPlaidTransferApplicationScriptPrefersTrustedGoogleSessionWhenAvailable'
```

Expected: PASS

### Task 3: Port the minimal trusted Chrome CDP pieces

**Files:**
- Create: `scripts/browser/chrome-cdp-start.sh`
- Modify: `scripts/browser/browser-access.sh`
- Modify: `scripts/browser/odin-huginn-server.js`

**Step 1: Keep the browser-access regression red**

Run:

```bash
go test ./tests/integration -run 'TestBrowserAccessTrustedSessionConnectsToChromeCDP'
```

Expected: FAIL until all pieces are wired.

**Step 2: Write minimal implementation**

- Port a small `chrome-cdp-start.sh` with Xvfb-backed Chrome startup and persistent profile reuse.
- Add a `/connect` route to the repo-local Huginn server.
- Add a trusted-session helper in `browser-access.sh` that starts or reuses Chrome CDP, starts or reuses the Huginn server, and attaches to the trusted Chrome session.

**Step 3: Run focused verification**

Run:

```bash
go test ./tests/integration -run 'TestBrowserAccessTrustedSessionConnectsToChromeCDP|TestPlaidTransferApplicationScriptPrefersTrustedGoogleSessionWhenAvailable'
```

Expected: PASS

### Task 4: Verify the focused Plaid suite and build

**Files:**
- Modify: none unless verification exposes another bug

**Step 1: Run the focused Plaid and browser suite**

Run:

```bash
go test ./tests/integration -run 'TestBrowserAccessTrustedSessionConnectsToChromeCDP|TestPlaidTransferApplicationScriptPrefersTrustedGoogleSessionWhenAvailable|TestPlaidTransferApplicationScriptLaunchesHeadedWhenGoogleCredentialIsAvailable|TestPlaidTransferApplicationScriptUsesStoredGoogleCredentialsAfterGoogleRedirect|TestPlaidTransferApplicationScriptClassifiesGoogleTwoStepVerificationAsMFA'
bash -n scripts/browser/browser-access.sh scripts/browser/chrome-cdp-start.sh scripts/drivers/plaid-transfer-application.sh
node --check scripts/browser/odin-huginn-server.js
go build -o ./bin/odin ./cmd/odin
```

Expected: PASS

### Task 5: Verify the live Odin-OS path

**Files:**
- Modify: none unless live verification exposes another bug

**Step 1: Run the live driver**

Run:

```bash
echo '{"tool_key":"plaid_transfer_application","input":{"application_url":"https://dashboard.plaid.com/transfer/application"}}' | ODIN_DIR=/var/odin ODIN_ROOT=/var/odin ODIN_BROWSER_SERVER_URL=http://127.0.0.1:19227 bash /home/orchestrator/odin-os/scripts/drivers/plaid-transfer-application.sh
```

Expected: a classified JSON result that either reuses trusted session state or reports the next live blocker.

### Task 6: Verify the real Odin CLI path

**Files:**
- Modify: none unless CLI verification exposes another bug

**Step 1: Rerun the Family-Ops Plaid task through the real CLI**

Run:

```bash
export ODIN_CODEX_DRIVER="$PWD/scripts/drivers/codex-headless.sh"
export ODIN_CODEX_SANDBOX_MODE="workspace-write"
export ODIN_BROWSER_SERVER_URL="http://127.0.0.1:19227"
export ODIN_DIR="/var/odin"
export ODIN_ROOT="/var/odin"
./bin/odin
```

Then in the shell:

```text
/project family-ops
/mode act
echo '{"tool_key":"plaid_transfer_application","input":{"application_url":"https://dashboard.plaid.com/transfer/application"}}' | ODIN_DIR=/var/odin ODIN_ROOT=/var/odin ODIN_BROWSER_SERVER_URL=http://127.0.0.1:19227 bash /home/orchestrator/odin-os/scripts/drivers/plaid-transfer-application.sh
/runs show <run-id>
```

Expected: the stored run transcript reflects the trusted-session result through the real `odin` path.
