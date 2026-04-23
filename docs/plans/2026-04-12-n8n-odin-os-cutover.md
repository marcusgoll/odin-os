# n8n to Odin OS Cutover Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Repoint live n8n Odin workflows away from legacy `/var/odin` and legacy SSH/script entrypoints into `odin-os`, while keeping high-risk mutations and approval decisions human-gated.

**Architecture:** Keep n8n as an external trigger plane only. Add a narrow `odin-os` intake boundary that accepts normalized n8n envelopes, persists source and payload durably, enforces dedup, creates project-scoped Odin tasks, and exposes approval resolution through explicit CLI commands. Preserve the current SSH transport shape for n8n with a new forced-command router on the host so workflow changes stay small, then migrate project by project starting with `pbs`.

**Tech Stack:** Go CLI/runtime, SQLite migrations, existing `odin` command surface, SSH forced-command ingress, Docker n8n container, workflow JSON exports, systemd/sshd.

---

## Current Live Trace

- Live n8n is a Docker container bound on `127.0.0.1:5678`.
- Active Odin-facing workflows still target the legacy host over SSH with `/home/node/.ssh/odin_ingress`.
- Active live examples include:
  - `Odin Sentry Alert`
  - `Odin Task Dispatch`
  - `PBS CI Alert`
  - `PBS GitHub Alert`
  - `Odin Telegram Bot`
  - `Odin Core Update`
  - `Marcusgoll CI Alert`
  - `Odin Performance Audit`
  - `Uptime Kuma Telegram Alerts`
- Those workflows either:
  - stream a base64-decoded task envelope into the legacy Odin SSH ingress,
  - call legacy helper verbs like `dedup-check` and `nonce-update`,
  - or execute legacy scripts on the host.
- `odin-os` currently has only one enrolled cutover-owned external project: `pbs`.
- `odin-os` intake today is operator-shaped, not n8n-shaped: `odin task create/run --project <key> --title <title>`.
- `odin-os` task execution currently uses `task.Title` as the prompt objective, so raw n8n payload is not yet a first-class runtime input.
- `odin-os` has approval records and approval listing, but no machine-facing approval resolution command or Telegram callback contract yet.
- `/opt/automation/task-api.js` is a separate automation plane using `/opt/automation/data/tasks.db`; it is not `odin-os` and should not be conflated with the Odin cutover.

## Scope

Phase 1 cutover target:

- n8n workflows whose primary job is to create or route work into Odin.
- `pbs` first, because it is already enrolled and marked `odin_os` primary.

Phase 2 cutover target:

- `cfipros` and `marcusgoll` after they are enrolled in `odin-os` and transitioned out of legacy ownership.

Explicit non-goals for this plan:

- Replacing `/opt/automation` `task-api` with `odin-os`.
- Migrating n8n workflows that directly execute old shell scripts instead of dispatching Odin work, unless they are first rewritten as Odin tasks.

## Desired End State

- n8n sends normalized intake envelopes to a host-side `odin-os` SSH router.
- The router supports:
  - stdin envelope ingestion
  - `dedup-check <kind> <project>`
  - `approval-resolve <approval_id> <approve|deny> <reason...>`
- `odin-os` persists:
  - intake source
  - intake type
  - dedup key
  - raw payload JSON
  - normalized task title/project/action
- Executors can see normalized intake context durably on queued runs.
- `pbs` n8n-triggered work lands in `odin-os` only.
- `cfipros` and `marcusgoll` workflows remain on legacy until their project manifests and transitions exist in `odin-os`.
- Legacy `/var/odin` inbox writing is retired only after import, soak, and rollback rehearsal succeed.

### Task 1: Freeze the Live Cutover Scope

**Files:**
- Create: `docs/operations/n8n-cutover-inventory.md`
- Create: `scripts/ops/export-live-n8n-odin-targets.sh`
- Test: `scripts/tests/export-live-n8n-odin-targets-test.sh`

**Step 1: Write the failing test**

Create a shell test that stubs a small `workflows-export.json` and expects the exporter to emit only active workflows that reference:

- `orchestrator@172.17.0.1`
- `odin_ingress`
- `/var/odin`
- `dedup-check`
- `nonce-update`

Expected output should classify each workflow as one of:

- `dispatch_envelope`
- `legacy_helper`
- `legacy_script`

**Step 2: Run test to verify it fails**

Run: `bash scripts/tests/export-live-n8n-odin-targets-test.sh`

Expected: FAIL because the exporter script does not exist.

