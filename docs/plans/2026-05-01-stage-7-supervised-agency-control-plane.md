# Stage 7 Supervised Agency Control Plane Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the Stage 7 **Supervised Agency Mode** control plane under `odin work supervise ...` without launching workers, writing GitHub, creating PRs, merging, or deploying.

**Domain Source of Truth:** `CONTEXT.md`, `docs/adr/0001-canonical-authority.md`, `docs/architecture/ADR-0001-brownfield-refactor-strategy.md`, `docs/operations/staged-operational-proving.md`, `docs/contracts/verification-model.md`, `docs/plans/2026-05-01-stage-7-supervised-agency-control-plane-design.md`

**Context:** Odin OS Delivery Workflow / Agency Orchestrator

**Owns / Does Not Own:** Owns Stage 7 control state, queue decisions, duplicate-dispatch claims, recovery observations, and `odin work supervise ...` operator controls. Does not own Codex worker execution, PR creation, CI watching, merge, deploy, runner/security/workspace mutation, token policy changes, or full autonomy.

**Invariants:**
- **Supervised Agency Mode** preserves `maxConcurrentTasks: 1`, `dryRun: false`, and `requireHumanApproval: true`.
- Stage 7 requires both `odin:ready` and `safety:low-risk` plus local path-scope preflight before any future dispatch.
- Only `docs/`, `prompts/`, `fixtures/`, and non-sensitive tests may pass scope preflight.
- Mutable Stage 7 control state lives in SQLite; config provides reviewed defaults only.
- This slice reports `codex_execution=not_started`, `prs=not_created`, `merge=not_merged`, and `deployment=not_started` for every command.

**Architecture:** Add a service-ready supervision control layer under `internal/runtime/supervision`, persisted by `internal/store/sqlite`, and exposed only through `odin work supervise ...`. The service records decisions and recovery evidence but never starts workers or mutates GitHub.

**Tech Stack:** Go, SQLite migrations embedded in `internal/store/sqlite`, existing `odin work` CLI command wiring, existing tracker/intake test fakes, `go test`, `make build`, real `./bin/odin` E2E checks.

---

## Context Mapping

Context: Odin OS Delivery Workflow / Agency Orchestrator.

Owns:

- **Supervised Agency Mode** control state.
- Stage 7 queue eligibility decisions.
- Duplicate-dispatch reservation claims.
- Restart recovery observations.
- Operator-visible `odin work supervise ...` reports.

Depends on:

- `projects.Registry` for enrolled project metadata and GitHub repo identity.
- `tracker/intake` concepts for issue facts.
- `internal/store/sqlite` for runtime authority.
- Existing CLI command construction in `internal/cli/commands/work.go`.

Does not own:

- GitHub as runtime truth.
- Codex runner launch.
- PR creation/update.
- CI run mutation or observation.
- Merge/deploy decisions.
- Protected runner, workspace, security, deployment, CI-secret, dashboard-auth code mutation.

Boundary crossings:

- GitHub issue labels and body are intake facts only.
- Reviewed config defaults become a config hash recorded in SQLite.
- `odin serve` may later call the supervision service, but this plan only exposes `odin work supervise ...`.

## Task 1: Supervision Persistence Contract

**Domain Goal:** Persist mutable Stage 7 control state in SQLite so restart recovery and duplicate-dispatch proof do not depend on process memory, logs, or GitHub labels.

**Domain Rules Enforced:**
- Mutable Stage 7 control truth lives in SQLite.
- Static config defaults do not outrank runtime control state.
- Dispatch claims are planned/reserved only in this slice.

**Why this matters:**
- Stage 7 cannot prove restart recovery or duplicate-dispatch prevention without durable state.

**Files:**
- Create: `internal/store/sqlite/migrations/0022_supervision.sql`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Test: `internal/store/sqlite/supervision_test.go`

**Step 1: Write the failing migration/repository tests**

Add tests for:

- `UpsertSupervisionControl` and `GetSupervisionControl`.
- `UpsertSupervisionQueueDecision` idempotently keyed by project/repo/issue.
- `UpsertSupervisionDispatchClaim` with one active claim per project/repo/issue.
- `CreateSupervisionRecoveryObservation` and list/latest readback.

Use names that encode the domain rules:

```go
func TestSupervisionControlPersistsKillSwitchAndConfigHash(t *testing.T) {}
func TestSupervisionQueueDecisionIsIdempotentForIssueSource(t *testing.T) {}
func TestSupervisionDispatchClaimPreventsDuplicateActiveClaim(t *testing.T) {}
func TestSupervisionRecoveryObservationRecordsBlockedRestart(t *testing.T) {}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/store/sqlite -run 'TestSupervision'`

Expected: FAIL because the tables and repository methods do not exist.

**Step 3: Add the migration**

Create `0022_supervision.sql` with tables:

- `supervision_controls`
- `supervision_queue_decisions`
- `supervision_dispatch_claims`
- `supervision_recovery_observations`

Use unique indexes for:

- one current control row per mode key
- one queue decision per project/repo/issue
- one active claim per project/repo/issue where claim status is active or reserved

