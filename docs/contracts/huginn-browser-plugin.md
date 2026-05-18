---
title: Huginn Browser Plugin Contract
status: active
date: 2026-05-12
---

# Huginn Browser Plugin Contract

The Huginn browser plugin is not a standalone plugin manager. It is an Odin
browser capability implemented through the existing browser executor seam,
`internal/adapters/huginnbrowser`, SQLite goal evidence, and SQLite approval
events.

## Runtime authority

- Odin owns policy, approvals, audit events, session metadata, and evidence.
- Huginn owns only bounded browser collection behind the adapter interface.
- Browser actions default to read-only.
- External mutation must not execute until Odin has an approved continuation
  path for the exact action.
- Hidden browser state is forbidden. Browser session/profile references are
  metadata-only references to Odin-managed browser session records.

## Request schema

```json
{
  "request_id": "optional-caller-id",
  "goal_id": 1,
  "task_id": 42,
  "worker_mode": "browser",
  "objective": "Collect account-visible evidence",
  "allowed_domains": ["example.com"],
  "start_urls": ["https://example.com/account"],
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
  "browser_session_id": 7,
  "browser_session": {
    "id": 7,
    "domain": "example.com",
    "status": "verified",
    "permission_tier": "authenticated_readonly",
    "profile_storage_policy": "encrypted_required",
    "profile_path": "browser-sessions/profiles/example"
  },
  "actions": ["navigate", "snapshot"],
  "requested_by": "operator"
}
```

Field rules:

- `goal_id` is required for read-only evidence execution because evidence is
  persisted as goal evidence.
- `task_id` is required for mutation-class requests because approvals are tied
  to Work Items.
- `objective`, `allowed_domains`, `start_urls`, `max_pages`, and
  `max_duration_seconds` follow the existing read-only browser executor
  validation rules.
- `actions` omitted or containing only `read`, `navigate`, `snapshot`, or
  `extract` is read-only.
- Any other action is classified as external mutation and must create approval
  instead of reaching the browser adapter.

## Response schema

```json
{
  "status": "recorded",
  "request_id": "optional-caller-id",
  "risk_class": "read_only",
  "approval_required": false,
  "approval_id": 0,
  "task_id": 0,
  "evidence": {
    "id": 10,
    "type": "browser_readonly",
    "uri": "https://example.com/account",
    "summary": "Collected account-visible evidence.",
    "created_by": "browser_executor"
  },
  "result": {
    "status": "recorded",
    "adapter_status": "completed",
    "adapter_kind": "stub_local"
  },
  "mutating_actions": [],
  "error_code": "",
  "error_message": ""
}
```

Mutation-class response:

```json
{
  "status": "approval_required",
  "request_id": "optional-caller-id",
  "risk_class": "external_mutation",
  "approval_required": true,
  "approval_id": 23,
  "task_id": 42,
  "mutating_actions": ["submit_form"],
  "error_code": "approval_required",
  "error_message": "external browser mutation requires Odin approval before execution"
}
```

## Evidence artifact schema

Read-only browser runs persist evidence through `goal_evidence` with:

- `evidence_type`: `browser_readonly`
- `summary`: adapter summary or default read-only browser evidence summary
- `uri`: first visited URL, otherwise first requested start URL
- `payload_json.executor`: `browser_readonly`
- `payload_json.task`: validated Odin browser request
- `payload_json.adapter`: Huginn adapter response
- audit event: `goal.evidence_recorded`

Work-linked read-only browser runs use `odin browser run --task-id <id> ...`
and persist evidence through canonical `run_artifacts` with:

- `artifact_type`: `browser_evidence`
- `run.executor`: `huginn_browser`
- safe evidence fields: screenshot metadata/path, page title/url, DOM text
  summary, selected links, downloaded file metadata, form state summary without
  secrets, browser error/recovery notes, confidence, and limitations
- audit event path: `run.started` and `run.finished` with the browser evidence
  artifact summary in `artifacts_json`

Successful work-linked browser evidence records the artifact without completing
the work item. Failed capture marks the browser evidence work item failed and
emits recovery guidance for operator review.

