# Software Factory Lane Design

Date: 2026-05-18
Status: approved design
Repo: odin-os

## Summary

The Software Factory Lane is the Odin-managed production path for managed-project software delivery. It addresses the velocity gap where AI makes individual coding faster, but delivery remains slow because planning, coding, review, testing, PR handling, and merge decisions still depend on manual handoffs.

Version 1 is a managed-project delivery profile over Odin's existing control plane. It is not a new runtime authority, queue, executor framework, or provider-specific cloud worker system. The lane takes explicit operator requests and reviewed intake promotions through governed Work Items, Run Attempts, approvals, reviews, PR handoffs, green-check waits, autonomous merge, and closeout evidence.

The autonomy boundary for v1 is merge-when-green. Production deployment remains outside autonomous execution, except that the lane may create a governed deploy handoff or review item.

## Goals

- Remove manual SDLC handoffs for managed-project work from request capture through merge.
- Keep Odin's existing Work Item, Run Attempt, approval, review, delegation, executor-routing, and operator readback surfaces as the source of truth.
- Support both explicit operator starts and reviewed event-driven intake promotion.
- Keep local Codex, cloud devbox, and future sandbox workers as executor route choices, not separate product paths.
- Fail closed when policy, state, checks, review, or merge readiness is ambiguous.

## Non-Goals

- No first-class factory-run table in v1.
- No hidden worker queue or provider-native swarm path.
- No autonomous production deployment in v1.
- No bypass of managed-project governance, branch/worktree policy, review queue, approval gates, or repo-owned verification.
- No direct execution from raw external intake without reviewed promotion unless a later project policy explicitly allows that source and event class.

## Existing Odin Surfaces Reused

- `Intake Item` and reviewed intake promotion for event-driven starts.
- `Work Item` as the durable unit of governed factory work.
- `Run Attempt` as the execution proof for each phase.
- `Approval Request` for high-risk, destructive, production, policy, or out-of-allowlist operations.
- `odin review`, `odin approvals`, `odin jobs`, `odin runs`, `odin work status`, and overview read models for operator visibility.
- Companion delegation records for bounded child work.
- Executor routing for local, cloud, devbox, and future sandbox execution.
- PR handoff and review state as the merge-readiness integration point.

## Architecture

The Software Factory Lane is a named managed-project delivery profile. It can be surfaced through a thin operator command such as `odin factory ...`, but that command is only an adapter into the existing managed-project delivery path.

The lane compiles all work into existing Odin runtime state:

1. Intake enters through a raw/reviewed Intake Item or explicit operator start.
2. Admission creates or updates a governed Work Item.
3. Each delivery phase executes through one or more Run Attempts.
4. Optional specialist or background work uses companion delegation records attached to the parent Work Item.
5. Review, approval, failure recovery, PR state, and merge readiness are projected through existing operator surfaces.
6. Executor selection uses the existing executor-routing contract. Local Codex and cloud/devbox workers are interchangeable route targets when policy and capability match.

There must not be a separate factory runtime authority. Factory state is derived from the owning Work Item, Run Attempts, approvals, review entries, PR handoff, and phase artifacts.

## Components

### Factory Delivery Profile

The profile defines the managed-project production phases:

1. Intake
2. Specification
3. Implementation plan
4. Implementation
5. Verification
6. Review
7. PR creation or PR handoff
8. Green-check wait
9. Merge
10. Closeout evidence

The profile should extend the existing managed-project delivery vocabulary rather than introduce a parallel workflow kind.

### Factory Admission Adapter

Admission accepts two trigger families:

- Explicit operator start.
- Reviewed intake promotion from raw intake, GitHub issues, CI failures, deploy failures, alerts, or other normalized sources.

Admission validates:

- Managed-project scope.
- Project policy and branch/worktree rules.
- Risk class.
- Requested autonomy boundary.
- Required approvals.
- Whether merge-when-green is enabled for this project and work class.
- Whether the selected trigger is allowed to enter Factory Lane.

Successful admission creates or updates a Work Item with factory-lane intent and explicit autonomy limits.

### Phase Orchestrator

The phase orchestrator advances the parent Work Item through phase-specific Run Attempts and artifacts. It does not own a new queue.

Each phase records enough evidence for operator readback:

- Spec path or spec artifact.
- Plan path or plan artifact.
- Branch and leased worktree.
- Executor route used.
- Commands run.
- Test and verification outputs.
- PR URL or handoff id.
- Review outcome.
- Check status.
- Merge result.
- Closeout summary and unproven boundaries.

### Delegation Planner