**Step 3: Write minimal implementation**

Add `scripts/ops/export-live-n8n-odin-targets.sh` that:

- reads the live export from the n8n container,
- emits a stable TSV or Markdown inventory,
- classifies each active workflow by transport pattern,
- writes the current inventory doc.

Document the initial live inventory in `docs/operations/n8n-cutover-inventory.md`.

**Step 4: Run test to verify it passes**

Run: `bash scripts/tests/export-live-n8n-odin-targets-test.sh`

Expected: PASS and inventory output includes the expected classes.

**Step 5: Commit**

```bash
git add docs/operations/n8n-cutover-inventory.md scripts/ops/export-live-n8n-odin-targets.sh scripts/tests/export-live-n8n-odin-targets-test.sh
git commit -m "docs: capture live n8n odin cutover inventory"
```

### Task 2: Define a Normalized n8n Intake Contract

**Files:**
- Create: `docs/contracts/external-intake.md`
- Create: `internal/cli/commands/intake.go`
- Test: `internal/cli/commands/intake_test.go`

**Step 1: Write the failing test**

Add parser tests for a new root command:

```go
func TestParseIntakeEnqueue(t *testing.T) {
    cmd, err := ParseIntake([]string{
        "enqueue",
        "--source", "n8n",
        "--project", "pbs",
        "--title", "Investigate PBS CI failure",
        "--type", "ci_failure",
        "--dedup-key", "ci_failure:pbs:1234",
        "--payload-file", "-",
        "--json",
    })
    if err != nil {
        t.Fatal(err)
    }
    if cmd.Source != "n8n" || cmd.ProjectKey != "pbs" || cmd.Type != "ci_failure" {
        t.Fatalf("unexpected command: %+v", cmd)
    }
}
```

Also add negative tests for missing `--source`, missing `--project`, invalid `--dedup-key`, and malformed JSON payload file.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/commands -run TestParseIntakeEnqueue -count=1`

Expected: FAIL because `ParseIntake` and the command type do not exist.

**Step 3: Write minimal implementation**

Define a normalized contract in `docs/contracts/external-intake.md`:

```json
{
  "schema_version": 1,
  "source": "n8n",
  "type": "ci_failure",
  "project_key": "pbs",
  "title": "Investigate PBS CI failure",
  "action_key": "",
  "dedup_key": "ci_failure:pbs:1234",
  "requested_by": "n8n",
  "payload": {}
}
```

Implement `ParseIntake` with explicit fields:

- `Name`
- `Source`
- `Type`
- `ProjectKey`
- `Title`
- `ActionKey`
- `DedupKey`
- `RequestedBy`
- `PayloadFile`
- `JSON`

**Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/commands -run TestParseIntakeEnqueue -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add docs/contracts/external-intake.md internal/cli/commands/intake.go internal/cli/commands/intake_test.go
git commit -m "feat: define normalized external intake contract"
```

### Task 3: Persist Intake Records and Dedup State

**Files:**
- Create: `internal/store/sqlite/migrations/0011_task_intakes.sql`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Test: `internal/store/sqlite/task_intakes_test.go`

**Step 1: Write the failing test**

Add tests that expect:

- a task intake row to be created with source, type, dedup key, and raw payload JSON,
- duplicate `(source, dedup_key)` inserts to fail cleanly,
- blank dedup keys to remain allowed when the source cannot provide one.

Example test assertions:

```go
intake, err := store.CreateTaskIntake(ctx, CreateTaskIntakeParams{
    TaskID:       task.ID,
    Source:       "n8n",
    IntakeType:   "ci_failure",
    DedupKey:     "ci_failure:pbs:1234",
    PayloadJSON:  `{"run_id":"1234"}`,
    RequestedBy:  "n8n",
})
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/store/sqlite -run TestCreateTaskIntake -count=1`

Expected: FAIL because the migration and store methods do not exist.

**Step 3: Write minimal implementation**

Add a table:

```sql
CREATE TABLE task_intakes (
  id INTEGER PRIMARY KEY,
  task_id INTEGER NOT NULL,
  source TEXT NOT NULL,
  intake_type TEXT NOT NULL,
  dedup_key TEXT NOT NULL DEFAULT '',
  requested_by TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_task_intakes_source_dedup
  ON task_intakes(source, dedup_key)
  WHERE dedup_key <> '';
```

