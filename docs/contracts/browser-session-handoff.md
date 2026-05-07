---
title: Browser Session Handoff Contract
status: partially implemented
date: 2026-05-06
---

# Browser Session Handoff Contract

This contract defines the Odin-native handoff for manual Huginn browser login and reusable authenticated read-only sessions. The session metadata, login request metadata, read-only handoff lookup, handoff runner metadata CLI, handoff runner process-boundary skeleton, bounded fixture runner, bounded exec process runner, fixture supervisor lifecycle wiring, NoVNC dry-run planning, NoVNC fixture-safe runner launch, real NoVNC command config validation, display/browser/noVNC dry-run detection metadata, manual verification metadata, profile path allocation, explicit empty profile directory preparation, and profile storage policy gate slices are implemented. Real NoVNC/Tailscale handoff, browser launch, browser profile persistence, login automation, read-only verification checks, and authenticated session attachment remain future work.

## Existing State

- `odin browser run` already routes through the existing goal-backed read-only browser executor.
- `internal/executors/browser` validates goal ID, objective, allowed domains, start URLs, limits, and read-only actions before writing `browser_readonly` goal evidence.
- `internal/adapters/huginnbrowser.LiveAdapter` is selected only by explicit environment configuration, requires an allowlisted command, and rejects live response fields that imply forms, messages, purchases, deletes, sessions, or other mutations.
- `bin/huginn-browser-worker` supports explicit `mode:"browser"` and otherwise defaults to bounded fetch. Browser mode currently uses a fresh temporary Chromium profile, records local evidence, and logs `no_cookies_or_session_profile`.
- Goal state, goal evidence, blockers, and audit events are persisted in SQLite. Goal runner ticks do not execute created or planned goals, and approved goals block when no executor/action exists.
- `odin browser session login-request --id <session_id> [--handoff-base-url <url>] --json` records metadata-only manual login requests with an opaque `handoff_id`; `handoff_url` is null unless a private base URL is provided.
- `odin browser session login-requests --id <session_id> --json` lists persisted login request metadata for one session.
- `odin browser session handoff show --handoff-id <id> --json` resolves safe manual-login metadata for one valid, unexpired requested handoff without mutating runtime state.
- `odin browser session runner create|list|show|plan-novnc|status|cancel --json` records, inspects, or dry-run plans metadata-only browser handoff runner records without launching a process.
- `internal/runtime/browserhandoff` defines the future runner process boundary request/response types, a default stub runner that returns structured `not_implemented` responses, and an explicit env-gated fixture runner for harmless local process lifecycle proof.
- `internal/runtime/browserhandoff` also defines a NoVNC dry-run planner and real-launch config validator that validate command paths, allowlists, bind address, private base URL, and timeout, then return normalized config or planned metadata without launching processes. The dry-run planner records display, browser, and noVNC/websockify detection metadata for each configured command.
- `internal/runtime/browserhandoff` defines a bounded process supervisor abstraction with fake-runner and harmless local exec-runner tests for command validation, timeout kill handling, cancellation handling, bounded stdout/stderr capture, and safe process metadata. `runner start` wires both the explicit fixture mode and the explicit NoVNC fixture-safe mode through this supervisor using only allowlisted harmless local commands. It does not add a real browser or NoVNC service launch path.
- `odin browser session verify --id <session_id> [--login-request-id <id>] --json` records metadata-only operator verification, sets `last_verified_at`, moves the session to `verified`, and completes the login request when one is provided.
- `odin browser session prepare-profile --id <session_id> --json` explicitly creates the empty profile directory under `ODIN_ROOT` and records an audit event without writing browser files.
- Browser session JSON reports `profile_storage_policy`. The default is `encrypted_required`, and `CanWriteBrowserProfile` denies writes for every current policy value until encrypted profile storage is implemented.
- Older Huginn/Plaid/Google notes describe narrow attended browser needs, but they do not define a durable Odin browser session profile authority.

## Non-Goals

- No automated username, password, passkey, TOTP, recovery-code, or 2FA handling.
- No password, TOTP seed, backup code, OAuth refresh token, or recovery-secret storage.
- No form submit, message send, purchase, account change, delete, or external mutation execution.
- No real NoVNC service implementation in this slice.
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
- `profile_storage_policy`: `disabled`, `prepared_unencrypted`, or `encrypted_required`.
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

## Future NoVNC/Tailscale Handoff Runner Contract

The handoff runner is an operator-attended process boundary for manual login only. Its future process implementation will launch one temporary visible browser session, expose its viewer only over an operator-approved private network path such as Tailscale to a NoVNC endpoint, and then stop after completion, expiration, or cancellation. Odin remains the metadata, policy, and audit authority. The runner must never become a credential collector, browser automation agent, profile registry, or goal execution authority.

The SQLite runner metadata store, metadata CLI, typed process-boundary skeleton, and fixture-safe local process launch are implemented. Real browser process implementation and service wiring remain design only. This slice does not add HTTP handlers, non-fixture process launch, NoVNC services, Tailscale services, browser profile writes, cookie writes, or credential storage.

### Local Process Topology

