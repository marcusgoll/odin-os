# Work Execution State Contract

Odin work execution is proven through durable runtime objects, not agent
self-report. This contract defines which object owns each operator-visible state
claim and which command surface must prove it.

## Canonical Objects

- Canonical operator object: `Work Item`.
- Physical compatibility table: `tasks`.
- Canonical execution object: `Run Attempt`.
- Physical compatibility table: `runs`.
- Canonical execution entrypoint family: `odin work` and `odin task run`.
- Canonical Work Item readback: `odin jobs` until a future rename or alias is
  explicitly designed.
- Canonical Run Attempt readback: `odin runs`.

Registry workflows and agent definitions can describe how work should be
performed. They are authored assets, not runtime authority, until Odin invokes
them through a Work Item and Run Attempt.

## State Ownership Rules

- `drafted` is not a Work Item status.
  Drafts belong to intake, review, proposal, ticket-generation, or another
  source-specific pre-work surface.
- `approved` is not a Work Item status.
  Approval belongs to an Approval Request or a source-owned review decision.
- `done` is never persisted as a primary Work Item status.
  It is an operator bucket derived from terminal states.
- `blocked` must include a non-empty `blocked_reason` on the owning Work Item
  when the block is represented in work execution.
- `running` requires a linked active Run Attempt visible through `odin runs`.
- Failed retryability is derived from retry policy, counters, and failure
  analysis; it is not encoded by the `failed` status alone.

## Work Execution State Matrix

| Operator concept | Canonical owner | Stored state | Required proof |
| --- | --- | --- | --- |
| `drafted` | Intake / Review / proposal source | source-specific review or draft status | visible in `odin review` or the source command; no Work Item is required |
| `queued` | Work Execution | `tasks.status = queued` | visible in `odin jobs`; no active `runs` row is required |
| `preparing` | Work Execution | `tasks.status = preparing`, `runs.status = preparing` | visible during dispatch preparation when captured by tests or operator readback |
| `running` | Work Execution | `tasks.status = running`, `runs.status = running`, `tasks.current_run_id = runs.id` | visible in both `odin jobs` and `odin runs` |
| `blocked` | owning workflow or policy gate | `tasks.status = blocked`, non-empty `blocked_reason` | visible in `odin jobs`, `odin work status`, and the relevant queue such as approvals or review |
| `approved` | Approval or source workflow | `approvals.status = approved` or source-specific decision | visible in `odin approvals`, `odin review`, or workflow detail, not as Work Item status |
| `completed` | Work Execution | `tasks.status = completed`, terminal run evidence | visible in `odin jobs` and `odin runs` / `odin runs show` |
| `failed` | Work Execution / Recovery | `tasks.status = failed`, failed run evidence, optional failure analysis | visible in `odin jobs`, `odin runs`, and `odin review` failed-work when retry or follow-up applies |
| `canceled` | Work Execution | `tasks.status = canceled` or compatibility `cancelled` | visible in `odin jobs`; no active run remains |
| `done` | operator projection | derived from terminal statuses | never persisted as primary status |

## Command Proof Matrix

Each state claim must map to one of these existing operator surfaces:

| Command | Proof responsibility |
| --- | --- |
| `odin work status` | counts Work Items, open Work Items, active Run Attempts, pending approvals, failed retryable Work Items, retry-blocked Work Items, explicit intent Work Items, and fallback intent Work Items |
| `odin work start --project <key> --title <text> [--intent ...]` | creates a queued Work Item and persists explicit intent when provided |
| `odin work proof --task <id\|key> --json` | correlates source intake, review, Work Item, Run Attempts, approvals, PR handoff, merge gate, deployment gate, and task events without mutating runtime state |
| `odin work proof --intake <id\|key> --json` | proves unclear or draft intake state before a Work Item exists, including clarification prompts, draft artifact, review queue state, and intake events without mutation |
| `odin work pr prepare --task <id\|key> --summary <text> --test <text> --risk <text> --command <text> --dry-run --json` | prepares durable PR handoff and review-selection evidence without external GitHub mutation; live PR mutation remains approval-gated and unsupported until an Approval Request resolver is wired |
| `odin work dispatch --task <id\|key> --json` | admits dispatch, creates a Run Attempt, or blocks with a policy/approval reason |
| `odin work execute --task <id\|key> --json` | executes an already dispatched Run Attempt and reports terminal state |
| `odin task run --project <key> --title <text> --json` | proves the compatibility create-and-execute path |
| `odin jobs --json` | proves Work Item status, intent, blocked reason, and current run linkage |
| `odin runs --json` | proves Run Attempt status, executor, attempt number, and Work Item linkage |
| `odin approvals all --json` | proves approval requests created by high-risk or policy-gated work |
| `odin review list --json` | proves draft/review visibility and failed-work review visibility when applicable |

## Compatibility Boundaries

Storage-era names such as `tasks`, `jobs`, and `runs` remain valid compatibility
surfaces. They should not be renamed or replaced in this contract slice.

Do not introduce parallel queues, workflow-run tables, executor frameworks, or
prompt-agent runtime authority to satisfy state readback. New workflow types
must compile down to the same Work Item, Run Attempt, approval, review, event,
and artifact surfaces unless a later ADR explicitly changes the authority
model.
