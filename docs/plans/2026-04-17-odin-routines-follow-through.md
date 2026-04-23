# Odin Routines And Follow-Through Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add first-class routine and follow-through support so Odin can manage Marcus's recurring life obligations through explicit initiatives, companions, operating-profile defaults, and due work-item materialization.

**Architecture:** Extend the existing workspace operating model instead of creating a second automation stack. Keep `initiative`, `companion`, `work item`, and `run attempt` as the primary runtime nouns, add `OperatingProfile` and `FollowUpObligation` as new control-plane objects, and materialize due obligations into the existing governed work-item execution path. Root CLI commands become the authoritative lifecycle surface; REPL and serve reuse those services.

**Tech Stack:** Go, SQLite migrations and repositories, standard-library CLI, existing `internal/app/lifecycle/run.go` command dispatch and serve loops, existing projections/read models, existing work-item execution and runtime events

---

### Task 1: Freeze the routines and follow-through contracts

**Files:**
- Create: `docs/contracts/follow-through-contract.md`
- Modify: `docs/contracts/odin-operating-model.md`
- Modify: `docs/contracts/harness-cli.md`
- Modify: `README.md`
- Test: `docs/plans/2026-04-17-odin-routines-follow-through-design.md`

**Step 1: Write the contract doc**

Document:
- `OperatingProfile`
- `FollowUpObligation`
- obligation-to-work-item materialization
- bounded proactive behavior
- command-surface rules

**Step 2: Update the operating-model contract**

Add explicit references to:
- operating profile ownership under workspace
- follow-up obligations as durable control-plane objects
- agenda and due-obligation visibility

**Step 3: Update CLI and repo overview docs**

Add the intended root commands:
- `odin initiative`
- `odin companion`
- `odin profile`
- `odin followup`
- `odin agenda`

**Step 4: Run grep verification**

Run: `rg -n "follow-up|followup|agenda|profile|routine" README.md docs/contracts`

Expected: new terminology appears in the contract set and does not contradict the existing workspace operating model.

**Step 5: Commit**

```bash
git add README.md docs/contracts/odin-operating-model.md docs/contracts/harness-cli.md docs/contracts/follow-through-contract.md docs/plans/2026-04-17-odin-routines-follow-through-design.md
git commit -m "docs: define Odin routines and follow-through model"
```

### Task 2: Add explicit initiative lifecycle commands for non-project initiatives

**Files:**
- Modify: `internal/core/initiatives/service.go`
- Modify: `internal/core/initiatives/service_test.go`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/store/sqlite/workspace_test.go`
- Modify: `internal/app/lifecycle/run.go`
- Create: `internal/cli/commands/initiative.go`
- Create: `internal/cli/commands/initiative_test.go`
- Test: `tests/integration/workspace_refactor_acceptance_test.go`

**Step 1: Write the failing command tests**

Add tests for:
- `odin initiative create --kind routine --key life-admin --title "Life Admin"`
- `odin initiative list --json`
- rejecting unsupported initiative kinds

**Step 2: Run the initiative command tests to verify they fail**

Run: `go test ./internal/cli/commands -run 'Test(Initiative|ParseInitiative)' -v`

Expected: FAIL because no initiative command exists yet.

**Step 3: Extend the initiative service**

Add service methods for:
- create/upsert non-project initiatives
- list initiatives for a workspace
- archive or pause an initiative if needed by the command surface

Do not create a parallel initiative repository; extend the existing initiative service and store methods.

**Step 4: Wire root command dispatch**

Add a new root command family in `internal/app/lifecycle/run.go` and render JSON/text responses through `internal/cli/commands`.

**Step 5: Run focused tests**

Run: `go test ./internal/core/initiatives ./internal/cli/commands ./internal/store/sqlite -run 'Test(Initiative|Workspace)' -v`

Expected: PASS

**Step 6: Run a real Odin check**

Run:

```bash
tmp=$(mktemp -d)
ODIN_ROOT="$tmp" ./bin/odin initiative create --kind routine --key life-admin --title "Life Admin"
ODIN_ROOT="$tmp" ./bin/odin initiative list --json
```

Expected:
- create command succeeds
- list output contains `life-admin` with `kind=routine`

**Step 7: Commit**

```bash
git add internal/core/initiatives/service.go internal/core/initiatives/service_test.go internal/store/sqlite/models.go internal/store/sqlite/store.go internal/store/sqlite/workspace_test.go internal/app/lifecycle/run.go internal/cli/commands/initiative.go internal/cli/commands/initiative_test.go tests/integration/workspace_refactor_acceptance_test.go
git commit -m "feat(cli): add initiative lifecycle commands"
```

### Task 3: Add explicit companion lifecycle commands

**Files:**
- Modify: `internal/core/companions/service.go`
- Modify: `internal/core/companions/service_test.go`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/store/sqlite/companions_test.go`
- Modify: `internal/app/lifecycle/run.go`
- Create: `internal/cli/commands/companion.go`
- Create: `internal/cli/commands/companion_test.go`
- Test: `tests/integration/workspace_refactor_acceptance_test.go`

