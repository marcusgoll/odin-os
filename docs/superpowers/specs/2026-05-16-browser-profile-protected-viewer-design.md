# Browser Profile Protected Viewer Design

Approval source: active Codex Goal to make browser handoff production-worthy for daily X tasks.

## Goal

Make attended browser handoff usable for daily X work without repeated logins:

- persist browser sessions as Odin-owned profile state
- keep profile material encrypted at rest
- expose NoVNC through an Odin-owned protected viewer route
- prove a later X task starts from saved profile state and does not ask for login
- keep X mutations explicit and approval-gated

## Current State

Odin already owns browser session metadata, login requests, handoff ids, runner records, runner lifecycle events, profile path validation, profile preparation, encrypted fixture artifacts, and materialization cleanup.

Current NoVNC runner can start display, browser, and websockify role commands, then record `viewer_url`, `runner_id`, and safe process metadata. Current docs explicitly block browser profile persistence, profile path reuse, protected routing setup, and authenticated session reuse.

The live stress test proved raw Chrome profile reuse works for X, but that proof used a host wrapper override and a temporary quick tunnel. That is not a product boundary.

## Design

### Managed Persistent Profile

Add an Odin-managed profile lifecycle around existing browser session profile paths.

At rest, profile state is stored as encrypted artifact data using existing `browserprofilecrypto`, `browserprofilekeys`, and `browserprofileartifacts` primitives. Odin must never emit cookies, profile bytes, OAuth tokens, passwords, passkeys, TOTP, or backup codes in command output, HTTP JSON, or audit event payloads.

During a runner start, Odin materializes the encrypted profile into a temporary `0700` directory under the runtime root, passes that materialized path to the browser role command, and records only safe metadata. When the handoff is completed or the runner exits cleanly, Odin re-encrypts the updated materialized profile and removes the temporary directory. Failed or cancelled runners keep profile materialization bounded and must not promote partial state unless the operator explicitly completes the handoff.

### Browser Role Environment

The NoVNC runner should pass a minimal, reviewed environment to the browser command:

- `ODIN_BROWSER_PROFILE_DIR=<materialized-profile-path>`
- `ODIN_BROWSER_START_URL=<domain-aware start URL>`
- no credential values
- no cookie values
- no arbitrary browser flags from untrusted inputs

The browser command remains allowlisted and absolute. The browser role is still operator-attended only.

### Protected Viewer Route

Add an Odin-owned protected viewer URL for handoff records. The operator surface should expose this URL instead of a raw `100.x` or quick-tunnel URL.

The route must require existing Odin admin/session authorization and only resolve active started runners. It may proxy NoVNC/websockify to an approved private upstream, but it must not create public unauthenticated VNC exposure. If the protected route is unavailable, Odin should fail closed and show that the viewer is unavailable rather than falling back to quick tunnel.

### X Daily Task Behavior

For X tasks, Odin should first try a saved verified profile. If a verified encrypted profile exists, the browser runner starts with the saved profile and opens X. Odin records read-only evidence that the account page is already authenticated. If X shows a login prompt or session-invalid state, Odin creates an attended login request.

Any X mutation, including profile bio changes, remains approval-gated after the authenticated page is visible. Login success does not grant mutation authority.

## Tests

Add focused tests for:

- runner start with an encrypted profile artifact materializes a temporary profile and passes only safe env vars to the browser role
- successful handoff completion re-encrypts updated profile state and removes materialization
- JSON, logs, and audit events do not include forbidden credential/profile tokens
- protected viewer route rejects unauthenticated requests
- protected viewer route returns active viewer metadata only for active started runners
- saved verified X session path skips login-request creation when authenticated evidence is present
- X mutation path still requires explicit approval

## Verification

Required proof before calling this done:

- `make build`
- targeted browser/session tests
- targeted HTTP protected viewer tests
- `git diff --check`
- real Odin command sequence showing:
  - encrypted profile artifact exists
  - protected viewer URL is returned instead of quick tunnel
  - X opens from saved profile without login prompt
  - mutation remains approval-gated

## Non-Goals

- automatic credential entry
- automated passkey or MFA handling
- unattended X mutations
- public noVNC exposure
- storing plaintext browser profile state as the final product contract
