# Huginn Trusted Google Session Design

**Date:** 2026-04-17

## Goal

Let `odin-os` reuse a trusted headed Chrome session for Google-backed Plaid login so Huginn can save browser state and resume Plaid checks without starting from a fresh browser flow each run.

## Existing State Found

- `scripts/drivers/plaid-transfer-application.sh` already owns the live Plaid workflow and now reaches Google `2-Step Verification`.
- `scripts/browser/browser-access.sh` already starts the repo-local Huginn server and launches a persistent-profile Chromium instance.
- `scripts/browser/odin-huginn-server.js` already speaks Chrome DevTools Protocol directly for a browser it launches itself.
- `odin-orchestrator/scripts/odin/lib/chrome-cdp-start.sh` already has the missing piece: a reusable, Xvfb-backed real Chrome CDP starter with a persistent profile.
- `odin-orchestrator/scripts/odin/lib/odin-huginn-server.js` already has a `/connect` endpoint for attaching Huginn to an already-running Chrome via CDP.

## What Is Partial

- `odin-os` can launch headed Chrome with a persistent profile, but it does not expose a reusable “connect to trusted browser” path.
- The Plaid driver can classify Google `2-Step Verification`, but it cannot yet prefer a long-lived trusted Chrome session over per-run browser startup.
- Family-Ops can capture the current blocker through `./bin/odin`, but there is no Odin-native session reuse primitive to reduce repeated Google re-auth prompts.

## What Is Missing

1. A repo-local Chrome CDP starter in `odin-os`.
2. A repo-local Huginn `/connect` path in `odin-os`.
3. A small browser helper that starts or reuses trusted Chrome and then attaches Huginn to it.
4. Plaid driver logic that prefers the trusted session path when Google login is involved.

## Recommended Approach

Port only the smallest reusable pieces from `odin-orchestrator`:

1. Add `scripts/browser/chrome-cdp-start.sh` to `odin-os`.
2. Extend `scripts/browser/odin-huginn-server.js` with `/connect` support that reuses an external Chrome CDP endpoint.
3. Extend `scripts/browser/browser-access.sh` with a trusted-session helper that:
   - starts or reuses Chrome CDP
   - starts or reuses the Huginn server
   - connects Huginn to the trusted Chrome session
4. Update `scripts/drivers/plaid-transfer-application.sh` to prefer the trusted-session helper when Google auth is in play, with fallback to the current headed launch.

## Why This Approach

- It reuses existing Odin structures instead of inventing a second browser stack.
- It keeps the change smaller than porting the full `browser-human.sh` library.
- It directly targets the real blocker: session continuity and trust, not Plaid form automation.
- It preserves the current read-only Plaid workflow and CLI evidence path.

## Expected Behavior

- If Google credentials are available, the Plaid driver should try the trusted Chrome CDP path first.
- If a trusted Google session already exists, Huginn should reuse it and skip unnecessary re-auth steps.
- If the trusted-session path is unavailable, the driver should fall back to the current headed persistent-profile launch.
- Family-Ops evidence must still be surfaced only through `./bin/odin`.

## Validation

- Add failing tests for:
  - trusted-session browser helper connection
  - Plaid driver preference for trusted Google session
- Verify focused test suites pass.
- Run the live repo-local Plaid driver.
- Rerun the Family-Ops Plaid task through `ODIN_DIR=/var/odin ./bin/odin` and inspect the stored run.
