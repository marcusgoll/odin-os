# Marcus Social Copilot Loop Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make Odin OS expose and run a supported Marcus Social Copilot loop that monitors explicit watch scopes, refreshes evidence/research, and queues approval-ready recommendations without taking unapproved social account actions.

**Domain Source of Truth:** `CONTEXT.md`, `AGENTS.md`, `docs/contracts/marcus-social-copilot.md`, `registry/workflows/marcus-social-growth-workflow.md`, `docs/adr/0001-canonical-authority.md`, `docs/adr/0002-migration-policy.md`

**Context:** Odin OS runtime and operator surface for the Marcus Social Copilot bounded context.

**Owns / Does Not Own:** Owns Social Copilot watch-scope normalization, one workflow-owned polling job, manual wake/poll status, and read-only queue-building handoff to existing social memory. Does not own autonomous posting, autonomous replies, likes, reposts, follows, DMs, LinkedIn browser automation, legacy `odin-orchestrator` workers, or a new social queue.

**Invariants:**
- The canonical term is Social Copilot, not social media manager, autoposter, growth bot, or engagement bot.
- Human approval is required before every post, reply, or account action.
- X publish/reply remains operator-attended through existing `social_outcome` plus `/memory publish ... via=huginn_x`.
- LinkedIn remains manual.
- V1 wake modes are bounded polling and explicit manual wake only, not reactive event ingestion.
- One workflow-owned Marcus social polling job exists per environment.
- Watch scope is operator-seeded only and lives on that one job as structured runtime metadata.
- No first-class social queue is introduced; queue state uses existing `social_research`, `social_draft`, `social_outcome`, `social_evidence`, and normal jobs/runs metadata.
- Stable target identity is explicit and normalized before dedupe, checkpoint ownership, cooldown lookup, or persistence.
- Ordinary manual wakes honor cooldown; there is no force-recheck or cooldown-bypass in v1.

**Architecture:** Reuse existing registry workflow selection, SQLite runtime authority, tasks/runs, context packets, social memory, browser X tools, and `odin serve`. Add a thin Social Copilot runtime service that wraps existing jobs/context-packet primitives rather than creating a separate social manager or queue. Extend the real `odin` shell under the existing `/workflow` and `/jobs` surfaces so status and manual wake are provable from the operator path.

**Tech Stack:** Go, SQLite, existing `internal/store/sqlite`, `internal/runtime/jobs`, `internal/runtime/checkpoints`/context packets, `internal/cli/repl`, `odin` REPL command tests, existing browser X tool drivers.

---

## Current State

The supported Social Copilot surface exists in `odin-os` as workflow, memory, publish, evidence, and retrospective primitives. A legacy `odin-orchestrator` worker is running but failing and must not become the target for new work. The supported live `odin-os` service is running, but the supported social root currently has no active jobs or runs for an always-on Social Copilot loop.

## What Already Exists

- `registry/workflows/marcus-social-growth-workflow.md` defines the Social Copilot workflow and v1 polling/watch-scope invariants.
- `docs/contracts/marcus-social-copilot.md` defines approval gates, watch scopes, cooldown behavior, memory ownership, and allowed/forbidden automation.
- `internal/cli/repl/shell.go` already supports `/workflow`, `/memory resolve`, `/memory publish`, `/tool run`, `/jobs`, and `/runs`.
- `tests/integration/social_workflow_test.go` already proves workflow selection, draft recording, approval resolution, native X publish via `huginn_x`, visible evidence, weekly evidence, and analytics reuse.
- `internal/runtime/jobs` and `internal/runtime/runs` already provide jobs/runs projections and execution history.
- `context_packets` already provide append-only SQLite metadata packets and can store workflow job metadata without creating a social memory type.
- `browser_x_post_publish`, `browser_x_post_visible_evidence`, and `browser_x_weekly_evidence_bundle` already exist as governed tools.

## Gaps

- No supported workflow-owned Marcus Social Copilot polling job exists in `odin-os`.
- No real `odin` command shows whether Social Copilot is scheduled, due, cooling down, or manually waked.
- No structured watch-scope normalizer exists for the closed v1 sections and X target identity rules.
- Jobs/runs do not currently distinguish one-shot operator tasks from the long-lived workflow-owned Social Copilot polling job.
- The active checkout is dirty and contains the newest social assets; execution must start with base stabilization instead of silently building on an ambiguous branch.

