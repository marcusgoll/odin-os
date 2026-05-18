# Software Factory Lane Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` for task-by-task execution or `superpowers:executing-plans` for inline execution. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Odin managed-project Software Factory Lane from explicit operator start or reviewed intake promotion through factory status, phase evidence, and merge-when-green gating over existing Odin runtime objects.

**Source of Truth:** `docs/superpowers/specs/2026-05-18-software-factory-lane-design.md`

**Architecture:** Factory Lane is a managed-project delivery profile and thin operator adapter over existing Work Items, Run Attempts, approvals, review entries, delegation records, PR handoffs, and executor routing. It must not introduce a second queue, factory-run table, or provider-specific worker authority.

**Tech Stack:** Go, SQLite runtime store, Odin registry markdown workflows, existing lifecycle command dispatcher, existing work/intake/review/jobs/runs surfaces, fake PR/check/merge providers for tests.

**Verification Strategy:** TDD per task with narrow Go tests first, lifecycle command tests for real operator surfaces, registry/profile validation, and final build plus targeted real `./bin/odin` smoke with a temporary `ODIN_ROOT`.

**Approval Source:** Reviewed spec approved by user in this thread; active Codex Goal authorizes design-to-plan handoff only.

---

## Scope Check

Proceeding with one plan: the first Factory Lane implementation slice for managed projects.

This is one coherent slice because every task serves the same operator workflow: admit factory work, keep state on existing Work Items and Run Attempts, expose status, promote reviewed intake, and gate merge. Real cloud/devbox lifecycle and production deployment remain outside this plan.

## File Map

- Create: `registry/workflows/software-factory-lane-workflow.md`
  - Responsibility: Authored delivery profile describing Factory Lane phases, constraints, and success criteria.
  - Used by: Registry loader, `odin work profiles`, factory admission defaults.

- Modify: `internal/registry/loader/load_test.go`
  - Current responsibility: Proves registry assets load and expose delivery-profile metadata.
  - Planned change: Assert the new factory lane profile is loaded as a workflow with `delivery_profile` and `factory_lane` tags.

- Create: `internal/cli/commands/factory.go`
  - Responsibility: Parse `odin factory start|status|promote-intake|merge-gate` flags into a typed command.
  - Used by: lifecycle command dispatcher.

- Create: `internal/cli/commands/factory_test.go`
  - Covers: Factory command parsing, required flags, JSON flag, unknown argument rejection.

- Create: `internal/runtime/factory/service.go`
  - Responsibility: Factory admission, status projection, phase artifact envelope, intake promotion wrapper, and merge-gate orchestration over existing store objects.
  - Used by: lifecycle `runFactory`, tests, future HTTP/PWA adapters.

- Create: `internal/runtime/factory/service_test.go`
  - Covers: Operator admission, policy blocking, phase artifacts, status projection, reviewed intake promotion behavior, and merge-gate decisions with fake providers.

- Modify: `internal/app/lifecycle/run.go`
  - Current responsibility: Top-level `odin` command dispatcher and app dependency wiring.
  - Planned change: Add `factory` to root usage and route it through `runFactory`.

- Modify: `internal/app/lifecycle/run_test.go`
  - Current responsibility: End-to-end command tests for operator-facing `odin` command paths.
  - Planned change: Add real lifecycle tests for factory start/status, reviewed intake promotion, blocked high-risk work, and merge gate readback.

- Modify: `internal/cli/commands/intake.go`
  - Current responsibility: Parse existing intake raw/process/review/approval commands.
  - Planned change: Add optional `--factory` only to `odin intake review accept <id|key>` so reviewed promotion can enter Factory Lane without a parallel intake path.

- Modify: `internal/cli/commands/intake_test.go`
  - Covers: `intake review accept <id|key> --factory --json` parsing and rejection of `--factory` on non-accept review actions.

- Modify: `internal/app/lifecycle/review.go`
  - Current responsibility: Implements intake review decisions and creates Work Items on accepted intake.
  - Planned change: Thread the parsed factory flag into accepted-intake task creation so the task receives `WorkKind=factory_lane`, `ExecutionIntent=mutation`, and factory artifact metadata.