Add read/write store methods and model types.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/store/sqlite -run TestCreateTaskIntake -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/store/sqlite/migrations/0011_task_intakes.sql internal/store/sqlite/models.go internal/store/sqlite/store.go internal/store/sqlite/task_intakes_test.go
git commit -m "feat: persist external task intake records"
```

### Task 4: Make Intake Payload Reach Execution

**Files:**
- Modify: `internal/runtime/jobs/service.go`
- Modify: `internal/runtime/checkpoints/service.go`
- Modify: `internal/runtime/checkpoints/types.go`
- Test: `internal/runtime/jobs/service_test.go`
- Test: `tests/integration/n8n_intake_execution_test.go`

**Step 1: Write the failing test**

Add a job service test that:

- creates a task plus intake payload,
- executes the task with a static executor,
- asserts the outgoing `contract.TaskSpec` contains normalized intake metadata.

The minimal assertion should verify:

- `Metadata["intake_source"] == "n8n"`
- `Metadata["intake_type"] == "ci_failure"`
- `Metadata["intake_payload_json"]` contains the raw payload

**Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/jobs -run TestExecuteTaskIncludesIntakeMetadata -count=1`

Expected: FAIL because job execution does not load intake data.

**Step 3: Write minimal implementation**

On task execution:

- load the newest task intake row, if present,
- add intake fields to `contract.TaskSpec.Metadata`,
- compact a wake/context packet that includes a concise intake summary.

Do not change the existing prompt objective in this task; keep the objective as the task title and attach intake data as metadata plus wake evidence.

**Step 4: Run test to verify it passes**

Run:

- `go test ./internal/runtime/jobs -run TestExecuteTaskIncludesIntakeMetadata -count=1`
- `go test ./tests/integration -run TestN8NIntakeExecutionMetadata -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/runtime/jobs/service.go internal/runtime/checkpoints/service.go internal/runtime/checkpoints/types.go internal/runtime/jobs/service_test.go tests/integration/n8n_intake_execution_test.go
git commit -m "feat: include external intake context in task execution"
```

### Task 5: Add Explicit Intake and Approval Commands

**Files:**
- Modify: `internal/app/lifecycle/run.go`
- Create: `internal/cli/commands/approval_resolve.go`
- Test: `internal/cli/commands/approval_resolve_test.go`
- Test: `tests/integration/intake_cli_test.go`

**Step 1: Write the failing tests**

Add CLI tests for:

- `odin intake enqueue ... --json`
- `odin approvals resolve --id <id> --decision approve --reason "..." --by telegram`

Expected JSON response for enqueue:

```json
{
  "task": {"id": 1, "key": "investigate-pbs-ci-failure-20260412-120000", "status": "queued", "scope": "pbs"},
  "intake": {"source": "n8n", "type": "ci_failure", "dedup_key": "ci_failure:pbs:1234"}
}
```

**Step 2: Run tests to verify they fail**

Run:

- `go test ./internal/cli/commands -run 'TestParseIntakeEnqueue|TestParseApprovalResolve' -count=1`
- `go test ./tests/integration -run 'TestIntakeEnqueueCLI|TestApprovalsResolveCLI' -count=1`

Expected: FAIL.

**Step 3: Write minimal implementation**

Add:

- `odin intake enqueue`
- `odin approvals resolve`

Behavior:

- enqueue reads payload JSON from stdin or `--payload-file`,
- create a durable task and intake row,
- resolve approval by explicit approval id only,
- write `decision_by` and `reason`,
- do not add a nonce layer in `odin-os`.

**Step 4: Run tests to verify they pass**

Run:

- `go test ./internal/cli/commands -run 'TestParseIntakeEnqueue|TestParseApprovalResolve' -count=1`
- `go test ./tests/integration -run 'TestIntakeEnqueueCLI|TestApprovalsResolveCLI' -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/app/lifecycle/run.go internal/cli/commands/approval_resolve.go internal/cli/commands/approval_resolve_test.go tests/integration/intake_cli_test.go
git commit -m "feat: add external intake and approval resolution commands"
```

### Task 6: Add a Host-Side SSH Router Compatible with n8n

**Files:**
- Create: `scripts/ops/odin-n8n-ssh-dispatch.sh`
- Create: `scripts/tests/odin-n8n-ssh-dispatch-test.sh`
- Create: `docs/operations/n8n-ssh-router.md`

**Step 1: Write the failing test**

Write a shell test that exercises three cases:

1. stdin JSON envelope routes to `odin intake enqueue`
2. `SSH_ORIGINAL_COMMAND='dedup-check ci_failure pbs'` returns `ok` or `cooldown:<n>`
3. `SSH_ORIGINAL_COMMAND='approval-resolve 17 approve confirmed'` routes to `odin approvals resolve`