## Reuse Plan

- Reuse `odin-os` as the only implementation target.
- Reuse `data/odin.db` through `internal/store/sqlite` as the runtime authority.
- Reuse `tasks` for the single workflow-owned polling job by creating a fixed-key, non-queued task owned by `odin-core` with `scope=workflow`.
- Reuse `context_packets` for append-only Social Copilot job metadata and checkpoint snapshots.
- Reuse `runs` for each bounded poll or manual wake attempt.
- Reuse existing social memory types for observations, drafts, outcomes, evidence, and learnings.
- Reuse `/memory resolve` and `/memory publish` for approval and publishing.
- Reuse `/workflow` for Social Copilot status/scope/wake commands instead of adding a new top-level social manager command group.

## New Additions

- New package `internal/runtime/socialcopilot` for domain-specific watch-scope normalization and the thin loop service.
- New tests for watch-scope invariants, single-job ownership, cooldown behavior, and no autonomous publish/reply.
- New `/workflow social status`, `/workflow social scope replace`, and `/workflow social wake` shell subcommands.
- Minimal service-loop integration in `odin serve` to ensure the one scheduled job exists and to perform bounded due checks only when enabled by configuration.

## Why New Additions Are Necessary

The current primitives prove manual Social Copilot publishing and evidence, but they do not represent the v1 always-on loop. Reusing one-shot task execution directly would either create overlapping jobs or cause the normal task runner to execute a long-lived polling job as a regular queued task. A thin Social Copilot service is necessary to encode the canonical watch-scope and cooldown rules while still storing mutable state in existing SQLite tasks, runs, context packets, and memory.

## Real odin E2E Verification

Every implementation batch must finish with real `odin` checks, not only unit tests. The final proof must run from a fresh runtime root and from the Marcus social root:

```bash
cd /home/orchestrator/odin-os
go test ./internal/runtime/socialcopilot ./internal/runtime/jobs ./internal/cli/repl ./internal/app/lifecycle ./tests/integration
go build -o ./bin/odin ./cmd/odin
ODIN_ROOT="$(mktemp -d)" ./bin/odin doctor --json
ODIN_ROOT="$(mktemp -d)" ./bin/odin healthcheck
```

Marcus social root proof:

```bash
cd /home/orchestrator/odin-os
set -a
. /home/orchestrator/.config/odin/odin.env
set +a
printf '%s\n' \
  '/workflow validate marcus-social-growth-workflow' \
  '/workflow use marcus-social-growth-workflow' \
  '/workflow social status' \
  '/jobs' \
  '/workflow social wake reason=manual-proof' \
  '/workflow social status' \
  '/runs' \
  '/memory list type=social_draft field.approval=pending order=desc limit=5' \
  '/memory list type=social_outcome field.publish_status=published order=desc limit=5' \
  '/quit' | ./bin/odin
```

Expected final proof:

- `/workflow social status` shows exactly one `marcus-social-growth-workflow` Social Copilot job.
- `/jobs` shows one scheduled workflow job, not overlapping peer social jobs.
- Manual wake creates a bounded run or a clear cooldown/no-op result.
- No `social_outcome` is newly published by the wake.
- Approval-ready items remain in `social_draft` or `social_research`.
- Existing published outcomes remain unchanged unless the operator explicitly approves and publishes through `/memory publish`.

## Remaining Risks

- Live X page access can fail independently of Odin runtime correctness.
- Existing dirty worktree state may hide social assets that are not on the selected execution base.
- The current installed `odin-os.service` binary may lag the repo binary and need a separate deploy/restart after implementation.
- A later decision may be needed before enabling automatic evidence capture on high-frequency targets.

## Best operating rule going forward

Treat Social Copilot as an approval-gated monitoring and queue-building loop. It may observe, refresh evidence, update research, and draft recommendations; it must never post, reply, like, repost, follow, DM, or schedule without an explicit operator approval and attended publish step.

---

## Task 1: Base Stabilization and Existing Surface Proof

**Domain Goal:** Start execution from a clean `odin-os` base that contains the canonical Social Copilot workflow and operator surfaces.

**Domain Rules Enforced:**
- `odin-os` is the only target for new implementation.
- Legacy `odin-orchestrator` is reference-only and must not be repaired as the future Social Copilot runtime.
- Real `odin` command proof is required before and after implementation.