**Step 1: Write the failing command tests**

Add tests for:
- `odin companion create --kind advisor --key finance --title "Finance Advisor"`
- `odin companion list --json`
- invalid companion kind rejection

**Step 2: Run the companion command tests to verify they fail**

Run: `go test ./internal/cli/commands -run 'Test(Companion|ParseCompanion)' -v`

Expected: FAIL because no companion root command exists yet.

**Step 3: Extend the companion service**

Add explicit list and create/update operations that preserve the existing contract:
- durable companion state
- JSON policy field validation
- no provider-specific leakage

**Step 4: Wire root command dispatch**

Add a root `companion` command family in `internal/app/lifecycle/run.go`.

**Step 5: Run focused tests**

Run: `go test ./internal/core/companions ./internal/cli/commands ./internal/store/sqlite -run 'Test(Companion|ParseCompanion)' -v`

Expected: PASS

**Step 6: Run a real Odin check**

Run:

```bash
tmp=$(mktemp -d)
ODIN_ROOT="$tmp" ./bin/odin companion create --kind advisor --key finance --title "Finance Advisor"
ODIN_ROOT="$tmp" ./bin/odin companion list --json
```

Expected:
- create command succeeds
- list output contains `finance` with `kind=advisor`

**Step 7: Commit**

```bash
git add internal/core/companions/service.go internal/core/companions/service_test.go internal/store/sqlite/models.go internal/store/sqlite/store.go internal/store/sqlite/companions_test.go internal/app/lifecycle/run.go internal/cli/commands/companion.go internal/cli/commands/companion_test.go tests/integration/workspace_refactor_acceptance_test.go
git commit -m "feat(cli): add companion lifecycle commands"
```

### Task 4: Add the workspace operating profile

**Files:**
- Create: `internal/core/profile/types.go`
- Create: `internal/core/profile/service.go`
- Create: `internal/core/profile/service_test.go`
- Create: `internal/store/sqlite/migrations/0017_workspace_profile.sql`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/store/sqlite/migrations_test.go`
- Modify: `internal/memory/users/service.go`
- Modify: `internal/memory/users/service_test.go`
- Modify: `internal/app/lifecycle/run.go`
- Create: `internal/cli/commands/profile.go`
- Create: `internal/cli/commands/profile_test.go`

**Step 1: Write the failing profile service tests**

Add tests for:
- bootstrapping a default workspace profile
- updating quiet hours and approval defaults
- reading profile state back through the service

**Step 2: Run the profile tests to verify they fail**

Run: `go test ./internal/core/profile ./internal/store/sqlite -run 'TestProfile' -v`

Expected: FAIL because profile persistence does not exist yet.

**Step 3: Add the SQLite schema and service**

Create a minimal `workspace_profile` table with:
- `workspace_id`
- `preferences_json`
- `boundaries_json`
- `cadence_defaults_json`
- `created_at`
- `updated_at`

**Step 4: Bridge the existing user memory**

Adjust `internal/memory/users/service.go` so new writes and reads can move toward workspace-owned operator context rather than the raw `global/global` special case.

Keep backward compatibility; do not break old reads.

**Step 5: Add root profile commands**

Support:
- `odin profile show`
- `odin profile set --quiet-hours ...`
- additional key/value updates only where tests require them

**Step 6: Run focused tests**

Run: `go test ./internal/core/profile ./internal/memory/users ./internal/cli/commands ./internal/store/sqlite -run 'Test(Profile|Users)' -v`

Expected: PASS

**Step 7: Run a real Odin check**

Run:

```bash
tmp=$(mktemp -d)
ODIN_ROOT="$tmp" ./bin/odin profile show
ODIN_ROOT="$tmp" ./bin/odin profile set --quiet-hours 22:00-07:00
ODIN_ROOT="$tmp" ./bin/odin profile show
```

Expected:
- first show renders default profile state
- set succeeds
- second show includes `22:00-07:00`

**Step 8: Commit**

```bash
git add internal/core/profile internal/store/sqlite/migrations/0017_workspace_profile.sql internal/store/sqlite/models.go internal/store/sqlite/store.go internal/store/sqlite/migrations_test.go internal/memory/users/service.go internal/memory/users/service_test.go internal/app/lifecycle/run.go internal/cli/commands/profile.go internal/cli/commands/profile_test.go
git commit -m "feat(workspace): add operating profile"
```

### Task 5: Add the follow-up obligation domain and persistence

**Files:**
- Create: `internal/core/followups/types.go`
- Create: `internal/core/followups/service.go`
- Create: `internal/core/followups/service_test.go`
- Create: `internal/store/sqlite/migrations/0018_follow_up_obligations.sql`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/store/sqlite/migrations_test.go`
- Modify: `internal/core/workitems/service.go`
- Test: `internal/core/workitems/service_test.go`

