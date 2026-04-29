---
title: Action Evidence Runtime Substrate
status: proposed
date: 2026-04-29
---

# Action Evidence Runtime Substrate

## Existing State

Odin already has SQLite as the canonical runtime authority, append-only runtime
events, workflow registry entries, workflow runs, and an existing approval
surface. The Marcus FLICA workflow suite in `CONTEXT.md` defines Action Record,
Prepared Action Payload, Action Evidence Event, Workflow Run Outcome, Submit
Path, Readback Path, External Readback, Substitute Proof, and the requirement
that live airline-facing workflows remain operator-invoked.

The current gap is that Odin does not yet have a durable action-level runtime
substrate tying a prepared external-action payload to operator approval,
submission evidence, internal downstream recording, and external readback proof.
Without that substrate, additional FLICA commands can submit work but cannot
prove the full action lifecycle in one canonical place.

## Domain Source Of Truth

- `CONTEXT.md`
- `docs/adr/0001-canonical-authority.md`
- `docs/contracts/runtime-events.md`
- `docs/contracts/registry-format.md`
- `docs/contracts/verification-model.md`
- `docs/contracts/homelab-operations.md`
- `registry/workflows/flica-schedule.md`
- `registry/workflows/flica-seniority-bid.md`
- `registry/workflows/flica-fcfs-bid.md`
- `registry/workflows/flica-tradeboard.md`
- `registry/workflows/flica-tradeboard-split-post.md`
- `registry/workflows/flica-annual-vacation.md`

## Goals

- Add a generic Odin runtime substrate for governed external actions.
- Prove the substrate first through Marcus FLICA workflows.
- Keep FLICA-specific behavior in schema-versioned payloads rather than in a
  FLICA-only persistence model.
- Reuse the existing Odin approval system by binding approvals to exactly one
  `action_id` and one immutable `payload_hash`.
- Derive action lifecycle state from append-only action evidence.
- Keep `/tradeboard` and future workflow commands as callers of the action
  substrate, not owners of action state.

## Non-Goals

- Do not build a full workflow engine.
- Do not move PBS-owned airline domain semantics into Odin.
- Do not add autonomous or scheduled live FLICA writes.
- Do not create a second approval system.
- Do not make filesystem artifacts, screenshots, or browser state outrank
  SQLite runtime state.
- Do not implement Annual Vacation submission in this design; keep it as a
  later proof case after the action substrate exists.

## Approach

Use an Action Core plus Event-Derived Lifecycle model.

Odin will add a generic `internal/runtime/actions` service backed by SQLite
tables for action records, immutable payloads, and append-only evidence events.
The service will own payload canonicalization, payload hashing, lifecycle
transition validation, proof evaluation, and approval binding checks.

The first concrete validation case is Marcus FLICA. FLICA workflows will use
generic action records with FLICA-specific payload schemas such as
`flica.bid_action.v1` and `flica.tradeboard_action.v1`.

## Architecture

The action substrate belongs in Odin runtime, not in `/tradeboard`, PBS, or the
registry loader.

Package boundaries:

- `internal/runtime/actions`: action service, lifecycle derivation, payload
  hashing, transition validation, proof evaluation, and approval binding.
- `internal/store/sqlite`: migrations and queries for action records, payloads,
  evidence events, and approval binding fields.
- `internal/runtime/events`: typed event names for important action mutations,
  or a mapping from action evidence into the existing event envelope.
- `internal/cli/repl`: thin operator commands for action inspection.
- `/tradeboard`: later caller of the action service for FLICA actions.

Every action links to exactly one workflow run and one workflow registry key.
The workflow registry entry declares the operator surface, proof requirement,
submit path, readback path, and substitute-proof policy. Runtime action state
records what actually happened during one invocation.

## Data Model

### `actions`

- `id`
- `workflow_key`
- `workflow_run_id`
- `action_type`
- `lifecycle_state`
- `current_payload_hash`
- `created_at`
- `updated_at`

`lifecycle_state` is a cached projection for reads. The canonical truth remains
the append-only evidence stream.

### `action_payloads`

- `id`
- `action_id`
- `payload_schema`
- `payload_schema_version`
- `payload_hash`
- `payload_json`
- `submit_path`
- `readback_path`
- `proof_requirement`
- `created_at`

The table enforces unique `(action_id, payload_hash)`.

### `action_evidence_events`

- `id`
- `action_id`
- `event_type`
- `event_version`
- `payload_hash`
- `approval_id`
- `run_id`
- `source`
- `evidence_json`
- `occurred_at`

Evidence is append-only. Corrections are later evidence events, not mutations of
old evidence.

### Approval Extension

Existing approvals gain nullable `action_id` and `payload_hash` fields.
Non-action approvals remain valid.

An action approval is valid only when:

- `approval.action_id` matches the action.
- `approval.payload_hash` matches an immutable payload for that action.
- The approval is resolved through the existing approval lifecycle.
- The action's current prepared payload hash still matches the approved hash at
  submission time.

## Payload Identity

Prepared Action Payload identity is the hash of canonicalized JSON plus core
submit/readback/proof fields.

Material changes create a new payload row and a new hash. Prior approvals remain
attached to their original hash and cannot authorize a later payload.

First FLICA payload schemas:

- `flica.bid_action.v1`
- `flica.tradeboard_action.v1`
- `flica.annual_vacation_action.v1` later, after the vacation operator surface
  is designed.

## Lifecycle

