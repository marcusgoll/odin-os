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

A future approval-enforcement slice must prove:

- current-gate and next-action projection
- approval blocking before merge, deploy, production mutation, public posting,
  financial mutation, legal mutation, medical mutation, deletion, permissions
  changes, purchases, calendar mutations with others, or message sending
