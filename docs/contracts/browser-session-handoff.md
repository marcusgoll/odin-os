---
title: Browser Session Handoff Contract
status: partially implemented
date: 2026-05-06
---

# Browser Session Handoff Contract

This contract defines the Odin-native handoff for manual Huginn browser login and reusable authenticated read-only sessions. The session metadata, login request metadata, manual verification metadata, profile path allocation, and explicit empty profile directory preparation slices are implemented. Real NoVNC/Tailscale handoff, browser launch, browser profile persistence, login automation, read-only verification checks, and authenticated session attachment remain future work.

## Existing State

- `odin browser run` already routes through the existing goal-backed read-only browser executor.
- `internal/executors/browser` validates goal ID, objective, allowed domains, start URLs, limits, and read-only actions before writing `browser_readonly` goal evidence.
- `internal/adapters/huginnbrowser.LiveAdapter` is selected only by explicit environment configuration, requires an allowlisted command, and rejects live response fields that imply forms, messages, purchases, deletes, sessions, or other mutations.
- `bin/huginn-browser-worker` supports explicit `mode:"browser"` and otherwise defaults to bounded fetch. Browser mode currently uses a fresh temporary Chromium profile, records local evidence, and logs `no_cookies_or_session_profile`.
- Goal state, goal evidence, blockers, and audit events are persisted in SQLite. Goal runner ticks do not execute created or planned goals, and approved goals block when no executor/action exists.
- `odin browser session login-request --id <session_id> --json` records metadata-only manual login requests with `handoff_url: null` until a real private handoff service exists.
- `odin browser session login-requests --id <session_id> --json` lists persisted login request metadata for one session.
- `odin browser session verify --id <session_id> [--login-request-id <id>] --json` records metadata-only operator verification, sets `last_verified_at`, moves the session to `verified`, and completes the login request when one is provided.
- `odin browser session prepare-profile --id <session_id> --json` explicitly creates the empty profile directory under `ODIN_ROOT` and records an audit event without writing browser files.
- Older Huginn/Plaid/Google notes describe narrow attended browser needs, but they do not define a durable Odin browser session profile authority.

## Non-Goals

- No automated username, password, passkey, TOTP, recovery-code, or 2FA handling.
- No password, TOTP seed, backup code, OAuth refresh token, or recovery-secret storage.
- No form submit, message send, purchase, account change, delete, or external mutation execution.
- No NoVNC implementation in this slice.
- No cookies, browser profile files, or profile bytes are created by this design.
- Empty profile directory preparation does not write browser files, cookies, storage state, credentials, or profile bytes.
- No browser-observed account binding or read-only domain verification check is performed by the metadata-only verification slice.
- No Codex, Huginn, or browser executor implementation is added by the metadata slices.

## Session Concepts

### Session Profile

A session profile is the Odin record for one reusable authenticated browser state. SQLite is the metadata authority. Browser profile files live under `ODIN_ROOT` and are referenced by metadata; the profile directory is never a second authority.

Required metadata fields for the future store slice:

- `id`: stable SQLite ID.
- `profile_key`: operator-readable unique key.
- `status`: `created`, `login_requested`, `verified`, `expired`, or `revoked`.
- `domain`: canonical registrable domain or hostname boundary.
- `account_label`: operator-provided label, such as `marcus-aa` or `marcus-robinhood`.
- `account_subject_hash`: optional hash of a stable account identifier when the site reveals one after login.
- `permission_tier`: one of the tiers below.
- `allowed_goal_types`: explicit list of goal categories this profile may support.
- `profile_path`: relative path under `ODIN_ROOT/browser-sessions/profiles/<profile_key>`.
- `profile_path_exists`: operator JSON field computed from the runtime filesystem; it is not persisted as a second authority.
- `encrypted_at_rest`: boolean assertion recorded by the implementation.
- `expires_at`: required expiration timestamp.
- `reauth_after`: optional earlier timestamp when domain policy requires fresh login.
- `last_verified_at`: timestamp of last successful verification.
- `created_by`, `created_at`, `updated_at`, `revoked_at`, `revoked_by`, `revoke_reason`.

### Domain and Account Binding

A session profile binds to one domain/account pair. It must not be silently reused across domains, subdomain families, user accounts, or tenants.

Rules:

- `domain` is enforced before any browser launch or navigation.
- `allowed_domains` from the goal browser request must be a subset of the session domain policy.
- If verification observes an account identifier, Odin stores only a hash or non-secret label, not the raw credential.
- If the observed account no longer matches the profile binding, verification fails and the profile cannot be used.
- One goal may reference at most one authenticated session profile unless a later contract defines multi-account joins.

### Permission Tiers

Session reuse is permitted only by an explicit tier:

- `public_readonly`: no authenticated profile may be attached. This matches the current browser executor behavior.
- `authenticated_readonly`: the profile may be attached for read-only navigation, snapshot, extraction, and screenshot evidence on allowed domains.
- `attended_mutation_candidate`: reserved for future operator-attended flows. It must not be executable by the goal runner until a separate approval resolver and mutation contract exist.
- `revoked`: profile cannot be used.

The initial implementation slice should support only `authenticated_readonly` plus revoked/expired denials. Mutation tiers remain documented but unavailable.

### Session Lifecycle

The session lifecycle is:

1. `created`: metadata exists, no login has been requested, and no profile may be attached to a browser run.
2. `login_requested`: Odin has created a short-lived manual login handoff, but the operator has not completed verification.
3. `verified`: read-only verification passed for the bound domain/account, expiration is active, and policy may allow attachment to matching authenticated read goals.
4. `expired`: expiration or reauth policy requires manual login again before reuse.
5. `revoked`: operator or policy permanently disables reuse until a new profile is created.

Allowed lifecycle transitions for the metadata implementation are `created -> login_requested -> verified`, `created -> verified` through explicit operator verification, `verified -> expired`, `expired -> login_requested`, and any non-revoked state to `revoked`. When a login request exists, verification should complete that request so manual login and verification remain auditable as separate records.

### Expiration and Reauth Rules

Every verified profile must have an expiration. Odin must deny session attachment when:

- `status` is not `verified`.
- `expires_at` or `reauth_after` is in the past.
- domain policy requires reauth because the site presented a login page, 2FA challenge, account switcher, suspicious-login prompt, or consent screen.
- the profile has not been verified after a key policy version change.

Reauth never means automated login. Reauth returns the goal to human login handoff.

### Allowed Goal Types

The first supported goal types should be review/evidence categories only:

- `authenticated_read`: collect account-visible evidence for operator review.
- `research_verification`: verify facts visible only after login.
- `statement_or_status_read`: read balances, statements, schedule, transfer status, or account status without mutation.

Forbidden goal types for session reuse:

- `purchase`
- `message_send`
- `social_post`
- `account_change`
- `transfer_submit`
- `credential_change`
- `profile_update`
- `delete_or_cancel`

## Manual Login Flow

1. A goal requires authenticated read evidence for a domain.
2. The goal runner or operator command checks for a verified compatible session profile.
3. If none exists, Odin records a goal blocker and moves the goal to `waiting_for_human_login` by extending the existing goal lifecycle in a future schema slice. Until that status exists, use the closest existing waiting status and record the precise reason in blocker/evidence payloads.
4. Odin appends `goal.waiting_for_human_login` with the goal ID, domain, requested session tier, reason, and operator instructions.
5. The operator runs `odin browser session login --profile <profile_key>` or opens the returned handoff URL.
6. The handoff URL is reachable only on the operator-approved private network path, such as Tailscale to a NoVNC endpoint. It must be time-limited and bound to the session profile.
7. The operator completes username, password, passkey, 2FA, consent, and account selection manually in the remote browser.
8. Odin saves only the encrypted browser profile state, never credentials or 2FA secrets.
9. The operator runs `odin browser session verify --id <session_id> [--login-request-id <id>] --json`, or a future login flow asks Odin to verify after the browser closes.
10. The current metadata-only verification records operator-attested verification, sets `last_verified_at`, marks the session `verified`, and optionally completes the login request. A later slice must add read-only checks for domain match, account binding match when visible, no active login challenge, and optional operator-approved URL snapshot before verified profiles can be attached to browser runs.
11. Odin appends `browser.session_status_changed` and `browser.session_verified` with status `verified`, and `browser.session_login_completed` when a login request is completed.
12. The blocked goal resumes from waiting state only after policy re-evaluates that the verified profile tier allows the requested read-only goal type. It must not transition to `approved_for_execution` unless a normal approval path already did that separately.

## CLI Contract

