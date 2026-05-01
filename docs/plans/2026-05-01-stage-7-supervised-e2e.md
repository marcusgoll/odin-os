# Stage 7 Supervised E2E Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a bounded live supervised E2E path that creates one controlled docs-only issue, processes that exact issue through a local worker, opens a draft PR, waits for CI/review evidence, and stops before merge or deploy.

**Domain Source of Truth:** `CONTEXT.md`, `docs/adr/0001-canonical-authority.md`, `docs/architecture/ADR-0001-brownfield-refactor-strategy.md`, `docs/operations/staged-operational-proving.md`, `docs/operations/stage-7-supervised-control-plane-proof-2026-05-01.md`, `docs/plans/2026-05-01-stage-7-supervised-e2e-design.md`

**Context:** Stage 7 **Supervised Agency Mode** inside the Odin **Delivery Workflow** for the `odin-core` system project.

**Owns / Does Not Own:** Owns the bounded supervised E2E command pair, run artifacts, exact-issue guard, exact-path guard, worker/PR handoff orchestration, and proof docs. Does not own unrestricted scheduler dispatch, autonomous merge, autonomous deploy, dashboard auth, token policy changes, runner internals, workspace deletion, or protected-path mutation.

**Invariants:**
- `maxConcurrentTasks=1`, `dryRun=false`, and `requireHumanApproval=true` remain enforced.
- GitHub issues are **Issue Intake Source** evidence, not runtime authority.
- SQLite remains the mutable runtime authority for control state, queue decisions, claims, and recovery observations.
- `run-once` may process only the explicit `--issue` issue.
- The worker may edit exactly one planned file under `docs/operations/`.
- Draft PR creation is allowed; merge and deploy are never performed.
- Human merge remains required.

**Architecture:** Extend the existing `odin work supervise ...` command group with a nested `e2e` command pair. Add a small orchestration layer in the CLI/runtime boundary that reuses tracker intake, supervision queueing, worktree safety patterns, Codex command execution, Stage 6 PR/CI/review helpers, and SQLite claim state. Write redacted artifact bundles under `ODIN_ROOT/runs/supervised-e2e/<run-id>/` while keeping runtime truth in SQLite.

**Tech Stack:** Go, SQLite, repo-owned `./bin/odin`, existing GitHub tracker adapter, existing VCS/worktree helpers, existing Codex command builder, GitHub Actions CI.

---

## Context Mapping

**Context:** Stage 7 Supervised Agency Mode.

**Owns:**
- supervised E2E setup issue
- supervised run-once report
- run artifact bundle
- exact issue/scope/diff guards
- Human Review Handoff evidence

**Depends on:**
- `internal/store/sqlite` for runtime state
- `internal/runtime/supervision` for queue/claim/recovery rules
- `internal/tracker/github` for GitHub issue, comment, PR, and CI APIs
- `internal/vcs` / existing git helpers for worktrees and branches
- existing worker prompt and PR rendering patterns

**Does not own:**
- general scheduler loops
- executor/router redesign
- dashboard surfaces
- deployment workflows
- GitHub token policy
- merge automation

**Boundary crossings:**
- GitHub issue and PR APIs cross only through `internal/tracker/github`.
- Worker execution crosses through the existing Codex command/runner boundary.
- Git branches/worktrees cross through existing VCS helpers.
- Runtime truth crosses into SQLite through existing store and supervision APIs.

## Task 1: Supervised E2E Command Contract

**Domain Goal:** Expose the approved E2E shape under the canonical `odin work supervise ...` surface without creating a parallel operator command.

**Domain Rules Enforced:**
- Stage 7 operator controls stay under `odin work supervise ...`.
- `--json` is required for this E2E slice.
- `run-once` requires an explicit `--issue`; it must not discover arbitrary work.

**Why this matters:**
- A bounded E2E command must be auditable and explicit before it can run a worker or write GitHub.

**Files:**
- Modify: `internal/cli/commands/work.go`
- Create: `internal/cli/commands/work_supervise_e2e_test.go`
- Test: `internal/cli/commands/work_supervise_e2e_test.go`

**Step 1: Write failing CLI contract tests**

Add tests:

```go
func TestRunWorkSuperviseE2ERequiresJSON(t *testing.T)
func TestRunWorkSuperviseE2EPrepareIssueRequiresProject(t *testing.T)
func TestRunWorkSuperviseE2ERunOnceRequiresExplicitIssue(t *testing.T)
```

Expected JSON report fields for later tasks:

```go
type superviseE2EReport struct {
    Mode string `json:"mode"`
    Phase string `json:"phase"`
    Status string `json:"status"`
    Project string `json:"project"`
    Repo string `json:"repo"`
    RunID string `json:"run_id"`
    Issue struct {
        Number int `json:"number"`
        URL string `json:"url"`
        PlannedPath string `json:"planned_path"`
    } `json:"issue"`
    PR struct {
        Number int `json:"number,omitempty"`
        URL string `json:"url,omitempty"`
        Draft bool `json:"draft,omitempty"`
    } `json:"pr"`
    HumanMergeRequired bool `json:"human_merge_required"`
    Merge string `json:"merge"`
    Deployment string `json:"deployment"`
}
```

**Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/cli/commands -run 'TestRunWorkSuperviseE2E' -count=1
```

Expected: FAIL because `supervise e2e` is not wired.

**Step 3: Wire nested command shape only**

Update `runWorkSuperviseJSON` to route:

```text
supervise e2e prepare-issue --project <key> --json
supervise e2e run-once --project <key> --issue <number> --json
```

Stub the commands with clear `not_implemented` errors after validation. Do not call GitHub or workers yet.

**Step 4: Run tests**

Run:

```bash
go test ./internal/cli/commands -run 'TestRunWorkSuperviseE2E' -count=1
```

Expected: PASS for validation tests.

**Step 5: Commit**

```bash
git add internal/cli/commands/work.go internal/cli/commands/work_supervise_e2e_test.go
git commit -m "feat(stage7): add supervised e2e command contract"
```

## Task 2: Explicit Prepare-Issue Setup

**Domain Goal:** Let Odin create one controlled docs-only Issue Intake Source record as an explicit setup step, not hidden scheduler behavior.

**Domain Rules Enforced:**
- Issue creation is a deliberate operator command.
- The issue carries `odin:ready` and `safety:low-risk`.
- The issue body contains exactly one planned path under `docs/operations/`.
- No PR, merge, deploy, worker, or dispatch occurs during setup.

**Why this matters:**
- Odin may create test work only when the operator explicitly asks for a bounded proof issue.

**Files:**
- Modify: `internal/cli/commands/work.go`
- Modify: `internal/cli/commands/work_supervise_e2e_test.go`
- Reuse: `internal/tracker/github/client.go` `CreateFollowUpIssue`

**Step 1: Write failing prepare-issue tests**

Add tests with a fake GitHub HTTP server:

```go
func TestRunWorkSuperviseE2EPrepareIssueCreatesLabeledDocsIssue(t *testing.T)
func TestRunWorkSuperviseE2EPrepareIssueRedactsToken(t *testing.T)
```

The fake server should assert a single `POST /repos/marcusgoll/odin-os/issues` body with:

```json
{
  "title": "Stage 7 supervised E2E docs proof ...",
  "labels": ["odin:ready", "safety:low-risk"],
  "body": "Planned scope: docs/operations/stage-7-supervised-e2e-..."
}
```

**Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/cli/commands -run 'TestRunWorkSuperviseE2EPrepareIssue' -count=1
```

Expected: FAIL because prepare-issue is stubbed.

**Step 3: Implement prepare-issue**

Reuse project registry lookup and `trackergithub.NewClientWithConfig`. Generate:

- `run_id`
- `planned_path`
- issue title/body
- `tracker.FollowUpIssue`

Call `CreateFollowUpIssue`.

Write artifacts:

```text
ODIN_ROOT/runs/supervised-e2e/<run-id>/prepared-issue.json
ODIN_ROOT/runs/supervised-e2e/<run-id>/final-report.json
```

Report:

- phase `prepared`
- status `prepared`
- issue number/URL
- planned path
- `prs=not_created`
- `merge=not_performed`
- `deployment=not_started`
- `human_merge_required=true`

**Step 4: Run tests**

Run:

```bash
go test ./internal/cli/commands -run 'TestRunWorkSuperviseE2EPrepareIssue' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/commands/work.go internal/cli/commands/work_supervise_e2e_test.go
git commit -m "feat(stage7): prepare supervised e2e issue"
```

## Task 3: Exact-Issue Queue And Claim

**Domain Goal:** `run-once` must consume only the prepared issue number and persist one supervised claim before any worker runs.

**Domain Rules Enforced:**
- GitHub issue labels are intake evidence only.
- SQLite queue decisions and claims own dispatch eligibility.
- Kill switch blocks worker launch.
- Claim conflicts fail closed.

**Why this matters:**
- The first live run cannot accidentally process a different issue or duplicate dispatch.

**Files:**
- Modify: `internal/cli/commands/work.go`
- Modify: `internal/cli/commands/work_supervise_e2e_test.go`
- Reuse: `internal/runtime/supervision`

**Step 1: Write failing queue/claim tests**

Add tests:

```go
func TestRunWorkSuperviseE2ERunOnceQueuesExactIssueAndClaims(t *testing.T)
func TestRunWorkSuperviseE2ERunOnceKillSwitchBlocksWorker(t *testing.T)
func TestRunWorkSuperviseE2ERunOnceMismatchedIssueFailsClosed(t *testing.T)
```

Fake GitHub should return one issue with:

```text
Planned scope: docs/operations/stage-7-supervised-e2e-2026-05-01-test.md
```

**Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/cli/commands -run 'TestRunWorkSuperviseE2ERunOnce' -count=1
```

Expected: FAIL because run-once is stubbed.

**Step 3: Implement queue/claim phase**

Fetch exact issue with `FetchIssueByID`, adapt it to `supervision.Issue`, and call `supervision.Service.Queue` with a single issue. Refuse if:

- issue number differs
- queue decision is not `eligible`
- claim is absent
- kill switch active
- planned path is not exactly one docs operations path

Write:

```text
queue-report.json
final-report.json
```

Stop after claim in this task; do not run worker yet.

**Step 4: Run tests**

Run:

```bash
go test ./internal/cli/commands ./internal/runtime/supervision -run 'Supervise|Supervision|Eligibility' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/commands/work.go internal/cli/commands/work_supervise_e2e_test.go
git commit -m "feat(stage7): claim supervised e2e issue"
```

## Task 4: Worker Execution And Exact Diff Audit

**Domain Goal:** Run the local worker only after claim ownership is proven, and allow PR creation only if the diff is exactly the planned docs file.

**Domain Rules Enforced:**
- Worker execution is limited to one claimed issue.
- The worker prompt includes brownfield guardrails.
- The diff may contain only the exact planned path.
- Token-shaped strings block the run before PR creation.

**Why this matters:**
- This is the highest-risk trust step; it must fail closed before any PR write.

**Files:**
- Modify: `internal/cli/commands/work.go`
- Modify: `internal/cli/commands/work_supervise_e2e_test.go`
- Reuse: existing worker dry-run prompt/worktree helpers in `work.go`
- Reuse: `internal/runner/codexexec` command builder

**Step 1: Write failing worker/diff tests**

Add tests with an injectable worker function:

```go
func TestRunWorkSuperviseE2ERunOnceWorkerEditsOnlyPlannedPath(t *testing.T)
func TestRunWorkSuperviseE2ERunOnceForbiddenDiffBlocksPR(t *testing.T)
func TestRunWorkSuperviseE2ERunOnceTokenInDiffBlocksPR(t *testing.T)
```

The fake worker should create the planned file in the worktree. The forbidden test should also write `internal/security/secret_test.go`.

**Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/cli/commands -run 'TestRunWorkSuperviseE2ERunOnce.*Worker|ForbiddenDiff|TokenInDiff' -count=1
```

Expected: FAIL because worker execution/diff audit is not implemented.

**Step 3: Implement worker and diff audit**

Create isolated worktree using the existing Stage 4 pattern. Render a worker prompt requiring:

- repo audit before editing
- exact planned path only
- no runner/security/workspace/token/deploy/CI changes
- final output mentions `make odin-e2e-local`

For tests, keep worker execution injectable. For live runs, use the existing Codex command builder and execute with bounded timeout.

Audit:

```bash
git diff --name-status <base>
git diff <base>
```