**Why this matters:**
- The active checkout is dirty and clean worktrees differ in which social assets they contain. Building on the wrong base would either lose the current workflow contract or encode behavior that cannot be verified through the real operator surface.

**Files:**
- Read: `AGENTS.md`
- Read: `CONTEXT.md`
- Read: `docs/contracts/marcus-social-copilot.md`
- Read: `registry/workflows/marcus-social-growth-workflow.md`
- Read: `internal/cli/repl/shell.go`
- Read: `tests/integration/social_workflow_test.go`
- Modify: none

**Step 1: Select a clean worktree base**

Run:

```bash
git -C /home/orchestrator/odin-os worktree list
git -C /home/orchestrator/odin-os status --short --branch
git -C /home/orchestrator/.config/superpowers/worktrees/odin-os/main-synced status --short --branch
```

Expected: identify a clean worktree or create one from a commit that contains `docs/contracts/marcus-social-copilot.md` and `registry/workflows/marcus-social-growth-workflow.md`.

**Step 2: Verify the selected base has the Social Copilot assets**

Run from the selected worktree:

```bash
test -f docs/contracts/marcus-social-copilot.md
test -f registry/workflows/marcus-social-growth-workflow.md
rg -n "Social Copilot|workflow-owned polling job|/memory publish|browser_x_post_visible_evidence" docs/contracts/marcus-social-copilot.md registry/workflows/marcus-social-growth-workflow.md internal/cli/repl/shell.go internal/tools/catalog/builtin.go
```

Expected: all required assets and operator hooks are present. If not, stop and stabilize the branch before coding.

**Step 3: Run existing social workflow proof**

Run:

```bash
go test ./tests/integration -run 'TestMarcusSocial' -count=1
go test ./internal/cli/repl -run 'Test.*Social|Test.*Workflow' -count=1
```

Expected: PASS or a clear, unrelated baseline failure that is recorded before new work starts.

**Step 4: Build and run real Odin smoke**

Run:

```bash
go build -o ./bin/odin ./cmd/odin
tmp_root="$(mktemp -d)"
ODIN_ROOT="$tmp_root" ./bin/odin doctor --json
ODIN_ROOT="$tmp_root" printf '%s\n' \
  '/workflow validate marcus-social-growth-workflow' \
  '/workflow use marcus-social-growth-workflow' \
  '/memory list type=social_outcome order=desc limit=5' \
  '/jobs' \
  '/runs' \
  '/quit' | ODIN_ROOT="$tmp_root" ./bin/odin
```

Expected: workflow validates/selects and `/jobs` plus `/runs` execute through the real shell.

**Step 5: Commit**

No commit for this task unless a dedicated branch/worktree is created.

---

## Task 2: Social Watch Scope Normalization

**Domain Goal:** Encode the v1 authoritative watch-scope shape and stable target identity rules.

**Domain Rules Enforced:**
- Closed sections only: Marcus-owned surfaces, explicit target URLs, operator-maintained watchlist entries.
- Marcus-owned surfaces only allow `marcus_own_timeline` and `marcus_own_mentions`.
- Operator-entered targets cannot normalize to the reserved `marcus_own_*` namespace.
- X status identity is the numeric `status_id`.
- X account identity is the lowercase handle without `@`.
- Duplicate stable target keys are rejected across the whole scope.

**Why this matters:**
- All dedupe, checkpoint, cooldown, and persistence rules depend on stable target identity. If this is loose, the loop can create duplicates or silently expand outside the operator-seeded scope.

**Files:**
- Create: `internal/runtime/socialcopilot/watch_scope.go`
- Create: `internal/runtime/socialcopilot/watch_scope_test.go`

**Step 1: Write failing tests**

Create table-driven tests for:

```go
func TestWatchScopeNormalizesExplicitXPostURLsByStatusID(t *testing.T)
func TestWatchScopeNormalizesWatchlistAccountHandles(t *testing.T)
func TestWatchScopeRejectsDuplicateStableTargetAcrossSections(t *testing.T)
func TestWatchScopeRejectsReservedMarcusOwnKeysOutsideBuiltins(t *testing.T)
func TestWatchScopeRejectsUnknownSectionKinds(t *testing.T)
```

Expected test inputs:

```go
WatchScopeInput{
    MarcusOwnedSurfaces: []string{"timeline", "mentions"},
    ExplicitTargetURLs: []string{"https://twitter.com/Example/status/12345?s=20#frag"},
    WatchlistEntries: []WatchlistEntryInput{{Kind: "account", Target: "@AviationDaily"}},
}
```

Expected normalized keys include:

```text
marcus_own_timeline
marcus_own_mentions
x_post:12345
x_account:aviationdaily
```

**Step 2: Verify RED**

Run:

```bash
go test ./internal/runtime/socialcopilot -run 'TestWatchScope' -count=1
```

Expected: FAIL because the package does not exist yet.

**Step 3: Minimal implementation**

Implement:

```go
type WatchScopeInput struct {
    MarcusOwnedSurfaces []string
    ExplicitTargetURLs []string
    WatchlistEntries []WatchlistEntryInput
}

type WatchlistEntryInput struct {
    Kind string
    Target string
    Label string
    Reason string
    Notes string
}

type WatchScope struct {
    Targets []WatchTarget
}

type WatchTarget struct {
    Section string
    Kind string
    StableKey string
    CanonicalURL string
    Label string
    Reason string
    Notes string
}

func NormalizeWatchScope(input WatchScopeInput) (WatchScope, error)
```

Use small private helpers for X post URL and profile normalization. Do not reach for browser access, social memory, or live X in this task.

**Step 4: Verify GREEN**

Run:

```bash
go test ./internal/runtime/socialcopilot -run 'TestWatchScope' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/runtime/socialcopilot/watch_scope.go internal/runtime/socialcopilot/watch_scope_test.go
git commit -m "feat: normalize social copilot watch scopes"
```

---

## Task 3: Single Workflow-Owned Polling Job State

**Domain Goal:** Represent the one Marcus Social Copilot polling job without creating a social queue or overlapping peer jobs.

**Domain Rules Enforced:**
- One workflow-owned polling job per environment.
- Watch-scope metadata lives on that job, not in standalone social memory.
- SQLite remains the runtime authority.
- The normal one-shot task runner must not execute the scheduled polling job as a queued operator task.

**Why this matters:**
- The previous live audit showed no supported `odin-os` job/run for the Social Copilot loop. The operator needs a real Odin status surface that says whether it is scheduled, due, cooling down, or recently waked.

**Files:**
- Create: `internal/runtime/socialcopilot/service.go`
- Create: `internal/runtime/socialcopilot/service_test.go`
- Modify: `internal/store/sqlite/models.go` only if a helper type is needed.
- Modify: `internal/store/sqlite/store.go` only if a focused lookup helper is missing.

**Step 1: Write failing tests**

Create tests:

```go
func TestEnsurePollingJobCreatesOneScheduledWorkflowTask(t *testing.T)
func TestEnsurePollingJobReusesExistingWorkflowTask(t *testing.T)
func TestReplaceWatchScopeAppendsWorkflowJobMetadataPacket(t *testing.T)
func TestReplaceWatchScopePreservesCheckpointsForRemainingTargetsOnly(t *testing.T)
```

Expected model:

```text
project=odin-core
task.key=workflow-marcus-social-growth-workflow-social-copilot-loop
task.scope=workflow
task.status=scheduled
requested_by=workflow:marcus-social-growth-workflow
context_packet.packet_scope=workflow_job_metadata
context_packet.trigger=watch_scope_replace
context_packet.checkpoint_key=social-copilot/marcus-social-growth-workflow/social-copilot-loop
```

**Step 2: Verify RED**

Run:

```bash
go test ./internal/runtime/socialcopilot -run 'TestEnsurePollingJob|TestReplaceWatchScope' -count=1
```

Expected: FAIL for missing service behavior.

**Step 3: Minimal implementation**

Implement service methods:

```go
type Service struct {
    Store *sqlite.Store
    Registry projects.Registry
    Now func() time.Time
}

type EnsureJobParams struct {
    WorkflowKey string
    WatchScope WatchScopeInput
    Cadence time.Duration
}

type JobStatus struct {
    Task sqlite.Task
    WatchScope WatchScope
    LastWakeAt *time.Time
    NextWakeAt *time.Time
    Due bool
}

func (service Service) EnsurePollingJob(ctx context.Context, params EnsureJobParams) (JobStatus, error)
func (service Service) ReplaceWatchScope(ctx context.Context, workflowKey string, input WatchScopeInput) (JobStatus, error)
func (service Service) Status(ctx context.Context, workflowKey string) (JobStatus, error)
```