**Step 1: Write the failing follow-up service tests**

Add tests for:
- creating a one-time obligation
- creating a recurring obligation
- evaluating due status from cadence and timestamps
- preventing duplicate materialization in the same due window

**Step 2: Run the follow-up tests to verify they fail**

Run: `go test ./internal/core/followups -run 'TestFollowUp' -v`

Expected: FAIL because the follow-up domain does not exist yet.

**Step 3: Add the follow-up schema**

Add a `follow_up_obligations` table with:
- workspace, initiative, and optional companion ownership
- title and status
- cadence JSON
- next due timestamp
- last materialized timestamp
- last completed timestamp
- policy JSON

**Step 4: Keep work-item ownership unchanged**

Do not add a second execution system. Extend work-item creation only where needed to track follow-up provenance or work kind.

**Step 5: Run focused tests**

Run: `go test ./internal/core/followups ./internal/core/workitems ./internal/store/sqlite -run 'Test(FollowUp|WorkItem)' -v`

Expected: PASS

**Step 6: Commit**

```bash
git add internal/core/followups internal/store/sqlite/migrations/0018_follow_up_obligations.sql internal/store/sqlite/models.go internal/store/sqlite/store.go internal/store/sqlite/migrations_test.go internal/core/workitems/service.go internal/core/workitems/service_test.go
git commit -m "feat(core): add follow-up obligations"
```

### Task 6: Add follow-up lifecycle commands