- Modify: `internal/review/pull_request.go`
  - Current responsibility: Defines PR handoff request/body contract with no merge authority.
  - Planned change: Leave existing `PullRequestManager` non-mutating; add merge-gate-specific interfaces only under `internal/runtime/factory` so review handoff does not gain direct merge authority.

- Test: `internal/runtime/delegations/service_test.go`
  - Covers: Existing delegation narrowing remains compatible when factory tasks later use child delegations.

## Task 1: Register The Factory Delivery Profile

**Purpose:** Make Factory Lane visible as authored managed-project delivery profile content before runtime behavior exists.

**Files:**
- Create: `registry/workflows/software-factory-lane-workflow.md`
- Modify: `internal/registry/loader/load_test.go`
- Test: `internal/registry/loader/load_test.go`

**Acceptance Criteria:**
- [ ] Registry loads `software-factory-lane-workflow` as an active workflow.
- [ ] The workflow has `delivery_profile`, `managed-project`, and `factory_lane` tags.
- [ ] `odin work profiles` can list the profile through existing delivery-profile filtering after build.

- [ ] **Step 1: Write the failing test**

Add/modify `internal/registry/loader/load_test.go` with a test named:

```go
func TestLoadRegistryIncludesSoftwareFactoryLaneDeliveryProfile(t *testing.T)
```

The test should load the repo registry, find key `software-factory-lane-workflow`, and assert:

```go
if item.Kind != registry.KindWorkflow { t.Fatalf(...) }
if item.Status != "active" { t.Fatalf(...) }
if !containsString(item.Tags, "delivery_profile") { t.Fatalf(...) }
if !containsString(item.Tags, "factory_lane") { t.Fatalf(...) }
if item.Entrypoint == "" { t.Fatalf(...) }
```

- [ ] **Step 2: Run the narrow test and confirm failure**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/registry/loader -run TestLoadRegistryIncludesSoftwareFactoryLaneDeliveryProfile -count=1
```

Expected:

```text
FAIL because software-factory-lane-workflow is not present in the registry.
```

- [ ] **Step 3: Implement the minimal change**

Create `registry/workflows/software-factory-lane-workflow.md` with frontmatter:

```yaml
kind: workflow
key: software-factory-lane-workflow
title: Software Factory Lane Workflow
summary: Produces managed-project software from reviewed intake or operator start through PR merge when green using existing Odin work, review, approval, run, and merge-gate surfaces.
status: active
tags:
  - managed-project
  - delivery
  - delivery_profile
  - factory_lane
owners:
  - odin-core
entrypoint: command:factory
composes:
  - managed-project-delivery-workflow
  - codex-code-workflow
  - review-only-workflow
```

Include required registry sections: Purpose, When to Use, Inputs, Procedure, Outputs, Constraints, Success Criteria. State explicitly that deployment is out of scope and merge-when-green is the v1 autonomy limit.

- [ ] **Step 4: Run the narrow test and confirm pass**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/registry/loader -run TestLoadRegistryIncludesSoftwareFactoryLaneDeliveryProfile -count=1
```

Expected:

```text
PASS
```

- [ ] **Step 5: Run broader relevant checks**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/registry/... ./internal/cli/commands -count=1
```

Expected:

```text
PASS
```

- [ ] **Step 6: Commit**

```bash
cd /home/orchestrator/odin-os && git status --short
cd /home/orchestrator/odin-os && git add registry/workflows/software-factory-lane-workflow.md internal/registry/loader/load_test.go
cd /home/orchestrator/odin-os && git commit -m "feat: register software factory delivery profile"
```

## Task 2: Add Thin Factory Command Parsing And Dispatch

**Purpose:** Create the operator-friendly `odin factory ...` adapter while keeping all business behavior in runtime services.

**Files:**
- Create: `internal/cli/commands/factory.go`
- Create: `internal/cli/commands/factory_test.go`
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/app/lifecycle/run_test.go`