Use `Store.GetProjectByKey` or create `odin-core` through existing project manifest data. Store metadata as context packets with compact JSON only. Do not create social memory rows.

**Step 4: Verify GREEN**

Run:

```bash
go test ./internal/runtime/socialcopilot -run 'TestEnsurePollingJob|TestReplaceWatchScope' -count=1
go test ./internal/store/sqlite -run 'TestContextPacket' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/runtime/socialcopilot internal/store/sqlite/models.go internal/store/sqlite/store.go
git commit -m "feat: persist social copilot polling job state"
```

---

## Task 4: Real Odin Operator Surface

**Domain Goal:** Let the operator inspect and manage the Social Copilot loop through the existing Odin shell.

**Domain Rules Enforced:**
- The operator surface is `/workflow`, not a new top-level social-manager command.
- Status must show proof of one scheduled workflow job.
- Scope replacement is whole-set replacement.
- Manual wake is explicit and cannot bypass cooldown.

**Why this matters:**
- The audit question was operational: is it running autonomously and working? The answer must be visible through real `odin`, not by querying SQLite directly.

**Files:**
- Modify: `internal/cli/commands/commands.go`
- Modify: `internal/cli/repl/shell.go`
- Modify: `internal/cli/repl/shell_test.go`
- Modify: `tests/integration/social_workflow_test.go`

**Step 1: Write failing shell tests**

Add tests:

```go
func TestWorkflowSocialStatusShowsNoJobBeforeEnsure(t *testing.T)
func TestWorkflowSocialScopeReplaceCreatesSingleScheduledJob(t *testing.T)
func TestWorkflowSocialWakeCreatesBoundedRunWithoutPublishing(t *testing.T)
```

Expected commands:

```text
/workflow use marcus-social-growth-workflow
/workflow social status
/workflow social scope replace marcus_own=timeline,mentions target=https://x.com/example/status/123 account=@AviationDaily
/workflow social wake reason=manual-proof
```

Expected output tokens:

```text
social_copilot=marcus-social-growth-workflow status=scheduled
job=workflow-marcus-social-growth-workflow-social-copilot-loop
watch_targets=4
wake=manual status=completed
account_actions=none
```

**Step 2: Verify RED**

Run:

```bash
go test ./internal/cli/repl -run 'TestWorkflowSocial' -count=1
```

Expected: FAIL because `/workflow social` is not implemented.

**Step 3: Minimal implementation**

Extend `handleWorkflow` with a `social` subcommand that requires the selected workflow to be `marcus-social-growth-workflow`. Keep parsing strict and simple in v1:

```text
/workflow social status
/workflow social scope replace marcus_own=timeline,mentions target=<x-status-url> account=<handle> thread=<x-status-url>
/workflow social wake reason=<short-token>
```

Do not add `force`, `bypass`, `like`, `repost`, `follow`, `dm`, or `publish` options.

**Step 4: Verify GREEN**

Run:

```bash
go test ./internal/cli/repl -run 'TestWorkflowSocial' -count=1
go test ./tests/integration -run 'TestMarcusSocial.*Workflow|TestMarcusSocial.*Publish|TestMarcusSocial.*Evidence' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/commands/commands.go internal/cli/repl/shell.go internal/cli/repl/shell_test.go tests/integration/social_workflow_test.go
git commit -m "feat: expose social copilot workflow controls"
```

---

## Task 5: Bounded Wake Planning and Cooldown

**Domain Goal:** Let a manual wake or due poll plan safe read-only work while honoring target-level cooldown.

**Domain Rules Enforced:**
- Manual wake does not bypass cooldown.
- Same target plus same observation fingerprint inside cooldown does not create a duplicate pending item.
- Wake metadata is compact and does not store rendered observations, screenshots, or draft text.
- Cached memory row pointers are hint-only and revalidated.

**Why this matters:**
- The Social Copilot must be able to wake throughout the day without duplicating approval items or mutating social accounts.

**Files:**
- Modify: `internal/runtime/socialcopilot/service.go`
- Modify: `internal/runtime/socialcopilot/service_test.go`
- Modify: `internal/cli/repl/shell_test.go`