**Step 4: Add repository models and methods**

Add typed params/models in `models.go` and store methods in `store.go` following existing project/task/external issue patterns.

Repository methods should use `store.now()` and transactional writes where uniqueness matters.

**Step 5: Run test to verify it passes**

Run: `go test ./internal/store/sqlite -run 'TestSupervision'`

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/store/sqlite/migrations/0022_supervision.sql internal/store/sqlite/models.go internal/store/sqlite/store.go internal/store/sqlite/supervision_test.go
git commit -m "feat(stage7): persist supervision control state"
```

## Task 2: Supervision Domain Service

**Domain Goal:** Implement the Stage 7 control-plane service that validates config, evaluates eligibility, records queue decisions, and reports side-effect status without dispatching work.

**Domain Rules Enforced:**
- Labels are necessary but not sufficient for dispatch.
- Scope preflight refuses unknown, forbidden, and sensitive work.
- Every report says worker/PR/merge/deploy actions are not started.

**Why this matters:**
- The service is the reusable boundary that `odin work supervise ...` and future `odin serve` will call.

**Files:**
- Create: `internal/runtime/supervision/types.go`
- Create: `internal/runtime/supervision/service.go`
- Create: `internal/runtime/supervision/eligibility.go`
- Create: `internal/runtime/supervision/config.go`
- Test: `internal/runtime/supervision/service_test.go`
- Test: `internal/runtime/supervision/eligibility_test.go`

**Step 1: Write failing eligibility tests**

Cover:

- both labels required
- `docs/`, `prompts/`, `fixtures/` pass
- non-sensitive tests pass
- forbidden paths refuse
- `*_test.go` under forbidden packages refuses
- unknown scope refuses

Use stable refusal reasons:

```go
const (
	RefusalMissingRequiredLabel = "missing_required_label"
	RefusalUnknownScope = "unknown_scope"
	RefusalForbiddenPath = "forbidden_path"
	RefusalSensitiveTestScope = "sensitive_test_scope"
)
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/supervision -run 'TestEligibility'`

Expected: FAIL because the package does not exist.

**Step 3: Implement minimal eligibility and config types**

Add:

- `Config` with max concurrency, dry-run, require-human-approval, allowed labels, forbidden paths.
- `DefaultConfig()` returning Stage 7 conservative defaults.
- `ConfigHash()` that hashes redacted stable config JSON.
- `EvaluateIssue()` that returns eligible/refused plus stable reason.
- `SideEffects` report with the four required not-started fields.

**Step 4: Add service tests**

Cover:

- `Start` records enabled control state.
- `Stop` records kill switch or stopped state.
- `Queue` records eligible/refused decisions and planned claims but does not create tasks/runs.
- `Recover` reports clean when no stale claims exist and blocked when config hash changed against active claims.

**Step 5: Implement service methods**

Add `Service` methods:

- `Status(ctx)`
- `Start(ctx, operator string)`
- `Stop(ctx, operator string)`
- `Queue(ctx, project, issues)`
- `Recover(ctx)`

Keep service inputs simple and testable; command wiring can adapt tracker/project data later.

**Step 6: Run tests**

Run: `go test ./internal/runtime/supervision ./internal/store/sqlite`

Expected: PASS.

**Step 7: Commit**

```bash
git add internal/runtime/supervision internal/store/sqlite
git commit -m "feat(stage7): add supervision control service"
```

## Task 3: `odin work supervise` CLI Surface

**Domain Goal:** Expose Stage 7 controls only through the canonical `odin work supervise ...` operator surface.

**Domain Rules Enforced:**
- No parallel `odin agency ...` command.
- `odin serve` is not the human control surface in this slice.
- All reports show zero worker/PR/merge/deploy action.

**Why this matters:**
- Operator-visible behavior is incomplete unless the real `odin work ...` path exists.

**Files:**
- Modify: `internal/cli/commands/work.go`
- Test: `internal/cli/commands/work_supervise_test.go`

**Step 1: Write failing CLI tests**

Add tests for:

- `RunWork(..., []string{"supervise", "status", "--json"})`
- `start --json`
- `stop --json`
- `queue --project alpha --json`
- `recover --json`
- unknown subcommand usage

Assertions:

- JSON parses.
- side effects are not started.
- queue records decisions.
- no rows are created in `runs`, `approvals`, or `worktree_leases`.
- no GitHub write audit appears.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/commands -run 'TestRunWorkSupervise'`

Expected: FAIL because `supervise` is not wired.

**Step 3: Wire `supervise` into `RunWork`**

Update `workUsage` and the `RunWork` switch.

Add a small `runWorkSupervise` dispatcher in `work.go` or a new command file in the same package if the file is becoming too large. Preserve the existing command package pattern.

**Step 4: Implement JSON reports**

Report fields should include:

- `mode`
- `enabled`
- `kill_switch`
- `config_hash`
- `queue`
- `claims`
- `recovery`
- `codex_execution`
- `prs`
- `merge`
- `deployment`

**Step 5: Run tests**

