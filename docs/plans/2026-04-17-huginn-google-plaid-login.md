# Huginn Google-First Plaid Login Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make Huginn prefer `Sign in with Google` for the Plaid login flow when that option is present, with a safe fallback to the existing email/password path.

**Architecture:** Extend the existing `odin-os` browser auth helper with a small login-form detector that can identify OAuth providers from page snapshots. Reuse that helper inside the existing Plaid driver so the driver chooses Google first, then falls back to email/password only when Google is absent or unusable.

**Tech Stack:** Bash, jq, repo-local Huginn HTTP browser server, Go test runner for integration tests.

---

### Task 1: Add failing tests for Google-first login selection

**Files:**
- Modify: `tests/integration/plaid_browser_reuse_test.go`
- Test: `tests/integration/plaid_browser_reuse_test.go`

**Step 1: Write the failing test**

Add a fixture-backed test that exposes:

- a Plaid login snapshot with `Sign in with Google`
- a browser stub that only succeeds when the first automation step clicks Google
- an expected driver result showing the Google path outcome

Add a second focused fallback test where Google is absent and the existing email/password flow still succeeds.

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./tests/integration -run 'TestPlaidTransferApplicationScriptPrefersGoogleLoginWhenOffered|TestPlaidTransferApplicationScriptFallsBackToPasswordWhenGoogleUnavailable'
```

Expected: FAIL because the current driver still takes the email/password path first.

**Step 3: Write minimal implementation**

Modify the auth helper and Plaid driver to detect Google login and prefer it before the existing password flow.

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./tests/integration -run 'TestPlaidTransferApplicationScriptPrefersGoogleLoginWhenOffered|TestPlaidTransferApplicationScriptFallsBackToPasswordWhenGoogleUnavailable'
```

Expected: PASS

### Task 2: Reuse a small auth detector in `browser-auth.sh`

**Files:**
- Modify: `scripts/browser/browser-auth.sh`
- Test: `tests/integration/plaid_browser_reuse_test.go`

**Step 1: Write the failing test**

Use the Google-preference regression from Task 1 to require a reusable detector that can tell the driver which OAuth providers are present.

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./tests/integration -run 'TestPlaidTransferApplicationScriptPrefersGoogleLoginWhenOffered'
```

Expected: FAIL

**Step 3: Write minimal implementation**

Add a helper in `scripts/browser/browser-auth.sh` that:

- inspects snapshot text
- detects login-form elements
- reports OAuth providers such as `google`

Keep it as small as possible and model the contract on the existing `odin-orchestrator` helper.

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./tests/integration -run 'TestPlaidTransferApplicationScriptPrefersGoogleLoginWhenOffered'
```

Expected: PASS

### Task 3: Update the Plaid driver to prefer Google and preserve fallback

**Files:**
- Modify: `scripts/drivers/plaid-transfer-application.sh`
- Test: `tests/integration/plaid_browser_reuse_test.go`

**Step 1: Write the failing test**

Add or extend a regression so the Plaid driver:

- clicks Google first when present
- surfaces the resulting state cleanly
- still falls back to email/password when Google is not offered

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./tests/integration -run 'TestPlaidTransferApplicationScriptPrefersGoogleLoginWhenOffered|TestPlaidTransferApplicationScriptFallsBackToPasswordWhenGoogleUnavailable'
```

Expected: FAIL

**Step 3: Write minimal implementation**

Add the smallest possible driver behavior:

- detect OAuth options from the current snapshot
- click the Google sign-in control when offered
- wait for a post-click snapshot
- classify the resulting state or fall back to password when appropriate

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./tests/integration -run 'TestPlaidTransferApplicationScriptPrefersGoogleLoginWhenOffered|TestPlaidTransferApplicationScriptFallsBackToPasswordWhenGoogleUnavailable|TestPlaidTransferApplicationScriptHandlesCombinedLoginPageAndReportsLoginRejection|TestPlaidTransferApplicationScriptIgnoresForgotPasswordUntilPasswordFieldAppears'
```

Expected: PASS

### Task 4: Verify the live Odin-OS browser path

**Files:**
- Modify: none unless verification exposes another bug
- Test: live driver command only

**Step 1: Run the live driver**

Run:

```bash
printf '{"tool_key":"plaid_transfer_application","input":{"application_url":"https://dashboard.plaid.com/transfer/application"}}' | ODIN_DIR=/var/odin ODIN_ROOT=/var/odin ODIN_BROWSER_SERVER_URL=http://127.0.0.1:19227 bash /home/orchestrator/odin-os/scripts/drivers/plaid-transfer-application.sh
```

Expected: a concrete JSON result showing the Google-first path outcome or a classified blocker.

### Task 5: Verify the real Odin CLI path

**Files:**
- Modify: none unless verification exposes another bug
- Test: live `odin` command path

**Step 1: Rerun the Family-Ops Plaid task through Odin CLI**

Run:

```bash
export ODIN_CODEX_DRIVER="$PWD/scripts/drivers/codex-headless.sh"
export ODIN_CODEX_SANDBOX_MODE="workspace-write"
export ODIN_BROWSER_SERVER_URL="http://127.0.0.1:19227"
./bin/odin
```

Then in the interactive shell:

```text
/project family-ops
Investigate Robinhood plus Plaid auto-transfer setup status for Family-Ops. Use only Odin-routed investigation. Do not modify Family-Ops files. Do not make or submit financial changes. Execute this exact read-only command from the Odin-OS worktree and then return only its JSON result plus one sentence interpreting it: printf '{"tool_key":"plaid_transfer_application","input":{"application_url":"https://dashboard.plaid.com/transfer/application"}}' | ODIN_DIR=/var/odin ODIN_ROOT=/var/odin ODIN_BROWSER_SERVER_URL=http://127.0.0.1:19227 bash /home/orchestrator/odin-os/scripts/drivers/plaid-transfer-application.sh
/runs show <run-id>
```

Expected: the real `odin` shell stores the substantive Plaid result in the run transcript and memory summary.