The first real runner implementation must keep one local supervised topology per browser handoff runner record:

1. **Browser process**: one visible browser instance launched for the linked session/login request only. The browser starts on the configured `allowed_domain` or a later approved start URL under that domain. It must run in a process group owned by the runner supervisor so cancellation and timeout cleanup can stop the full tree.
2. **Virtual display or VNC server**: one display boundary for that browser process. The implementation may use a combined VNC server/display command or separate virtual display plus VNC commands, but Odin must treat them as child processes of the same runner lifecycle.
3. **noVNC/websockify viewer**: one local viewer proxy that exposes the display through a private URL. The viewer process must be bound to loopback or an explicitly configured private interface by default.
4. **Lifecycle supervisor**: Odin-owned runner code that validates config, starts children in order, records safe metadata, watches exits, enforces timeout, and performs cleanup. The supervisor is not a browser automation agent and must not inspect, collect, or persist credential material.

The runner metadata row remains the durable authority. Process IDs, runner IDs, bind addresses, and viewer URLs are operational metadata attached to the row; they must not create a second runner registry.

### Required Runner Config

The future NoVNC runner must fail closed unless all required config is present and valid. Config may come from environment variables first, then a later Odin config file only if it uses the same validation rules.

Required fields for the real process boundary:

- `ODIN_BROWSER_HANDOFF_RUNNER=novnc`: selects the real local NoVNC runner. Empty or `stub` continues to select `StubRunner`; `fixture` continues to select the bounded fixture runner.
- `ODIN_NOVNC_BROWSER_COMMAND`: absolute path to the browser executable.
- `ODIN_NOVNC_DISPLAY_COMMAND`: absolute path to the virtual display or VNC/display command when the platform requires one.
- `ODIN_NOVNC_WEBSOCKIFY_COMMAND`: absolute path to the noVNC or websockify command.
- `ODIN_NOVNC_ALLOWED_COMMANDS`: comma-separated common allowlist of absolute executable paths. Every selected command must match one clean allowlist entry exactly.
- `ODIN_NOVNC_BIND_ADDR`: optional bind host and port for the viewer proxy. Empty defaults to `127.0.0.1:0`.
- `ODIN_NOVNC_PRIVATE_BASE_URL`: private operator URL base used to generate `viewer_url`, for example a Tailscale-only HTTPS origin.
- `ODIN_NOVNC_TIMEOUT_SECONDS`: positive timeout capped by policy and never longer than the linked login request expiration.

Optional fields:

- Future explicit browser, display, and websockify arg fields may be added only if they remain structured argument lists passed without a shell. This slice does not add arg loading for the real NoVNC boundary.

Config validation rules:

- Every configured command path must be absolute, clean, and present in `ODIN_NOVNC_ALLOWED_COMMANDS`.
- Dry-run display, browser, and noVNC/websockify detection must also verify each configured command exists and has an executable bit before reporting `validation_status: "valid"`.
- The implementation must not search `PATH`, invoke a shell, or accept command strings that combine executable and args.
- `bind_addr` must be loopback by default. A non-loopback private interface requires an explicit later policy option and must still reject public wildcard binds such as `0.0.0.0`.
- `private_base_url` must be absolute `http` or `https`; the first real slice should prefer `https` for any non-loopback origin.
- `public_base_url` remains unsupported and must be rejected until a separate security contract approves public exposure.
- Missing config must produce a structured `failed` runner result and audit event, not a partial process launch.
- `LoadNoVNCLaunchConfigFromEnv` and `ValidateNoVNCLaunchConfig` validate the NoVNC launch config used by the skeleton and future real process boundary. They must not import `os/exec`, start child processes, create viewer routes, write profile data, or append runtime events by themselves.

### Runner Request

A future runner start command or service call must use a stable JSON request envelope:

```json
{
  "handoff_runner_request": {
    "session_id": 1,
    "login_request_id": 1,
    "handoff_id": "opaque-handoff-id",
    "profile_path": "browser-sessions/profiles/marcus-example",
    "allowed_domain": "example.com",
    "timeout_seconds": 600,
    "bind_addr": "127.0.0.1:0",
    "private_base_url": "https://odin-handoff.tailnet.local",
    "public_base_url": null
  }
}
```

Field rules:

- `session_id`, `login_request_id`, and `handoff_id` must refer to one valid, non-expired requested login request and its linked non-revoked browser session.
- `profile_path` must be the session profile path already recorded in SQLite, relative to `ODIN_ROOT`, and must pass the same path safety rules as `prepare-profile`.
- `allowed_domain` must equal the session domain policy or a stricter allowed hostname. The runner must reject broader or unrelated domains.
- `timeout_seconds` must be bounded by policy and must not exceed the linked login request expiration.
- `bind_addr` must default to loopback or another explicitly configured private interface. Binding to a public interface is forbidden unless a later policy contract explicitly allows it.
- `private_base_url` is the preferred viewer origin for Tailscale or another private network route.
- `public_base_url` must be null for the initial implementation. A non-null public base URL requires a later explicit security contract and approval path.

### Runner Response

A future non-stub runner start result must use a stable JSON response envelope:

```json
{
  "handoff_runner": {
    "runner_id": "browser-handoff-runner-1",
    "process_id": 12345,
    "session_id": 1,
    "login_request_id": 1,
    "handoff_id": "opaque-handoff-id",
    "status": "started",
    "viewer_url": "https://odin-handoff.tailnet.local/session/browser-handoff-runner-1",
    "expires_at": "2026-05-06T00:10:00Z",
    "error_code": null,
    "error_message": null
  }
}
```

Allowed response statuses are:

- `started`: runner process and private viewer route are ready for manual operator login.
- `failed`: runner did not start or failed before a usable viewer URL existed.
- `expired`: timeout or login request expiration stopped the runner.
- `completed`: operator-attested completion was recorded and the runner stopped.

`viewer_url` must be absent or null for `failed` results. `error_code` and `error_message` are required for `failed` and optional for terminal non-failure states. `process_id` may be null when an implementation uses a supervised runner ID rather than an OS process ID, but `runner_id` must always be present.

### Runner Process Boundary Skeleton

`internal/runtime/browserhandoff` defines the safe process boundary shape before any real process runner exists:

- `StartRequest`: `session_id`, `login_request_id`, `handoff_id`, `profile_path`, `allowed_domain`, `timeout_seconds`, optional loopback `bind_addr`, optional `private_base_url`, and unsupported `public_base_url`.
- `StartResponse`: safe runner metadata, status, optional future process/viewer fields, and structured error metadata.
- `CancelRequest`: future runner ID plus optional reason.
- `StatusResponse`: structured status for cancellation and later status lookups.

The default `StubRunner` validates required request fields and returns `not_implemented`. It must not import `os/exec`, launch a browser, start NoVNC/Tailscale, write profile data, create viewer URLs, or store credential material. `public_base_url` is rejected until a later security contract explicitly permits it. `bind_addr` must stay on loopback in the stub boundary.

The `FixtureRunner` is selected only when `ODIN_BROWSER_HANDOFF_RUNNER=fixture`. It requires `ODIN_BROWSER_HANDOFF_FIXTURE_COMMAND` to be an absolute path and that same path to appear in comma-separated `ODIN_BROWSER_HANDOFF_FIXTURE_ALLOWED_COMMANDS`. Optional `ODIN_BROWSER_HANDOFF_FIXTURE_ARGS` supplies explicit fixture arguments, and `ODIN_BROWSER_HANDOFF_FIXTURE_TIMEOUT_SECONDS` bounds execution. The fixture runner does not use a shell, does not create viewer URLs, does not launch a browser, and must only be used for harmless local test commands such as process lifecycle fixtures. It can return `started`, `failed`, or `expired` with safe `runner_id` and `process_id` metadata.

The `NoVNCRunner` fixture-safe launch path is selected only when `ODIN_BROWSER_HANDOFF_RUNNER=novnc`. It loads and validates `ODIN_NOVNC_*` launch config through `LoadNoVNCLaunchConfigFromEnv` and `ValidateNoVNCLaunchConfig`, then starts the configured `display`, `browser`, and `novnc/websockify` roles through the bounded supervisor. This path is for harmless local fixture commands only; command basenames are currently restricted to `true`, `false`, `sleep`, and `yes` even when another absolute path is present in the allowlist. It records safe runner metadata, generates a private `viewer_url` from the validated private base URL, waits for child process completion, and returns `completed`, `failed`, or `expired`. It must not launch Chrome/Chromium, start real NoVNC/websockify/Tailscale services, write profile data, or store credential material.

### Bounded Process Supervisor Abstraction

`internal/runtime/browserhandoff` includes a reusable process supervisor contract for the future display, browser, and noVNC/websockify children. The abstraction is intentionally separate from `RunnerFromEnv`, `StubRunner`, `FixtureRunner`, and `runner start`; this slice does not wire it into live runner behavior.

`StartProcessRequest` defines:

- `role`: process role such as `display`, `browser`, or `novnc/websockify`.
- `command_path`: absolute executable path.
- `args`: explicit arguments passed without a shell by any future real runner.
- `env`: explicit environment entries.
- `working_directory`: optional absolute working directory.
- `timeout_seconds`: positive timeout for the process.
- `allowed_commands`: absolute command allowlist for the role.

`ProcessHandle` records `pid`, `role`, `command_path`, `started_at`, and status `started`.

`ProcessResult` records `pid`, `role`, `command_path`, `started_at`, optional `exited_at`, status `started`, `exited`, `failed`, `timeout`, or `cancelled`, plus safe stdout/stderr/error metadata. Runner metadata now persists terminal `exited_at` for completed, expired, cancelled, and failed lifecycle transitions. Result payloads must not contain passwords, cookies, bearer tokens, passkey material, TOTP values, backup codes, browser profile bytes, or screenshots.

Supervisor validation is fail-closed:

- command paths must be absolute and exactly present in the role allowlist.
- `timeout_seconds` is required and positive.
- `role` is required.
- optional working directories must be absolute.
- cancellation returns an audited-safe `cancelled` result shape, but this abstraction does not append runtime events by itself.

