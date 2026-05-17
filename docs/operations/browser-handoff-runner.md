# Browser Handoff Runner Runbook

Use this runbook to prove and operate the Odin browser handoff runner from a repo-local build. It is for operator-attended browser handoff only.

## Purpose

The browser handoff runner creates and supervises one runner lifecycle for a browser session login request. In NoVNC mode it can start configured display/VNC, browser, and websockify/noVNC role commands, record safe runner metadata, emit audit events, and return a private viewer URL derived from configured private routing.

## Current Capabilities

- `odin browser session runner start --id <runner_id> --json` loads runner metadata and linked session/login-request state from SQLite.
- `ODIN_BROWSER_HANDOFF_RUNNER=novnc` selects the NoVNC runner.
- Display/VNC, browser, and websockify/noVNC role commands must be absolute, executable, and allowlisted before launch.
- Real display, websockify, and browser role commands are independently gated by explicit environment variables.
- When all three real gates are enabled, `runner start` records a `started`
  runner with a `novnc-real-*` runner ID, private `viewer_url`, process
  metadata, and `real_browser_evidence=true`, then returns without waiting for
  the attended browser stack to exit.
- If display, browser, or websockify exits during startup probing, `runner
  start` records `failed` with `error_code=novnc_process_failed`, stderr/stdout
  excerpts, child process role metadata, and a `browser.handoff_runner_failed`
  audit event.
- Encrypted profile artifacts can be materialized for a runner without putting
  profile bytes in command output or logs.
- `odin browser session prove --id <session_id> --url <url> --expect-title
  <title> --json` creates a fresh login request and runner, starts that runner
  from the saved encrypted profile, and records a
  `browser.session_proof_recorded` event after checking encrypted profile
  metadata, materialization evidence, protected viewer routing, loopback-only
  noVNC bind, and the visible browser title.
- Runner lifecycle changes are persisted through existing store transition paths and runtime audit events.
- Repo-local proof uses `./bin/odin` after `make build`.

## Explicit Non-Capabilities

- No automatic login, form fill, credential entry, 2FA handling, passkey handling, consent clicking, or account selection.
- No credential storage. Profile capture and reuse must go through explicit
  encrypted profile artifact commands with the profile key supplied only by the
  deployed environment.
- No Tailscale service creation or public routing setup.
- No browser args, custom environment, working directory, or profile path reuse are injected into the browser role.
- No goal approval, policy approval, or execution authority is granted by starting a handoff runner.
- The installed `/home/orchestrator/.local/bin/odin` may be stale until separately updated.

## Required Runtime Environment

Set these only for the real command rollout after safe fixture proof is green:

```bash
export ODIN_BROWSER_HANDOFF_RUNNER=novnc
export ODIN_NOVNC_REAL_DISPLAY=true
export ODIN_NOVNC_REAL_WEBSOCKIFY=true
export ODIN_NOVNC_REAL_BROWSER=true
export ODIN_NOVNC_DISPLAY_COMMAND=/absolute/path/to/display-or-vnc
export ODIN_NOVNC_WEBSOCKIFY_COMMAND=/absolute/path/to/websockify-or-novnc
export ODIN_NOVNC_BROWSER_COMMAND=/absolute/path/to/browser
export ODIN_NOVNC_ALLOWED_COMMANDS=/absolute/path/to/display-or-vnc,/absolute/path/to/websockify-or-novnc,/absolute/path/to/browser
export ODIN_NOVNC_BIND_ADDR=127.0.0.1:6080
export ODIN_NOVNC_PRIVATE_BASE_URL=https://odin-handoff.tailnet.local
export ODIN_NOVNC_TIMEOUT_SECONDS=600
export ODIN_BROWSER_PROFILE_KEY_B64=<base64-32-byte-key-from-secret-store>
```

`ODIN_NOVNC_BIND_ADDR` must stay loopback or an operator-approved private bind address. Do not use `0.0.0.0`. `ODIN_NOVNC_PRIVATE_BASE_URL` must be private operator routing, not a public URL.

## Preflight Checks

Run from the repo checkout or worktree you intend to prove:

```bash
which odin
command -v jq
make build
./bin/odin doctor --json
git status --short --branch
```

For every real command path:

```bash
test -x "$ODIN_NOVNC_DISPLAY_COMMAND"
test -x "$ODIN_NOVNC_WEBSOCKIFY_COMMAND"
test -x "$ODIN_NOVNC_BROWSER_COMMAND"
printf '%s\n' "$ODIN_NOVNC_ALLOWED_COMMANDS"
```

Stop if any command path is relative, missing, not executable, omitted from `ODIN_NOVNC_ALLOWED_COMMANDS`, or resolves to a wrapper that starts a public network service outside the reviewed private boundary.

## Safe Fixture Proof

This proof uses harmless local commands and must pass before real role commands are attempted:

```bash
runtime_root=$(mktemp -d)

session_json=$(ODIN_ROOT="$runtime_root" ./bin/odin browser session create \
  --name "handoff-fixture-proof" \
  --domain "example.com" \
  --permission-tier authenticated_read \
  --json)
session_id=$(printf '%s' "$session_json" | jq -r '.session.id')

login_json=$(ODIN_ROOT="$runtime_root" ./bin/odin browser session login-request \
  --id "$session_id" \
  --json)
login_request_id=$(printf '%s' "$login_json" | jq -r '.login_request.id')

runner_json=$(ODIN_ROOT="$runtime_root" ./bin/odin browser session runner create \
  --login-request-id "$login_request_id" \
  --json)
runner_id=$(printf '%s' "$runner_json" | jq -r '.runner.id')

ODIN_ROOT="$runtime_root" \
ODIN_BROWSER_HANDOFF_RUNNER=novnc \
ODIN_NOVNC_DISPLAY_COMMAND=/usr/bin/true \
ODIN_NOVNC_WEBSOCKIFY_COMMAND=/usr/bin/true \
ODIN_NOVNC_BROWSER_COMMAND=/usr/bin/true \
ODIN_NOVNC_ALLOWED_COMMANDS=/usr/bin/true \
ODIN_NOVNC_BIND_ADDR=127.0.0.1:6080 \
ODIN_NOVNC_PRIVATE_BASE_URL=https://odin-handoff.tailnet.local \
ODIN_NOVNC_TIMEOUT_SECONDS=2 \
./bin/odin browser session runner start --id "$runner_id" --json

ODIN_ROOT="$runtime_root" ./bin/odin logs --json
test ! -e "$runtime_root/browser-sessions"
test ! -e "$runtime_root/cookies"
test ! -e "$runtime_root/credentials"
```

Expected result: the fixture-safe runner reaches a terminal state through the metadata lifecycle, emits browser handoff runner audit events, and creates no browser profile, cookie, or credential artifacts. This fixture proof is not real browser evidence.

## Real Command Rollout Checklist

1. Complete the preflight checks and safe fixture proof.
2. Review each real command path and confirm it is absolute, executable, and listed in `ODIN_NOVNC_ALLOWED_COMMANDS`.
3. Confirm the three real gates are intentional for this run:
   - `ODIN_NOVNC_REAL_DISPLAY=true`
   - `ODIN_NOVNC_REAL_WEBSOCKIFY=true`
   - `ODIN_NOVNC_REAL_BROWSER=true`
4. Use `./bin/odin`, not installed `odin`, unless the installed binary was deliberately updated and verified.
5. Create a session, login request, and runner with the same metadata commands from the fixture proof.
6. Run `runner plan-novnc` before `runner start` when validating new command paths:

```bash
ODIN_ROOT="$runtime_root" ./bin/odin browser session runner plan-novnc \
  --id "$runner_id" \
  --browser-command "$ODIN_NOVNC_BROWSER_COMMAND" \
  --browser-allowed-command "$ODIN_NOVNC_BROWSER_COMMAND" \
  --display-command "$ODIN_NOVNC_DISPLAY_COMMAND" \
  --display-allowed-command "$ODIN_NOVNC_DISPLAY_COMMAND" \
  --novnc-command "$ODIN_NOVNC_WEBSOCKIFY_COMMAND" \
  --novnc-allowed-command "$ODIN_NOVNC_WEBSOCKIFY_COMMAND" \
  --bind-addr "$ODIN_NOVNC_BIND_ADDR" \
  --private-base-url "$ODIN_NOVNC_PRIVATE_BASE_URL" \
  --timeout-seconds "$ODIN_NOVNC_TIMEOUT_SECONDS" \
  --json
```

