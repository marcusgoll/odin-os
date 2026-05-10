---
title: Scheduler And Trigger System Design
date: 2026-05-10
status: approved-for-implementation-planning
scope: odin-os trigger proof and event-envelope parity, slice 1
---

# Scheduler And Trigger System Design

## Audit Summary

Inspected:

- `AGENTS.md`
- `CONTEXT.md`
- `docs/contracts/tui-overview.md`
- `docs/contracts/follow-through-contract.md`
- `docs/plans/2026-05-09-odin-os-governed-operating-system.md`
- `config/policies.yaml`
- `config/projects.yaml`
- `internal/cli/commands/trigger.go`
- `internal/runtime/triggers/service.go`
- `internal/runtime/triggers/service_test.go`
- `internal/runtime/events/events.go`
- `internal/store/sqlite/migrations/0025_automation_triggers.sql`
- `internal/store/sqlite/automation_triggers_test.go`
- `internal/store/sqlite/store.go`
- `internal/app/lifecycle/run.go`
- `internal/adapters/calendar/google_driver.go`
- `internal/core/followups`

Command proof used a temporary `ODIN_ROOT` and repo-local binary:

```bash
which odin
odin help
./bin/odin help
ODIN_ROOT="$tmp" ./bin/odin trigger --help
ODIN_ROOT="$tmp" ./bin/odin trigger upsert schedule-run initiative=odin-core kind=schedule status=enabled next=2026-05-02T08:00:00Z title=Schedule_run --json
ODIN_ROOT="$tmp" ./bin/odin scheduler tick now=2026-05-02T08:00:00Z recovery=false --json
ODIN_ROOT="$tmp" ./bin/odin trigger upsert quiet-proof initiative=odin-core kind=schedule status=enabled next=2026-05-02T23:00:00Z title=Quiet_proof quiet=22:00-07:00 --json
ODIN_ROOT="$tmp" ./bin/odin trigger evaluate now=2026-05-02T23:30:00Z --json
ODIN_ROOT="$tmp" ./bin/odin trigger upsert gh-opened initiative=odin-core kind=event event=external.github.issue match_provider=github match_repo=marcusgoll/odin-os title=GH_opened --json
ODIN_ROOT="$tmp" ./bin/odin trigger ingest github-issue project=odin-core repo=marcusgoll/odin-os number=123 action=opened title=Issue_opened labels=bug --json
ODIN_ROOT="$tmp" ./bin/odin trigger evaluate source=events --json
go test ./internal/runtime/triggers ./internal/store/sqlite ./internal/cli/commands ./internal/app/lifecycle -run 'Trigger|Scheduler|AutomationTrigger|Calendar|Quiet|Cron' -count=1
```

Observed:

- The initial design audit found command-surface drift between installed `/home/orchestrator/.local/bin/odin` and repo-local `./bin/odin`; final implementation proof must re-check both paths before making claims about promotion.
- Repo-local `./bin/odin` exposes `scheduler`.
- `./bin/odin scheduler tick now=2026-05-02T08:00:00Z recovery=false --json` evaluated one due schedule trigger and materialized one queued Work Item.
- Quiet hours deferred a due trigger from `2026-05-02T23:00:00Z` to `2026-05-03T07:00:00Z`.
- Event trigger evaluation materialized a Work Item when the event type was `external.github.issue`.
- Event trigger evaluation matched nothing when the trigger was configured with `external.github_issue`, showing event-envelope drift risk.

## Existing State

Odin already has a real scheduler and trigger substrate:

- `Automation Trigger` is the canonical domain term for schedule-based and event-based rules that create or update governed Work Items.
- `Follow-Up Obligation` is the v1 schedule-backed Automation Trigger surface for promised next actions, reminders, recurring check-ins, and recurring obligations.
- `automation_triggers` and `automation_trigger_materializations` are durable SQLite authorities.
- `runtime/triggers.Service` owns trigger creation, schedule evaluation, event evaluation, GitHub issue ingest, cron validation, cadence validation, quiet-hours deferral, and materialization.
- `odin trigger` exposes `list`, `show`, `upsert`, `fire`, `evaluate`, and `ingest github-issue`.
- Repo-local `scheduler tick` composes due trigger evaluation, supervision tick, and optional recovery cycle.
- Trigger materialization creates queued Work Items with `work_kind=automation_trigger` and `requested_by=automation_trigger:<key>`.
- Trigger events are written to the runtime event stream for created, fire requested, evaluated, materialized, deferred, errored, and status changed.

## Reused Components

Implementation should reuse:

- `internal/runtime/triggers.Service`
- `internal/store/sqlite` automation trigger methods
- `automation_triggers` and `automation_trigger_materializations`
- runtime event type constants and payload structs in `internal/runtime/events`
- `odin trigger` command parser and JSON renderers
- repo-local `scheduler tick` as the runtime proof path
- current `quiet_hours`, `cron`, `cadence`, and event-match rule fields in `rule_json`
- existing trigger and scheduler tests

## New Components

Add only small parity and proof components:

- A canonical event-envelope helper in `internal/runtime/triggers` for external GitHub issue event type naming and trigger-match fields.
- A safe trigger proof surface, preferably `odin trigger test <key> ... --json`, that evaluates whether a trigger would fire, defer, or match without creating a Work Item or materialization.
- Tests proving the canonical event type for GitHub issue ingest is `external.github.issue` and that help/examples, ingest output, and event matching use the same value.
- Tests proving trigger proof does not create tasks, materializations, approvals, runs, external adapter mutations, or dispatch. On current `main`, `trigger test` records an `automation_trigger.tested` audit event while still reporting `mutates=false`.
- Documentation updates for trigger operator examples if the implementation changes help text or contract wording.

No new scheduler engine, queue table, adapter runtime, calendar connector, batching service, energy model, or direct worker-launch path is needed in this slice.

## Why New Components Are Necessary

The scheduler is not decorative, but the operator cannot safely inspect trigger behavior without mutating state. `odin trigger evaluate` and `scheduler tick` are useful proof paths because they create real work when due. They are not safe as the only diagnostic surface.

The event-envelope mismatch is a concrete defect class: a trigger configured with the wrong event type silently evaluates zero matching events. A typed helper and dry-run proof surface make this visible before an operator trusts the trigger.

## Locked Domain Decisions

- Canonical object: `Automation Trigger`.
- `scheduler tick` is the runtime tick proof path, not the CRUD surface for triggers.
- `odin trigger` remains the operator surface for trigger create, list, show, fire, evaluate, and proof.
- Triggers must materialize governed Work Items before any worker dispatch.
- Event hooks and cron schedules must not launch execution directly.
- The canonical GitHub issue external event type is `external.github.issue`.
- Event trigger matching must compare against typed runtime event constants or helper output, not copy-pasted string variants.
- Trigger proof must be read-only with respect to work execution: no Work Item, materialization, approval, run, external adapter mutation, or dispatch. An `automation_trigger.tested` audit event is allowed because it is the established operator audit trail on current `main`.
- Quiet-hours deferral remains narrow in v1: UTC-only `HH:MM-HH:MM`.
- Calendar-aware scheduling, energy-aware scheduling, batching, and external adapter parity are out of scope for this first implementation slice.

No ADR is needed. The selected design aligns existing domain language and runtime behavior rather than introducing a surprising or hard-to-reverse architecture.

## Selected Design

Implement a PR-sized trigger hardening slice.

First, centralize external event envelope naming. GitHub issue ingest and event trigger upsert examples should share the same canonical event type string, `external.github.issue`, from the runtime event constant or a small helper. The code should not leave a separate underscore variant in command examples or tests unless it is intentionally treated as a compatibility alias with explicit proof.

Second, add a read-only trigger proof command:

```bash
odin trigger test <key> [now=<RFC3339>] [source=events] [event_type=<type>] [--json]
```

