# PBS Live May Bid Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make `odin-os` execute a real May PBS bid workflow end-to-end using live Google Calendar and Huginn drivers, a structured PBS bridge, explicit approval, resume, submit, verification, and rollback metadata.

**Architecture:** `odin-os` remains the durable orchestrator. It persists an action-aware task, runs bounded live-tool collection, requests approval, checkpoints a resume payload, and resumes into submit after approval. `/home/orchestrator/pbs` exposes the structured preview/submit bridge and performs the actual PBS API write plus verification.

**Tech Stack:** Go (`odin-os`), Python (`pbs`), SQLite runtime store, shell driver scripts, existing PBS API client and submit logic.

---

### Task 1: Reconcile This Branch To The Harness CLI Baseline

**Files:**
- Reference: `/home/orchestrator/odin-os/docs/plans/2026-04-10-odin-os-harness-cli-cutover.md`
- Modify: `/home/orchestrator/odin-os/internal/app/lifecycle/run.go`
- Modify: `/home/orchestrator/odin-os/internal/app/lifecycle/run_test.go`
- Modify: `/home/orchestrator/odin-os/internal/cli/commands/commands.go`
- Modify: `/home/orchestrator/odin-os/internal/cli/commands/commands_test.go`

**Step 1: Write or port the failing lifecycle tests**

Add tests that prove the machine CLI can:

- create a task with `--action`
- run a task with `--action`
- list approvals
- approve an approval from the CLI

**Step 2: Run the focused lifecycle tests and confirm the baseline is missing**

Run:

```bash
go test ./internal/app/lifecycle ./internal/cli/commands -run 'TestRun.*Task|TestRun.*Approval|TestParse.*' -count=1
```

Expected: failures or missing-command errors in this branch.

**Step 3: Port the minimal CLI surface**

Land the smallest machine CLI needed for the May-bid flow:

- `task create`
- `task run`
- `approvals`
- `approvals approve`
- `approvals reject`

Keep the output JSON-capable where the tests need it.

**Step 4: Re-run the focused lifecycle tests**

Run the same command as step 2.

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/app/lifecycle/run.go internal/app/lifecycle/run_test.go internal/cli/commands/commands.go internal/cli/commands/commands_test.go
git commit -m "feat: restore machine cli baseline for bounded workflows"
```

### Task 2: Add Bounded `pbs_submit_may_bid` Project Policy Support

**Files:**
- Modify: `/home/orchestrator/odin-os/config/projects.yaml`
- Modify: `/home/orchestrator/odin-os/internal/core/projects/manifest.go`
- Add: `/home/orchestrator/odin-os/internal/core/projects/limited_actions.go`
- Modify: `/home/orchestrator/odin-os/internal/core/projects/validate.go`
- Modify: `/home/orchestrator/odin-os/internal/core/projects/manifest_test.go`
- Modify: `/home/orchestrator/odin-os/internal/core/projects/validate_test.go`

**Step 1: Write the failing policy tests**

Add coverage for:

- parsing `policy.limited_actions`
- rejecting unknown bounded action keys
- accepting `pbs_submit_may_bid` for project `pbs`

**Step 2: Run the project policy tests**

Run:

```bash
go test ./internal/core/projects -run 'Test.*LimitedAction|Test.*Manifest' -count=1
```

Expected: FAIL because `limited_actions` and `pbs_submit_may_bid` are not modeled yet.

**Step 3: Implement the minimal manifest + validation changes**

Add:

- `Policy.LimitedActions map[string]LimitedActionRule`
- known bounded action key constant `pbs_submit_may_bid`
- validation that only known bounded actions can be declared
- `config/projects.yaml` allowlist for project `pbs`

Use the earlier phase-33 worktree as the shape reference, but do not bulk-port unrelated bounded actions.

**Step 4: Re-run the project policy tests**

Run the same command as step 2.

Expected: PASS.

**Step 5: Commit**

```bash
git add config/projects.yaml internal/core/projects/manifest.go internal/core/projects/limited_actions.go internal/core/projects/validate.go internal/core/projects/manifest_test.go internal/core/projects/validate_test.go
git commit -m "feat: allowlist bounded pbs may bid action"
```

### Task 3: Persist Action-Aware Tasks In The Runtime Store

**Files:**
- Modify: `/home/orchestrator/odin-os/internal/store/sqlite/models.go`
- Modify: `/home/orchestrator/odin-os/internal/store/sqlite/store.go`
- Modify: `/home/orchestrator/odin-os/internal/store/sqlite/store_test.go`
- Add: `/home/orchestrator/odin-os/internal/store/sqlite/migrations/0011_task_action_key.sql`

**Step 1: Write the failing store tests**

Add tests that prove:

- `CreateTask` stores `action_key`
- `GetTask` returns it
- task listings keep it available for lifecycle and job-runner code

**Step 2: Run the store tests**

Run:

```bash
go test ./internal/store/sqlite -run 'Test.*Task.*ActionKey|TestStore' -count=1
```

Expected: FAIL because `Task` and `CreateTaskParams` do not persist `action_key`.

**Step 3: Implement the migration and store plumbing**

Add `action_key` to:

- task model structs
- create/select/update queries
- migration file

Keep the migration additive and backward-compatible.

**Step 4: Re-run the store tests**

Run the same command as step 2.

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/store/sqlite/models.go internal/store/sqlite/store.go internal/store/sqlite/store_test.go internal/store/sqlite/migrations/0011_task_action_key.sql
git commit -m "feat: persist bounded action keys on tasks"
```

