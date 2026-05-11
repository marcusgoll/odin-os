# Delivery Gates Contract

Delivery gates are Odin-owned checkpoints for Work Items. They prevent a
workflow, prompt asset, or worker response from being treated as delivery
progress until Odin has durable evidence attached to the Work Item.

## Authority

- Work Item authority remains `tasks`.
- Run Attempt authority remains `runs`.
- Evidence authority is the append-only `events` stream.
- Operator proof starts at `odin work ...` and reads back through `odin logs`.

Registry workflows, skills, agents, and docs can describe expected gates, but
they do not advance gates by themselves.

## Gate Order

The canonical delivery gate spine is:

1. `domain_locked`
2. `design_approved`
3. `plan_ready`
4. `execution_selected`
5. `execution_complete`
6. `verified`
7. `branch_finished`
8. `learning_reviewed`

## Evidence Event

`odin work evidence` records `delivery.evidence_recorded` on the Work Item task
stream.

Required payload fields:

- `task_id`
- `work_item_key`
- `gate`
- `kind`
- `summary`

Optional payload fields:

- `ref`
- `recorded_by`

The command must not create a Run Attempt, Approval Request, branch, PR, issue
comment, deployment, or external mutation. It only records evidence that a later
gate-advancement command can evaluate.

## Follow-Up Boundary

`odin work advance` records `delivery.gate_advanced` on the Work Item task
stream only after Odin sees evidence for the requested gate.

Required payload fields:

- `task_id`
- `work_item_key`
- `gate`
- `next_gate`

Optional payload fields:

- `advanced_by`

The command must reject advancement when:

- no valid evidence exists for the requested gate
- a previous gate in the canonical spine has not advanced
- the requested gate has already advanced

This contract does not make merge, deploy, production mutation, public posting,
financial mutation, legal mutation, medical mutation, deletion, permissions
changes, purchases, calendar mutations with others, or message sending
automatic. Those operations remain approval-gated follow-up work.

## Merge and Deploy Approval Requests

`odin work approval request --task <id|key> --kind <merge|deploy>` creates or
reuses one task-scoped Approval Request for the named release gate. It records
the purpose through the existing `approval.requested` event stream:

- merge approvals use `requested_by=work_merge_gate`
- deploy approvals use `requested_by=work_deploy_gate`

The command must reject approval creation unless Odin can see:

- a PR handoff for the Work Item
- review-selection evidence for every selected review role
- an advanced `branch_finished` delivery gate
- for deploy approval, an already approved merge gate

`odin work proof --task <id|key> --json` reads the approval events back into
`merge_gate` and `deployment_gate` fields with approval ID, approval status,
and approved state. Merge and deploy approvals are separate approvals.

The command must not call GitHub merge APIs, deployment systems, branch
deletion, release creation, production mutation, or repository settings APIs.
It only creates approval evidence and blocks the Work Item with
`blocked_reason=approval_required`.

## Remaining Approval Enforcement

Future slices must still prove approval blocking before production mutation,
public posting, financial mutation, legal mutation, medical mutation, deletion,
permissions changes, purchases, calendar mutations with others, or message
sending.

## Security Review

This slice adds no external mutation client and no token handling. It reuses the
existing SQLite approval table, event stream, and `odin approvals resolve`
surface. Approval request creation is local-only; approval resolution changes
Odin runtime state but still does not merge, deploy, delete branches, or mutate
production systems.