Mutation-class requests do not create browser evidence because the browser
adapter is not invoked. They create a pending Approval Request and emit
`approval.requested`.

## Risk classification

Risk classes:

- `read_only`: navigation, extraction, snapshot, and visible evidence
  collection.
- `external_mutation`: login, form submit, download, upload, click that mutates
  external state, message send, purchase, posting, deletion, production action,
  account change, permission change, transfer submit, or any unrecognized
  browser action.

Unknown action names fail closed as `external_mutation`.

## Approval rules

- Read-only requests run without approval after executor validation.
- Mutation-class requests require an existing `task_id`.
- Mutation-class requests call Odin's SQLite approval path and return
  `approval_required`.
- Mutation-class requests must not call `huginnbrowser.Adapter.Run`.
- Approval records are visible through `odin review` and `odin approvals`.
- Approved mutation continuation is exposed only through
  `odin browser continue --approval-id <id>`. It loads the immutable
  browser-mutation payload persisted with the Approval Request, verifies that
  the approval is `approved`, starts a Run Attempt, invokes an allowlisted
  mutation driver, records `browser_mutation_evidence`, and finishes the run.
- The continuation command does not accept new action text, URLs, selectors, or
  form values. If the approved payload is stale or incomplete, a new Approval
  Request is required.
- The v1 continuation driver is configured through
  `ODIN_HUGINN_BROWSER_MUTATION_DRIVER` and must also be listed in
  `ODIN_HUGINN_BROWSER_MUTATION_ALLOWED_COMMANDS`.
- The mutation driver receives one JSON request and returns one JSON response.
  The response must include `status`, `adapter_kind`, `action_kind`, and
  redacted outcome evidence. It must not return credentials, cookies, tokens,
  passwords, TOTP seeds, backup codes, or unredacted sensitive form values.
- The first supported generic continuation slice is fixture-backed form submit.
  Existing X and Robinhood drivers remain narrow compatibility lanes until they
  are migrated onto this generic continuation contract without weakening their
  approval boundaries.

## Timeout and cancellation

- Caller cancellation is carried by `context.Context`.
- `max_duration_seconds` is bounded by the browser executor limit.
- The live Huginn adapter enforces its process timeout and kills the process
  group on timeout.
- Timeout responses are persisted as adapter evidence for read-only requests.
- Mutation-class requests do not launch a process, so cancellation can only
  interrupt approval creation before the SQLite transaction completes.

## Session and profile references

- `browser_session_id` resolves through SQLite before read-only execution.
- The session must be `verified`.
- The session domain must match `allowed_domains` and every `start_url`.
- Adapter requests receive only safe session metadata.
- Profile bytes, credentials, cookies, tokens, passwords, TOTP seeds, and
  backup codes must never appear in request, response, evidence, or logs.

## Error and recovery states

- `adapter_failed`: adapter returned an execution error.
- `command_not_configured`: live adapter command missing.
- `command_not_allowed`: live adapter command not allowlisted.
- `command_timeout`: live adapter timed out.
- `response_contract_invalid`: live adapter response contained invalid or
  mutation-looking fields.
- `approval_required`: request was classified as external mutation and was
  converted into an Odin approval.

Recovery is operator-driven: inspect `odin review list --json`,
`odin review show <approval-id> --json`, failed work details, goal evidence,
run artifacts, and runtime logs.

## CLI/API readback surfaces

- `odin browser run ... --json` reads back read-only goal evidence execution.
- `odin browser run --task-id <id> ... --json` reads back work-linked browser
  evidence execution.
- `odin browser continue --approval-id <id> --json` reads back approved
  browser-mutation continuation evidence.
- `odin review list --json` reads back pending browser mutation approvals and
  browser evidence counts for linked work.
- `odin approvals ... --json` reads back approval state.
- `odin logs ... --json` reads back `goal.evidence_recorded` and
  `approval.requested` audit events.
- `odin overview --json` projects activity, approvals, browser evidence, and
  capability truth without becoming a second runtime authority.
