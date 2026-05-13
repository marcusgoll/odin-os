---
title: Skill System Binding Design
date: 2026-05-10
status: approved-for-implementation-planning
scope: odin-os skill system v1, slice 1
---

# Skill System Binding Design

## Purpose

Skills are bounded, reusable operational units that Odin can invoke under policy.

The core skill runtime is already implemented. This design hardens the next
missing boundary: intake and scheduler workflows need a first-class way to
recommend or request a skill invocation without bypassing existing policy,
review, execution, and artifact recording paths.

This slice does not build a new skill runner. It binds existing skill execution
to existing intake, trigger, work item, policy, and review primitives.

## Audit Summary

Inspected:

- `/home/orchestrator/odin-os/AGENTS.md`
- `docs/contracts/skill-lifecycle.md`
- `docs/contracts/runtime-events.md`
- `docs/contracts/tui-overview.md`
- `docs/contracts/registry-format.md`
- `docs/contracts/repo-layout.md`
- `docs/contracts/planning-contract.md`
- `docs/contracts/initiative-and-companion-binding.md`
- `docs/contracts/marcus-social-copilot.md`
- `docs/superpowers/specs/2026-05-10-classification-dedupe-routing-design.md`
- `docs/superpowers/specs/2026-05-10-approval-gated-execution-review-queue-design.md`
- `docs/superpowers/specs/2026-05-10-approval-gated-execution-policy-parity-design.md`
- `internal/skills`
- `internal/app/lifecycle/skills.go`
- `internal/app/lifecycle/review.go`
- `internal/app/lifecycle/review_sources.go`
- `internal/app/lifecycle/run.go`
- `internal/runtime/triggers/service.go`
- `internal/runtime/jobs/service.go`
- `internal/store/sqlite/migrations/0024_intake_items.sql`
- `internal/store/sqlite/migrations/0025_automation_triggers.sql`
- `internal/store/sqlite/migrations/0027_skill_artifacts.sql`
- `internal/store/sqlite/migrations/0028_skill_artifact_review.sql`
- `internal/store/sqlite/migrations/0029_task_execution_intent.sql`
- `registry/skills/*.md`
- `scripts/skills/*.sh`

Verified from a detached clean `origin/main` worktree at
`43738c48c2e3ae9297101c805437567c1234a408`:

```bash
go test ./internal/skills ./internal/app/lifecycle -run 'TestRunSkills|TestInvoke|TestRunUnifiedReviewQueue|TestReviewQueueSources|TestRunReviewActSkillArtifact' -count=1
go build -o ./bin/odin ./cmd/odin
tmp="$(mktemp -d)"
ODIN_ROOT="$tmp" ./bin/odin skills list --json
ODIN_ROOT="$tmp" ./bin/odin skills invoke triage-skill --input '{"message":"hello"}' --json
ODIN_ROOT="$tmp" ./bin/odin skills artifacts --json
ODIN_ROOT="$tmp" ./bin/odin review list --json
rm -rf "$tmp"
```

Observed results:

- `odin skills list --json` loaded registry-backed skill entries.
- `odin skills invoke triage-skill ... --json` executed the command handler
  through `restricted_command_v1`.
- The invocation response included permissions, runtime effect, raw output, and
  a durable review artifact.
- `odin skills artifacts --json` listed a `review_required` skill artifact.
- `odin review list --json` exposed the artifact as `skill-artifact:<id>` with
  `accept`, `reject`, and `archive` actions.

The main checkout was dirty during this audit. The audit and verification used
a temporary detached worktree so unrelated local changes were not touched.

## Existing State

Odin already has a real skill runtime:

- Skill registry source of truth: `registry/skills/*.md`.
- Skill lifecycle service: `internal/skills.Service`.
- Operator command path: `odin skills list|get|create|update|delete|invoke`.
- Command handler contract: `handler_type=command`, `handler_ref` under
  `scripts/skills/`.
- Restricted execution wrapper: minimal environment, repo-root working
  directory, timeout, process-group cancellation, JSON stdin/stdout.
- Permission vocabulary and policy: `internal/skills/policy.go`.
- Lifecycle audit events: `skill.lifecycle_recorded`.
- Result artifact persistence: `skill_artifacts`.
- Result review columns: `review_decision`, `reviewed_at`, `reviewed_by`,
  `review_reason`, `follow_on_task_id`, and `follow_on_task_key`.
- Unified review visibility: `skill_artifact` entries in `odin review`.

The current skill registry has broad inventory but shallow handler coverage.
There are 19 registry skill files, and only two have dedicated handlers:

- `triage-skill` -> `scripts/skills/triage-skill.sh`
- `karpathy-guidelines` -> `scripts/skills/karpathy-guidelines.sh`

The other active skill entries point at `scripts/skills/registry-skill-stub.sh`.

Odin also has relevant workflow primitives:

- Intake processing stores classification, dedupe, routing, draft artifact, and
  review evidence in `intake_items.routing_notes`.
- Intake review can promote reviewable draft artifacts into Work Items through
  existing job admission policy.
- Automation triggers persist schedule or event rules and materialize Work
  Items through `internal/runtime/triggers.Service`.
- Work Items store `execution_intent` and `execution_intent_source`.
- Job admission blocks governance and destructive execution when approval is
  required.

## Current Gap

The missing boundary is workflow binding, not skill execution.

Current intake and trigger paths can create reviewable artifacts or Work Items,
but they do not have a typed `skill_key`, skill input envelope, or skill
invocation source. As a result:

- intake classification cannot produce a reviewable "invoke this skill" route;
- scheduler and event triggers cannot materialize a skill invocation request;
- Work Items cannot carry a durable skill invocation contract;
- skill result review exists only after manual `odin skills invoke`;
- real-world library expansion has no clear pilot path from intake or schedule
  into a concrete skill.

## Reused Components

Implementation should reuse:

- `internal/skills.Service.Invoke`
- `internal/skills.ResolveInvocationPolicy`
- `runRestrictedCommand`
- `skillReviewArtifactRecorder`
- `skill_artifacts`
- `odin skills invoke`
- `odin skills artifacts`
- `odin skills artifact review`
- `odin review list/show/act`
- `intake_items.routing_notes`
- existing intake review handlers
- `internal/runtime/triggers.Service`
- `automation_triggers.rule_json`
- `jobs.Service.CreateTaskOnce`
- job admission policy
- runtime events and existing overview projections

## New Components

Add the smallest set of binding pieces:

- `SkillInvocationBinding` value shape with:
  - `skill_key`
  - `skill_version`, optional advisory snapshot
  - `input_json`
  - `source_type`
  - `source_id` or `source_key`
  - `scope`
  - `project_key`, when project scoped
  - `execution_intent`
  - `execution_intent_source`
  - `review_state`
- intake routing-note fields for a proposed skill invocation;
- trigger rule JSON fields for a proposed skill invocation;
- a Work Item metadata carrier for accepted skill invocation requests;
- one lifecycle command or internal handler that resolves a Work Item skill
  binding and calls `skills.Service.Invoke`;
- tests proving intake and trigger paths create reviewable skill invocation
  requests without directly executing handlers.

If the current `tasks` table cannot safely carry binding metadata without
overloading existing columns, add one narrow durable table such as
`skill_invocation_requests`. The table should reference the source Work Item,
source intake, trigger materialization, skill key, scope, input JSON, status,
and resulting skill artifact. It must not duplicate `skill_artifacts`.

## Why New Components Are Necessary

Skill execution and skill result review already exist. The new binding layer is
necessary because intake, scheduler, and Work Items need durable intent before
execution.

Without a typed binding, implementations would be forced to hide skill
selection in task titles, unstructured JSON notes, per-skill trigger branches,
or direct handler calls. Those would split runtime authority and bypass the
existing skill policy and artifact review contract.

The binding shape makes the chain auditable:

```text
intake or trigger -> reviewed skill invocation request -> Work Item or request record -> skills.Service.Invoke -> skill artifact -> odin review
```

## Locked Domain Decisions

- Canonical executable skill authority: `internal/skills.Service.Invoke`.
- Canonical operator skill command: `odin skills`.
- Canonical skill registry: `registry/skills/*.md`.
- Canonical skill result object: `Skill Artifact`.
- Canonical cross-source review queue: `odin review`.
- A **Skill Invocation Binding** is an execution request, not a new skill
  definition.
- A binding must reference one registry `skill_key`.
- A binding must carry explicit input JSON. The executor must not reconstruct
  skill input from task title alone.
- Intake and triggers may propose or materialize skill invocation requests, but
  they must not directly run handler scripts.
- Skill execution must always pass through `skills.Service.Invoke`, permission
  policy, restricted command execution, and artifact recording.
- Skill result review remains source-owned through `skill_artifact`.
- Stub-backed registry skills are selectable catalog entries, but they are not
  evidence of real-world skill library depth.