**Acceptance Criteria:**
- [ ] `factory start` requires `--project` and `--title`.
- [ ] `factory status` accepts `--task <id|key>` and `--json`.
- [ ] `factory promote-intake` requires an intake id/key and accepts `--json`.
- [ ] `factory merge-gate` requires `--task <id|key>` and accepts `--json`.
- [ ] The top-level dispatcher recognizes `factory`.

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/commands/factory_test.go` with:

```go
func TestParseFactoryStart(t *testing.T)
func TestParseFactoryStatus(t *testing.T)
func TestParseFactoryPromoteIntake(t *testing.T)
func TestParseFactoryMergeGate(t *testing.T)
func TestParseFactoryRejectsUnknownArgument(t *testing.T)
```

Add `internal/app/lifecycle/run_test.go` test:

```go
func TestRunFactoryHelpUsesTopLevelDispatcher(t *testing.T)
```

It should call `Run(ctx, root, []string{"factory", "--help"}, ...)` and expect factory usage text instead of `unknown command`.

- [ ] **Step 2: Run the narrow tests and confirm failure**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/cli/commands -run TestParseFactory -count=1
cd /home/orchestrator/odin-os && go test ./internal/app/lifecycle -run TestRunFactoryHelpUsesTopLevelDispatcher -count=1
```

Expected:

```text
FAIL because ParseFactory and the factory dispatcher do not exist.
```

- [ ] **Step 3: Implement the minimal change**

Create `internal/cli/commands/factory.go` with:

```go
const FactoryUsage = "usage: odin factory start --project <key> --title <text> [--json] | odin factory status --task <id|key> [--json] | odin factory promote-intake <id|key> [--json] | odin factory merge-gate --task <id|key> [--json]"

type FactoryCommand struct {
    Name string
    ProjectKey string
    Title string
    TaskRef string
    IntakeRef string
    JSON bool
}

func ParseFactory(args []string) (FactoryCommand, error)
```

Modify `internal/app/lifecycle/run.go`:

```go
// rootUsageBanner: add factory
case "factory":
    return runFactory(ctx, app, args[1:], stdout)
```

Add a temporary `runFactory` in `run.go` that parses help and returns usage for non-help actions until Task 3 supplies runtime behavior:

```go
func runFactory(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error
```

- [ ] **Step 4: Run the narrow tests and confirm pass**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/cli/commands -run TestParseFactory -count=1
cd /home/orchestrator/odin-os && go test ./internal/app/lifecycle -run TestRunFactoryHelpUsesTopLevelDispatcher -count=1
```

Expected:

```text
PASS
```

- [ ] **Step 5: Run broader relevant checks**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/cli/commands ./internal/app/lifecycle -run 'Test(ParseFactory|RunFactory)' -count=1
```

Expected:

```text
PASS
```

- [ ] **Step 6: Commit**

```bash
cd /home/orchestrator/odin-os && git status --short
cd /home/orchestrator/odin-os && git add internal/cli/commands/factory.go internal/cli/commands/factory_test.go internal/app/lifecycle/run.go internal/app/lifecycle/run_test.go
cd /home/orchestrator/odin-os && git commit -m "feat: add factory command adapter"
```

## Task 3: Implement Factory Operator Admission And Status

**Purpose:** Let explicit operator starts create Factory Lane Work Items with durable factory intent and status readback.

**Files:**
- Create: `internal/runtime/factory/service.go`
- Create: `internal/runtime/factory/service_test.go`
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/app/lifecycle/run_test.go`

**Acceptance Criteria:**
- [ ] `odin factory start --project <key> --title <text> --json` creates a queued Work Item.
- [ ] The created task has `WorkKind=factory_lane`, `ExecutionIntent=mutation`, and `ExecutionIntentSource=factory_lane:operator`.
- [ ] Factory task artifacts include profile key, autonomy boundary `merge_when_green`, trigger `operator`, and phase `admitted`.
- [ ] `odin factory status --task <id|key> --json` reads the same task without creating new state.
- [ ] Governance or destructive titles block behind existing approval rules instead of bypassing them.

- [ ] **Step 1: Write the failing tests**

Create `internal/runtime/factory/service_test.go` with:

```go
func TestAdmitOperatorStartCreatesFactoryLaneWorkItem(t *testing.T)
func TestStatusReadsFactoryLaneTask(t *testing.T)
func TestAdmitOperatorStartBlocksGovernanceWorkBehindApproval(t *testing.T)
```

Add lifecycle tests:

```go
func TestRunFactoryStartCreatesFactoryWorkItem(t *testing.T)
func TestRunFactoryStatusReadsFactoryWorkItem(t *testing.T)
func TestRunFactoryStartBlocksHighRiskWork(t *testing.T)
```

- [ ] **Step 2: Run the narrow tests and confirm failure**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/runtime/factory -run TestAdmitOperatorStart -count=1
cd /home/orchestrator/odin-os && go test ./internal/app/lifecycle -run 'TestRunFactory(Start|Status)' -count=1
```