For schedule triggers, the command should report whether the trigger is ready, waiting, deferred by quiet hours, errored by rule validation, or would materialize if evaluated. It must not update `last_evaluated_at`, `next_eligible_at`, materialization rows, or tasks. It may record the established `automation_trigger.tested` audit event.

For event triggers, the command should report candidate event matches and why an event did or did not match. It must use the same event type vocabulary as `trigger ingest github-issue`.

Third, keep `scheduler tick` as the real mutation proof. After `make build`, the final proof should still show `scheduler tick` can evaluate and materialize work through the existing runtime path. Promotion to the installed `odin` binary must be explicitly proven whenever the installed command path differs from repo-local `./bin/odin`.

## Rejected Alternatives

Rejected: build a new scheduler subsystem.

Reason: existing trigger storage, trigger service, event stream, and scheduler tick path already work.

Rejected: put create/show/test under `odin scheduler`.

Reason: the domain object is `Automation Trigger`, and `odin trigger` is already the operator CRUD surface. `scheduler tick` should remain the runtime loop/proof path.

Rejected: make `trigger evaluate` dry-run by default.

Reason: existing behavior is a real materialization path. Changing its semantics would break current operator proof and tests.

Rejected: solve calendar-aware scheduling and batching in this slice.

Reason: the current proven defect is trigger proof and event-envelope parity. Calendar and batching require separate adapter and policy decisions.

## Test And Verification Plan

Focused tests:

```bash
go test ./internal/runtime/triggers -run 'Trigger|Event|Envelope|Proof|Quiet|Cron' -count=1
go test ./internal/cli/commands -run 'Trigger' -count=1
go test ./internal/app/lifecycle -run 'Scheduler|Trigger' -count=1
```

Store and event regression tests:

```bash
go test ./internal/store/sqlite -run 'AutomationTrigger|ExternalIssue|Event' -count=1
```

Broader verification:

```bash
go test ./internal/runtime/triggers ./internal/store/sqlite ./internal/cli/commands ./internal/app/lifecycle -run 'Trigger|Scheduler|AutomationTrigger|Calendar|Quiet|Cron|Event' -count=1
make build
```

Real operator proof after build:

```bash
which odin
realpath "$(which odin)"
tmp="$(mktemp -d)"
ODIN_ROOT="$tmp" ./bin/odin help
ODIN_ROOT="$tmp" ./bin/odin trigger upsert schedule-run initiative=odin-core kind=schedule status=enabled next=2026-05-02T08:00:00Z title=Schedule_run --json
ODIN_ROOT="$tmp" ./bin/odin trigger test schedule-run now=2026-05-02T08:00:00Z --json
ODIN_ROOT="$tmp" ./bin/odin trigger list --json
ODIN_ROOT="$tmp" ./bin/odin scheduler tick now=2026-05-02T08:00:00Z recovery=false --json
ODIN_ROOT="$tmp" ./bin/odin trigger upsert quiet-proof initiative=odin-core kind=schedule status=enabled next=2026-05-02T23:00:00Z title=Quiet_proof quiet=22:00-07:00 --json
ODIN_ROOT="$tmp" ./bin/odin trigger test quiet-proof now=2026-05-02T23:30:00Z --json
ODIN_ROOT="$tmp" ./bin/odin trigger upsert gh-opened initiative=odin-core kind=event event=external.github.issue match_provider=github match_repo=marcusgoll/odin-os title=GH_opened --json
ODIN_ROOT="$tmp" ./bin/odin trigger ingest github-issue project=odin-core repo=marcusgoll/odin-os number=123 action=opened title=Issue_opened labels=bug --json
ODIN_ROOT="$tmp" ./bin/odin trigger test gh-opened source=events --json
ODIN_ROOT="$tmp" ./bin/odin trigger evaluate source=events --json
ODIN_ROOT="$tmp" ./bin/odin logs --json
rm -rf "$tmp"
```

Required proof conditions:

- `trigger test` emits valid JSON.
- `trigger test` reports matching or deferral reasons without creating tasks or materializations.
- `trigger evaluate` and `scheduler tick` still materialize governed Work Items when intentionally invoked.
- GitHub issue ingest, help examples, and event trigger matching agree on `external.github.issue`.
- Installed `odin` command-surface drift is reported unless `scheduler` is promoted and proven through `odin help`.

## Documentation Changes

Implementation should update:

- `internal/cli/commands/trigger.go` help examples if the event type is made explicit there.
- `docs/contracts/follow-through-contract.md` only if trigger proof changes the operator contract.
- `CONTEXT.md` only if new domain language is introduced.

No `CONTEXT.md` update is required for this design because `Automation Trigger`, `Follow-Up Obligation`, and the no-direct-execution invariant are already locked.

## Open Blockers

None for implementation planning.

Implementation must preserve unrelated dirty worktree changes. At design time, the worktree already had dirty changes in `CONTEXT.md`, `internal/app/lifecycle/run.go`, `internal/app/lifecycle/run_test.go`, `internal/cli/repl/shell.go`, and `internal/cli/repl/shell_test.go`.

## Planning Handoff

Implement one small trigger hardening PR:

- keep the scheduler runtime path intact.
- add read-only trigger proof.
- align GitHub issue event envelope naming.
- avoid external adapter mutation.
- avoid calendar-aware, energy-aware, and batching expansion.
- prove both dry-run and real materialization behavior with temporary `ODIN_ROOT`.

## Implementation Goal Prompt

```text
/goal Implement trigger proof and event-envelope parity in /home/orchestrator/odin-os.

Use the approved design at docs/superpowers/specs/2026-05-10-scheduler-trigger-system-design.md. Keep the work PR-sized. Make atomic commits that each leave the repo coherent. Reuse internal/runtime/triggers.Service, internal/store/sqlite automation trigger authority, runtime event constants, odin trigger commands, existing trigger tests, and repo-local scheduler tick. Do not introduce a parallel scheduler, queue table, adapter runtime, calendar-aware planner, batching model, or direct worker-launch path.

Required proof:
- go test ./internal/runtime/triggers -run 'Trigger|Event|Envelope|Proof|Quiet|Cron' -count=1
- go test ./internal/cli/commands -run 'Trigger' -count=1
- go test ./internal/app/lifecycle -run 'Scheduler|Trigger' -count=1
- go test ./internal/store/sqlite -run 'AutomationTrigger|ExternalIssue|Event' -count=1
- go test ./internal/runtime/triggers ./internal/store/sqlite ./internal/cli/commands ./internal/app/lifecycle -run 'Trigger|Scheduler|AutomationTrigger|Calendar|Quiet|Cron|Event' -count=1
- make build
- tmp="$(mktemp -d)"; ODIN_ROOT="$tmp" ./bin/odin trigger upsert schedule-run initiative=odin-core kind=schedule status=enabled next=2026-05-02T08:00:00Z title=Schedule_run --json; ODIN_ROOT="$tmp" ./bin/odin trigger test schedule-run now=2026-05-02T08:00:00Z --json; ODIN_ROOT="$tmp" ./bin/odin scheduler tick now=2026-05-02T08:00:00Z recovery=false --json; ODIN_ROOT="$tmp" ./bin/odin trigger upsert gh-opened initiative=odin-core kind=event event=external.github.issue match_provider=github match_repo=marcusgoll/odin-os title=GH_opened --json; ODIN_ROOT="$tmp" ./bin/odin trigger ingest github-issue project=odin-core repo=marcusgoll/odin-os number=123 action=opened title=Issue_opened labels=bug --json; ODIN_ROOT="$tmp" ./bin/odin trigger test gh-opened source=events --json; ODIN_ROOT="$tmp" ./bin/odin trigger evaluate source=events --json; rm -rf "$tmp"

Delivery:
- preserve unrelated dirty worktree changes
- open a PR with Summary, Proven, Unproven, and Commands Run
- monitor checks
- fix failures in follow-up atomic commits
- merge only if checks pass and repo policy permits
```