The lane may use child delegations when the parent objective has bounded independent subproblems or a verifier step. Child work must:

- Narrow the parent's authority.
- Carry explicit budget, retry, and stop conditions.
- Emit structured artifacts with evidence, confidence, risks, and next actions.
- Reconcile back into the parent Work Item through the declared convergence mode.

Child delegations are not a second queue and must remain visible through existing delegation, jobs, runs, and review surfaces.

### Merge-When-Green Gate

The merge gate can merge only when all configured conditions pass:

- Required checks are green.
- Branch protection is satisfied.
- No unresolved high-risk approval is pending.
- No unresolved review blocker remains.
- The PR is not stale relative to the admitted Work Item and branch.
- Project policy permits autonomous merge for this lane and work class.
- The Work Item was admitted with merge-when-green autonomy.

Deployment is out of scope for v1 autonomy. If deployment should follow, the lane creates a governed deploy handoff or review item.

## Data Flow

```text
operator start or reviewed intake
  -> Factory Admission Adapter
  -> Work Item with factory-lane intent
  -> phase Run Attempts and artifacts
  -> optional child delegations
  -> PR handoff
  -> review and check gate
  -> merge
  -> closeout evidence
```

Raw external events never execute directly. They become Intake Items first, then reviewed promotions into Factory Lane unless a future project policy explicitly allows a narrow source/event class to skip review.

## Error Handling

Factory Lane fails closed at phase boundaries.

When scope, policy, worktree lease, executor availability, review state, checks, branch protection, mergeability, or evidence is ambiguous, the Work Item becomes blocked with a specific reason. The blocked state must be visible through existing operator readbacks such as `odin review`, `odin jobs`, and `odin work status`.

Failed implementation or verification attempts use existing run failure evidence and retry policy. Odin must not silently spawn replacement work, switch executor authority, bypass checks, or merge on partial evidence.

Unsupported approval resolvers remain pending and visibly immutable. A resolver gap is a blocked factory Work Item, not a reason to bypass governance.

## Governance

Human approval remains mandatory for:

- High-risk changes.
- Destructive git or filesystem operations.
- Policy or governance changes.
- Production deployment.
- Secrets, credentials, and permission changes.
- Mutations outside the managed project allowlist.
- Any cloud/devbox executor action that would expand authority beyond the admitted Run Attempt.

Autonomous merge is allowed only for Work Items admitted into Factory Lane with merge-when-green policy enabled and all merge gate conditions satisfied.

Cloud/devbox workers are executor route choices. They do not change the policy boundary, project authority, memory scope, or approval requirements.

## Operator Surfaces

The required operator proof path should use existing surfaces first:

- `odin work status`
- `odin jobs`
- `odin runs`
- `odin review`
- `odin approvals`
- overview projections

A future `odin factory ...` surface may provide:

- `odin factory start`
- `odin factory status`
- `odin factory resume`
- `odin factory merge-gate`

Those commands must remain thin adapters over the same Work Item, Run Attempt, approval, review, and PR handoff state.

## Testing And Proof

Implementation should include:

- Registry/profile validation for the Factory Lane.
- Admission tests for operator start and reviewed intake promotion.
- Policy tests for allowed merge, blocked high-risk merge, blocked deploy, blocked destructive work, and blocked unsupported approval resolution.
- Phase orchestration tests proving artifacts and state transitions.
- Delegation tests proving child authority narrows and reconciles.
- PR/check gate tests using fake GitHub/check providers.
- Real Odin proof with a temporary `ODIN_ROOT`.

The real proof should demonstrate:

1. Start or admit factory work.
2. Progress through a fixture managed project.
3. Create phase artifacts and a PR handoff.
4. Simulate green checks.
5. Merge under merge-when-green policy.
6. Read back the result through `odin work status`, `odin jobs`, `odin runs`, and `odin review`.

## Success Criteria

- A managed-project request can move from operator start or reviewed intake to merged PR without manual handoffs between planning, coding, review, testing, PR handling, and merge.
- Every state claim is backed by existing Odin runtime objects and operator readbacks.
- High-risk, destructive, production, policy, and out-of-scope work blocks behind approval instead of proceeding.
- Executor routing can select local or cloud/devbox execution without changing the lane's product model.
- Deployment remains a governed handoff, not v1 autonomous behavior.

## Open Boundary

The v1 design intentionally leaves provider-specific cloud/devbox lifecycle outside the Factory Lane. Cloud execution should enter through executor routing once a provider adapter satisfies Odin's executor contract, capability checks, logging, and artifact requirements.