**Step 1: Write failing tests**

Add tests:

```go
func TestManualWakeHonorsCooldownForSameObservation(t *testing.T)
func TestManualWakeAllowsMateriallyChangedObservation(t *testing.T)
func TestWakeRevalidatesPendingMemoryHintBeforeReuse(t *testing.T)
func TestWakeDoesNotRecordPublishedSocialOutcome(t *testing.T)
```

**Step 2: Verify RED**

Run:

```bash
go test ./internal/runtime/socialcopilot -run 'TestManualWake|TestWake' -count=1
```

Expected: FAIL.

**Step 3: Minimal implementation**

Implement:

```go
type WakeParams struct {
    WorkflowKey string
    Trigger string
    Reason string
    Observations []Observation
}

type Observation struct {
    StableTargetKey string
    Fingerprint string
    RecommendedMemoryType string
    Summary string
    Fields map[string]string
}

func (service Service) Wake(ctx context.Context, params WakeParams) (WakeResult, error)
```

The first implementation may accept deterministic observations from tests and CLI proof. Browser/live observation drivers can be added later behind the same service.

**Step 4: Verify GREEN**

Run:

```bash
go test ./internal/runtime/socialcopilot -run 'TestManualWake|TestWake' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/runtime/socialcopilot/service.go internal/runtime/socialcopilot/service_test.go internal/cli/repl/shell_test.go
git commit -m "feat: plan social copilot wakes with cooldowns"
```

---

## Task 6: Serve Integration Without Account Actions

**Domain Goal:** Ensure `odin serve` can keep the one Social Copilot job present and perform bounded due checks when explicitly enabled.

**Domain Rules Enforced:**
- Startup may ensure job state, but must not publish, reply, like, repost, follow, DM, or schedule content.
- The loop is bounded polling only.
- The loop runs only against explicit watch scope.
- One job per environment remains enforced.

**Why this matters:**
- The operator needs the supported `odin-os.service` path to become the runtime truth instead of a failing legacy worker.

**Files:**
- Modify: `internal/app/config/config.go`
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/app/lifecycle/serve_test.go`
- Modify: `config/odin.yaml` only if the repo already uses service feature flags there.

**Step 1: Write failing lifecycle tests**

Add tests:

```go
func TestServeEnsuresSocialCopilotJobWhenEnabled(t *testing.T)
func TestServeSocialCopilotLoopDoesNotRunWhenDisabled(t *testing.T)
func TestServeSocialCopilotDueCheckRecordsNoAccountActions(t *testing.T)
```

**Step 2: Verify RED**

Run:

```bash
go test ./internal/app/lifecycle -run 'TestServe.*SocialCopilot' -count=1
```

Expected: FAIL.

**Step 3: Minimal implementation**

Add a service config section or env-gated flag only if it fits existing config patterns:

```yaml
service:
  social_copilot:
    enabled: false
    workflow_key: marcus-social-growth-workflow
    cadence_seconds: 1800