- V1 pilot implementation should use a dedicated handler-backed skill,
  preferably `triage-skill`, before expanding library coverage.
- No ADR is required for this slice. The design extends existing skill,
  review, intake, trigger, and job contracts without introducing a hard-to-
  reverse authority change.

## Selected Design

Implement a binding-first slice.

### Intake Binding

Extend intake processing evidence so routing may include:

```json
{
  "skill_invocation": {
    "skill_key": "triage-skill",
    "input_json": {"message": "..."},
    "source_type": "intake",
    "source_key": "intake-1",
    "scope": "project",
    "project_key": "odin-os",
    "execution_intent": "read_only",
    "execution_intent_source": "skill_binding:intake"
  }
}
```

Processing still stops at review. It does not create a Run Attempt or call a
handler.

Review acceptance may promote the binding into a Work Item or a
`skill_invocation_requests` row, depending on the final persistence choice.

### Trigger Binding

Extend schedule and event trigger rule JSON so a trigger may declare a skill
binding:

```json
{
  "cadence": "daily",
  "execution_intent": "read_only",
  "skill_invocation": {
    "skill_key": "triage-skill",
    "input_json": {"message": "Daily triage"},
    "scope": "project",
    "project_key": "odin-os"
  }
}
```

Trigger evaluation still materializes governed work. It does not directly run
the skill handler inside the trigger evaluator.

### Execution Binding

Add one path that resolves an accepted binding and invokes:

```go
skills.Service.Invoke(ctx, skills.InvokeRequest{...})
```

That path must:

- load the registry fresh;
- resolve project scope;
- validate requested `skill_key`;
- apply existing skill invocation policy;
- run through restricted command execution;
- record the existing skill artifact;
- append existing lifecycle events;
- return the artifact ID and status.

### Review UX

Keep result review in the current queue:

- `odin skills artifacts --json`
- `odin skills artifact show <id> --json`
- `odin skills artifact review <accept|reject|archive> <id> --json`
- `odin review list --json`
- `odin review act skill-artifact:<id> <accept|reject|archive> --json`

The implementation may improve labels and summaries, but it must not create a
parallel result review queue.

### Library Coverage

Use `triage-skill` as the first end-to-end binding pilot because it is
handler-backed and read-only. Do not treat the 17 stub-backed registry entries
as real-world coverage.

After the binding slice is proven, expand real-world library coverage in small
follow-up PRs by replacing stub handlers with dedicated handlers and tests.

## Rejected Alternatives

### Invoke skills directly from intake processing

Rejected. Intake processing must remain review-gated and should not create Run
Attempts, dispatch work, or execute handlers by default.

### Invoke skills directly from trigger evaluation

Rejected. Trigger evaluation already materializes governed Work Items. Running
handlers in the trigger evaluator would bypass job admission and make scheduler
execution harder to audit.

### Add per-skill branches to the broker or scheduler

Rejected. `docs/contracts/skill-lifecycle.md` explicitly rejects per-skill
invocation branches. Skill execution must route through the generic service.

### Create a second skill artifact table

Rejected. `skill_artifacts` is already the canonical result review object.
Another result table would duplicate review state and event semantics.

### Promote every registry skill to real handler coverage in this slice

Rejected. The first gap is workflow binding. Bulk handler expansion would
increase review load and make it harder to prove the path end to end.

## Test And Verification Plan

Focused tests:

```bash
go test ./internal/skills -run 'Invoke|Policy|Restricted|Lifecycle' -count=1
go test ./internal/app/lifecycle -run 'Skill|ReviewQueue|Intake|Trigger' -count=1
go test ./internal/runtime/triggers ./internal/runtime/jobs -run 'Skill|Trigger|ExecutionIntent|Admission' -count=1
go test ./internal/store/sqlite -run 'Skill|AutomationTrigger|Intake|Task' -count=1
```

Broader local verification:

```bash
go test ./...
make build
```

Real operator proof after build:

```bash
which odin
realpath "$(which odin)"
tmp="$(mktemp -d)"
ODIN_ROOT="$tmp" ./bin/odin skills list --json
ODIN_ROOT="$tmp" ./bin/odin intake raw create --text "Triage this release-readiness note with the skill system" --json
ODIN_ROOT="$tmp" ./bin/odin intake process --id intake-1 --json
ODIN_ROOT="$tmp" ./bin/odin intake review list --json
ODIN_ROOT="$tmp" ./bin/odin review list --json
ODIN_ROOT="$tmp" ./bin/odin review act <skill-invocation-or-intake-queue-id> accept --json
ODIN_ROOT="$tmp" ./bin/odin jobs list --json
ODIN_ROOT="$tmp" ./bin/odin skills invoke triage-skill --input '{"message":"operator proof"}' --json
ODIN_ROOT="$tmp" ./bin/odin skills artifacts --json
ODIN_ROOT="$tmp" ./bin/odin review list --json
rm -rf "$tmp"
```