### Task 4: Land Runtime Approvals And The `pbs_submit_may_bid` Action

**Files:**
- Add: `/home/orchestrator/odin-os/internal/runtime/approvals/service.go`
- Add: `/home/orchestrator/odin-os/internal/runtime/approvals/service_test.go`
- Add: `/home/orchestrator/odin-os/internal/runtime/actions/runner.go`
- Add: `/home/orchestrator/odin-os/internal/runtime/actions/pbs_submit_may_bid.go`
- Add: `/home/orchestrator/odin-os/internal/runtime/actions/pbs_submit_may_bid_test.go`
- Reference: `/home/orchestrator/.config/superpowers/worktrees/odin-os/phase-33-may-bid-v2-design/internal/runtime/actions/pbs_submit_may_bid.go`
- Reference: `/home/orchestrator/.config/superpowers/worktrees/odin-os/phase-33-may-bid-v2-design/internal/runtime/approvals/service.go`

**Step 1: Write the failing action and approval tests**

Cover:

- initial preview run requests approval and persists resume payload
- approval service resolves pending approvals and requeues correctly
- resumed run performs submit and records final metadata
- checkpoint persistence failure rejects the orphaned approval

**Step 2: Run the focused runtime tests**

Run:

```bash
go test ./internal/runtime/checkpoints ./internal/runtime/approvals ./internal/runtime/actions -count=1
```

Expected: FAIL because these packages do not exist in this branch.

**Step 3: Implement the minimal bounded workflow**

Port only the May-bid-specific pieces from the phase-33 worktree:

- bounded action result types
- preview/submit bridge request and response structs
- approval service
- resume payload handling
- checkpoint compaction for `approval_wait`

Keep the implementation scoped to `pbs_submit_may_bid`; do not generalize prematurely.

**Step 4: Re-run the focused runtime tests**

Run the same command as step 2.

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/runtime/approvals internal/runtime/actions
git commit -m "feat: add bounded pbs may bid action runtime"
```

### Task 5: Wire The Job Runner And CLI To The Bounded Action

**Files:**
- Modify: `/home/orchestrator/odin-os/internal/runtime/jobs/service.go`
- Modify: `/home/orchestrator/odin-os/internal/runtime/jobs/service_test.go`
- Modify: `/home/orchestrator/odin-os/internal/app/lifecycle/run.go`
- Modify: `/home/orchestrator/odin-os/internal/app/lifecycle/run_test.go`
- Modify: `/home/orchestrator/odin-os/tests/integration/alpha_acceptance_test.go`

**Step 1: Write the failing dispatch tests**

Add coverage for:

- task creation with `--action pbs_submit_may_bid`
- job runner dispatching to the bounded action instead of the generic executor path
- first run ending as `awaiting_approval`
- approval CLI requeueing and resumed submit

**Step 2: Run the focused lifecycle + jobs tests**

Run:

```bash
go test ./internal/runtime/jobs ./internal/app/lifecycle ./tests/integration -run 'Test.*PBSMayBid|TestRun.*Approval|TestAlphaAcceptance' -count=1
```

Expected: FAIL because the runner does not recognize the bounded action yet.

**Step 3: Implement the minimal dispatch path**

In `jobs.Service`:

- parse `task.ActionKey`
- detect `pbs_submit_may_bid`
- call the bounded runner
- translate runner status to run/task status:
  - `awaiting_approval`
  - `completed`
  - `failed`
- persist final metadata into memory/transcript state

In lifecycle CLI:

- allow `task create/run --action`
- surface approval resolution commands

**Step 4: Re-run the focused lifecycle + jobs tests**

Run the same command as step 2.

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/runtime/jobs/service.go internal/runtime/jobs/service_test.go internal/app/lifecycle/run.go internal/app/lifecycle/run_test.go tests/integration/alpha_acceptance_test.go
git commit -m "feat: wire pbs may bid action into jobs and cli"
```

### Task 6: Land The Structured PBS Bridge In `/home/orchestrator/pbs`

**Files:**
- Add: `/home/orchestrator/pbs/src/bidding/odin_bridge.py`
- Add: `/home/orchestrator/pbs/tests/test_bidding_odin_bridge.py`
- Add: `/home/orchestrator/pbs/scripts/pbs_bid_bridge.py`
- Reference: `/home/orchestrator/pbs/.worktrees/phase-33-pbs-bridge/src/bidding/odin_bridge.py`
- Reference: `/home/orchestrator/pbs/.worktrees/phase-33-pbs-bridge/tests/test_bidding_odin_bridge.py`

