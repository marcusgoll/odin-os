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

These env vars should point to executable commands. The repo-local driver scripts are:

- `scripts/drivers/google-calendar-off-dates.sh`
- `scripts/drivers/huginn-pbs-session.sh`

Example:

```bash
export ODIN_GOOGLE_CALENDAR_DRIVER="/home/orchestrator/odin-os/scripts/drivers/google-calendar-off-dates.sh"
export ODIN_HUGINN_DRIVER="/home/orchestrator/odin-os/scripts/drivers/huginn-pbs-session.sh"
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
- `browser-access.sh` can reuse an already running browser server via `ODIN_BROWSER_SERVER_URL`, or start one from `ODIN_BROWSER_SERVER_SCRIPT` when that script is present and `node` is available.

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