Expected:

```text
FAIL because the factory runtime service and start/status behavior do not exist.
```

- [ ] **Step 3: Implement the minimal change**

Create `internal/runtime/factory/service.go` with these public types:

```go
const WorkKindFactoryLane = "factory_lane"
const ProfileKey = "software-factory-lane-workflow"
const AutonomyMergeWhenGreen = "merge_when_green"

type Service struct {
    Store *sqlite.Store
    Jobs jobs.Service
}

type AdmitOperatorInput struct {
    ProjectKey string
    Title string
    RequestedBy string
}

type AdmissionResult struct {
    Task sqlite.Task
    Created bool
    Trigger string
    Autonomy string
    Phase string
}

type StatusResult struct {
    Task sqlite.Task
    Trigger string
    Autonomy string
    Phase string
}
```

Implement:

```go
func (service Service) AdmitOperatorStart(ctx context.Context, input AdmitOperatorInput) (AdmissionResult, error)
func (service Service) Status(ctx context.Context, taskRef string) (StatusResult, error)
```

Use `jobs.Service.CreateTaskOnce` with:

```go
WorkKind: "factory_lane"
ExecutionIntent: "mutation"
ExecutionIntentSource: "factory_lane:operator"
ArtifactsJSON: `[{"type":"factory_lane","profile_key":"software-factory-lane-workflow","trigger":"operator","autonomy":"merge_when_green","phase":"admitted"}]`
```

In `runFactory`, call the service and render JSON fields:

```json
{
  "factory_lane": true,
  "trigger": "operator",
  "autonomy": "merge_when_green",
  "phase": "admitted",
  "work_item": {"id": 1, "key": "...", "status": "queued"}
}
```

- [ ] **Step 4: Run the narrow tests and confirm pass**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/runtime/factory -run 'Test(AdmitOperatorStart|StatusReads)' -count=1
cd /home/orchestrator/odin-os && go test ./internal/app/lifecycle -run 'TestRunFactory(Start|Status)' -count=1
```

Expected:

```text
PASS
```

- [ ] **Step 5: Run broader relevant checks**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/runtime/factory ./internal/app/lifecycle -run 'Test.*Factory' -count=1
```

Expected:

```text
PASS
```

- [ ] **Step 6: Commit**

```bash
cd /home/orchestrator/odin-os && git status --short
cd /home/orchestrator/odin-os && git add internal/runtime/factory/service.go internal/runtime/factory/service_test.go internal/app/lifecycle/run.go internal/app/lifecycle/run_test.go
cd /home/orchestrator/odin-os && git commit -m "feat: admit factory lane work"
```

## Task 4: Support Reviewed Intake Promotion Into Factory Lane

**Purpose:** Let reviewed intake enter the same Factory Lane without creating a parallel intake execution path.

**Files:**
- Modify: `internal/cli/commands/intake.go`
- Modify: `internal/cli/commands/intake_test.go`
- Modify: `internal/app/lifecycle/review.go`
- Modify: `internal/app/lifecycle/run_test.go`
- Modify: `internal/runtime/factory/service.go`
- Modify: `internal/runtime/factory/service_test.go`

**Acceptance Criteria:**
- [ ] `odin intake review accept <id|key> --factory --json` is valid.
- [ ] `--factory` is rejected for `show`, `reject`, `clarify`, and `archive`.
- [ ] Factory intake promotion creates exactly one Work Item on repeated acceptance.
- [ ] The created Work Item has `WorkKind=factory_lane`, `ExecutionIntent=mutation`, and `ExecutionIntentSource=factory_lane:intake_review`.
- [ ] Risky intake still returns approval-required behavior before work creation.

- [ ] **Step 1: Write the failing tests**

Update `internal/cli/commands/intake_test.go`:

```go
func TestParseIntakeReviewAcceptFactory(t *testing.T)
func TestParseIntakeReviewRejectsFactoryForNonAcceptActions(t *testing.T)
```

Update `internal/app/lifecycle/run_test.go`:

```go
func TestRunIntakeReviewAcceptFactoryPromotesToFactoryLane(t *testing.T)
func TestRunIntakeReviewAcceptFactoryIsIdempotent(t *testing.T)
func TestRunIntakeReviewAcceptFactoryKeepsRiskyApprovalGate(t *testing.T)
```

- [ ] **Step 2: Run the narrow tests and confirm failure**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/cli/commands -run TestParseIntakeReview.*Factory -count=1
cd /home/orchestrator/odin-os && go test ./internal/app/lifecycle -run TestRunIntakeReviewAcceptFactory -count=1
```

Expected:

```text
FAIL because intake review parsing and promotion do not support the factory flag.
```

- [ ] **Step 3: Implement the minimal change**

Add `Factory bool` to `commands.IntakeCommand`.

In `parseIntakeReview`, accept `--factory` only when `ReviewAction == "accept"`:

```go
case "--factory":
    if command.ReviewAction != "accept" {
        return IntakeCommand{}, fmt.Errorf("--factory is only supported for intake review accept")
    }
    command.Factory = true
```

In `internal/app/lifecycle/review.go`, when accepted intake creates a task, branch on `command.Factory` and call a helper that uses the factory service/admission metadata. Preserve existing risk approval behavior by applying the current approval-required check before creating a factory Work Item.

Add a factory service method:

```go
func (service Service) PromoteAcceptedIntake(ctx context.Context, item sqlite.IntakeItem, title string, acceptance []string) (AdmissionResult, error)
```

It must produce `trigger=intake_review`, `phase=admitted`, and `ExecutionIntentSource=factory_lane:intake_review`.

- [ ] **Step 4: Run the narrow tests and confirm pass**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/cli/commands -run TestParseIntakeReview.*Factory -count=1
cd /home/orchestrator/odin-os && go test ./internal/app/lifecycle -run TestRunIntakeReviewAcceptFactory -count=1
```

Expected:

```text
PASS
```

- [ ] **Step 5: Run broader relevant checks**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/cli/commands ./internal/app/lifecycle -run 'Test(ParseIntakeReview|RunIntakeReview)' -count=1
cd /home/orchestrator/odin-os && go test ./internal/runtime/factory -count=1
```

Expected:

```text
PASS
```

- [ ] **Step 6: Commit**

```bash
cd /home/orchestrator/odin-os && git status --short
cd /home/orchestrator/odin-os && git add internal/cli/commands/intake.go internal/cli/commands/intake_test.go internal/app/lifecycle/review.go internal/app/lifecycle/run_test.go internal/runtime/factory/service.go internal/runtime/factory/service_test.go
cd /home/orchestrator/odin-os && git commit -m "feat: promote reviewed intake to factory lane"
```

## Task 5: Add Factory Phase Evidence And Status Projection

**Purpose:** Make the factory lane explain where work is in the SDLC without adding a factory-run table.

**Files:**
- Modify: `internal/runtime/factory/service.go`
- Modify: `internal/runtime/factory/service_test.go`
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/app/lifecycle/run_test.go`

**Acceptance Criteria:**
- [ ] Factory service can append phase evidence for `specification`, `implementation_plan`, `implementation`, `verification`, `review`, `pr_handoff`, `green_check_wait`, `merge`, and `closeout`.
- [ ] Phase evidence is stored in task artifacts or run artifacts, not a new table.
- [ ] `odin factory status --task <id|key> --json` returns current phase, known phases, latest run id, PR handoff id when present, and blocked reason when present.
- [ ] Non-factory tasks are rejected by factory status with a clear error.

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/factory/service_test.go`:

```go
func TestRecordPhaseEvidenceAppendsFactoryArtifact(t *testing.T)
func TestStatusSummarizesFactoryPhases(t *testing.T)
func TestStatusRejectsNonFactoryTask(t *testing.T)
```

Add lifecycle test:

```go
func TestRunFactoryStatusShowsPhaseEvidence(t *testing.T)
```

- [ ] **Step 2: Run the narrow tests and confirm failure**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/runtime/factory -run 'Test(RecordPhaseEvidence|StatusSummarizes|StatusRejects)' -count=1
cd /home/orchestrator/odin-os && go test ./internal/app/lifecycle -run TestRunFactoryStatusShowsPhaseEvidence -count=1
```

