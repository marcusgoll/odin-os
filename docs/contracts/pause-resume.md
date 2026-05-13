---
title: Pause And Resume Contract
status: active
date: 2026-05-09
---

# Pause And Resume Contract

This contract defines Odin's `odin:paused` semantics for governed work. It
exists so dashboard, scheduler, tracker, and runtime work do not invent separate
pause models.

## Scope

This contract covers:

- operator pause/resume of a governed Work Item
- scheduler and dispatch impact for paused work
- the relationship between runtime pause state and the GitHub `odin:paused`
  label
- the already-existing Follow-Up Obligation `paused` lifecycle state

Dashboard pause/resume routes are implemented as HTTP adapters over Work Item
runtime state. The routes do not own lifecycle truth.

## Ownership

Runtime state owns pause and resume.

- Work Item pause/resume is owned by the Work Execution runtime and persisted in
  SQLite.
- Dashboard and HTTP routes are adapters. They may request pause/resume, but they
  do not own lifecycle truth.
- Scheduler and dispatch loops read SQLite state only.
- GitHub labels are projections or source facts. They are never runtime
  authority for pause/resume.
- Follow-Up Obligation pause/resume is owned by the follow-through layer, not by
  the Work Item scheduler.

## Work Item Pause

A paused Work Item is an operator-held Work Item that must not dispatch until an
operator resumes it.

Odin must not add a new `paused` Work Item status. The canonical Work Item
operator statuses remain:

- `queued`
- `running`
- `blocked`
- `completed`
- `failed`
- `canceled`

Work Item pause is represented as:

- `status = blocked`
- `blocked_reason = operator_paused`

This keeps pause inside the existing blocked-work visibility model while making
the operator intent distinguishable from approval, denial, stale context, or
policy blockers.

Pause is valid only for non-terminal Work Items. In v1, pausing a running Work
Item must not silently kill or interrupt the active Run Attempt. The
implementation must either reject running pause with an explicit unsupported
reason or persist a future pause request that takes effect after the run returns
to a dispatchable state. It must not pretend the live run was paused unless a
separate run-interruption contract exists.

The initial dashboard implementation rejects running Work Item pause requests
with an explicit unsupported-action response. It pauses queued Work Items by
writing `status = blocked` and `blocked_reason = operator_paused` in SQLite.

## Work Item Resume

Resume reverses only the operator pause hold.

Resume is valid when:

- the Work Item is `blocked`
- `blocked_reason = operator_paused`
- no separate unresolved governance blocker owns the Work Item

Resume must:

- set `status = queued`
- clear `blocked_reason`
- leave approval, recovery, incident, and run evidence history intact
- let normal queue eligibility rules determine dispatch timing

The initial dashboard implementation resumes only Work Items currently blocked
with `blocked_reason = operator_paused`. It returns the Work Item to `queued`
and preserves its existing queue eligibility timestamp.

Generic resume must not unblock:

- `blocked_reason = approval_required`
- `blocked_reason = operator_denied`
- `blocked_reason = stale_context`
- policy, transition, or worktree blockers

Those blockers must be resolved by their owning workflow.

## Scheduler Impact

The Work Item dispatch scheduler must continue to dispatch only eligible queued
work. A paused Work Item is blocked, so it is excluded by the existing
`status = queued` dispatch query.

Pause does not delete queue metadata, rewrite run history, or mutate GitHub.
Resume returns the Work Item to the normal queued path and does not create a
special scheduler lane.

Follow-Up Obligation pause is separate. A paused Follow-Up Obligation must not
materialize due occurrences into Work Items. Resuming the obligation belongs to
the follow-through command/service surface and should return the obligation to
`active` without changing already materialized Work Items.

## GitHub Label Projection

`odin:paused` is a projection label.

Allowed uses:

- Odin may apply `odin:paused` to an external GitHub issue to mirror an
  Odin-owned runtime pause after that outbound mutation is explicitly approved
  through `docs/contracts/github-tracker-mutations.md`.
- Odin may remove `odin:paused` from an external GitHub issue after the
  Odin-owned runtime pause is resumed, under the same approved outbound mutation
  path.
- Odin may store inbound labels in external issue source-fact records for audit
  or operator display.

Disallowed uses:

- A manually added GitHub `odin:paused` label must not pause a Work Item.
- A manually removed GitHub `odin:paused` label must not resume a Work Item.
- GitHub label state must not be read as scheduler truth.
- GitHub labels must not bypass SQLite, approvals, policy, or transition gates.

If an inbound GitHub issue event includes `odin:paused`, Odin may record that
label as external evidence, but any runtime pause still requires an Odin-owned
pause command or approved runtime mutation.

## Implementation Tests

The dashboard pause/resume implementation covers:

- successful pause of a queued Work Item persists `status=blocked` and
  `blocked_reason=operator_paused`
- successful resume of an operator-paused Work Item returns it to `queued` and
  clears `blocked_reason`
- invalid issue or Work Item IDs return a typed not-found or validation error
- unauthenticated dashboard pause/resume requests fail before reaching runtime
  mutation code
- paused Work Items are excluded from dispatch because the scheduler only selects
  eligible `queued` rows
- resume refuses blocked Work Items whose `blocked_reason` belongs to approvals,
  denial, stale context, policy, or transition gates
- pause/resume does not call GitHub unless a later implementation explicitly
  wires the approval protocol in `docs/contracts/github-tracker-mutations.md`

Follow-Up Obligation pause/resume behavior remains separate from dashboard Work
Item pause/resume tests.

## Operating Rules

- Do not add a new Work Item `paused` status without a new ADR.
- Do not treat GitHub labels as runtime authority.
- Do not let dashboard routes mutate lifecycle state outside SQLite.
- Do not use runtime readiness `mode=paused` as Work Item pause state.
- Keep Follow-Up Obligation pause and Work Item pause separate in code, docs,
  tests, and operator output.
