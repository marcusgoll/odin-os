---
title: Jobs, Runs, And Work Execution Design
date: 2026-05-10
status: approved-for-implementation-planning
scope: odin-os work execution v1, state contract and proof matrix
---

# Jobs, Runs, And Work Execution Design

## Purpose

Odin tracks work as durable runtime objects rather than relying on agent self-report. This slice hardens the operator-visible truth model for jobs, run attempts, and work execution so future workflow and executor expansion can be proven through runtime state, command output, and durable evidence.

The first implementation slice locks the Work Item / Run Attempt state contract and proof matrix. It does not add a new queue, rename physical tables, widen executor support, or turn registry prompt assets into runtime authority.

## Audit Summary

Inspected:

- `/home/orchestrator/odin-os/AGENTS.md`
- `README.md`
- `CONTEXT.md`
- `WORKFLOW.md`
- `docs/adr/0001-canonical-authority.md`
- `docs/architecture/ADR-0001-brownfield-refactor-strategy.md`
- `docs/architecture/ADR-0002-worker-panic-retry-policy.md`
- `docs/contracts/executor-contract.md`
- `docs/contracts/executor-routing.md`
- `docs/contracts/runtime-events.md`
- `docs/contracts/verification-model.md`
- `docs/contracts/operational-autonomy.md`
- `docs/contracts/follow-through-contract.md`
- `docs/contracts/companion-swarm-orchestration.md`
- `docs/contracts/tui-overview.md`
- `docs/contracts/failure-analysis.md`
- `docs/contracts/pause-resume.md`
- `docs/plans/2026-04-24-managed-project-delivery-workflow-design.md`
- `docs/plans/2026-04-17-odin-routines-follow-through-design.md`
- `registry/workflows/managed-project-delivery-workflow.md`
- `registry/agents/universal-ticket-generator-agent.md`
- `registry/agents/plan-first-execution-agent.md`
- `registry/agents/failed-automation-investigator-agent.md`
- `internal/store/sqlite/migrations/0001_runtime.sql`
- `internal/store/sqlite/migrations/0018_task_queue_fields.sql`
- `internal/store/sqlite/migrations/0029_task_execution_intent.sql`
- `internal/store/sqlite/migrations/0042_task_acceptance_criteria.sql`
- `internal/runtime/jobs/service.go`
- `internal/runtime/runs/service.go`
- `internal/cli/commands/work.go`
- `internal/cli/commands/task.go`
- `internal/executors/contract/types.go`
- `internal/executors/router/router.go`
- `config/executors.yaml`
- integration coverage in `tests/integration/alpha_acceptance_test.go`, `tests/integration/followup_acceptance_test.go`, and `tests/integration/workspace_refactor_acceptance_test.go`
- current installed command surface through `odin help`, `odin work --help`, `odin jobs --help`, `odin runs --help`, `odin task --help`, `odin trigger --help`, `odin companion --help`, and `odin followup --help`

Live command surface found:

```bash
which odin
realpath "$(which odin)"
odin help
odin work --help
odin jobs --help
odin runs --help
```

The installed operator binary resolves through `/home/orchestrator/.local/bin/odin` to `/home/orchestrator/odin-os/releases/current/bin/odin`, and it exposes `work`, `task`, `jobs`, `runs`, `approvals`, `review`, `trigger`, `companion`, `followup`, `serve`, and `e2e`.

Fresh-root probe performed during design:

```bash
tmp="$(mktemp -d)"
ODIN_ROOT="$tmp" odin work status
ODIN_ROOT="$tmp" odin jobs --json
ODIN_ROOT="$tmp" odin runs --json
ODIN_ROOT="$tmp" odin work start --project odin-core --title "Jobs runs execution design audit" --intent read_only
ODIN_ROOT="$tmp" odin work status
ODIN_ROOT="$tmp" odin jobs --json
ODIN_ROOT="$tmp" odin runs --json
ODIN_ROOT="$tmp" odin work dispatch --task 1 --json
ODIN_ROOT="$tmp" odin jobs --json
ODIN_ROOT="$tmp" odin runs --json
ODIN_ROOT="$tmp" odin approvals all --json
```

