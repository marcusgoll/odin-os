# Huginn Google-First Plaid Login Design

**Date:** 2026-04-17

## Goal

Prefer `Sign in with Google` for Plaid login in Huginn when the option is present, while preserving the existing email/password fallback and keeping the behavior reusable for other login flows.

## Existing State Found

- `scripts/drivers/plaid-transfer-application.sh` already owns the live Plaid browser workflow.
- `scripts/browser/browser-access.sh` already provides `browser_snapshot` and `browser_evaluate`.
- `scripts/browser/browser-auth.sh` is currently minimal and only supports credential retrieval and basic login/2FA checks.
- `odin-orchestrator/scripts/odin/lib/browser-auth.sh` already has a stronger reusable contract for login-form detection, including OAuth provider discovery from snapshots.
- The current Plaid driver defaults to email/password even when the page offers `Sign in with Google`.

## Recommended Approach

Extend the local `odin-os` auth helper with a small reusable login detector that can identify OAuth providers from the current snapshot, then update the Plaid driver to:

1. Detect whether the live Plaid login page offers Google.
2. Prefer clicking `Sign in with Google` when it is offered.
3. Fall back to the existing email/password path when Google is not offered or cannot be advanced.
4. Preserve current state classification and evidence capture.

## Why This Approach

- It reuses the existing Huginn snapshot/evaluate pipeline instead of inventing a second browser control path.
- It mirrors an auth capability that already exists in `odin-orchestrator`, avoiding a Plaid-only string-matching fork.
- It keeps the change small and reversible while establishing a reusable login preference primitive in `odin-os`.

## Expected Behavior

- If the Plaid sign-in snapshot shows `Sign in with Google`, Huginn should click that first.
- If the Google path reaches a recognizable blocker or next step, the driver should report that result.
- If Google is absent or clearly unavailable, the driver should continue with the current email/password automation.
- Family-Ops evidence should still be surfaced only through `./bin/odin`.

## Validation

- Focused integration tests for Google-first selection, fallback behavior, and result classification.
- Direct live driver verification against the repo-local Huginn browser server.
- Real `./bin/odin` rerun for the Family-Ops Plaid task after the Odin-OS fix.