The current implementation includes both an injected fake `ProcessCommandRunner` for unit tests and an `ExecCommandRunner` for harmless allowlisted local commands. `ExecCommandRunner` uses `exec.CommandContext` without a shell, requires supervisor validation before start, starts commands in a process group, enforces `timeout_seconds`, kills the process group on timeout or cancellation, and captures stdout/stderr up to `4096` bytes per stream by default. The explicit `ODIN_BROWSER_HANDOFF_RUNNER=fixture` path is wired through `BoundedProcessSupervisor` and `ExecCommandRunner` from `runner start`: Odin records `started`, waits for the harmless process to exit, then records `completed`, `expired`, `failed`, or `cancelled` through the existing store transition path and audit event stream. The explicit `ODIN_BROWSER_HANDOFF_RUNNER=novnc` path is also wired through the supervisor, but still only for harmless fixture commands configured as display, browser, and novnc/websockify roles. The exec runner remains separate from real browser handoff behavior; it must not launch Chrome/Chromium, start real NoVNC/websockify, start Tailscale, write profile files, or store credential material.

### Runner Safety Policy

The runner policy is fail-closed:

- Manual login only. The operator performs username, password, passkey, 2FA, consent, and account selection directly in the visible browser.
- No auto-submit, form fill, credential scraping, prompt capture, password manager integration, TOTP handling, recovery-code handling, or OAuth token extraction.
- No cross-domain navigation unless the destination is the configured `allowed_domain` or a narrower approved hostname. Redirects outside policy must stop the runner or require a later explicit approval contract.
- `allowed_domain` is required for every start request and must be validated before process launch.
- `profile_path` must already be allocated in SQLite and the empty profile directory must already be prepared when a persistent profile run is requested.
- No browser profile writes until `profile_storage_policy` and `CanWriteBrowserProfile(session)` allow writes. In the current implementation every policy value denies profile writes, so a future runner must run with ephemeral browser state or stop before persistence.
- Persistent profile mode is forbidden until the profile write policy explicitly allows writes. A prepared empty directory is necessary for persistent mode, but it is not sufficient authorization.
- No cookies, storage state, browser profile bytes, screenshots containing secrets, raw credential prompts, passwords, passkeys, TOTP values, backup codes, OAuth tokens, or bearer tokens may be written to Odin metadata, events, logs, evidence, or profile storage.
- Runner startup does not approve a goal, transition a goal to executable state, satisfy a policy approval, or grant mutation authority.
- Viewer access must be time-limited, bound to the login request, and accessible only through an operator-approved private network path.
- No public viewer URL is generated by default. The first real runner must return `viewer_url` only when it can be derived from validated private config and bound runner metadata.

### Runner Lifecycle

Runner metadata uses a separate lifecycle from browser session status and login request status:

1. `requested`: Odin has accepted the runner request metadata but has not exposed a viewer.
2. `started`: the visible browser and private viewer route are available for manual login.
3. `completed`: the operator completed login and Odin recorded metadata-only completion or later read-only verification.
4. `expired`: timeout or login request expiration stopped the runner before completion.
5. `cancelled`: the operator or policy stopped the runner before completion.

Allowed transitions are `requested -> started`, `requested -> expired`, `requested -> cancelled`, `requested -> failed`, `started -> completed`, `started -> expired`, `started -> cancelled`, and `started -> failed`. Terminal states are `failed`, `completed`, `expired`, and `cancelled`. A terminal runner must not be restarted in place; create a new login request and runner record instead.

Runner lifecycle must not silently mutate session lifecycle. A `completed` runner may complete the linked login request only through the same completion path as `POST /browser/session/handoff/complete`; session verification still requires the existing verification policy for the current slice and stronger browser-observed checks in a later slice.

### Runner Start, Status, Cancel, and Cleanup Behavior

Start behavior:

- Load the runner metadata row and require status `requested`.
- Resolve the linked login request and browser session through the existing handoff lookup validation.
- Validate config before launching any child process.
- Start child processes in dependency order: display/VNC first when needed, browser second, noVNC/websockify last.
- Record `started` only after the viewer proxy is reachable on the configured bind address and the generated `viewer_url` is private and time-limited.
- If any process fails before that point, terminate already-started children and record `failed` with a structured `error_code`.

Status behavior:

- `runner show --id <id> --json` remains the operator status read path.
- A future status implementation may inspect process liveness, but SQLite remains the state authority. If process liveness disagrees with SQLite, the runner must reconcile by appending an audited transition such as `failed`, `expired`, or `cancelled`; it must not silently rewrite state.
- Stale `started` runners whose process group no longer exists must be marked `failed` or `expired` with an explicit stale-runner reason.

Cancel behavior:

- Cancel must signal the runner process group, wait a bounded grace period, then kill remaining children.
- Cancel must remove transient viewer/display resources owned by the runner.
- Cancel records `cancelled` through the existing store transition path. If cleanup cannot fully complete, record safe error metadata and keep the terminal state auditable.

Timeout cleanup:

- The supervisor must stop the process group when `ODIN_NOVNC_TIMEOUT_SECONDS` expires or when the linked login request expires, whichever comes first.
- Timeout records `expired` through the existing store transition path.
- Cleanup must be idempotent so repeated cancel/status/tick calls do not relaunch or double-record terminal transitions.

Stale runner cleanup:

- A later `odin serve` or maintenance tick may scan for active runners past expiration and call the same cleanup path.
- The scan must be metadata-driven from SQLite, not from a sidecar pid file registry.
- Missing pid/process metadata must fail closed by marking the runner terminal when the viewer cannot be proven active and private.

### Runner Audit Events

Runner state changes append runtime events in the same transaction as runner metadata changes. Event payloads must follow the existing browser session audit redaction rules.

Required event types:

- `browser.handoff_runner_requested`
- `browser.handoff_runner_started`
- `browser.handoff_runner_expired`
- `browser.handoff_runner_cancelled`
- `browser.handoff_runner_completed`
- `browser.handoff_runner_failed`

Suggested payload fields:

- `runner_id`
- `process_id`
- `session_id`
- `login_request_id`
- `handoff_id`
- `allowed_domain`
- `bind_addr`
- `viewer_url`
- `expires_at`
- `status`
- `previous_status`
- `reason`
- `actor`
- `profile_path`
- `profile_storage_policy`
- `error_code`
- `policy_decision`

Runner audit payloads must not contain passwords, cookies, bearer tokens, passkey material, TOTP values, backup codes, profile bytes, raw credential prompts, or screenshot text that reveals secrets.

## CLI Contract

The metadata foundation extends the existing `odin browser` command group:

```bash
odin browser session create --name <name> --domain <domain> --permission-tier <tier> [--account-hint <hint>] [--profile-path <path>] --json
odin browser session list --json
odin browser session show --id <id> --json
odin browser session status --id <id> --status <status> --json
odin browser session revoke --id <id> --json
odin browser session login-request --id <id> [--handoff-base-url <url>] --json
odin browser session login-requests --id <id> --json
odin browser session handoff show --handoff-id <id> --json
odin browser session runner create --login-request-id <id> --json
odin browser session runner list --login-request-id <id> --json
odin browser session runner show --id <id> --json
odin browser session runner start --id <id> --json
odin browser session runner plan-novnc --id <id> --browser-command <path> --browser-allowed-command <path> --display-command <path> --display-allowed-command <path> --novnc-command <path> --novnc-allowed-command <path> --bind-addr <addr> --private-base-url <url> --timeout-seconds <n> --json
odin browser session runner status --id <id> --status <started|completed|expired|cancelled|failed> --json
odin browser session runner cancel --id <id> --json
odin browser session verify --id <id> [--login-request-id <id>] --json
odin browser session prepare-profile --id <id> --json
```

`odin serve` also exposes a read-only inspection route. JSON remains the default response for API clients:

```http
GET /browser/session/handoff?handoff_id=<id>
```

The same route returns a static HTML shell when the request sends `Accept: text/html` or includes `format=html`:

```http
GET /browser/session/handoff?handoff_id=<id>&format=html
```

Operators can also record metadata-only manual completion through the served handoff surface:

```http
POST /browser/session/handoff/complete
```

`--permission-tier authenticated_read` is accepted by the CLI as an operator-facing alias for stored tier `authenticated_readonly`. If `--profile-path` is omitted, Odin records the metadata-only default `browser-sessions/profiles/<sanitized-name>` and does not create a directory. Explicit profile paths must remain under `browser-sessions/profiles/`, must be relative to `ODIN_ROOT`, and must not contain path traversal.

`login-request` creates request metadata only. Odin always records an opaque `handoff_id` with the request and reuses the existing `expires_at` as the metadata expiration. With no base URL, its JSON envelope returns `handoff_url: null`. When `--handoff-base-url <url>` is provided, Odin validates that the base is an absolute `http` or `https` URL and returns a metadata URL with `handoff_id` in the query string. The handoff ID must not encode the browser session ID directly.

The handoff URL is not proof that a browser handoff service exists. Odin now exposes only a read-only HTTP metadata inspection route. This slice does not add NoVNC, Tailscale service, browser launch, browser profile write, credential storage, or session verification. Operators should treat any base URL as a future private-network browser handoff surface only, intended for Tailscale or another operator-approved private path after that service is implemented.

`handoff show` and `GET /browser/session/handoff?handoff_id=<id>` are read-only lookups for safe manual-login metadata. They require a handoff ID, reject missing IDs, unknown handoffs, non-`requested` login requests, expired requests, and revoked or missing linked sessions. They return only the handoff ID, login request ID, session ID, session name, domain, optional account hint, expiration, request status, and `allowed_actions: manual_login_only`. They must not append runtime events, launch a browser, create NoVNC/Tailscale resources, write profile files, or store credential material.

`runner create` resolves the login request and linked session from the existing store, then writes only runner metadata with status `requested`. It rejects missing, completed, expired, cancelled, or otherwise invalid login requests through the store validation path. It does not launch a browser, expose a viewer, start NoVNC/Tailscale, write profile data, or handle credential material. `viewer_url`, `runner_id`, `process_id`, and network fields remain null until a future process-boundary implementation records safe metadata.