Observed:

- a fresh runtime had no work items, jobs, runs, or approvals
- `odin work start` created a queued Work Item with explicit `execution_intent=read_only`
- `odin jobs --json` showed the queued Work Item
- `odin runs --json` stayed empty until dispatch
- `odin work dispatch --task 1 --json` created a running Run Attempt with `executor=codex_headless`
- `odin jobs --json` then showed the Work Item as `running` with `current_run_id=1`
- `odin runs --json` showed the Run Attempt as `running`
- no Approval Request was created for the read-only work item

## Existing State

Odin already has the core runtime primitives:

- SQLite is the canonical runtime authority for `tasks`, `runs`, approvals, leases, events, context packets, incidents, recoveries, executor health, and projections.
- Canonical operator language is `Work Item` and `Run Attempt`, while physical storage and some transport surfaces still use `tasks` and `runs`.
- `tasks` persist title, status, scope, current run, queue metadata, retry metadata, blocked reason, acceptance criteria, execution intent, terminal summary, terminal reason, and artifact JSON.
- `runs` persist task linkage, executor, status, attempt number, start and finish timestamps, summary, and artifacts.
- `jobs.Service` owns task creation, queue admission, dispatch, execution, retry, approval blocking, transition checks, executor health checks, worktree lease preparation, run finalization, failure analysis, and recovery recommendation hooks.
- `runs.Service` owns run listing, detail readback, run envelope readback, and direct start/complete/fail helpers.
- `odin work start` creates a queued Work Item.
- `odin work dispatch` creates or blocks a Run Attempt without necessarily executing the worker to completion.
- `odin work execute` executes an already dispatched Run Attempt.
- `odin task run` creates and executes a task in one operator flow.
- `odin jobs` lists Work Item state.
- `odin runs` lists Run Attempt state.
- `odin approvals` and `odin review` show approval and review gates.
- `odin serve` runs bounded queue execution and recovery loops.
- Executor routing uses `config/executors.yaml`, `internal/executors/router`, and `internal/executors/contract`.
- The alpha acceptance suite already proves at least one durable executor lane through `sandcastle_headless`.

Registry and prompt assets exist for delivery, ticket generation, plan-first execution, failed automation investigation, and many agent roles. These are authored catalog assets. They do not become runtime authority until a runtime command or service invokes them through Odin-owned work execution.

## Partial Or Contradictory State

The current system is implemented but unevenly proven:

- `Work Item` and `Run Attempt` are canonical language, but commands and code still expose `jobs`, `tasks`, and `runs`.
- `drafted`, `approved`, `running`, `blocked`, and `done` appear as useful operator concepts, but only some are stored statuses.
- `approved` can mean approval outcome, review decision, or operator readiness unless the source object is named.
- `done` is not a stored Work Item status; terminal status is currently `completed`, `failed`, or `canceled`.
- `work dispatch` can create a `running` Run Attempt before `work execute` finishes it, so dispatch and execution must be distinguished in proof.
- Several workflow and agent assets are prompt/catalog definitions only, not end-to-end runtime workflow types.
- Executor breadth exists as config/catalog shape, but durable end-to-end proof is strongest for specific fixture-backed lanes.
- Compatibility shims and storage-era names remain, but repo policy says to keep them until a removal slice inventories callers and proves migration.

## Reused Components

The implementation should reuse:

- `odin work status|start|dispatch|execute|retry`
- `odin task run`
- `odin jobs --json`
- `odin runs --json`
- `odin approvals all --json`
- `odin review list --json`
- `odin serve` queue execution loop
- `internal/runtime/jobs.Service`
- `internal/runtime/runs.Service`
- `internal/runtime/projections`
- `internal/runtime/approvals.Service`
- `internal/runtime/recovery` failure analysis and retry guidance
- SQLite `tasks`, `runs`, `approvals`, `events`, `context_packets`, and `worktree_leases`
- `internal/executors/contract`
- `internal/executors/router`
- existing fixture-backed executor paths such as `sandcastle_headless`
- existing integration helper patterns that run the repo-owned `./bin/odin` against a controlled `ODIN_ROOT`

