---
title: Live Driver Tool Wiring
status: active
date: 2026-04-16
---

# Live Driver Tool Wiring

`odin-os` invokes live external tools for the PBS bid workflow through JSON-over-stdin/stdout driver commands.

## Environment variables

- `ODIN_GOOGLE_CALENDAR_DRIVER`
- `ODIN_HUGINN_DRIVER`
- `ODIN_HUGINN_ROBINHOOD_TRANSFER_DRIVER`

These env vars should point to executable commands. The repo-local driver scripts are:

- `scripts/drivers/google-calendar-off-dates.sh`
- `scripts/drivers/huginn-pbs-session.sh`
- `scripts/drivers/robinhood-transfer-flow.sh`

Example:

```bash
export ODIN_GOOGLE_CALENDAR_DRIVER="/home/orchestrator/odin-os/scripts/drivers/google-calendar-off-dates.sh"
export ODIN_HUGINN_DRIVER="/home/orchestrator/odin-os/scripts/drivers/huginn-pbs-session.sh"
export ODIN_HUGINN_ROBINHOOD_TRANSFER_DRIVER="/home/orchestrator/odin-os/scripts/drivers/robinhood-transfer-flow.sh"
```

## Repo-local libraries

The driver scripts resolve their helper libraries from this repo by default:

- `scripts/drivers/lib/google.sh`
- `scripts/drivers/lib/browser-access.sh`

Optional overrides:

- `ODIN_GOOGLE_LIB_PATH`
- `ODIN_BROWSER_ACCESS_LIB_PATH`

These overrides are for explicit local customization only. `odin-os` no longer requires `/home/orchestrator/odin-orchestrator` to run the live drivers.

## Runtime prerequisites

- `google.sh` requires `curl` and `python3`, plus Google OAuth refresh-token credentials in the environment or `~/.odin-env`.
- `browser-access.sh` can reuse an already running browser server via `ODIN_BROWSER_SERVER_URL`, or start one when `ODIN_BROWSER_SERVER_SCRIPT` is explicitly set to a compatible server script and `node` is available.

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

Both drivers return one JSON response on stdout with:

- `status`
- `tool_key`
- `summary`
- `artifacts`

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

Use [Marcus Robinhood Live Transfer Runbook](../operations/marcus-robinhood-live-transfer-runbook.md) for the operator-attended command sequence and the current `family-ops` registry-alignment caveat.
