---
title: Huginn Browser Plugin Contract
status: active
date: 2026-05-12
---

# Huginn Browser Plugin Contract

This contract defines the Odin-owned Huginn browser plugin boundary. The plugin is not a separate plugin manager. It is a governed capability/tool/executor adapter that reuses:

- `internal/executors/browser` for admission, risk classification, evidence persistence, and executor-facing results.
- `internal/adapters/huginnbrowser` for deterministic fake runs and the bounded live worker command boundary.
- `internal/store/sqlite` goal evidence, approvals, events, and browser session profile storage.
- `internal/runtime/approvals` and `odin review` for operator approval readback and resolution.
- `odin overview --json` for operator audit and attention readback.

The default behavior is read-only evidence collection. Any external mutation requires Odin approval before execution.

## Request Schema

Executor-facing requests use this JSON shape:

```json
{
  "goal_id": 41,
  "task_id": 99,
  "worker_mode": "browser",
  "objective": "Collect public evidence from the target page",
  "allowed_domains": ["example.com"],
  "start_urls": ["https://example.com/docs"],
  "max_pages": 2,
  "max_duration_seconds": 30,
  "evidence_required": true,
  "site_profiles": [
    {
      "domain": "example.com",
      "max_pages": 1,
      "min_delay_ms": 250,
      "max_duration_seconds": 10,
      "mode_allowed": "browser"
    }
  ],
  "actions": ["navigate", "snapshot"]
}
```

Required fields:

- `goal_id`: positive Odin goal ID used for durable evidence.
- `objective`: non-empty operator objective.
- `allowed_domains`: non-empty hostname allowlist.
- `start_urls`: non-empty absolute HTTP(S) URLs. Credentials and raw IP hosts are rejected.
- `max_pages`: positive page limit, capped by the executor.
- `max_duration_seconds`: positive timeout, capped by the executor.

Optional fields:

- `task_id`: required when any requested action requires approval.
- `worker_mode`: `fetch` or `browser`; empty defaults to read-only adapter behavior.
- `evidence_required`: requests durable evidence artifacts such as screenshots where the adapter supports them.
- `site_profiles`: per-domain constraints. Profiles can narrow limits or modes; they never widen `allowed_domains`.
- `actions`: requested browser actions. Empty means read-only.

## Response Schema

Read-only success:

```json
{
  "status": "recorded",
  "goal_id": 41,
  "task_id": 99,
  "evidence_id": 123,
  "evidence_type": "browser_readonly",
  "risk_class": "read_only",
  "adapter_status": "completed",
  "adapter_kind": "stub_local",
  "start_urls": ["https://example.com/docs"],
  "allowed_domains": ["example.com"],
  "max_pages": 2,
  "max_duration_seconds": 30,
  "visited_urls": ["https://example.com/docs"],
  "page_results": [
    {
      "url": "https://example.com/docs",
      "status": "visited",
      "mode": "browser",
      "title": "Docs",
      "summary": "Collected public documentation page."
    }
  ],
  "extracted_text_summary": "Collected public documentation page.",
  "screenshots": ["artifact://huginn-browser/screenshots/example-docs.png"],
  "action_log": ["validated_read_only_request", "captured_read_only_evidence"]
}
```

Approval-required response:

```json
{
  "status": "approval_required",
  "goal_id": 41,
  "task_id": 99,
  "evidence_id": 124,
  "evidence_type": "browser_approval_required",
  "risk_class": "external_mutation",
  "approval_required": true,
  "approval_id": 17,
  "start_urls": ["https://example.com/login"],
  "allowed_domains": ["example.com"],
  "max_pages": 1,
  "max_duration_seconds": 30,
  "action_log": ["approval_required", "adapter_not_executed"]
}
```

Failure response:

```json
{
  "status": "failed",
  "goal_id": 41,
  "task_id": 99,
  "evidence_id": 125,
  "evidence_type": "browser_failed",
  "risk_class": "read_only",
  "error_code": "adapter_failed",
  "error_message": "browser adapter failed"
}
```

## Evidence Artifact Schema

All accepted browser runs persist goal evidence through SQLite and emit the existing `goal.evidence_recorded` runtime event.

Read-only evidence payload:

```json
{
  "executor": "browser_readonly",
  "status": "adapter_response_recorded",
  "task": {},
  "adapter": {}
}
```

Approval-required evidence payload:

```json
{
  "executor": "huginn_browser_plugin",
  "status": "approval_required",
  "task": {},
  "risk_class": "external_mutation",
  "approval_required": true,
  "approval_id": 17,
  "executed": false
}
```

Failure evidence payload:

```json
{
  "executor": "huginn_browser_plugin",
  "status": "failed",
  "task": {},
  "risk_class": "read_only",
  "error_code": "adapter_failed",
  "error": "browser adapter failed"
}
```

Screenshot and other local artifacts must use Odin artifact paths returned by the adapter. Browser artifacts are local runtime evidence, not hidden browser state.

## Risk Classification

Risk classes:

- `read_only`: navigation, page fetch, DOM/text extraction, snapshot, screenshot, and other inspection-only activity.
- `external_mutation`: any action that could change external state.

Read-only action names:

- empty action
- `read`
- `navigate`
- `snapshot`
- `extract`

All other action names classify as `external_mutation` and require approval. This fail-closed behavior covers unknown action names.

## Approval Requirement Rules

The following require Odin approval before execution:

- login or authentication actions
- form submission
- downloads or uploads
- clicks that mutate external state
- message send
- purchase or money movement
- posting or publishing
- deletion
- any production action
- any unknown action name

When approval is required:

- `task_id` must be present.
- The task is blocked with `blocked_reason=approval_required`.
- A pending approval is created through SQLite approvals.
- The browser adapter is not called.
- Goal evidence is recorded with `evidence_type=browser_approval_required`.
- Review readback is through `odin review list --json`.

Approval resolution is owned by the existing Odin approval and review surfaces. This contract does not define a hidden browser-side continuation.

## Timeout And Cancellation

The executor validates `max_duration_seconds` before adapter invocation.

The live adapter reads its timeout from the injected adapter config, `ODIN_HUGINN_BROWSER_TIMEOUT_SECONDS`, or the request `max_duration_seconds`. It runs the worker command with a context deadline and kills the process group on cancellation or timeout.

Timeout responses use:

```json
{
  "status": "timeout",
  "error_code": "command_timeout"
}
```

Cancelled requests must not leave hidden browser state. Live workers must use explicit temporary profiles or approved session/profile references and must clean up temporary profiles before returning.

## Session And Profile References

No hidden browser state is allowed.

Session/profile state must be represented through Odin-visible references:

- `internal/store/sqlite` browser session profiles.
- `browser.session_created`
- `browser.session_login_requested`
- `browser.session_verified`
- `browser.session_revoked`
- `browser.session_profile_prepared`

Read-only public browsing may run without a saved session profile. Authenticated read-only browsing must reference a verified browser session profile and stay within that profile's permission tier. Login requests are external mutation and require approval before any browser-side login flow starts.

## Error And Recovery States

Canonical statuses:

- `recorded`: read-only evidence was collected and persisted.
- `approval_required`: request was classified as external mutation, approval was created, and adapter execution was skipped.
- `failed`: validation, adapter setup, worker execution, or response contract failed.
- `timeout`: live worker exceeded its deadline.
- `not_implemented`: live adapter command boundary exists but did not return evidence JSON.

Canonical error codes:

- `adapter_failed`
- `command_not_configured`
- `command_allowlist_empty`
- `command_not_allowed`
- `command_failed`
- `command_timeout`
- `invalid_response_json`
- `response_contract_invalid`

Recovery rules:

- Retry read-only failures only after preserving the failed evidence event.
- Do not retry `approval_required` by executing the adapter. Wait for approval resolution.
- Treat rejected approvals as terminal operator denial.
- Treat unsupported continuation after approval as an explicit resolver gap, not implicit permission to run.

## CLI And API Readback Surfaces

Current readback surfaces:

- `./bin/odin overview --json`: shows operator attention, pending approvals, runtime/event-backed status, and binary/source lanes.
- `./bin/odin review list --json`: shows pending approval-backed review items.
- `./bin/odin logs --json`: reads runtime events where available.
- Goal evidence readback surfaces show `browser_readonly`, `browser_approval_required`, and `browser_failed` artifacts.

API surfaces must reuse the same store-backed approval, event, and goal evidence data. They must not maintain a second browser run ledger.

## Deterministic Fake Adapter

`internal/adapters/huginnbrowser.StubAdapter` is the deterministic fake adapter. It returns `stub_local` read-only evidence, includes `no_live_browser_launched`, and performs no external browser launch or mutation.

Tests must use the fake adapter or an allowlisted fixture command. Live browser execution is disabled unless explicitly configured through the live adapter command and allowlist.