**Step 1: Write the failing PBS bridge tests**

Cover:

- preview uses structured `off_dates` directly
- submit uses prior preview payload without recomputing recommendation
- session mismatch fails closed before mutation
- unsupported `new_line` creation fails closed before mutation
- CLI exits nonzero on structured submit failures

**Step 2: Run the focused PBS bridge tests**

Run:

```bash
cd /home/orchestrator/pbs && pytest -q tests/test_bidding_odin_bridge.py
```

Expected: FAIL because the bridge files do not exist on `pbs` main.

**Step 3: Implement the bridge**

Port the bridge from the phase-33 PBS worktree:

- `preview_request(...)`
- `submit_request(...)`
- CLI `main()`

Keep these invariants:

- no `/var/odin/pbs-bid-overrides.json` reads
- submit anchored to preview payload
- session mismatch and unsupported `new_line` fail closed

**Step 4: Re-run the focused PBS bridge tests**

Run the same command as step 2.

Expected: PASS.

**Step 5: Commit**

```bash
cd /home/orchestrator/pbs
git add src/bidding/odin_bridge.py tests/test_bidding_odin_bridge.py scripts/pbs_bid_bridge.py
git commit -m "feat: add structured odin pbs bid bridge"
```

### Task 7: Configure The Real Bridge And Add Cross-Repo Acceptance Coverage

**Files:**
- Modify: `/home/orchestrator/odin-os/internal/app/lifecycle/run_test.go`
- Modify: `/home/orchestrator/odin-os/internal/runtime/jobs/service_test.go`
- Modify: `/home/orchestrator/odin-os/tests/integration/alpha_acceptance_test.go`
- Modify: `/home/orchestrator/odin-os/docs/contracts/live-driver-tools.md`
- Add: `/home/orchestrator/odin-os/docs/runbooks/2026-04-pbs-live-may-bid.md`

**Step 1: Write or port the failing acceptance tests**

Add one acceptance that proves:

- `task run --action pbs_submit_may_bid` creates preview and pending approval
- approval resolution requeues
- resumed execution records submit result, verification, and rollback metadata

Keep the test on fixture drivers and fixture bridge output.

**Step 2: Run the acceptance slice**

Run:

```bash
go test ./tests/integration ./internal/app/lifecycle ./internal/runtime/jobs -run 'Test.*PBSMayBid|TestAlphaAcceptance' -count=1
```

Expected: FAIL until the full wiring is present.

**Step 3: Document real env wiring and operator steps**

Update docs so the real live run uses:

- `ODIN_GOOGLE_CALENDAR_DRIVER`
- `ODIN_HUGINN_DRIVER`
- `ODIN_PBS_BID_BRIDGE`

and shows the exact operator sequence for create, inspect, approve, resume, and verify.

**Step 4: Re-run the acceptance slice**

Run the same command as step 2.

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/app/lifecycle/run_test.go internal/runtime/jobs/service_test.go tests/integration/alpha_acceptance_test.go docs/contracts/live-driver-tools.md docs/runbooks/2026-04-pbs-live-may-bid.md
git commit -m "test: cover live pbs may bid workflow acceptance"
```

### Task 8: Run The Real Live Validation

**Files:**
- No code changes required unless live validation exposes a bug.
- Use: `/home/orchestrator/odin-os/docs/runbooks/2026-04-pbs-live-may-bid.md`

**Step 1: Export the real drivers**

Run:

```bash
export ODIN_GOOGLE_CALENDAR_DRIVER="/home/orchestrator/odin-os/scripts/drivers/google-calendar-off-dates.sh"
export ODIN_HUGINN_DRIVER="/home/orchestrator/odin-os/scripts/drivers/huginn-pbs-session.sh"
export ODIN_PBS_BID_BRIDGE="python /home/orchestrator/pbs/scripts/pbs_bid_bridge.py"
```

**Step 2: Run the full verification slice before the live submit**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/adapters/... ./internal/tools/... ./internal/runtime/... ./internal/app/lifecycle ./tests/integration -count=1
cd /home/orchestrator/pbs && pytest -q tests/test_bidding_odin_bridge.py tests/test_autopilot.py tests/test_submit.py tests/test_bid_preview.py
```

Expected: PASS on the targeted slices. If unrelated legacy PBS tests still fail, document them explicitly and do not ignore failures in the bridge slice.

**Step 3: Execute the real workflow**

Run:

```bash
cd /home/orchestrator/odin-os
odin task run --project pbs --action pbs_submit_may_bid --title "Prepare and submit May bid" --json
odin approvals approve --id <approval_id> --by operator --reason "approved for live submit" --json
odin serve
```

**Step 4: Verify the final state**

Confirm:

- run status is `completed`
- approval is `approved`
- final metadata includes verification and rollback info
- the expected bid is visible in PBS

**Step 5: Commit docs or bugfixes only if validation exposed gaps**

If live validation required fixes, commit them intentionally. If not, do not create a no-op commit.
