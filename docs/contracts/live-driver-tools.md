---
title: Live Driver Tool Wiring
status: active
date: 2026-04-23
---

# Live Driver Tool Wiring

`odin-os` invokes live external tools for bounded browser-backed and calendar-backed workflow lanes through JSON-over-stdin/stdout driver commands.

## Environment variables

- `ODIN_GOOGLE_CALENDAR_DRIVER`
- `ODIN_HUGINN_DRIVER`
- `ODIN_HUGINN_VISUAL_DRIVER`
- `ODIN_HUGINN_X_POST_DRIVER`
- `ODIN_HUGINN_X_PUBLISH_DRIVER`
- `ODIN_HUGINN_ROBINHOOD_TRANSFER_DRIVER`

These env vars should point to executable commands. The repo-local driver scripts are:

- `scripts/drivers/google-calendar-off-dates.sh`
- `scripts/drivers/huginn-pbs-session.sh`
- `scripts/drivers/huginn-visual-audit.sh`
- `scripts/drivers/huginn-x-post-evidence.sh`
- `scripts/drivers/huginn-x-post-publish.sh`
- `scripts/drivers/robinhood-transfer-flow.sh`

Example:

```bash
export ODIN_GOOGLE_CALENDAR_DRIVER="/home/orchestrator/odin-os/scripts/drivers/google-calendar-off-dates.sh"
export ODIN_HUGINN_DRIVER="/home/orchestrator/odin-os/scripts/drivers/huginn-pbs-session.sh"
export ODIN_HUGINN_VISUAL_DRIVER="/home/orchestrator/odin-os/scripts/drivers/huginn-visual-audit.sh"
export ODIN_HUGINN_X_POST_DRIVER="/home/orchestrator/odin-os/scripts/drivers/huginn-x-post-evidence.sh"
export ODIN_HUGINN_X_PUBLISH_DRIVER="/home/orchestrator/odin-os/scripts/drivers/huginn-x-post-publish.sh"
export ODIN_HUGINN_ROBINHOOD_TRANSFER_DRIVER="/home/orchestrator/odin-os/scripts/drivers/robinhood-transfer-flow.sh"
```

## Repo-local library reuse

The repo-local scripts reuse shell libraries inside `odin-os`.

- `huginn-pbs-session.sh` sources `scripts/browser/browser-access.sh`
- `huginn-visual-audit.sh` sources `scripts/browser/browser-access.sh`
- `huginn-x-post-evidence.sh` sources `scripts/browser/browser-access.sh`
- `huginn-x-post-publish.sh` sources `scripts/browser/browser-access.sh`
- `robinhood-transfer-flow.sh` sources `scripts/browser/browser-access.sh`

Override paths when needed:

- `ODIN_GOOGLE_LIB_PATH`
- `ODIN_BROWSER_ACCESS_LIB_PATH`

These overrides are for explicit local customization only. `odin-os` no longer requires `/home/orchestrator/odin-orchestrator` to run the live drivers.

## Runtime prerequisites

- `google.sh` requires `curl` and `python3`, plus Google OAuth refresh-token credentials in the environment or `~/.odin-env`.
- `browser-access.sh` can reuse an already running compatible browser server via `ODIN_BROWSER_SERVER_URL`.
- If `ODIN_BROWSER_SERVER_URL` is unset, `browser-access.sh` starts the repo-local compatible browser server itself and therefore requires `node`.
## Request contract

Calendar driver request:

```json
{
  "tool_key": "google_calendar_off_dates",
  "input": {
    "bid_period": "2026-05",
    "calendar_id": "primary",
    "timezone": "America/Chicago"
  }
}
```

Huginn driver request:

```json
{
  "tool_key": "huginn_pbs_session",
  "input": {
    "bid_period": "2026-05",
    "workflow_key": "pbs_may_bid",
    "timezone": "America/Chicago"
  }
}
```

Visual audit driver request:

```json
{
  "tool_key": "browser_visual_audit",
  "input": {
    "target_url": "https://example.com/dashboard",
    "label": "cfipros-dashboard-baseline",
    "wait_ms": "2000",
    "allow_private_host": "false",
    "headless": "true"
  }
}
```

X post visible evidence driver request:

```json
{
  "tool_key": "browser_x_post_visible_evidence",
  "input": {
    "target_url": "https://x.com/marcus/status/123",
    "label": "marcus-crosswind",
    "wait_ms": "2000",
    "headless": "true"
  }
}
```

Weekly X evidence bundles do not introduce a new driver env var. The builtin tool `browser_x_weekly_evidence_bundle` reuses `ODIN_HUGINN_X_POST_DRIVER` once per explicit X post URL and aggregates the results inside Odin.

All live drivers return one JSON response on stdout with:

- `status`
- `tool_key`
- `summary`
- `artifacts`

Current operator docs and examples use canonical `browser_*` tool keys. Legacy `huginn_*` keys remain accepted on `/tool` only as hidden compatibility aliases during transition.

## Social evidence boundary

- `browser_x_post_publish` is an operator-attended single-item action for approved X content, including approved replies when an explicit reply target is provided.
- `browser_x_post_visible_evidence` is read-only and visible-page only.
- It is intended for explicit X post URLs, not full-profile scraping.
- `browser_x_weekly_evidence_bundle` is an Odin-side orchestration layer over that same explicit-post driver, not a broader crawler.
- LinkedIn browser evidence capture is not part of this live driver surface.
- Unofficial API replay or hidden network-call harvesting is not part of this contract.

## Robinhood transfer proof boundary

The Robinhood transfer lane has two separate proof modes:

- deterministic shell proof for CI and local verification
- principal-attended live Robinhood use for real Marcus transfers

Deterministic shell proof uses `ODIN_HUGINN_ROBINHOOD_TRANSFER_DRIVER` with a fixture command and runs through the real repo-owned `./bin/odin repl` surface. The focused proof target is:

```bash
go test ./tests/integration -run 'TestRobinhoodTransferShellFlowDeterministic|TestRobinhoodTransferFlowScript' -count=1
```

Attended live Robinhood use should point `ODIN_HUGINN_ROBINHOOD_TRANSFER_DRIVER` at the repo-local script:

```bash
export ODIN_HUGINN_ROBINHOOD_TRANSFER_DRIVER="/home/orchestrator/odin-os/scripts/drivers/robinhood-transfer-flow.sh"
```

On headless hosts, attended Robinhood use should also point `ODIN_BROWSER_SERVER_URL` at a compatible headed browser server that Marcus can see and operate:

```bash
export ODIN_BROWSER_SERVER_URL="http://<headed-browser-host>:<port>"
```

Use [Marcus Robinhood Live Transfer Runbook](../operations/marcus-robinhood-live-transfer-runbook.md) for the operator-attended command sequence and the current `family-ops` registry-alignment caveat.