**Step 2: Run test to verify it fails**

Run: `bash scripts/tests/odin-n8n-ssh-dispatch-test.sh`

Expected: FAIL because the router script does not exist.

**Step 3: Write minimal implementation**

Implement a forced-command router that:

- on empty `SSH_ORIGINAL_COMMAND`, reads normalized intake JSON from stdin and calls `odin intake enqueue`,
- on `dedup-check`, uses a small runtime file or SQLite-backed dedup cache under the `odin-os` runtime root,
- on `approval-resolve`, calls the new CLI approval command,
- rejects unknown commands.

Do not support `nonce-update` in the new router. Telegram callback workflows must move to `approval-resolve`.

**Step 4: Run test to verify it passes**

Run: `bash scripts/tests/odin-n8n-ssh-dispatch-test.sh`

Expected: PASS.

**Step 5: Commit**

```bash
git add scripts/ops/odin-n8n-ssh-dispatch.sh scripts/tests/odin-n8n-ssh-dispatch-test.sh docs/operations/n8n-ssh-router.md
git commit -m "feat: add odin-os n8n ssh router"
```

### Task 7: Enroll Non-PBS Projects Before Their Workflow Cutover

**Files:**
- Modify: `config/projects.yaml`
- Create: `docs/operations/project-overlays-cfipros-marcusgoll.md`
- Test: `tests/integration/project_registry_external_workflows_test.go`

**Step 1: Write the failing test**

Add an integration test that bootstraps the registry and expects:

- `pbs` present and cutover-owned,
- `cfipros` present in `shadow`,
- `marcusgoll` present in `shadow`.

**Step 2: Run test to verify it fails**

Run: `go test ./tests/integration -run TestRegistryIncludesN8NTargetProjects -count=1`

Expected: FAIL because only `pbs` exists today.

**Step 3: Write minimal implementation**

Add manifest entries for:

- `cfipros`
- `marcusgoll`

Keep them fail-closed:

- `shadow` or `inventory` only at first,
- no default mutation authority,
- branch/worktree rules mirror `pbs`.

Document the intended transition path in the ops doc.

**Step 4: Run test to verify it passes**

Run: `go test ./tests/integration -run TestRegistryIncludesN8NTargetProjects -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add config/projects.yaml docs/operations/project-overlays-cfipros-marcusgoll.md tests/integration/project_registry_external_workflows_test.go
git commit -m "feat: enroll n8n target projects in odin-os"
```

### Task 8: Create Canonical n8n Workflow Exports for Odin OS

**Files:**
- Create: `ops/n8n/workflows/odin-os-dispatch.json`
- Create: `ops/n8n/workflows/odin-os-sentry-alert.json`
- Create: `ops/n8n/workflows/odin-os-pbs-ci-alert.json`
- Create: `ops/n8n/workflows/odin-os-pbs-github-alert.json`
- Create: `ops/n8n/workflows/odin-os-telegram-bot.json`
- Create: `scripts/ops/import-n8n-workflow.sh`
- Test: `scripts/tests/n8n-workflow-export-lint-test.sh`

**Step 1: Write the failing test**

Add a shell test that validates each canonical workflow export:

- contains no `/var/odin` references,
- contains no `/home/orchestrator/odin-orchestrator/scripts/odin/` references,
- contains `approval-resolve` instead of `nonce-update`,
- uses the normalized intake contract before SSH dispatch.

**Step 2: Run test to verify it fails**

Run: `bash scripts/tests/n8n-workflow-export-lint-test.sh`

Expected: FAIL because the canonical exports do not exist.

**Step 3: Write minimal implementation**

Create canonical workflow exports that:

- build normalized intake JSON,
- call the new SSH router,
- use `approval-resolve` for Telegram approval callbacks,
- keep per-project dedup keys deterministic.

The shared dispatch workflow should be the only place that knows the SSH target.

**Step 4: Run test to verify it passes**

Run: `bash scripts/tests/n8n-workflow-export-lint-test.sh`

Expected: PASS.

**Step 5: Commit**

```bash
git add ops/n8n/workflows/ scripts/ops/import-n8n-workflow.sh scripts/tests/n8n-workflow-export-lint-test.sh
git commit -m "feat: add canonical n8n workflow exports for odin-os"
```

### Task 9: Pilot Cutover on `pbs`