7. Start the runner only after the plan reports valid command detection for every role. The runner passes no args or custom env to role commands; if real system binaries need display, profile, target URL, or port arguments, configure reviewed host-side wrapper commands and allowlist those wrapper paths.
8. Confirm `runner start --json` returns `status: "started"`, a `novnc-real-*`
   runner ID, `real_browser_evidence: true`, and a private `viewer_url`.
9. Use the returned private `viewer_url` only from the approved private network path.
10. Complete any username, password, passkey, MFA, consent, or account selection manually in the visible browser. Odin must not collect or store those values.
11. Inspect `./bin/odin logs --json`, `/mobile/review-queue`, and `runner show --id <runner_id> --json` after the run. The mobile review item should expose `open-viewer` while the runner is `started`.

## Saved Profile Login-Skip Proof

Run this only after an encrypted profile artifact exists for the session. The
proof command creates its own login request and runner, starts the configured
real NoVNC browser handoff from the encrypted profile, records
`browser.session_proof_recorded`, and performs no site mutation:

```bash
ODIN_BROWSER_PROOF_TITLE_COMMAND=/home/orchestrator/.config/odin/browser-handoff/prove-title \
ODIN_BROWSER_PROOF_TITLE_ALLOWED_COMMANDS=/home/orchestrator/.config/odin/browser-handoff/prove-title \
./bin/odin browser session prove \
  --id "$session_id" \
  --url "https://x.com/" \
  --expect-title "Home / X" \
  --json
```

`prove` expects:

- an active encrypted profile artifact for the session
- a newly started `novnc-real-*` runner
- a materialized profile directory for the proof runner
- protected Odin viewer and proxy routes for the handoff ID
- a loopback-only raw noVNC bind address
- a visible browser title containing the expected title

The title check runs exactly the executable configured in
`ODIN_BROWSER_PROOF_TITLE_COMMAND`; the path must be absolute, executable, and
listed in `ODIN_BROWSER_PROOF_TITLE_ALLOWED_COMMANDS`. Use a reviewed host-side
wrapper such as `/home/orchestrator/.config/odin/browser-handoff/prove-title`
to run `xwininfo` against the live display and print the observed browser title
or window tree. Odin does not run this through a shell.

## Rollback Steps

- Stop creating new login requests or runner records.
- Unset the real gates or set `ODIN_BROWSER_HANDOFF_RUNNER=stub`.
- If a runner is still active, run:

```bash
ODIN_ROOT="$runtime_root" ./bin/odin browser session runner cancel --id "$runner_id" --json
ODIN_ROOT="$runtime_root" ./bin/odin browser session runner show --id "$runner_id" --json
```

- Stop any externally managed display, browser, or websockify service that was started outside Odin.
- Preserve `ODIN_ROOT` until logs and runner metadata are reviewed.

## Cleanup Steps

- For fixture proof roots created with `mktemp -d`, remove the temp root only after logs are reviewed:

```bash
rm -rf "$runtime_root"
```

- Do not delete a production `ODIN_ROOT` to clean up one failed handoff. Use runner cancellation, service-manager cleanup, and audited metadata review instead.
- Remove temporary command wrappers used only for fixture proof.
- Clear shell environment variables after the rollout:

```bash
unset ODIN_BROWSER_HANDOFF_RUNNER
unset ODIN_NOVNC_REAL_DISPLAY ODIN_NOVNC_REAL_WEBSOCKIFY ODIN_NOVNC_REAL_BROWSER
unset ODIN_NOVNC_DISPLAY_COMMAND ODIN_NOVNC_WEBSOCKIFY_COMMAND ODIN_NOVNC_BROWSER_COMMAND
unset ODIN_NOVNC_ALLOWED_COMMANDS ODIN_NOVNC_BIND_ADDR ODIN_NOVNC_PRIVATE_BASE_URL ODIN_NOVNC_TIMEOUT_SECONDS
```

## Security Warnings

- Treat every viewer URL as sensitive operational metadata, even when it is private.
- Never paste passwords, passkeys, TOTP values, backup codes, OAuth tokens, cookies, or browser profile bytes into Odin logs, evidence, issue comments, or runbooks.
- Do not expose the viewer bind address publicly.
- Do not add browser flags, profile directories, or environment variables outside a reviewed code change and security contract.
- Do not claim authenticated session reuse from a successful runner start alone.
  Require `odin browser session prove` and the resulting
  `browser.session_proof_recorded` audit event.