Run: `go test ./internal/cli/commands -run 'TestRunWorkSupervise'`

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/cli/commands/work.go internal/cli/commands/work_supervise_test.go
git commit -m "feat(stage7): expose supervise work commands"
```

## Task 4: Queue Intake Adapter For Control-Plane Proof

**Domain Goal:** Let `odin work supervise queue --project <key> --json` evaluate issue facts through the existing project/tracker seam without live GitHub writes or worker dispatch.

**Domain Rules Enforced:**
- GitHub Issues are **Issue Intake Source** evidence only.
- Labels do not outrank Odin-owned queue decisions.
- Queue evaluation can record decisions, but cannot dispatch.

**Why this matters:**
- Stage 7 needs queue proof against issue-like facts while staying read-only externally.

**Files:**
- Modify: `internal/cli/commands/work.go`
- Modify: `internal/runtime/supervision/service.go`
- Test: `internal/cli/commands/work_supervise_test.go`

**Step 1: Add failing test for queue evaluation from fake tracker**

Follow the pattern in `work_intake_test.go`:

- override `newIntakeTracker`
- return issues with accepted labels and refused labels
- include bodies with explicit path hints such as `docs/example.md`, `internal/security/policy_test.go`, and unknown scope
- assert recorded queue decisions and refusal reasons

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/commands -run 'TestRunWorkSuperviseQueue'`

Expected: FAIL because queue is not using issue facts yet.

**Step 3: Adapt tracker issues to supervision issue inputs**

Reuse existing tracker issue fields:

- provider
- repo
- number
- title
- body
- labels
- URL/state where useful

Do not create **Work Items**, **Run Attempts**, approvals, worktree leases, PRs, or GitHub writes.

**Step 4: Run targeted tests**

Run: `go test ./internal/cli/commands ./internal/runtime/supervision -run 'Supervise|Eligibility'`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/commands/work.go internal/cli/commands/work_supervise_test.go internal/runtime/supervision
git commit -m "feat(stage7): evaluate supervise queue intake"
```

## Task 5: Real Odin E2E Proof And Documentation Artifact

**Domain Goal:** Prove the operator-visible Stage 7 control plane through the real repo-owned `odin` binary and record evidence.

**Domain Rules Enforced:**
- Real `odin` command proof is required for operator-visible behavior.
- This slice performs no worker launch, GitHub write, PR, merge, or deploy.
- Tokens and secrets are not exposed.

**Why this matters:**
- Internal tests do not prove the command surface is usable by operators.

**Files:**
- Create: `docs/operations/stage-7-supervised-control-plane-proof-2026-05-01.md`
- Test/verify: real `./bin/odin`

**Step 1: Run focused tests**

Run:

```bash
go test ./internal/store/sqlite ./internal/runtime/supervision ./internal/cli/commands -run 'Supervision|Supervise|Eligibility'
```

Expected: PASS.

**Step 2: Run broader relevant tests**

Run:

```bash
go test ./internal/store/sqlite ./internal/runtime/supervision ./internal/cli/commands ./internal/tracker/...
```

Expected: PASS.

**Step 3: Build the binary**

Run:

```bash
make build
```

Expected: PASS and `./bin/odin` exists.

**Step 4: Run real command proof against controlled runtime root**

Run:

```bash
export ODIN_ROOT="$(mktemp -d)"
./bin/odin work supervise status --json
./bin/odin work supervise start --json
./bin/odin work supervise queue --project odin-core --json
./bin/odin work supervise stop --json
./bin/odin work supervise recover --json
```

Expected:

- commands exit 0
- reports are valid JSON
- reports contain not-started side-effect fields
- runtime state exists under controlled `ODIN_ROOT`
- no token values appear in output

**Step 5: Confirm no forbidden side effects**

Run SQL or command-level checks against the controlled runtime database to confirm:

- `runs` unchanged for worker execution
- `approvals` unchanged
- `worktree_leases` unchanged
- no PR/merge/deploy action reported

**Step 6: Write proof doc**

Record:

- commands run
- command outputs summarized
- config snapshot hash and redaction proof
- queue decisions
- kill-switch proof
- recovery proof
- explicit unproven boundaries: no worker launch, no PR, no overnight run

**Step 7: Commit**

```bash
git add docs/operations/stage-7-supervised-control-plane-proof-2026-05-01.md
git commit -m "docs(stage7): record supervised control proof"
```

## Final Review Checklist

- Domain naming matches `CONTEXT.md`: **Supervised Agency Mode**, **Agency Orchestrator**, **Issue Intake Source**, **Work Item**, **Run Attempt**, **Human Review Handoff**.
- Invariant coverage exists for labels, scope preflight, sensitive tests, SQLite authority, kill switch, duplicate-dispatch proof, and no side effects.
- ADR 0001 is honored: mutable control state persists in SQLite.
- Brownfield ADR is honored: extend `odin work`, `internal/store/sqlite`, and existing tracker seams instead of creating parallel authority.
- Boundary crossings are explicit: GitHub issues are intake facts; config supplies defaults; SQLite owns mutable state.
- No unresolved domain blockers remain for this control-plane slice.