Expected:

```text
FAIL because phase evidence recording and enriched status do not exist.
```

- [ ] **Step 3: Implement the minimal change**

Add:

```go
type Phase string

const (
    PhaseAdmitted Phase = "admitted"
    PhaseSpecification Phase = "specification"
    PhaseImplementationPlan Phase = "implementation_plan"
    PhaseImplementation Phase = "implementation"
    PhaseVerification Phase = "verification"
    PhaseReview Phase = "review"
    PhasePRHandoff Phase = "pr_handoff"
    PhaseGreenCheckWait Phase = "green_check_wait"
    PhaseMerge Phase = "merge"
    PhaseCloseout Phase = "closeout"
)

type PhaseEvidenceInput struct {
    TaskID int64
    RunID *int64
    Phase Phase
    Summary string
    Details map[string]string
}
```

Implement:

```go
func (service Service) RecordPhaseEvidence(ctx context.Context, input PhaseEvidenceInput) (StatusResult, error)
```

If `RunID` is present, use `Store.RecordRunArtifact` with `ArtifactType="factory_phase"`. Always update the task artifact envelope through `Store.UpdateTaskStatus` without changing status when possible.

- [ ] **Step 4: Run the narrow tests and confirm pass**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/runtime/factory -run 'Test(RecordPhaseEvidence|StatusSummarizes|StatusRejects)' -count=1
cd /home/orchestrator/odin-os && go test ./internal/app/lifecycle -run TestRunFactoryStatusShowsPhaseEvidence -count=1
```

Expected:

```text
PASS
```

- [ ] **Step 5: Run broader relevant checks**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/runtime/factory ./internal/runtime/runs ./internal/store/sqlite -run 'Test.*(Factory|RunArtifacts|UpdateTaskStatus)' -count=1
```

Expected:

```text
PASS
```

- [ ] **Step 6: Commit**

```bash
cd /home/orchestrator/odin-os && git status --short
cd /home/orchestrator/odin-os && git add internal/runtime/factory/service.go internal/runtime/factory/service_test.go internal/app/lifecycle/run.go internal/app/lifecycle/run_test.go
cd /home/orchestrator/odin-os && git commit -m "feat: record factory phase evidence"
```

## Task 6: Implement Merge-When-Green Gate With Fake Provider Proof

**Purpose:** Add the v1 autonomy boundary: merge when green, but only through explicit gate conditions and test-controlled provider evidence.

**Files:**
- Modify: `internal/runtime/factory/service.go`
- Modify: `internal/runtime/factory/service_test.go`
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/app/lifecycle/run_test.go`
- Test: `internal/app/lifecycle/review_test.go`

**Acceptance Criteria:**
- [ ] Merge gate refuses non-factory tasks.
- [ ] Merge gate refuses factory tasks without a PR handoff.
- [ ] Merge gate refuses pending approvals, unresolved PR blockers, stale PR state, failed checks, or missing branch protection evidence.
- [ ] Merge gate records phase `merge` and closes out only after provider merge succeeds.
- [ ] Merge gate creates a deploy handoff/review item instead of deploying.
- [ ] Existing review handoff remains non-mutating; merge authority lives in factory gate provider interface.

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/factory/service_test.go`:

```go
func TestMergeGateRefusesMissingPullRequestHandoff(t *testing.T)
func TestMergeGateRefusesPendingApproval(t *testing.T)
func TestMergeGateRefusesFailedChecks(t *testing.T)
func TestMergeGateMergesGreenPullRequestAndRecordsEvidence(t *testing.T)
func TestMergeGateCreatesDeployHandoffInsteadOfDeploying(t *testing.T)
```

Add lifecycle test:

```go
func TestRunFactoryMergeGateMergesGreenPullRequest(t *testing.T)
```

- [ ] **Step 2: Run the narrow tests and confirm failure**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/runtime/factory -run TestMergeGate -count=1
cd /home/orchestrator/odin-os && go test ./internal/app/lifecycle -run TestRunFactoryMergeGateMergesGreenPullRequest -count=1
```

Expected:

```text
FAIL because merge-gate behavior does not exist.
```

- [ ] **Step 3: Implement the minimal change**

In `internal/runtime/factory/service.go`, add provider seams:

```go
type PullRequestStateProvider interface {
    Read(ctx context.Context, handoff sqlite.PullRequestHandoff) (PullRequestState, error)
}