Canonical lifecycle states:

- `prepared`
- `preflighted`
- `approved`
- `submitted`
- `internally_recorded`
- `externally_read_back`
- `completed`
- `failed`
- `abandoned`

`failed`, `abandoned`, and `completed` are terminal for submission/completion
events. Correction evidence may still be appended to terminal actions.

Completion is allowed only when the workflow's declared proof requirement is
satisfied. If the workflow declares External Readback, completion requires an
external readback event. Substitute Proof is accepted only when declared by the
workflow payload or proof fields.

## Operator Flow

Initial read-only operator surfaces:

- `/actions`: list recent actions with action ID, workflow key, action type,
  lifecycle state, current payload hash, and proof status.
- `/actions <id>`: show workflow run, registry workflow, lifecycle, current
  payload hash, submit/readback/proof fields, approval binding, and latest
  evidence.
- `/actions <id> evidence`: show ordered append-only evidence.

Existing `/approvals` remains the approval surface. When an approval is
action-bound, it renders `action_id` and `payload_hash`. Resolving an approval
validates that the payload hash still matches the current prepared payload.

Write flow:

1. A workflow command prepares an action and payload.
2. Odin appends `action.prepared`.
3. Schedule preflight appends `action.preflighted` or `action.failed`.
4. Odin creates an existing approval request bound to `action_id` and
   `payload_hash`.
5. The operator resolves approval through the existing approval surface.
6. The submitter checks approval binding before submission.
7. Submission appends `action.submitted`.
8. PBS, flight-api, or another downstream system appends
   `action.internally_recorded`.
9. Huginn or a declared readback route appends
   `action.externally_read_back`.
10. Odin appends `action.completed` only after proof is satisfied.

## Error Handling

Operator-visible failure codes:

- `payload_changed_after_approval`
- `approval_missing`
- `approval_payload_mismatch`
- `invalid_lifecycle_transition`
- `schedule_preflight_missing`
- `schedule_preflight_stale`
- `submit_path_missing`
- `readback_path_missing`
- `external_readback_missing`
- `substitute_proof_not_declared`
- `terminal_action_closed`

Failed actions remain inspectable. A corrected payload creates a new payload
hash and requires a new approval. A failed action may be abandoned or superseded
by a new action. Evidence events include `source`, `reason`, and
machine-readable `details`.

## Invariants

- An action belongs to exactly one workflow run.
- An action references one workflow registry key.
- A payload hash is immutable.
- Material payload changes create a new payload hash.
- Approval authorizes only one `action_id` plus one `payload_hash`.
- Submission is rejected without a resolved approval matching the current
  payload hash.
- Completion is rejected unless the proof requirement is satisfied.
- External Readback is required when the workflow declares it.
- Substitute Proof is accepted only when declared.
- Terminal actions reject submission and completion events.
- Corrections append evidence and do not rewrite prior evidence.
- FLICA live writes remain operator-invoked only.
- Huginn is required when live browser proof, authentication, Duo, or FLICA UI
  readback is part of the submit or readback path.

## Testing And Verification

Unit tests:

- Payload canonicalization and hashing.
- Lifecycle transition validation.
- Approval binding validation.
- Proof requirement evaluation.
- Failure code mapping.

Store and integration tests:

- Create action, payload, and evidence events.
- Enforce append-only evidence.
- Derive current lifecycle projection from evidence.
- Enforce payload hash uniqueness.
- Reject approval resolution against mismatched payload hash.
- Reject unsafe events on terminal actions.

Command-level E2E:

- Build `./bin/odin`.
- Run scripted shell with a temp `ODIN_ROOT`.
- Prove `/actions` lists a fixture action.
- Prove `/actions <id>` shows payload hash, lifecycle, approval binding, and
  proof status.
- Prove `/actions <id> evidence` shows ordered evidence.
- Prove operator-visible failure output for approval mismatch or missing proof.

FLICA proof sequence:

1. Use deterministic fake or fixture-backed FLICA action data.
2. Wire one real `/tradeboard` integration slice.
3. Prove live behavior only when Huginn, FLICA SSO/Duo, PBS or flight-api, and
   FLICA readback are available.

Live FLICA proof must show:

- schedule preflight evidence
- action payload hash
- approval binding
- submission evidence
- FLICA readback or declared substitute proof
- final `/actions <id> evidence` output from the real `odin` binary

Unproven until a live run:

- Huginn SSO/Duo behavior
- FLICA readback availability
- PBS or flight-api action log integration

## Rejected Alternatives

### FLICA-only action substrate

Rejected because it would duplicate the same approval, payload identity, and
evidence model for later governed external actions.

### Mostly JSON action envelope

Rejected because it weakens approval binding, lifecycle validation, and
operator-visible proof requirements.

### Approval-centric action envelope

Rejected because it conflates approval lifecycle with action lifecycle and makes
completed-without-action workflow runs awkward to model.

### Mutable lifecycle as canonical truth

Rejected because it conflicts with Odin's append-only audit model. A cached
state column is acceptable for reads, but evidence remains authoritative.

## Open Questions

- Which existing approval query paths need the smallest compatible extension for
  action-bound approvals?
- Should action evidence events be mirrored into the generic runtime `events`
  table immediately, or should the action evidence table be the first source
  with generic event mirroring added in a later slice?
- Which FLICA workflow should be the first real integration proof after the fake
  action fixture: TradeBoard split-post or FCFS pickup?