Fail unless changed files equal exactly the planned path. Scan diff, prompt, output, PR body, and JSON artifacts for token-like strings.

Write:

```text
worker-prompt.md
worker-command.json
worker-output.txt
diff-summary.md
final-report.json
```

**Step 4: Run tests**

Run:

```bash
go test ./internal/cli/commands -run 'TestRunWorkSuperviseE2ERunOnce.*Worker|ForbiddenDiff|TokenInDiff' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/commands/work.go internal/cli/commands/work_supervise_e2e_test.go
git commit -m "feat(stage7): run supervised e2e worker"
```

## Task 5: Draft PR, CI Wait, And Review Handoff

**Domain Goal:** Convert the exact docs-only worker diff into a draft PR with CI and review evidence, then stop for human merge.

**Domain Rules Enforced:**
- PR creation is allowed only after exact diff audit.
- CI must include `make odin-e2e-local`.
- Review evidence comments must exist.
- Merge and deploy are not performed.

**Why this matters:**
- This completes the supervised delivery loop without granting autonomous merge or deploy.

**Files:**
- Modify: `internal/cli/commands/work.go`
- Modify: `internal/cli/commands/work_supervise_e2e_test.go`
- Reuse: Stage 6 helpers in `work.go`:
  - `ensureRemoteBranch`
  - `renderPRCreateBody`
  - `verifyPRTemplate`
  - `ensureStage6EvidenceComments`
  - `waitForStage6CI`
  - `auditStage6Deployment`

**Step 1: Write failing PR/CI/handoff tests**

Add tests with fake GitHub:

```go
func TestRunWorkSuperviseE2ERunOnceCreatesDraftPRAndHandoff(t *testing.T)
func TestRunWorkSuperviseE2ERunOnceCITimeoutLeavesDraftUnmerged(t *testing.T)
func TestRunWorkSuperviseE2ERunOnceDeploymentWorkflowFailsClosed(t *testing.T)
```

Fake GitHub should cover:

- branch push path where feasible via local bare origin
- PR create/reuse
- issue comments
- workflow runs containing `Odin E2E`

**Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/cli/commands -run 'TestRunWorkSuperviseE2ERunOnce.*PR|CI|Deployment' -count=1
```

Expected: FAIL because PR/handoff is not wired into run-once.

**Step 3: Implement PR/CI/handoff phase**

After diff audit:

- push supervised branch
- create or reuse a draft PR
- write PR body and verify template
- add or reuse review evidence comments
- wait for CI with `--ci-timeout`, default bounded timeout
- audit no deployment workflow ran
- write Human Review Handoff evidence

Report:

- phase `review_handoff`
- status `passed`
- PR URL/number/draft
- CI URL/conclusion/timeout
- comment URLs
- `human_merge_required=true`
- `merge=not_performed`
- `deployment=not_started`

**Step 4: Run tests**

Run:

```bash
go test ./internal/cli/commands -run 'TestRunWorkSuperviseE2ERunOnce' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/commands/work.go internal/cli/commands/work_supervise_e2e_test.go
git commit -m "feat(stage7): create supervised e2e handoff"
```

## Task 6: Idempotency, Recovery Evidence, And Final Report

**Domain Goal:** Make `run-once` safe to rerun without duplicate worker dispatch, duplicate PRs, or hidden side effects.

**Domain Rules Enforced:**
- Duplicate-dispatch claims are authoritative.
- Existing draft PRs are reused only if they match the same issue and branch.
- Final report preserves evidence and leaves human merge required.

**Why this matters:**
- A supervised E2E that cannot be safely retried is not operationally trustworthy.

**Files:**
- Modify: `internal/cli/commands/work.go`
- Modify: `internal/cli/commands/work_supervise_e2e_test.go`
- Modify: `docs/operations/staged-operational-proving.md` if the Stage 7 text needs a bounded-run-once note

**Step 1: Write failing idempotency tests**

Add tests:

```go
func TestRunWorkSuperviseE2ERunOnceSecondRunDoesNotDuplicateDispatchOrPR(t *testing.T)
func TestRunWorkSuperviseE2ERunOnceFinalReportRecordsZeroMergeDeploy(t *testing.T)
```

**Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/cli/commands -run 'TestRunWorkSuperviseE2ERunOnceSecondRun|FinalReport' -count=1
```