type PullRequestMerger interface {
    Merge(ctx context.Context, handoff sqlite.PullRequestHandoff, mode string) (MergeResult, error)
}

type PullRequestState struct {
    RequiredChecksGreen bool
    BranchProtectionSatisfied bool
    Mergeable bool
    Stale bool
    UnresolvedReviewBlockers []string
}

type MergeResult struct {
    Merged bool
    CommitSHA string
    URL string
}
```

Add:

```go
func (service Service) EvaluateMergeGate(ctx context.Context, taskRef string) (MergeGateResult, error)
```

Use `sqlite.ListPullRequestHandoffs` to find the task-linked PR through factory artifact metadata. If the current repo lacks a task-to-PR link, record `pull_request_handoff_id` in the factory phase artifact during tests and read it from task artifacts.

Do not modify `review.PullRequestManager`; it remains non-mutating.

- [ ] **Step 4: Run the narrow tests and confirm pass**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/runtime/factory -run TestMergeGate -count=1
cd /home/orchestrator/odin-os && go test ./internal/app/lifecycle -run TestRunFactoryMergeGateMergesGreenPullRequest -count=1
```

Expected:

```text
PASS
```

- [ ] **Step 5: Run broader relevant checks**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/runtime/factory ./internal/app/lifecycle ./internal/review -run 'Test.*(MergeGate|PullRequest|Review)' -count=1
```

Expected:

```text
PASS
```

- [ ] **Step 6: Commit**

```bash
cd /home/orchestrator/odin-os && git status --short
cd /home/orchestrator/odin-os && git add internal/runtime/factory/service.go internal/runtime/factory/service_test.go internal/app/lifecycle/run.go internal/app/lifecycle/run_test.go
cd /home/orchestrator/odin-os && git commit -m "feat: gate factory merge when green"
```

## Task 7: Add Real Odin Fixture Proof

**Purpose:** Prove the operator path with repo-local commands and a disposable runtime root.

**Files:**
- Modify: `internal/app/lifecycle/run_test.go`
- Modify: `internal/e2e/run.go`
- Modify: `internal/e2e/run_test.go`
- Create: `fixtures/e2e/software-factory-lane.yaml`
- Modify: `scripts/odin-e2e-local.sh`

**Acceptance Criteria:**
- [ ] A fixture-backed E2E scenario admits factory work from operator start.
- [ ] The scenario admits factory work from reviewed intake promotion.
- [ ] The scenario records PR handoff and simulated green merge-gate evidence.
- [ ] The scenario reads back status through `factory status`, `work status`, `jobs`, `runs`, and `review`.
- [ ] `make odin-e2e-local` includes the new fixture without weakening existing scenarios.

- [ ] **Step 1: Write the failing test**

Add `internal/e2e/run_test.go`:

```go
func TestSoftwareFactoryLaneScenario(t *testing.T)
```

Create fixture `fixtures/e2e/software-factory-lane.yaml` with exact expected commands:

```yaml
name: software-factory-lane
commands:
  - odin factory start --project actual-use-demo --title "Implement fixture change" --json
  - odin factory status --task task-1 --json
  - odin intake raw create --source github --title "Factory issue" --type issue --project actual-use-demo --dedup-key factory-issue-1 --json
  - odin intake process --id intake-1 --json
  - odin intake review accept intake-1 --factory --json
  - odin work status --json
  - odin jobs --json
  - odin runs --json
  - odin review list --json
```

- [ ] **Step 2: Run the narrow test and confirm failure**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/e2e -run TestSoftwareFactoryLaneScenario -count=1
```

Expected:

```text
FAIL because the software-factory-lane fixture and/or command support is absent from the E2E runner.
```

- [ ] **Step 3: Implement the minimal change**

Wire the new fixture into `internal/e2e/run.go` using the existing fixture scenario pattern. Update `scripts/odin-e2e-local.sh` to include:

```bash
./bin/odin e2e --scenario fixtures/e2e/software-factory-lane.yaml --json
```

Keep the scenario deterministic with fixture/fake merge evidence only. Do not call live GitHub merge APIs from the E2E fixture.