## New Components

Add only a small contract and proof seam:

- a documented Work Execution State Matrix in `docs/contracts/` or this spec's implementation-linked section
- characterization tests for the derived state matrix across existing `work`, `jobs`, `runs`, and approvals surfaces
- a focused real-command proof script or integration test if no existing test cleanly covers the matrix

No new queue table, workflow-run table, executor framework, prompt-agent runtime, status enum table, or physical table rename is needed for this slice.

## Why New Components Are Necessary

The runtime already stores Work Items and Run Attempts, but the operator contract is still too implicit. A future workflow type can claim to be drafted, approved, running, blocked, or done while only partially touching runtime state. That is the false-completion risk this slice is meant to close.

The new proof matrix is necessary to make these claims testable:

- a draft is not executable work
- a queued Work Item has no Run Attempt yet
- a blocked Work Item explains the blocker and must not have an active Run Attempt unless a specific workflow contract says otherwise
- a running Work Item has an active Run Attempt visible through `odin runs`
- terminal Work Items expose terminal state and run evidence
- approval and review outcomes are source-specific decisions, not generic Work Item statuses

## Locked Domain Decisions

- Canonical operator object: `Work Item`.
- Physical compatibility table: `tasks`.
- Canonical execution object: `Run Attempt`.
- Physical compatibility table: `runs`.
- Canonical execution entrypoint family: `odin work` and `odin task run`.
- Canonical Work Item readback: `odin jobs` until a future rename or alias is explicitly designed.
- Canonical Run Attempt readback: `odin runs`.
- `drafted` is pre-work state owned by Intake, Review, proposal, or ticket-generation surfaces. It is not a Work Item status.
- `approved` is a source-owned decision or Approval Request outcome. It is not a Work Item status.
- `queued` means a Work Item is eligible or waiting to be eligible for normal dispatch.
- `preparing` means Odin has created a Run Attempt and is preparing dispatch prerequisites such as lease and policy state.
- `running` means a Run Attempt is active or claimed and must be visible through `odin runs`.
- `blocked` means a Work Item cannot dispatch or continue until its owning blocker is resolved. `blocked_reason` is required for operator truth.
- `done` is a derived operator bucket over terminal Work Item statuses, not a stored state.
- Terminal Work Item statuses remain `completed`, `failed`, and `canceled` / `cancelled` compatibility spelling.
- A failed Run Attempt can be retryable or not retryable, but retry eligibility is derived from retry policy, counters, and failure analysis rather than from the stored status alone.
- Registry workflows and agents may describe how to perform work, but they are not runtime authority until Odin invokes them through a Work Item and Run Attempt.
- Compatibility shims and storage-era names may remain until a separate migration slice inventories callers and proves replacement.

No ADR is required for this slice. The design follows existing authority and brownfield ADRs rather than introducing a surprising or hard-to-reverse architectural decision.

## Selected Design

Implement a Work Execution State Matrix and prove it through existing commands and services.

### State Matrix

| Operator concept | Canonical owner | Stored state | Required proof |
| --- | --- | --- | --- |
| `drafted` | Intake / Review / proposal source | source-specific review or draft status | visible in `odin review` or source command; no Work Item required |
| `queued` | Work Execution | `tasks.status = queued` | visible in `odin jobs`; no active `runs` row required |
| `preparing` | Work Execution | `tasks.status = preparing`, `runs.status = preparing` | visible during dispatch preparation when captured by tests |
| `running` | Work Execution | `tasks.status = running`, `runs.status = running`, `tasks.current_run_id = runs.id` | visible in both `odin jobs` and `odin runs` |
| `blocked` | owning workflow or policy gate | `tasks.status = blocked`, non-empty `blocked_reason` | visible in `odin jobs`, `work status`, and relevant queue such as approvals/review |
| `approved` | Approval or source workflow | `approvals.status = approved` or source-specific decision | visible in `odin approvals`, `odin review`, or workflow detail, not as Work Item status |
| `completed` | Work Execution | `tasks.status = completed`, terminal run evidence | visible in `odin jobs` and `odin runs` / `runs show` |
| `failed` | Work Execution / Recovery | `tasks.status = failed`, failed run evidence, optional failure analysis | visible in `odin jobs`, `odin runs`, `odin review` failed-work when retry/follow-up applies |
| `canceled` | Work Execution | `tasks.status = canceled` or compatibility `cancelled` | visible in `odin jobs`; no active run remains |
| `done` | operator projection | derived from terminal statuses | never persisted as primary status |