**Files:**
- Create: `internal/cli/commands/followup.go`
- Create: `internal/cli/commands/followup_test.go`
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/core/followups/service.go`
- Modify: `internal/core/followups/service_test.go`
- Test: `tests/integration/workspace_refactor_acceptance_test.go`

**Step 1: Write the failing command tests**

Add tests for:
- `odin followup add --initiative life-admin --title "Review mail" --cadence weekly`
- `odin followup list --json`
- `odin followup complete <id>`
- `odin followup snooze <id> --until <timestamp>`

**Step 2: Run the follow-up command tests to verify they fail**

Run: `go test ./internal/cli/commands -run 'Test(FollowUp|ParseFollowUp)' -v`

Expected: FAIL because no root follow-up commands exist yet.

**Step 3: Implement the command surface**

Use the follow-up service directly. Keep the initial flag surface explicit and narrow.

**Step 4: Run focused tests**

Run: `go test ./internal/cli/commands ./internal/core/followups -run 'Test(FollowUp|ParseFollowUp)' -v`

Expected: PASS

**Step 5: Run a real Odin check**

Run:

```bash
tmp=$(mktemp -d)
ODIN_ROOT="$tmp" ./bin/odin initiative create --kind routine --key life-admin --title "Life Admin"
ODIN_ROOT="$tmp" ./bin/odin followup add --initiative life-admin --title "Review mail" --cadence weekly
ODIN_ROOT="$tmp" ./bin/odin followup list --json
```

Expected:
- add succeeds
- list shows one active obligation tied to `life-admin`

**Step 6: Commit**

```bash
git add internal/cli/commands/followup.go internal/cli/commands/followup_test.go internal/app/lifecycle/run.go internal/core/followups/service.go internal/core/followups/service_test.go tests/integration/workspace_refactor_acceptance_test.go
git commit -m "feat(cli): add follow-up lifecycle commands"
```

### Task 7: Add agenda and follow-up read models

**Files:**
- Modify: `internal/runtime/projections/projections.go`
- Create: `internal/runtime/projections/followups_test.go`
- Modify: `internal/api/http/operational.go`
- Modify: `internal/api/http/operational_test.go`
- Modify: `internal/cli/repl/shell.go`
- Modify: `internal/cli/repl/shell_test.go`
- Create: `internal/cli/commands/agenda.go`
- Create: `internal/cli/commands/agenda_test.go`

**Step 1: Write the failing projection tests**

Add tests for:
- listing due follow-ups
- listing overdue follow-ups
- rendering an agenda view with due work, blocked work, and approvals

**Step 2: Run the projection tests to verify they fail**

Run: `go test ./internal/runtime/projections ./internal/api/http ./internal/cli/repl -run 'Test(FollowUp|Agenda|Operational)' -v`

Expected: FAIL because those views do not exist yet.

**Step 3: Extend projections and surfaces**

Add:
- follow-up summary views
- agenda read model
- `/agenda` operational endpoint if the JSON surface needs it
- `odin agenda`
- optional REPL wrappers such as `/agenda`

Do not duplicate query logic across CLI, REPL, and HTTP.

**Step 4: Run focused tests**

Run: `go test ./internal/runtime/projections ./internal/api/http ./internal/cli/commands ./internal/cli/repl -run 'Test(FollowUp|Agenda|Operational)' -v`

Expected: PASS

**Step 5: Run real Odin checks**

Run:

```bash
tmp=$(mktemp -d)
ODIN_ROOT="$tmp" ./bin/odin agenda
ODIN_ROOT="$tmp" ./bin/odin serve
```

Use a scripted test harness or timeout wrapper so `serve` starts, emits surfaces, and exits cleanly for the test scenario.

**Step 6: Commit**

```bash
git add internal/runtime/projections/projections.go internal/runtime/projections/followups_test.go internal/api/http/operational.go internal/api/http/operational_test.go internal/cli/repl/shell.go internal/cli/repl/shell_test.go internal/cli/commands/agenda.go internal/cli/commands/agenda_test.go
git commit -m "feat(ops): add agenda and follow-up read models"
```

### Task 8: Materialize due obligations in `odin serve`

**Files:**
- Modify: `internal/app/lifecycle/run.go`
- Create: `internal/app/lifecycle/followup_loop_test.go`
- Modify: `internal/core/followups/service.go`
- Modify: `internal/core/followups/service_test.go`
- Modify: `internal/runtime/events/events.go`
- Modify: `internal/runtime/projections/projections.go`
- Test: `tests/integration/workspace_refactor_acceptance_test.go`

**Step 1: Write the failing serve-loop tests**

Add tests for:
- due obligations materialize exactly one work item
- archived initiatives pause linked obligations
- blocked obligations surface without dispatching side effects

**Step 2: Run the serve-loop tests to verify they fail**

Run: `go test ./internal/app/lifecycle ./internal/core/followups -run 'Test(FollowUpLoop|Serve)' -v`

Expected: FAIL because no follow-up loop exists yet.

**Step 3: Add the bounded follow-up loop**

Implement a new background loop alongside the existing task and self-heal loops:
- evaluate due obligations
- materialize work items through the existing work-item service
- emit stable runtime events
- do not execute external side effects directly

**Step 4: Run focused tests**

Run: `go test ./internal/app/lifecycle ./internal/core/followups ./internal/runtime/projections -run 'Test(FollowUpLoop|Serve)' -v`

Expected: PASS

**Step 5: Run real Odin checks**

Run:

```bash
tmp=$(mktemp -d)
ODIN_ROOT="$tmp" ./bin/odin initiative create --kind routine --key life-admin --title "Life Admin"
ODIN_ROOT="$tmp" ./bin/odin followup add --initiative life-admin --title "Review mail" --cadence daily
ODIN_ROOT="$tmp" ./bin/odin serve
ODIN_ROOT="$tmp" ./bin/odin jobs --json
```

Expected:
- `serve` materializes one queued or blocked work item for the due obligation
- `jobs --json` shows exactly one work item for that due window

**Step 6: Commit**

```bash
git add internal/app/lifecycle/run.go internal/app/lifecycle/followup_loop_test.go internal/core/followups/service.go internal/core/followups/service_test.go internal/runtime/events/events.go internal/runtime/projections/projections.go tests/integration/workspace_refactor_acceptance_test.go
git commit -m "feat(runtime): materialize due follow-ups in serve"
```

### Task 9: Refine memory and overdue visibility

**Files:**
- Modify: `internal/memory/workspaces/service.go`
- Modify: `internal/memory/companions/service.go`
- Modify: `internal/memory/users/service.go`
- Modify: `internal/runtime/projections/projections.go`
- Modify: `internal/runtime/projections/operator_test.go`
- Test: `internal/memory/workspaces/service_test.go`
- Test: `internal/memory/companions/service_test.go`

**Step 1: Write the failing memory tests**

Add tests for:
- storing operating-profile preference updates with workspace ownership
- storing completion history for follow-up obligations
- keeping companion and initiative memory boundaries intact

**Step 2: Run the memory tests to verify they fail**

Run: `go test ./internal/memory/workspaces ./internal/memory/companions ./internal/memory/users -run 'Test(Memory|Profile|FollowUp)' -v`

Expected: FAIL because the new ownership and follow-up records are not wired yet.

**Step 3: Add minimal memory support**

Record:
- operating-profile changes as workspace-owned durable memory
- follow-up completions and overdue states as initiative or companion-scoped memory where appropriate

Do not create a parallel memory subsystem.

**Step 4: Run focused tests**

Run: `go test ./internal/memory/workspaces ./internal/memory/companions ./internal/memory/users ./internal/runtime/projections -run 'Test(Memory|Profile|FollowUp)' -v`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/memory/workspaces/service.go internal/memory/workspaces/service_test.go internal/memory/companions/service.go internal/memory/companions/service_test.go internal/memory/users/service.go internal/memory/users/service_test.go internal/runtime/projections/projections.go internal/runtime/projections/operator_test.go
git commit -m "feat(memory): record follow-up and profile state"
```