- [ ] **Step 4: Run the narrow test and confirm pass**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/e2e -run TestSoftwareFactoryLaneScenario -count=1
```

Expected:

```text
PASS
```

- [ ] **Step 5: Run broader relevant checks**

Run:

```bash
cd /home/orchestrator/odin-os && make build
cd /home/orchestrator/odin-os && ODIN_ROOT="$(mktemp -d)" ./bin/odin e2e --scenario fixtures/e2e/software-factory-lane.yaml --json
cd /home/orchestrator/odin-os && make odin-e2e-local
```

Expected:

```text
All commands pass. The e2e output includes factory admission, reviewed intake promotion, status readback, and merge-gate evidence.
```

- [ ] **Step 6: Commit**

```bash
cd /home/orchestrator/odin-os && git status --short
cd /home/orchestrator/odin-os && git add internal/app/lifecycle/run_test.go internal/e2e/run.go internal/e2e/run_test.go fixtures/e2e/software-factory-lane.yaml scripts/odin-e2e-local.sh
cd /home/orchestrator/odin-os && git commit -m "test: prove software factory lane e2e"
```

## Final Verification

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/registry/... ./internal/cli/commands ./internal/runtime/factory ./internal/runtime/delegations ./internal/app/lifecycle ./internal/e2e -count=1
cd /home/orchestrator/odin-os && make build
cd /home/orchestrator/odin-os && ODIN_ROOT="$(mktemp -d)" ./bin/odin work profiles
cd /home/orchestrator/odin-os && ODIN_ROOT="$(mktemp -d)" ./bin/odin factory start --project cfipros --title "Implement fixture change" --json
cd /home/orchestrator/odin-os && ODIN_ROOT="$(mktemp -d)" ./bin/odin e2e --scenario fixtures/e2e/software-factory-lane.yaml --json
cd /home/orchestrator/odin-os && make odin-e2e-local
```

Expected:

```text
All commands pass. `odin work profiles` lists software-factory-lane-workflow. Factory start creates a factory-lane Work Item for registered project `cfipros`. The fixture E2E proves operator start, reviewed intake promotion, status readback, review visibility, and fake-provider merge-gate evidence through real `./bin/odin` commands.
```

## Rollout Notes

- No database migration is planned for v1; use `tasks.work_kind`, `tasks.execution_intent`, `tasks.artifacts_json`, `run_artifacts`, existing approvals, existing review queue, existing PR handoff rows, and existing delegations.
- Real cloud/devbox lifecycle is deferred to executor-provider work. This plan only preserves the route boundary.
- Live GitHub merge wiring is deferred until the fake-provider merge gate is proven. The production merge provider must be added under the same `internal/runtime/factory` provider interface and must obey branch protection and project policy.
- Production deployment remains a handoff/review item, not an autonomous action.

## Self-Review Checklist

- [x] Every requirement maps to at least one task.
- [x] No placeholder language remains.
- [x] File paths are exact.
- [x] Commands are exact and start with `cd /home/orchestrator/odin-os &&`.
- [x] Test names, function names, types, and properties are consistent across tasks.
- [x] Tasks are independently reviewable.
- [x] Each task ends with verification and commit.
- [x] The plan avoids speculative abstractions.
- [x] The plan does not sneak implementation work into the planning phase.

## Requirement Mapping

- Managed-project delivery profile: Task 1.
- Thin `odin factory ...` adapter: Task 2.
- Explicit operator start: Task 3.
- Reviewed intake promotion: Task 4.
- Existing runtime objects as source of truth: Tasks 3, 4, 5.
- Phase evidence without a new factory table: Task 5.
- Merge-when-green autonomy: Task 6.
- No production deploy autonomy: Task 6 and Rollout Notes.
- Executor-agnostic route boundary: Task 3 and Rollout Notes.
- Real Odin proof: Task 7 and Final Verification.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-18-software-factory-lane.md`.

Execution options:

1. Create Goals for Codex — generate Codex-ready `Create Goals` prompts from this plan.
2. Subagent-Driven — use `superpowers:subagent-driven-development`, fresh subagent per task, review between tasks.
3. Inline Execution — use `superpowers:executing-plans`, execute tasks in this session with checkpoints.

Recommended: 1 because this plan is seven commit-sized tasks and should run in a fresh execution goal rather than continuing inside the brainstorming/design thread.