```

Wire `runServe` to call `socialcopilot.Service.EnsurePollingJob` and run a bounded ticker only when enabled. The ticker calls `Wake` only for due targets and records `account_actions=none`.

**Step 4: Verify GREEN**

Run:

```bash
go test ./internal/app/lifecycle -run 'TestServe.*SocialCopilot|TestRunServe' -count=1
go test ./internal/runtime/socialcopilot -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/app/config/config.go internal/app/lifecycle/run.go internal/app/lifecycle/serve_test.go config/odin.yaml
git commit -m "feat: supervise social copilot polling loop"
```

---

## Task 7: Integration and Real Odin Proof

**Domain Goal:** Prove the Social Copilot loop from the real `odin` command path.

**Domain Rules Enforced:**
- Verification uses real `odin`, not direct SQLite edits.
- Publish/reply remains approval-gated.
- No autonomous engagement actions occur.

**Why this matters:**
- The operator asked whether the manager is running autonomously and working. The final answer must be based on operator-surface proof.

**Files:**
- Modify: `tests/integration/social_workflow_test.go`
- Modify: `docs/operations/marcus-live-x-post-runbook.md` only if status/wake commands change runbook evidence.

**Step 1: Write failing integration test**

Add:

```go
func TestMarcusSocialCopilotLoopCLIIntegration(t *testing.T)
```

The test must run:

```text
/workflow validate marcus-social-growth-workflow
/workflow use marcus-social-growth-workflow
/workflow social scope replace marcus_own=timeline,mentions target=https://x.com/example/status/123 account=@AviationDaily
/workflow social status
/workflow social wake reason=manual-proof
/jobs
/runs
/memory list type=social_outcome field.publish_status=published
```

Expected:

```text
status=scheduled
watch_targets=4
account_actions=none
```

And no new `social_outcome` with `publish_status=published`.

**Step 2: Verify RED**

Run:

```bash
go test ./tests/integration -run 'TestMarcusSocialCopilotLoopCLIIntegration' -count=1
```

Expected: FAIL before CLI/service implementation is wired.

**Step 3: Minimal implementation**

Complete only the missing wiring required by the integration test. Do not add live browser polling until the deterministic loop is proven.

**Step 4: Verify GREEN**

Run:

```bash
go test ./tests/integration -run 'TestMarcusSocialCopilotLoopCLIIntegration|TestMarcusSocialWorkflowCLIIntegration|TestMarcusSocialNativeXPublishCLIIntegration|TestMarcusSocialNativeXReplyCLIIntegration|TestMarcusSocialXVisibleEvidenceCLIIntegration' -count=1
go test ./internal/runtime/socialcopilot ./internal/cli/repl ./internal/app/lifecycle -count=1
go build -o ./bin/odin ./cmd/odin
```

Expected: PASS and build succeeds.

**Step 5: Real Odin E2E**

Run the Marcus social root command block from the Real Odin E2E Verification section. Record exact outputs for:

- workflow validation
- selected workflow
- social status
- jobs
- manual wake
- runs
- social draft/research queue
- published outcomes unchanged

**Step 6: Commit**

```bash
git add tests/integration/social_workflow_test.go docs/operations/marcus-live-x-post-runbook.md
git commit -m "test: prove social copilot loop through odin"
```

---

## Task 8: Operator Cutover Notes

**Domain Goal:** Document how to retire the failing legacy social manager and operate the supported `odin-os` Social Copilot loop.

**Domain Rules Enforced:**
- Do not claim the legacy `odin-orchestrator` process is fixed.
- Make supported versus legacy boundaries explicit.
- Use real `odin` commands for operations and verification.

**Why this matters:**
- The audit found a legacy worker consuming capacity and failing on Claude access. Operators need a clear cutover procedure after the supported loop exists.

**Files:**
- Modify: `docs/operations/marcus-live-x-post-runbook.md`
- Create or modify: `docs/operations/marcus-social-copilot-loop.md`

**Step 1: Write the doc update**

Include:

```text
Current supported path: odin-os Social Copilot loop.
Legacy path: odin-orchestrator social-media-manager-1 is retired/reference-only.
Daily check: /workflow social status, /jobs, /runs.
Manual wake: /workflow social wake reason=<short-token>.
Approval path: /memory list type=social_draft field.approval=pending, /memory resolve, /memory publish.
Forbidden: autonomous publish, reply, like, repost, follow, DM, force-recheck.
```

**Step 2: Verify docs match commands**

Run:

```bash
rg -n "/workflow social status|/workflow social wake|/memory publish|autonomous publish|legacy" docs/operations/marcus-social-copilot-loop.md docs/operations/marcus-live-x-post-runbook.md
```

Expected: docs include the supported operator commands and forbidden actions.

**Step 3: Commit**

```bash
git add docs/operations/marcus-social-copilot-loop.md docs/operations/marcus-live-x-post-runbook.md
git commit -m "docs: document social copilot loop operations"
```

---

## Review Checklist

- Domain naming matches `CONTEXT.md`: Social Copilot, Social Draft, Social Outcome, Social Evidence, Social Watch Scope, Social Trigger Model.
- ADR 0001 is honored: mutable runtime state is SQLite-backed.
- ADR 0002 is honored: legacy `odin-orchestrator` is reference-only.
- Existing operator surfaces are reused: `/workflow`, `/memory`, `/jobs`, `/runs`, `/tool`.
- No new social queue exists.
- No autonomous publish/reply/engagement action exists.
- Watch-scope whole replacement and closed section taxonomy are tested.
- Duplicate target and cooldown suppression are tested.
- Real `odin` proof is recorded before completion.
- Any live browser/X failure is reported separately from Odin runtime correctness.
