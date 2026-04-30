---
kind: workflow
key: flica-tradeboard-split-post
title: FLICA TradeBoard Split Post Workflow
summary: Operator-attended workflow for posting only selected legs of a FLICA pairing to the airline TradeBoard through Odin, PBS, and Huginn.
status: active
tags:
  - flica
  - tradeboard
  - huginn
  - pbs
  - operator-attended
owners:
  - odin-core
  - pbs-flight-api
entrypoint: command:/tradeboard post
composes:
  - command:/tradeboard sync-status
  - command:/tradeboard scan
  - huginn-browser
  - pbs-flight-api
---

# FLICA TradeBoard Split Post Workflow

## Purpose
Provide a repeatable operator workflow for posting a partial FLICA pairing to the airline TradeBoard without dropping the whole trip. Odin remains the operator surface, PBS owns the FLICA browser/session and posting code, and Huginn is the browser used for live authentication and readback proof.

## When to Use
Use this workflow when Marcus needs to post or pick up a FLICA TradeBoard item and the action must be performed through Odin with live FLICA verification. Use it especially when the request is to split a pairing and advertise only selected legs, such as a turn inside a multi-day trip.

## Inputs
Required inputs are the target pairing ID, active TradeBoard BCID for the pairing month, trade type, split legs to drop, split legs to keep, and a short meaningful comment. The comment should identify the turn or trip fragment plus report and end times, for example `DFW-DAY-DFW_turn_report_1351_end_2030`.

Use FLICA's live pairing detail page or split picker to map the operator's desired legs to FLICA checkbox indexes. The API expects zero-based checkbox indexes in `split_legs`; FLICA readback displays one-based split numbers. For example, zero-based `split_legs=9,10` reads back as `[10,11]`.

## Procedure
Audit first. Confirm the current active TradeBoard month and BCID from FLICA, not from stale scan state. Completed months must not be used for a new post.

Confirm FLICA Sync status through Odin:

```text
/tradeboard sync-status
```

If the saved FLICA session is stale, authenticate through Huginn using the AA SSO start URL:

```text
https://idp.aa.com/idp/startSSO.ping?PartnerSpId=psa.flica.net
```

When Duo appears, use the operator-attended path requested by the user, including `Call Me` when push approval is not the chosen method. After authentication, export the Huginn browser cookies into PBS session state only through the existing PBS auth-state path.

Refresh or verify TradeBoard state through Odin:

```text
/tradeboard scan headless=true
```

Inspect the live TradeBoard post-request page and pairing detail with Huginn when the split mapping is not already proven. The split picker must show that the selected segment starts and ends at base. Do not submit if the split picker cannot represent the requested legs.

Post through Odin only after the split mapping is known:

```text
/tradeboard post <PAIRING> type=Drop bcid=<ACTIVE_BCID> comment=<SHORT_COMMENT> split_legs=<ZERO_BASED_DROP_INDEXES> split_keep_legs=<ZERO_BASED_KEEP_INDEXES> confirm
```

After Odin queues the request, inspect the PBS action log and the live FLICA My Requests page. Treat the workflow as incomplete until FLICA My Requests shows the posted pairing, split marker, comment, and posted timestamp.

## Outputs
The workflow outputs a submitted or failed PBS action record, FLICA screenshots under the PBS TradeBoard screenshot directory, and a live FLICA My Requests readback. A successful readback should include the pairing ID, split marker, type, comment, and posted timestamp.

## Constraints
Operator invocation is required; do not run this workflow autonomously. Do not use plain Playwright as the operator browser when Huginn is available. Do not bypass Odin for the final post command. Do not post against a completed-month BCID. Do not fall back from a split request to a full-pairing drop. If split controls, base-to-base validation, or final readback cannot be proven, stop with a failed action.

AA credentials remain in PBS environment and must not be copied into Odin registry files. Duo is an operator-attended authentication step, not a credential failure. The workflow must avoid printing secrets and must preserve saved session state only in the existing PBS browser auth file.

## Success Criteria
The real Odin `/tradeboard post` command completes, PBS records the action as `submitted`, and FLICA My Requests confirms the intended split. The readback must show the correct active month, pairing, split marker, and comment. For the proven DAY turn example on April 28, 2026, the readback was `W7084C:26APR [10,11]` with comment `DFW-DAY-DFW_turn_report_1351_end_2030`.