`runner start` loads existing runner metadata, validates the linked login request and browser session through the existing handoff lookup rules, and calls the selected `internal/runtime/browserhandoff` runner path. The default stub returns `not_implemented`, so Odin records a safe `failed` runner status through the existing store transition path with `error_code: "not_implemented"`. The explicit NoVNC fixture-safe mode validates configured `ODIN_NOVNC_*` launch settings, starts the configured harmless display, browser, and `novnc/websockify` commands through the bounded supervisor, records `started` with private `viewer_url`, `runner_id`, process metadata, bind address, and private base URL, waits for child process exit, and then records `completed`, `expired`, or `failed` through the existing event stream. The explicit fixture mode uses the bounded process supervisor and `ExecCommandRunner` to start only the configured allowlisted harmless command, record `started` with `runner_id`, `process_id`, and `started_at`, wait for process exit, and then record `completed`, `expired`, `failed`, or `cancelled` with terminal `exited_at` and safe error metadata. These paths must not launch a browser, create real NoVNC/Tailscale resources, write profile files, or store credential material.

`runner plan-novnc` loads existing runner metadata, validates the linked login request and browser session through the existing handoff lookup rules, and calls the pure NoVNC dry-run planner. It returns planned command roles, validated bind/private URL/timeout config, a planned private `viewer_url`, and display/browser/noVNC command detection metadata including `detected_path`, `command_role`, and `validation_status` for every planned command. It must not update runner status, append runtime events, launch processes, start browsers, start NoVNC/websockify, create Tailscale resources, write profile files, or store credential material. The command accepts explicit command paths and allowlists as flags; later config-file support must use the same validation rules.

`runner list`, `runner show`, `runner status`, and `runner cancel` expose and mutate only the `browser_handoff_runners` metadata rows. Status changes append runner lifecycle events transactionally through the existing browser session runtime event stream. `runner show --id <id> --json` is the non-mutating runner status inspection path; `runner status` is reserved for explicit lifecycle transitions and accepts `started`, `completed`, `expired`, `cancelled`, and `failed`. Terminal runners cannot be restarted in place.

The HTML shell is static and informational. It displays safe metadata, states that no browser session is launched yet, states that Odin is not collecting credentials, and states that login and 2FA will be manual in a future handoff step. Dynamic values must be escaped. The page must not include external scripts, inline scripts, forms, credential inputs, password fields, or profile/session write affordances.

`POST /browser/session/handoff/complete` accepts either a JSON body or form body with `handoff_id`. It requires the existing Odin admin-token protection and fails closed when admin actions are disabled or unauthenticated. The route validates the same handoff states as the read-only lookup, then records operator-attested metadata by marking the browser session `verified` and the linked login request `completed` through the existing store verification path. It emits the existing browser session runtime events, including status change, session verified, and login completed. This route is not browser proof: it must not launch a browser, inspect a profile directory, store profile bytes, collect credentials, create NoVNC/Tailscale resources, or approve/execute any goal.

Form completion returns a static escaped HTML result page. JSON completion returns a stable `completion` envelope with the handoff ID, login request ID, session ID, session name, domain, optional account hint, session status, login request status, completion timestamp, and `allowed_actions: manual_login_only`. The completion HTML must not include scripts, forms, credential inputs, password fields, textareas, or profile/session write affordances.

`verify` records metadata-only operator verification. It must not launch a browser, inspect a profile directory, store credential material, or approve/execute a goal. Revoked sessions cannot be verified. Expired or cancelled login requests cannot be completed.

`prepare-profile` creates only the empty allocated profile directory under `ODIN_ROOT`, applies restrictive directory permissions where supported, is idempotent when the directory already exists, and appends `browser.session_profile_prepared`. Revoked sessions cannot prepare profiles. The command must not write browser files, cookies, storage state, credentials, or profile bytes.

`profile_storage_policy` is a fail-closed metadata gate. The current default is `encrypted_required`; `disabled` and `prepared_unencrypted` are explicit non-writeable states. Empty directory preparation does not change the policy into write approval. Until encrypted-at-rest profile storage is implemented, `CanWriteBrowserProfile(session)` returns false for every current policy value, including verified sessions.

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
    "profile_storage_policy": "encrypted_required",
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
    "handoff_id": "opaque-handoff-id",
    "handoff_url": null,
    "expires_at": "2026-05-06T00:10:00Z",
    "completed_at": null,
    "created_at": "2026-05-06T00:00:00Z",
    "updated_at": "2026-05-06T00:00:00Z"
  }
}
```

When called with `--handoff-base-url https://odin-handoff.tailnet.local/manual-login`, `handoff_url` may be returned as `https://odin-handoff.tailnet.local/manual-login?handoff_id=<opaque-id>`. This remains metadata-only; no route is served by Odin in this slice.

Implemented `handoff show --json` and `GET /browser/session/handoff?handoff_id=<id>` return read-only manual-login metadata without secrets:

```json
{
  "handoff": {
    "handoff_id": "opaque-handoff-id",
    "login_request_id": 1,
    "session_id": 1,
    "session_name": "marcus-example",
    "domain": "example.com",
    "account_hint": "marcus-example",
    "expires_at": "2026-05-06T00:10:00Z",
    "status": "requested",
    "allowed_actions": "manual_login_only"
  }
}
```

Implemented `POST /browser/session/handoff/complete` returns metadata-only operator-attested completion without secrets:

```json
{
  "completion": {
    "handoff_id": "opaque-handoff-id",
    "login_request_id": 1,
    "session_id": 1,
    "session_name": "marcus-example",
    "domain": "example.com",
    "account_hint": "marcus-example",
    "session_status": "verified",
    "login_request_status": "completed",
    "completed_at": "2026-05-06T00:04:00Z",
    "allowed_actions": "manual_login_only"
  }
}
```

Implemented `runner create --json` returns metadata-only runner details without secrets:

```json
{
  "runner": {
    "id": 1,
    "session_id": 1,
    "login_request_id": 1,
    "handoff_id": "opaque-handoff-id",
    "status": "requested",
    "viewer_url": null,
    "runner_id": null,
    "process_id": null,
    "bind_addr": null,
    "private_base_url": null,
    "public_base_url": null,
    "expires_at": "2026-05-06T00:10:00Z",
    "created_at": "2026-05-06T00:00:00Z",
    "updated_at": "2026-05-06T00:00:00Z",
    "error_code": null,
    "error_message": null
  }
}
```

Implemented `runner start --json` with the current `StubRunner` returns safe failure metadata without secrets:

```json
{
  "runner": {
    "id": 1,
    "session_id": 1,
    "login_request_id": 1,
    "handoff_id": "opaque-handoff-id",
    "status": "failed",
    "viewer_url": null,
    "runner_id": null,
    "process_id": null,
    "expires_at": "2026-05-06T00:10:00Z",
    "error_code": "not_implemented",
    "error_message": "browser handoff runner process boundary is not implemented"
  }
}
```

Implemented `runner plan-novnc --json` returns a dry-run plan without mutating runner metadata:

```json
{
  "plan": {
    "id": 1,
    "session_id": 1,
    "login_request_id": 1,
    "handoff_id": "opaque-handoff-id",
    "commands": [
      {
        "role": "display",
        "path": "/usr/bin/x11vnc",
        "detected_path": "/usr/bin/x11vnc",
        "command_role": "display",
        "validation_status": "valid"
      },
      {
        "role": "browser",
        "path": "/usr/bin/chromium",
        "detected_path": "/usr/bin/chromium",
        "command_role": "browser",
        "validation_status": "valid"
      },
      {
        "role": "novnc",
        "path": "/usr/bin/websockify",
        "detected_path": "/usr/bin/websockify",
        "command_role": "novnc/websockify",
        "validation_status": "valid"
      }
    ],
    "bind_addr": "127.0.0.1:6080",
    "private_base_url": "https://odin-handoff.tailnet.local",
    "viewer_url": "https://odin-handoff.tailnet.local/session/dry-run-opaque-handoff-id",
    "timeout_seconds": 300
  }
}
```

`revoke` is always mutating and must require a reason. `verify` is mutating when it changes status, binding, expiration, or verification timestamps.

## Storage Contract

SQLite tables for browser sessions are additive and must not replace goal, intake, approval, or runtime event tables.

Tables:

- `browser_session_profiles`: implemented profile metadata and policy binding.
- `browser_session_login_requests`: implemented metadata-only manual login requests with status, opaque handoff ID, optional future handoff URL, expiration, completion timestamp, and audit timestamps.
- `browser_handoff_runners`: implemented metadata-only runner records linked to login requests and browser sessions, with lifecycle status, optional future viewer/process/network fields, expiration, started/exited timestamps, terminal timestamps, and safe error metadata.
- `browser_session_events`: optional profile-local lifecycle detail if the global runtime events stream alone is not sufficient for efficient profile show/history.
- `browser_session_goal_links`: explicit goal/profile relation with reason, requested tier, and verification evidence references.

Browser profile files:

- Default root: `ODIN_ROOT/browser-sessions/profiles/<sanitized-name>`.
- Paths stored in SQLite must be relative to `ODIN_ROOT`.
- Odin allocates profile paths as metadata only. It must not create browser profile contents, cookies, storage state, or credential material during session creation.
- Odin creates profile directories only through explicit `prepare-profile`. The prepared directory must be empty immediately after creation.
- `profile_path_exists` reports whether the allocated relative path currently exists under `ODIN_ROOT`; the field is informational and does not make the filesystem a profile registry.
- `profile_storage_policy` defaults to `encrypted_required`; prepared directories alone are not approval to write browser files.
- Profile files must be encrypted at rest before real session writes are allowed. If host-level encryption is the first slice, policy must still deny profile writes unless the operator explicitly accepts that documented gap in a later policy slice.
- No credential material may be written to Odin-specific metadata, events, logs, screenshots, or evidence payloads.

## Policy Gates

Policy must fail closed. A browser session may be attached to a goal only when all checks pass:

- goal status is not terminal and is not already running under a conflicting profile.
- goal type is listed in `allowed_goal_types`.
- goal requested action is read-only: `read`, `navigate`, `snapshot`, or `extract`.
- permission tier is `authenticated_readonly`.
- `CanWriteBrowserProfile(session)` allows profile attachment after encrypted storage exists; in the current implementation it denies every policy value.
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

Required runner event types:

- `browser.handoff_runner_requested`
- `browser.handoff_runner_started`
- `browser.handoff_runner_expired`
- `browser.handoff_runner_cancelled`
- `browser.handoff_runner_completed`
- `browser.handoff_runner_failed`

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
- `profile_storage_policy`
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
3. Login request metadata surface: implemented `odin browser session login-request|login-requests` to record and inspect metadata-only manual login requests with opaque `handoff_id` and optional metadata-only `handoff_url`.
4. Manual verification metadata surface: implemented `odin browser session verify` to set session status `verified`, record `last_verified_at`, and optionally complete a login request, with no browser launch or credential handling.
5. Profile path and empty directory preparation: implemented safe profile path allocation plus explicit `odin browser session prepare-profile` for empty-directory creation.
6. Profile storage policy gate: implemented `profile_storage_policy` with default `encrypted_required`, CLI JSON output, and a deny-all `CanWriteBrowserProfile` helper until encrypted storage exists.
7. Encrypted profile storage: add encrypted profile root handling and policy denial for unencrypted profiles.
8. Authenticated read-only attachment: allow `odin browser run` and goal runner evidence collection to attach a verified `authenticated_readonly` profile for allowed domains only.
9. Handoff runner metadata store: implemented additive SQLite runner metadata with `requested`, `started`, `failed`, `completed`, `expired`, and `cancelled` states; append runner audit events transactionally.
10. Handoff runner CLI: implemented minimal `odin browser session runner create|list|show|start|status|cancel` commands, reusing current session and login request validation without process launch.
11. Process boundary: implemented a typed `internal/runtime/browserhandoff` skeleton with validating `StubRunner`; real browser process launch, shutdown semantics, and fail-closed cleanup remain future work.
12. Bounded fixture runner: implemented explicit env-gated `FixtureRunner` for harmless allowlisted local commands with timeout handling and safe process metadata. Local NoVNC fixture remains future work.
13. NoVNC runner contract refinement: document process topology, config, security rules, lifecycle cleanup, and staged implementation constraints before process code.
14. Command contract and config validation: implemented pure NoVNC dry-run and real-launch config models with validation for absolute allowlisted command paths, loopback/private bind settings, private base URL, and bounded timeout without launching processes.
15. Dry-run NoVNC runner: implemented pure request/config planning and `odin browser session runner plan-novnc` JSON output that returns planned command roles, validated bind/timeout config, command detection metadata for every planned display/browser/noVNC role, and a private planned `viewer_url` without starting child processes or mutating runner metadata. Real process wiring remains future work.
16. Bounded process supervisor abstraction: implemented injected-runner process supervision contracts, request/handle/result shapes, allowlist validation, timeout result handling, and cancellation result handling without wiring into real runner start.
17. Bounded exec process runner: implemented `ExecCommandRunner` behind the supervisor abstraction for harmless allowlisted local commands with process-group kill on timeout/cancel, bounded stdout/stderr capture, and no wiring into runner start.
18. Local fixture process runner lifecycle: implemented `runner start` wiring from fixture mode through the bounded process supervisor and existing runner metadata transitions, proving harmless allowlisted command completion, timeout, disallowed command rejection, `started` and terminal audit events, and no browser/NoVNC/Tailscale/profile writes.
19. NoVNC runner skeleton: implemented explicit `ODIN_BROWSER_HANDOFF_RUNNER=novnc` selection that validates `ODIN_NOVNC_*` launch config and returns `not_implemented` through the existing `runner start` status/audit path without process launch.
20. NoVNC fixture-safe launch: implemented explicit NoVNC runner wiring through the bounded supervisor using only allowlisted harmless local commands for display, browser, and `novnc/websockify` roles, with private viewer URL metadata, started/completed/failed/expired lifecycle transitions, and no real browser/NoVNC/Tailscale/profile writes.
21. Display/browser/websockify command detection: implemented dry-run detection metadata for explicit display, browser, and noVNC/websockify command paths with absolute path, allowlist, existence, and executable-bit checks without process launch.
22. Real noVNC process boundary: start display/VNC, browser, and noVNC/websockify under one supervisor, generate only private `viewer_url`, and prove cleanup with no persistent profile writes.
23. Real browser visible session: allow operator-attended manual login in the visible browser only after profile write policy, private viewer routing, cancellation, timeout, and audit behavior are proven.
24. Profile write gate integration: connect runner persistence only after encrypted profile storage and `CanWriteBrowserProfile(session)` allow writes.
25. Operator runbooks and overview visibility: surface session health, expiring profiles, active handoff runners, and waiting login goals in existing overview lanes if a clean projection lane exists.

## Best Operating Rule

Keep session metadata in SQLite, profile bytes under `ODIN_ROOT`, and all state changes in the existing runtime event stream. Do not create a second browser session registry, credential vault, or sidecar goal authority.
