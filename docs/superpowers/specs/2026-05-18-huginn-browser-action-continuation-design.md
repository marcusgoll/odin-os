# Huginn Browser Action Continuation Design

Date: 2026-05-18

## Current State

Huginn already exists in `odin-os` as Browser Control, not as a standalone workflow authority. The current `odin browser run` surface is intentionally read-only for live browser evidence:

- `odin browser run ... --action read|navigate|snapshot|extract`
- `odin browser session ...` for session metadata, login handoff, runner planning, profile artifacts, and profile materialization
- `internal/executors/browser.Service.RunPlugin` classifies any action outside read-only as `external_mutation`
- mutation-class requests create an Approval Request and do not call the Huginn adapter
- `docs/contracts/huginn-browser-plugin.md` explicitly says approved mutation continuation is not yet defined
- `docs/contracts/live-driver-tools.md` already has narrow operator-attended X publish and Robinhood transfer driver lanes

The existing system can read pages, gather screenshots, persist evidence, create browser-session metadata, and request approval for proposed mutations. It cannot yet take a generic approved browser mutation request and continue through one bounded action.

## Recommended Product Decision

Use the canonical term **Browser Mutation Continuation** for this extension.

Recommended definition: after Odin has an approved Approval Request for an exact browser action payload, Browser Control may execute one bounded action through a supported driver and write the observed outcome back as Run Attempt evidence.

Do not frame this as "Huginn acts like a human anywhere on the web." That phrase points at the desired ergonomics, but it is too broad for a governed execution system. The safe product shape is "operator-approved, bounded, visible browser action continuations."

## Design Tree Decisions

1. Authority boundary

   Recommended: Odin remains the control plane and approval authority. Huginn remains the browser action adapter. The adapter never decides whether posting, submitting, deleting, buying, or changing account state is allowed.

2. Scope of v1 actions

   Recommended: v1 supports a small explicit action taxonomy:

   - `open_link`
   - `type_text`
   - `select_option`
   - `click_control`
   - `submit_form`
   - `publish_post`
   - `publish_reply`

   Each action must carry a typed payload. No free-form "browse and do the thing" action is allowed.

3. Authentication and sign-in

   Recommended: sign-in is a Browser Intervention, not an autonomous credential action. Huginn may open the sign-in page and pause with `login_required`, `mfa_required`, `captcha_required`, or `human_confirmation_required`. It must not store passwords, TOTP seeds, backup codes, or session tokens in request or evidence.

4. Social media

   Recommended: preserve the existing social boundary:

   - X may use operator-attended approved text posts and approved replies, one item at a time.
   - LinkedIn remains manual unless an official interface is approved and documented.
   - Likes, reposts, follows, DMs, scraping, or engagement manipulation are out of scope.

5. Form filling

   Recommended: allow form filling only from a reviewed field map on an allowed domain. Evidence should record field labels/selectors and redacted value summaries, not secrets or full sensitive payloads.

6. Clicking and opening links

   Recommended: split clicks into `open_link` and `click_control`. `open_link` can be read-only when it only navigates inside allowed domains. `click_control` is mutating by default unless the driver can prove it is navigation-only.

7. Human-like behavior

   Recommended: implement human-safe pacing and visible headed-session handoff, not stealth evasion. Delays, focus, scroll, and screenshots are acceptable for reliability and operator inspection. Avoid any language or behavior that implies CAPTCHA bypass, bot-detection evasion, hidden automation, or platform-rule circumvention.

8. Evidence

   Recommended: every continuation records:

   - approved `approval_id`
   - owning `task_id` and `run_id`
   - action kind and payload hash
   - allowed domain set
   - browser session id when used
   - pre-action visible state summary
   - post-action visible state summary
   - screenshot metadata where available
   - final URL
   - result status
   - limitations and intervention reason if blocked

9. Failure mode

   Recommended: fail closed. If the page changes, selector is ambiguous, auth appears, the domain changes, a button text differs from the approved payload, or the requested action is unsupported, stop and record evidence for operator review.

## Implementation Plan

### Goal 1: Contract and CLI shape

Add a continuation section to `docs/contracts/huginn-browser-plugin.md`.

Add a thin operator surface under the existing browser family:

```text
odin browser continue --approval-id <id> --json
```

The command should only resume a pending approved browser-mutation Approval Request. It should not accept new free-form action text. The approved payload is read from persisted approval/runtime state.

### Goal 2: Approval payload model

Persist a browser mutation request envelope alongside the approval source:

```json
{
  "schema_version": 1,
  "action_kind": "submit_form",
  "allowed_domains": ["example.com"],
  "start_url": "https://example.com/form",
  "browser_session_id": 7,
  "selector_plan": [
    {"kind": "type_text", "selector": "#name", "value_ref": "field:name"},
    {"kind": "click_control", "selector": "button[type=submit]", "expected_text": "Submit"}
  ],
  "redaction_policy": "secrets_and_sensitive_values",
  "requested_by": "operator"
}
```

The payload must be immutable after approval. If the page or field plan changes, Odin creates a new approval request.

### Goal 3: Driver contract

Introduce a separate mutation driver contract rather than widening the read-only Huginn adapter response.

Recommended environment variable:

```text
ODIN_HUGINN_BROWSER_MUTATION_DRIVER
```

The driver reads one JSON request and returns one JSON response. The response must include `status`, `adapter_kind`, `action_kind`, `final_url`, `evidence`, and `intervention_reason` when blocked. It must never return credentials, cookies, tokens, or unredacted sensitive form values.

### Goal 4: Runtime service

Add a browser mutation continuation service in `internal/executors/browser` that:

1. loads the Approval Request
2. verifies it is approved and source-compatible
3. verifies payload hash and allowed domains
4. starts a Run Attempt
5. invokes the allowlisted mutation driver
6. records a `browser_mutation_evidence` run artifact
7. finishes the Run Attempt as completed, failed, or intervention-required

Keep the existing read-only service path unchanged.

### Goal 5: First supported vertical slice

Recommended first vertical slice: approved form submit on a local fixture site.

Reason: it proves the generic continuation mechanics without platform-policy risk from public social media or finance.

Acceptance proof:

```text
ODIN_ROOT=<tmp> ./bin/odin browser run --task-id <id> --url https://fixture.local/form --action submit_form --json
ODIN_ROOT=<tmp> ./bin/odin review list --json
ODIN_ROOT=<tmp> ./bin/odin approvals resolve <approval-id> approve --reason fixture-proof --json
ODIN_ROOT=<tmp> ./bin/odin browser continue --approval-id <approval-id> --json
ODIN_ROOT=<tmp> ./bin/odin logs --json
```

Expected result: the first command creates `approval_required` without calling the adapter; the continuation command calls the fixture mutation driver only after approval and records redacted browser mutation evidence.

### Goal 6: Social-specific follow-up

After the generic continuation works on fixtures, map X posts and replies onto it only if it preserves the current `/memory publish ... via=huginn_x` flow.

The approved `social_outcome` should remain the social source of truth. Browser continuation is the execution mechanism, not a second social queue.

## Security Review Requirements

This work touches runners, executors, shell wrappers, browser profiles, external mutation, approval policy, and command allowlists. Any implementation PR must include a security review section covering:

- command allowlist enforcement
- environment variable allowlist changes
- profile materialization and cleanup
- secret redaction
- domain allowlist enforcement
- selector ambiguity handling
- approval payload immutability
- audit event coverage
- denial of credential capture and CAPTCHA/MFA bypass

## Questions to Confirm

These are not blockers for the first fixture-backed implementation:

1. Should the first real-world continuation after fixtures be X reply publishing, a non-social form submission, or Robinhood transfer repair?
2. Should `click_control` require visible text matching for every button, or allow selector-only clicks when the selector comes from a driver-owned recipe?
3. Should approved continuation be exposed only through CLI first, or also through the mobile/PWA approval surface in the same slice?

## Out of Scope

- autonomous LinkedIn activity
- bulk social engagement
- likes, follows, reposts, DMs, or engagement farming
- CAPTCHA solving or bot-detection evasion
- background credential entry
- general-purpose "browse the web and act" prompts
- arbitrary JavaScript execution from operator input
- treating Huginn as a workflow authority or source of truth

## Implementation Handoff

Build this as a PR-sized brownfield slice:

- extend existing `internal/executors/browser` rather than adding a parallel browser executor
- add `odin browser continue` in the existing lifecycle/browser command family
- reuse SQLite Approval Requests, Run Attempts, run artifacts, and runtime events
- add a fixture mutation driver and deterministic integration test before any live site
- keep current live X and Robinhood drivers as narrow compatibility lanes until the generic continuation proves equivalent safety

The decisive invariant: no external browser mutation reaches the driver unless a real Odin Approval Request for the exact immutable payload has been approved and can be read back through an Operator Surface.