The final command set should be adjusted to the exact accepted operator path,
but it must prove:

- registry skills load;
- an intake or trigger can carry a typed skill binding;
- the binding does not execute during intake processing or trigger evaluation;
- review acceptance creates exactly one governed request or Work Item;
- skill invocation uses `skills.Service.Invoke`;
- result recording creates exactly one `skill_artifact`;
- the artifact is visible through unified `odin review`;
- no external send, calendar mutation, purchase, delete, deploy, production
  mutation, permission change, public publish, or sensitive record change occurs.

## Documentation Changes

This spec records the selected design and implementation handoff.

Implementation should update:

- `docs/contracts/skill-lifecycle.md` with the skill invocation binding rule.
- `docs/contracts/tui-overview.md` only if new queue source labels or actions
  are introduced.
- `docs/contracts/runtime-events.md` only if new binding request events are
  added.

Implementation may update `CONTEXT.md` only if it can do so without conflicting
with unrelated dirty worktree changes.

## Security Review

This slice touches skill execution, restricted command invocation, scheduler
materialization, and policy gates. The implementation must preserve:

- no direct handler execution outside `skills.Service.Invoke`;
- no inherited broad environment for command handlers;
- no trigger-side bypass around job admission;
- no intake-side bypass around review;
- no new external mutation;
- approval-needed outcomes for governance or destructive skill permissions;
- deterministic audit events for request, invocation, artifact, and review
  state transitions.

## Open Blockers

No design blockers remain for implementation planning.

The active main checkout may contain unrelated dirty changes. Implementation
should start from an isolated worktree or explicitly coordinate before editing
files that overlap with current local work.

The exact persistence carrier is intentionally left to implementation audit:
prefer existing task artifact or routing-note storage if it can support stable
typed readback; add `skill_invocation_requests` only if existing task fields
would force title parsing or untestable JSON conventions.

## Planning Handoff

Implement one PR-sized binding slice:

1. Add failing characterization tests for skill bindings in intake and triggers.
2. Add typed binding structs and validation.
3. Persist or carry accepted bindings without title parsing.
4. Add one execution path that invokes `skills.Service.Invoke`.
5. Record existing skill artifacts and expose them through existing review
   surfaces.
6. Prove the full path with `triage-skill`.

## Implementation Goal Prompt

```text
/goal Implement skill invocation binding hardening in /home/orchestrator/odin-os.

Use docs/superpowers/specs/2026-05-10-skill-system-binding-design.md as the approved design. Keep the work PR-sized and make atomic commits. Reuse registry/skills, internal/skills.Service.Invoke, existing skill policy, restricted command runner, skill_artifacts, odin skills, odin review, intake routing evidence, trigger rule JSON, jobs admission, and runtime events. Do not add a parallel skill runner, per-skill scheduler branches, a second skill artifact table, direct trigger-side handler execution, or broad external mutation.

Implement a typed SkillInvocationBinding for intake and trigger routes, persist or carry accepted bindings without task-title parsing, and add one execution path that invokes skills.Service.Invoke for the accepted binding. Use triage-skill as the pilot handler-backed skill. Keep processing and trigger evaluation review-gated; they may propose or materialize a skill request, but must not run handler scripts directly.

Required proof:
- go test ./internal/skills -run 'Invoke|Policy|Restricted|Lifecycle' -count=1
- go test ./internal/app/lifecycle -run 'Skill|ReviewQueue|Intake|Trigger' -count=1
- go test ./internal/runtime/triggers ./internal/runtime/jobs -run 'Skill|Trigger|ExecutionIntent|Admission' -count=1
- go test ./internal/store/sqlite -run 'Skill|AutomationTrigger|Intake|Task' -count=1
- go test ./...
- make build
- real ./bin/odin proof with temporary ODIN_ROOT showing skill registry load, intake or trigger skill binding creation, no pre-review execution, review acceptance, skills.Service.Invoke result, skill_artifact persistence, and unified odin review visibility.

Delivery:
- preserve unrelated dirty worktree changes
- open a PR with Summary, Proven, Unproven, and Commands Run
- monitor remote checks
- fix failures in follow-up atomic commits
```