### Task 10: Verify the end-to-end Marcus workflow

**Files:**
- Modify: `tests/integration/workspace_refactor_acceptance_test.go`
- Create: `tests/integration/followup_acceptance_test.go`
- Modify: `docs/operations/workspace-bootstrap.md`
- Create: `docs/operations/followup-routines.md`

**Step 1: Write the failing integration test**

Cover:
- bootstrap default workspace
- create routine initiative
- create advisor companion
- update operating profile
- create recurring follow-up
- materialize due work via `odin serve`
- inspect agenda and jobs

**Step 2: Run the integration test to verify it fails**

Run: `go test ./tests/integration -run 'Test(FollowupAcceptance|WorkspaceRefactorAcceptance)' -v`

Expected: FAIL because the full flow is not yet wired.

**Step 3: Implement only the missing glue**

Use the already-built services and commands. Avoid introducing new orchestration layers during the test fix-up pass.

**Step 4: Run full verification**

Run:

```bash
go test ./...
make build
make test
```

Expected: PASS

**Step 5: Run real Odin command verification**

Run:

```bash
tmp=$(mktemp -d)
ODIN_ROOT="$tmp" ./bin/odin initiative create --kind routine --key life-admin --title "Life Admin"
ODIN_ROOT="$tmp" ./bin/odin companion create --kind advisor --key finance --title "Finance Advisor"
ODIN_ROOT="$tmp" ./bin/odin profile set --quiet-hours 22:00-07:00
ODIN_ROOT="$tmp" ./bin/odin followup add --initiative life-admin --title "Review mail" --cadence daily
ODIN_ROOT="$tmp" ./bin/odin agenda
ODIN_ROOT="$tmp" ./bin/odin serve
ODIN_ROOT="$tmp" ./bin/odin jobs --json
ODIN_ROOT="$tmp" ./bin/odin runs --json
```

Expected:
- initiative, companion, and profile state are durable
- agenda shows the due obligation
- serve materializes and queues governed work
- jobs and runs reflect the expected state

**Step 6: Commit**

```bash
git add tests/integration/workspace_refactor_acceptance_test.go tests/integration/followup_acceptance_test.go docs/operations/workspace-bootstrap.md docs/operations/followup-routines.md
git commit -m "test: verify Odin routines and follow-through flow"
```