Expected: FAIL until idempotency/final report behavior is complete.

**Step 3: Implement idempotency and final reporting**

Ensure second run:

- reuses same claim key
- does not start a second worker if final report already passed
- reuses existing draft PR
- does not create duplicate review comments

Persist final artifacts:

```text
pr-report.json
ci-report.json
review-evidence.json
final-report.json
```

**Step 4: Run tests**

Run:

```bash
go test ./internal/cli/commands ./internal/runtime/supervision ./internal/store/sqlite -run 'Supervise|Supervision|Stage7|E2E' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/commands/work.go internal/cli/commands/work_supervise_e2e_test.go docs/operations/staged-operational-proving.md
git commit -m "feat(stage7): make supervised e2e idempotent"
```

## Task 7: Real Odin Proof And Proof Document

**Domain Goal:** Prove the bounded supervised E2E through the real `./bin/odin` command path and record honest evidence.

**Domain Rules Enforced:**
- Real operator path verification is required.
- Live writes are limited to one issue, one draft PR, review comments, and branch push.
- No merge or deploy occurs.
- Overnight 24/7 remains unproven.

**Why this matters:**
- Tests cannot prove the operator can safely run the live supervised delivery path.

**Files:**
- Create: `docs/operations/stage-7-supervised-e2e-proof-2026-05-01.md`
- Test/verify: `./bin/odin`

**Step 1: Run focused tests**

Run:

```bash
go test ./internal/store/sqlite ./internal/runtime/supervision ./internal/cli/commands ./internal/tracker/...
```

Expected: PASS.

**Step 2: Build**

Run:

```bash
make build
```

Expected: PASS.

**Step 3: Run controlled fixture command proof**

Use a temp `ODIN_ROOT` and local fake GitHub endpoint:

```bash
export ODIN_ROOT="$(mktemp -d)/runtime"
export ODIN_GITHUB_API_BASE_URL="http://127.0.0.1:<local-fake-github-port>"
export GITHUB_TOKEN="<synthetic-token>"

./bin/odin work supervise e2e prepare-issue --project odin-core --json
./bin/odin work supervise e2e run-once --project odin-core --issue <number> --json
```

Expected: PASS with no token leaks and no merge/deploy.

**Step 4: Run live proof only after fixture proof passes**

Use the real GitHub API and the operator-approved token:

```bash
export ODIN_ROOT="$(mktemp -d)/runtime"
unset ODIN_GITHUB_API_BASE_URL

./bin/odin work supervise e2e prepare-issue --project odin-core --json
./bin/odin work supervise e2e run-once --project odin-core --issue <number> --json --ci-timeout 15m
```

Expected:

- one live issue created
- one draft PR created
- CI reaches success
- review evidence comments exist
- human merge required
- merge not performed
- deploy not started

**Step 5: Write proof doc**

Record:

- issue URL
- PR URL
- run ID
- planned path
- commands run
- CI URL/conclusion
- review comment URLs
- SQLite counts and claim key
- merge/deploy zero-action proof
- explicit unproven boundary: no overnight daemon yet

**Step 6: Commit**

```bash
git add docs/operations/stage-7-supervised-e2e-proof-2026-05-01.md
git commit -m "docs(stage7): record supervised e2e proof"
```

## Final Review Checklist

- Domain naming matches `CONTEXT.md`: **Supervised Agency Mode**, **Agency Orchestrator**, **Issue Intake Source**, **Work Item**, **Run Attempt**, **Human Review Handoff**.
- Invariant coverage exists for exact issue, labels, exact path, kill switch, claim idempotency, no token leak, no merge, and no deploy.
- ADR 0001 is honored: mutable runtime state persists in SQLite; artifacts do not outrank SQLite.
- Brownfield ADR is honored: existing `odin work`, tracker, supervision, PR, CI, and worktree primitives are reused.
- Boundary crossings are explicit: GitHub is intake/PR evidence, worker execution is bounded, SQLite owns runtime state.
- Any live failure leaves evidence for human inspection and does not trigger automatic cleanup.
- Overnight 24/7 mode remains unproven and must not be claimed by this bounded run-once proof.
