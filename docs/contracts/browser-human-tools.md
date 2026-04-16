---
title: Browser Human Tool Contract
status: active
date: 2026-04-16
---

# Browser Human Tool Contract

This contract covers the two `odin-os` catalog tools that route through the generic browser driver path:

- `huginn_browser_session`
- `plaid_transfer_application`

The catalog handlers invoke `internal/tools/invocation`, which in turn uses the generic browser driver from `internal/adapters/browserhuman`.

## Driver configuration

The driver command is read from `ODIN_BROWSER_HUMAN_DRIVER`.

Each invocation gets its own derived `ODIN_DIR` runtime root so parallel runs do not share browser state or artifact paths.

The command must distinguish between two failure classes:

- successful tool runs return structured JSON on stdout with `status: "completed"`
- transport or setup failures exit non-zero so the caller can detect the broken driver

The command must also:

- read one JSON request from stdin
- write one JSON response to stdout
- keep any opaque browser artifacts in the response envelope

## Request envelope

All browser-human requests use this JSON shape:

```json
{
  "tool_key": "huginn_browser_session",
  "input": {}
}
```

Rules:

- `tool_key` is required and must match the catalog tool key.
- `input` is tool-specific and must stay bounded.
- unknown request fields are not part of the contract.

## Tool inputs

### `huginn_browser_session`

Supported input fields:

- `action` string, required by the catalog schema, enum `health`, `launch`, `snapshot`, `screenshot`, `stop`
- `url` string, optional
- `path` string, optional; interpreted as an artifact filename and scoped under the invocation runtime root

Use this tool for a bounded browser session check or state inspection.

### `plaid_transfer_application`

Supported input fields:

- `application_url` string, optional
- `path` string, optional; interpreted as an artifact filename and scoped under the invocation runtime root

This tool is a bounded Plaid workflow and intentionally has no free-form browser control surface.

## Response envelope

The driver must return exactly one JSON response with these required fields:

```json
{
  "status": "completed",
  "tool_key": "huginn_browser_session",
  "summary": "browser session complete",
  "artifacts": {
    "session_state": "ready",
    "current_url": "https://example.com",
    "screenshot_path": "/tmp/browser.png",
    "next_action": "run plaid_transfer_application",
    "evidence": ["driver invoked"]
  }
}
```

Rules:

- `status` must be `completed` for successful runs; the adapter rejects any other status as an error.
- `tool_key` must echo the request tool key.
- `summary` must be a short operator-facing sentence.
- `artifacts` must be present and may contain opaque structured data.

Expected artifact fields:

- `session_state`
- `current_url`
- `screenshot_path`
- `next_action`
- `evidence`

The first four fields are treated as scalar key facts by the catalog. `evidence` stays as structured artifact data.

## Catalog mapping

The catalog layer maps the driver response to `StructuredResult` as follows:

- `CapabilityKey` mirrors the driver `tool_key`
- `Summary` mirrors the driver `summary`
- `KeyFacts` carries bounded scalar artifact values
- `FollowOnOptions` stays short and fixed to the approved browser follow-up actions
- `RawRef` is recorded as `browserhuman://<tool_key>/result`
- `RawOutput` preserves the exact driver stdout

## Bounded follow-ons

The browser tools must not expose open-ended action menus.

Approved follow-on options are currently:

- `inspect browser artifacts`
- `run plaid_transfer_application`
- `stop browser session`

Any new option requires a contract update.