The metadata foundation extends the existing `odin browser` command group:

```bash
odin browser session create --name <name> --domain <domain> --permission-tier <tier> [--account-hint <hint>] [--profile-path <path>] --json
odin browser session list --json
odin browser session show --id <id> --json
odin browser session status --id <id> --status <status> --json
odin browser session revoke --id <id> --json
odin browser session login-request --id <id> --json
odin browser session login-requests --id <id> --json
odin browser session verify --id <id> [--login-request-id <id>] --json
odin browser session prepare-profile --id <id> --json
```

`--permission-tier authenticated_read` is accepted by the CLI as an operator-facing alias for stored tier `authenticated_readonly`. If `--profile-path` is omitted, Odin records the metadata-only default `browser-sessions/profiles/<sanitized-name>` and does not create a directory. Explicit profile paths must remain under `browser-sessions/profiles/`, must be relative to `ODIN_ROOT`, and must not contain path traversal.

`login-request` creates request metadata only. Its JSON envelope returns a `login_request` object with `handoff_url: null` until the future private handoff service can provide a short-lived operator URL. It must not launch a browser, write a browser profile, store credential material, or mark the session verified.

`verify` records metadata-only operator verification. It must not launch a browser, inspect a profile directory, store credential material, or approve/execute a goal. Revoked sessions cannot be verified. Expired or cancelled login requests cannot be completed.

`prepare-profile` creates only the empty allocated profile directory under `ODIN_ROOT`, applies restrictive directory permissions where supported, is idempotent when the directory already exists, and appends `browser.session_profile_prepared`. Revoked sessions cannot prepare profiles. The command must not write browser files, cookies, storage state, credentials, or profile bytes.

JSON output should follow the existing Odin style: stable top-level envelopes, snake-case keys, and explicit IDs. Suggested envelopes:

```json
{
  "session": {
    "id": 1,
    "name": "marcus-example",
    "status": "verified",
    "domain": "example.com",
    "account_hint": "marcus-example",
    "permission_tier": "authenticated_readonly",
    "profile_path": "browser-sessions/profiles/marcus-example",
    "profile_path_exists": false,
    "created_at": "2026-05-06T00:00:00Z",
    "updated_at": "2026-05-06T00:00:00Z",
    "last_verified_at": "2026-05-06T00:00:00Z"
  }
}
```

Implemented `prepare-profile --json` returns preparation metadata without secrets:

```json
{
  "profile": {
    "session_id": 1,
    "profile_path": "browser-sessions/profiles/marcus-example",
    "profile_path_exists": true,
    "created": true
  }
}
```

Implemented `login-request --json` returns metadata without secrets:

```json
{
  "login_request": {
    "id": 1,
    "session_id": 1,
    "status": "requested",
    "handoff_url": null,
    "expires_at": "2026-05-06T00:10:00Z",
    "completed_at": null,
    "created_at": "2026-05-06T00:00:00Z",
    "updated_at": "2026-05-06T00:00:00Z"
  }
}
```

`revoke` is always mutating and must require a reason. `verify` is mutating when it changes status, binding, expiration, or verification timestamps.

## Storage Contract

SQLite tables for browser sessions are additive and must not replace goal, intake, approval, or runtime event tables.

Tables:

- `browser_session_profiles`: implemented profile metadata and policy binding.
- `browser_session_login_requests`: implemented metadata-only manual login requests with status, optional future handoff URL, expiration, completion timestamp, and audit timestamps.
- `browser_session_events`: optional profile-local lifecycle detail if the global runtime events stream alone is not sufficient for efficient profile show/history.
- `browser_session_goal_links`: explicit goal/profile relation with reason, requested tier, and verification evidence references.

Browser profile files:

- Default root: `ODIN_ROOT/browser-sessions/profiles/<sanitized-name>`.
- Paths stored in SQLite must be relative to `ODIN_ROOT`.
- Odin allocates profile paths as metadata only. It must not create browser profile contents, cookies, storage state, or credential material during session creation.
- Odin creates profile directories only through explicit `prepare-profile`. The prepared directory must be empty immediately after creation.
- `profile_path_exists` reports whether the allocated relative path currently exists under `ODIN_ROOT`; the field is informational and does not make the filesystem a profile registry.
- Profile files must be encrypted at rest before `authenticated_readonly` is enabled. If host-level encryption is the first slice, the CLI must report `encrypted_at_rest=false` or `encryption_gap=host_only` and policy must deny reuse unless the operator explicitly accepts that documented gap in a later policy slice.
- No credential material may be written to Odin-specific metadata, events, logs, screenshots, or evidence payloads.