**Files:**
- Create: `tests/integration/n8n_pbs_cutover_test.go`
- Modify: `docs/operations/odin-os-cutover.md`
- Create: `docs/operations/n8n-cutover.md`

**Step 1: Write the failing integration test**

The test should simulate:

- a normalized `n8n` intake envelope for `pbs`,
- `odin intake enqueue`,
- `odin serve`,
- task completion under `odin-os`,
- no `/var/odin` dependency.

**Step 2: Run test to verify it fails**

Run: `go test ./tests/integration -run TestN8NPBSCutoverFlow -count=1`

Expected: FAIL because the intake pipeline is incomplete.

**Step 3: Write minimal implementation**

Update the cutover docs with exact operator steps:

1. import `odin-os` workflow exports into n8n
2. activate only `pbs` workflows first
3. replace the SSH forced command on the host with the new router
4. trigger manual smoke events
5. verify `odin status --json`, `odin jobs --json`, `odin runs --json`
6. verify no new `/var/odin/inbox/*.json` files appear for `pbs`

**Step 4: Run test to verify it passes**

Run: `go test ./tests/integration -run TestN8NPBSCutoverFlow -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add tests/integration/n8n_pbs_cutover_test.go docs/operations/odin-os-cutover.md docs/operations/n8n-cutover.md
git commit -m "docs: add pbs n8n cutover procedure"
```

### Task 10: Migrate Telegram Approval Flow and Retire Legacy Ingress

**Files:**
- Modify: `ops/n8n/workflows/odin-os-telegram-bot.json`
- Create: `docs/operations/n8n-rollback.md`
- Modify: `docs/operations/odin-os-rollback.md`
- Test: `tests/integration/telegram_approval_cutover_test.go`

**Step 1: Write the failing test**

Add an integration test that:

- creates a pending approval in `odin-os`,
- simulates a Telegram callback with `approval-resolve`,
- asserts the approval row is resolved and no nonce state is needed.

**Step 2: Run test to verify it fails**

Run: `go test ./tests/integration -run TestTelegramApprovalCutover -count=1`

Expected: FAIL because the callback flow is not yet wired.

**Step 3: Write minimal implementation**

Update the canonical Telegram workflow so callback data carries:

- `approval_id`
- `decision`

Then:

- activate the `odin-os` Telegram workflow,
- deactivate the legacy Telegram approval callback workflow,
- remove legacy inbox dependence from approval handling.

Add rollback instructions that restore:

- the old SSH forced-command entry,
- the old Telegram workflow activation state,
- the old workflow exports if `odin-os` intake becomes degraded.

**Step 4: Run test to verify it passes**

Run: `go test ./tests/integration -run TestTelegramApprovalCutover -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add ops/n8n/workflows/odin-os-telegram-bot.json docs/operations/n8n-rollback.md docs/operations/odin-os-rollback.md tests/integration/telegram_approval_cutover_test.go
git commit -m "feat: cut over telegram approval callbacks to odin-os"
```

## Rollout Order

1. Build and test the normalized intake path inside `odin-os`.
2. Add the SSH router and keep it dark.
3. Import the canonical `pbs` workflows into n8n and leave them inactive.
4. Switch the host SSH forced command to the new router.
5. Activate only `pbs` workflows.
6. Soak for at least one week of real events.
7. Enroll and shadow `cfipros` and `marcusgoll`.
8. Move their workflows only after `odin-os` owns their transitions.
9. Cut Telegram approval callbacks last.

## Acceptance Criteria

- `pbs` n8n-triggered work no longer creates files in `/var/odin/inbox/`.
- `odin-os` stores source, intake type, dedup key, and payload for every n8n-created task.
- `odin-os` task execution can expose normalized intake metadata to the executor.
- The host-side SSH key no longer needs the legacy inbox writer.
- Approval callbacks resolve `odin-os` approvals directly.
- Legacy `/var/odin` ingress is used only by workflows that have been explicitly deferred.

## Deferred Work

- Migrating `/opt/automation/task-api` to `odin-os`.
- Rewriting non-ingress workflows that directly run old `odin-orchestrator` shell scripts.
- Replacing n8n as a scheduling plane for work that should become native `odin-os` recurring jobs later.

Plan complete and saved to `docs/plans/2026-04-12-n8n-odin-os-cutover.md`. Two execution options:

**1. Subagent-Driven (this session)** - I dispatch fresh subagent per task, review between tasks, fast iteration

**2. Parallel Session (separate)** - Open new session with executing-plans, batch execution with checkpoints

Which approach?
