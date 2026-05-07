# Browser Handoff Runner Release Checkpoint

Use this checkpoint before treating the browser handoff runner as homelab operator-ready.

## What Is Merged

- Browser session metadata, login request metadata, handoff lookup, runner metadata, and runner lifecycle audit events.
- `odin browser session runner create|list|show|start|status|cancel --json`.
- `odin browser session runner plan-novnc --json` for dry-run NoVNC command validation.
- Bounded process supervision with allowlisted command paths, timeout handling, cancellation handling, and safe process metadata.
- `ODIN_BROWSER_HANDOFF_RUNNER=novnc` selection.
- Independent explicit real-role gates:
  - `ODIN_NOVNC_REAL_DISPLAY=true`
  - `ODIN_NOVNC_REAL_WEBSOCKIFY=true`
  - `ODIN_NOVNC_REAL_BROWSER=true`
- Existing store transition paths and runtime event stream remain the audit authority.

## What Is Still Blocked

- Profile persistence and encrypted profile storage.
- Cookie, browser storage state, and credential storage.
- Authenticated session attachment to browser runs or goal evidence.
- Automatic login, form fill, credential entry, MFA handling, passkey handling, or consent clicking.
- Tailscale service creation, public viewer exposure, or automatic private routing setup.
- Browser args, custom browser environment, working directory, or profile path reuse.
- Goal approval or policy approval through browser handoff runner startup.

## Repo-Local Verification

Run from a fresh checkout or worktree on the intended commit:

```bash
which odin
make build
./bin/odin doctor --json
go test ./...
make odin-e2e-local
git diff --check
```

`which odin` may report `/home/orchestrator/.local/bin/odin`. That installed binary is not proof of the current checkout. Use `./bin/odin` after `make build` for release verification.

## Repo-Local Runner Smoke

Use the safe fixture proof in `docs/operations/browser-handoff-runner.md` before any real command rollout. The proof must show:

- runner metadata is created from a real login request
- `runner start --json` returns a terminal runner state
- browser handoff runner audit events appear in `./bin/odin logs --json`
- no `browser-sessions`, `cookies`, or `credentials` artifacts are created in the fixture `ODIN_ROOT`

## Installed Odin Update

Do not update `/home/orchestrator/.local/bin/odin` as part of release verification. If an operator decides to update the installed command after repo-local verification is green, run this as a separate manual step:

```bash
make install-local
which odin
odin doctor --json
```

Only treat installed `odin` as current after `which odin` points to the intended installed path and `odin doctor --json` runs from the updated build.

## Release Hold Points

Stop the rollout if any of these are true:

- `git status --short --branch` shows unexpected source changes.
- `make build`, `go test ./...`, `make odin-e2e-local`, or `git diff --check` fails.
- Any configured command path is relative, missing, not executable, or absent from its allowlist.
- `ODIN_NOVNC_BIND_ADDR` is public or wildcard.
- `ODIN_NOVNC_PRIVATE_BASE_URL` is not an approved private URL.
- Any command output, log, or event payload contains credential material or browser profile bytes.

## Closeout Evidence

Record these before declaring a homelab release checkpoint complete:

- exact commit SHA
- `which odin` output
- `make build` result
- `go test ./...` result
- `make odin-e2e-local` result
- `git diff --check` result
- safe fixture proof output summary
- real command rollout decision: not attempted, attempted and passed, or attempted and rolled back
- installed Odin decision: not updated, or updated separately with command output