## Policy Gates

Policy must fail closed. A browser session may be attached to a goal only when all checks pass:

- goal status is not terminal and is not already running under a conflicting profile.
- goal type is listed in `allowed_goal_types`.
- goal requested action is read-only: `read`, `navigate`, `snapshot`, or `extract`.
- permission tier is `authenticated_readonly`.
- profile status is `verified`.
- profile is not expired, revoked, or awaiting reauth.
- requested URL domains match the profile domain/account binding.
- live browser adapter command is allowlisted.
- the result contract contains no mutation fields.

Forbidden without a later approved contract:

- form submit
- message send
- purchase
- account setting change
- transfer submit
- social post
- destructive account operation
- background credential refresh

If a future attended action is approved, it must use the existing approval system and append approval/runtime events before any external mutation. Session existence alone never grants mutation authority.

## Audit Events

All session state changes must append runtime events in the same SQL transaction as the profile row mutation. Browser session metadata uses a `browser_session` stream type rather than overloading the goal stream for profile-local lifecycle.

Required event types for the metadata foundation:

- `browser.session_created`
- `browser.session_status_changed`
- `browser.session_verified`
- `browser.session_revoked`
- `browser.session_profile_prepared`
- `browser.session_login_requested`
- `browser.session_login_completed`
- `browser.session_login_expired`
- `goal.waiting_for_human_login`

`browser.session_status_changed` covers profile status changes. Login request metadata uses specific request lifecycle events.

Suggested payload fields:

- `session_id`
- `profile_key`
- `domain`
- `account_label`
- `permission_tier`
- `allowed_goal_types`
- `goal_id`
- `status`
- `previous_status`
- `reason`
- `actor`
- `created`
- `profile_path`
- `expires_at`
- `handoff_expires_at`
- `last_verified_at`
- `policy_decision`

Audit payloads must not contain passwords, cookies, tokens, passkey material, TOTP values, backup codes, raw credential prompts, or screenshot text that reveals secrets.

## Goal Integration

Authenticated browser reads remain goal-owned work. Session profiles only provide reusable browser state.

Rules:

- A waiting login state must be represented as a goal status/blocker/evidence path, not as a detached sidecar queue.
- A session profile can unblock a goal only by satisfying policy; it does not approve the goal.
- Converted intake goals, created goals, and planned goals must remain unapproved after session creation or verification.
- The goal runner must not execute an authenticated read unless the goal is already in the normal executable state and the session policy check passes.

## Implementation Slices

1. Contract tests and store schema: add browser session profile metadata tables, event constants, and tests proving create/verify/revoke append runtime events in the same transaction.
2. CLI metadata surface: add `odin browser session create|list|show|status|revoke` with JSON output, no browser launch, and fail-closed policy.
3. Login request metadata surface: implemented `odin browser session login-request|login-requests` to record and inspect metadata-only manual login requests with `handoff_url: null`.
4. Manual verification metadata surface: implemented `odin browser session verify` to set session status `verified`, record `last_verified_at`, and optionally complete a login request, with no browser launch or credential handling.
5. Profile path and empty directory preparation: implemented safe profile path allocation plus explicit `odin browser session prepare-profile` for empty-directory creation.
6. Goal waiting integration: add `waiting_for_human_login` status or blocker-specific goal event handling, then prove the runner skips waiting goals.
7. Encrypted profile storage: add encrypted profile root handling and policy denial for unencrypted profiles.
8. Authenticated read-only attachment: allow `odin browser run` and goal runner evidence collection to attach a verified `authenticated_readonly` profile for allowed domains only.
9. NoVNC/Tailscale handoff service: add a private-network browser handoff endpoint with short-lived tokens after the metadata and policy gates are proven.
10. Operator runbooks and overview visibility: surface session health, expiring profiles, and waiting login goals in existing overview lanes if a clean projection lane exists.

## Best Operating Rule

Keep session metadata in SQLite, profile bytes under `ODIN_ROOT`, and all state changes in the existing runtime event stream. Do not create a second browser session registry, credential vault, or sidecar goal authority.