### Proof Matrix

The implementation should add or extend tests so each proof claim maps to at least one concrete surface:

- `odin work status` counts Work Items, open Work Items, active Run Attempts, pending approvals, failed retryable Work Items, retry-blocked Work Items, explicit intent Work Items, and fallback intent Work Items.
- `odin work start --project <key> --title <text> [--intent ...]` proves queued Work Item creation and intent persistence.
- `odin work dispatch --task <id|key> --json` proves dispatch admission, approval/policy blocking, or Run Attempt creation.
- `odin work execute --task <id|key> --json` proves active Run Attempt execution and terminal state.
- `odin task run --project <key> --title <text> --json` proves the single-command create-and-execute operator path.
- `odin jobs --json` proves Work Item state.
- `odin runs --json` proves Run Attempt state.
- `odin approvals all --json` proves approval side effects when high-risk work blocks.
- `odin review list --json` proves failed-work visibility and draft/review visibility when applicable.

### Scope Of First Implementation Slice

The first slice should focus on state contract and proof. It should not change normal behavior unless a test exposes a clear contradiction with the locked matrix.

Expected implementation work:

1. Add a state-matrix contract section under `docs/contracts/verification-model.md`, `docs/contracts/tui-overview.md`, or a new focused `docs/contracts/work-execution-state.md`.
2. Add characterization tests around `work start`, `work dispatch`, `work execute`, `task run`, `jobs`, `runs`, approval blocking, and failed-work retry visibility.
3. Prefer extending existing integration tests over adding a broad new suite.
4. Use fixture-backed execution so no live external mutation is required.
5. Preserve all existing storage-era command names and compatibility fields.

## Rejected Alternatives

Rejected: rename `tasks` to `work_items` and `runs` to `run_attempts`.

Reason: the canonical language is already locked, but physical rename would be a large compatibility migration and not needed to prove operator truth.

Rejected: add a new `workflow_runs` table.

Reason: workflows should compile down to Work Items, Run Attempts, approvals, context packets, and events. A new workflow-run table would duplicate runtime authority.

Rejected: start with broader executor support.

Reason: executor breadth is useful, but weak state proof would let new executor lanes claim completion without durable operator evidence.

Rejected: treat registry agents as live execution lanes.

Reason: catalog prompt assets are not runtime authority. They may inform task prompts or future routing, but execution is only real when Odin creates Work Items and Run Attempts through the runtime.

Rejected: add a new stored `done` status.

Reason: `done` is an operator projection over terminal outcomes. Storing it would hide the important difference between completed, failed, and canceled.

Rejected: collapse `approved` into Work Item status.

Reason: approval is source-owned governance state. A Work Item can have multiple approvals across its lifecycle, and approval does not itself prove execution finished.

## Test And Verification Plan

Focused local tests:

```bash
go test ./internal/cli/commands ./internal/runtime/jobs ./internal/runtime/runs -run 'Test(Work|Dispatch|Execute|Retry|Run|Approval)' -count=1
go test ./tests/integration -run 'TestAlphaAcceptance|TestCompanionRunAcceptance|TestFollowUpAcceptance' -count=1
```

Real command proof after build:

```bash
make build
which odin
realpath "$(which odin)"
tmp="$(mktemp -d)"
ODIN_ROOT="$tmp" ./bin/odin work status
ODIN_ROOT="$tmp" ./bin/odin jobs --json
ODIN_ROOT="$tmp" ./bin/odin runs --json
ODIN_ROOT="$tmp" ./bin/odin work start --project odin-core --title "Work execution state matrix" --intent read_only
ODIN_ROOT="$tmp" ./bin/odin jobs --json
ODIN_ROOT="$tmp" ./bin/odin runs --json
ODIN_ROOT="$tmp" ./bin/odin work dispatch --task 1 --json
ODIN_ROOT="$tmp" ./bin/odin jobs --json
ODIN_ROOT="$tmp" ./bin/odin runs --json
ODIN_ROOT="$tmp" ./bin/odin approvals all --json
ODIN_ROOT="$tmp" ./bin/odin review list --json
```

Fixture-backed terminal execution proof should use an existing safe lane such as the `sandcastle_headless` integration path or the nearest repo-owned E2E scenario. If a terminal execution proof needs config setup that is too large for this slice, the implementation must state that boundary in `Unproven` and still prove queued, blocked, dispatched, and running states through real commands.

Required proof conditions:

- no-work state is visible and empty
- drafted/reviewable state remains outside Work Item execution until accepted
- queued Work Item appears in `jobs` and not in `runs`
- dispatch either blocks with a reason or creates a visible Run Attempt
- running state is visible in both Work Item and Run Attempt readbacks
- approval-required work creates or reuses an Approval Request and blocks with `blocked_reason=approval_required`
- failed work is visible through failed-work retry policy and review queue when applicable
- terminal work preserves Run Attempt evidence
- no command claims `done` as a stored status

## Documentation Changes

Implementation should update one of:

- `docs/contracts/verification-model.md` with a Work Execution State Proof section, or
- a new `docs/contracts/work-execution-state.md` if the matrix is too large for the verification model.

Implementation may update:

- `docs/contracts/tui-overview.md` if a read-only overview lane needs clearer Work Item / Run Attempt state language.
- `CONTEXT.md` only if a new term or invariant is locked beyond this spec.

No ADR is required.

## Open Blockers

None for implementation planning.

The default `/home/orchestrator/odin-os` checkout is dirty and on a gone branch. Implementation must start from an isolated current-main worktree and must not overwrite unrelated edits in the default checkout.

## Planning Handoff

Implementation should be one PR-sized slice:

1. Add or update the Work Execution State Matrix contract.
2. Add characterization tests for existing state transitions and command output.
3. Extend the nearest existing integration or E2E proof instead of adding a duplicate harness.
4. Preserve existing command names and storage fields.
5. Prove behavior through focused tests and real `./bin/odin` commands.

Do not implement executor breadth, physical table renames, prompt-agent invocation, compatibility-shim removal, or new workflow-run storage in this slice.

## Implementation Goal Prompt

```text
/goal Implement Jobs, Runs, And Work Execution slice 1 in /home/orchestrator/odin-os.

Use the approved design at docs/superpowers/specs/2026-05-10-jobs-runs-work-execution-design.md. Keep the work PR-sized. Make atomic commits that each leave the repo coherent. Reuse existing odin work, odin task run, odin jobs, odin runs, approvals/review surfaces, jobs.Service, runs.Service, SQLite tasks/runs/approvals/events, projections, executor contract, and existing integration helpers. Do not introduce parallel queues, workflow-run tables, executor frameworks, prompt-agent runtime authority, or physical table renames.

Required proof:
- go test ./internal/cli/commands ./internal/runtime/jobs ./internal/runtime/runs -run 'Test(Work|Dispatch|Execute|Retry|Run|Approval)' -count=1
- go test ./tests/integration -run 'TestAlphaAcceptance|TestCompanionRunAcceptance|TestFollowUpAcceptance' -count=1
- make build
- which odin && realpath "$(which odin)"
- ODIN_ROOT="$(mktemp -d)" ./bin/odin work status
- with that ODIN_ROOT, prove queued work through work start + jobs/runs readback, dispatch or blocking through work dispatch, approval readback through approvals/review where applicable, and running or terminal Run Attempt state through runs readback

Delivery:
- open a PR with Summary, Proven, Unproven, Security Review, and Commands Run
- monitor checks
- fix failures in follow-up atomic commits
- merge only if checks pass and repo policy permits
```
