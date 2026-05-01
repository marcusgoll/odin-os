---
title: Stage 7 Supervised E2E Design
status: approved
date: 2026-05-01
---

# Stage 7 Supervised E2E Design

## Current State

Stage 7 **Supervised Agency Mode** has a proven control-plane slice under the
canonical `odin work supervise ...` surface. It can report status, start, stop,
queue, and recover through the repo-owned `./bin/odin` binary. SQLite persists
control state, queue decisions, duplicate-dispatch claims, and recovery
observations.

What is not yet proven is the full supervised delivery path from a live
low-risk issue to a draft pull request with CI and review handoff evidence.

## Domain Source Of Truth

- `CONTEXT.md`
- `docs/adr/0001-canonical-authority.md`
- `docs/architecture/ADR-0001-brownfield-refactor-strategy.md`
- `docs/operations/staged-operational-proving.md`
- `docs/operations/stage-7-supervised-control-plane-proof-2026-05-01.md`
- `docs/plans/2026-05-01-stage-7-supervised-agency-control-plane-design.md`

## Locked Decisions

- Target repo: `odin-os`, through the `odin-core` project.
- Execution shape: bounded run-once E2E, not an overnight daemon.
- Issue source: Odin creates one controlled docs-only test issue in an explicit
  setup step.
- Worker execution: local Codex worker may run for this docs-only issue.
- Docs target: a unique dated proof note under `docs/operations/`.
- PR behavior: create a draft PR, wait for CI and review evidence, then stop.
- Merge and deploy: never performed by Odin in this stage.

## What Already Exists

- `odin work supervise status|start|stop|queue|recover --json`.
- `internal/runtime/supervision` for conservative config, eligibility, queue
  decisions, claims, and recovery reports.
- `internal/store/sqlite` as the mutable runtime authority.
- `internal/tracker/intake` and `internal/tracker/github` as the GitHub Issue
  Intake Source seam.
- Stage 4 worker dry-run prompt/worktree safety patterns.
- Stage 5 PR dry-run diff and PR body generation.
- Stage 6 live docs-only PR creation, bounded CI wait, and review evidence
  comment behavior.

## Gaps

- No command ties prepare-issue, queue, claim, worker execution, diff audit,
  draft PR creation, CI wait, review evidence, and Human Review Handoff into one
  supervised run.
- No durable artifact bundle exists for one full supervised E2E run.
- No exact-issue guard prevents a run-once command from silently discovering or
  processing a different issue.
- No exact-path audit binds the worker diff to the prepared issue's planned
  docs path.

## Reuse Plan

Keep the new behavior under the existing operator surface:

```bash
./bin/odin work supervise e2e prepare-issue --project odin-core --json
./bin/odin work supervise e2e run-once --project odin-core --issue <number> --json
```

Do not create a new scheduler, daemon authority, GitHub runtime authority, or
parallel registry. The run-once command orchestrates existing services and
records evidence; it does not become a general-purpose autonomous loop.

## Command Surface

### `prepare-issue`

`prepare-issue` creates exactly one live GitHub issue in `marcusgoll/odin-os`
with:

- labels: `odin:ready`, `safety:low-risk`
- title: `Stage 7 supervised E2E docs proof <run-id>`
- body containing the exact planned scope:
  `Planned scope: docs/operations/stage-7-supervised-e2e-<date>-<run-id>.md`
- body text that states no merge, no deploy, and human review required

The JSON report includes issue number, issue URL, run ID, planned path, and
side-effect fields showing no PR, merge, or deploy activity.

### `run-once`

`run-once` accepts only the explicit issue number from `prepare-issue`.

It:

1. Starts or validates **Supervised Agency Mode**.
2. Fetches and queues the exact issue.
3. Verifies labels and planned scope.
4. Creates or reuses one reserved duplicate-dispatch claim.
5. Creates an isolated worktree.
6. Runs local Codex with brownfield guardrails.
7. Audits the diff before any PR.
8. Creates a draft PR.
9. Waits for CI, including `make odin-e2e-local`.
10. Adds or verifies review evidence comments.
11. Records Human Review Handoff evidence.
12. Exits with merge and deploy reported as `not_performed`.

The command never loops over unrelated issues.

## Run Artifact Contract

Each run writes a redacted artifact bundle:

```text
ODIN_ROOT/runs/supervised-e2e/<run-id>/
  prepared-issue.json
  queue-report.json
  worker-prompt.md
  worker-command.json
  worker-output.txt
  diff-summary.md
  pr-body.md
  pr-report.json
  ci-report.json
  review-evidence.json
  final-report.json
```

Artifacts are evidence and cache, not runtime authority. Mutable state still
belongs in SQLite.

## Guards

Before worker execution:

- supervised mode is enabled or started by the command
- kill switch is inactive
- issue number matches `--issue`
- labels include `odin:ready` and `safety:low-risk`
- planned scope is exactly one path under `docs/operations/`
- no active unrelated claim exists

After worker execution and before PR creation:

- git diff contains exactly the planned docs file
- no `.github/`, `deploy/`, `internal/runner/`, `internal/workspace/`,
  `internal/security/`, dashboard auth, token, CI secret, or deployment path is
  touched
- no token-shaped string appears in diff, prompt, worker output, PR body, or
  JSON reports
- worktree state is suitable for a single docs-only PR

Before final success:

- draft PR exists
- CI reaches a successful terminal state
- review evidence comments exist
- Human Review Handoff is recorded
- merge is not performed
- deployment is not started

## Failure Handling

The E2E report carries a phase and status. Phases include:

- `prepared`
- `queued`
- `claimed`
- `worker_completed`
- `diff_verified`
- `pr_created`
- `ci_passed`
- `review_handoff`
- `failed`

Failure rules:

- Before worker: no worktree mutation and no PR.
- After worker but before PR: keep the worktree and artifact bundle for human
  inspection; do not create a PR.
- After PR: never merge; report the draft PR URL and failing phase.
- On claim conflict: refuse and do not start worker.
- On kill switch: refuse and do not start worker.
- On redaction failure: stop immediately; if before PR, do not create a PR.
- On CI failure or timeout: leave draft PR open, record failure evidence, and do
  not retry automatically.
- No automatic cleanup of live GitHub artifacts in the first E2E proof.

## Human Review Handoff

Final success means:

- draft PR exists
- CI evidence is recorded
- review comments exist
- final report says `human_merge_required=true`
- Work Item remains non-terminal until human action
- merge/deployment fields are `not_performed`

## Verification Strategy

Use layered verification:

1. Unit and service tests with fake GitHub, fake worker, and fake CI.
2. Real `./bin/odin` fixture proof with local fake GitHub.
3. Controlled live proof on one `odin-os` issue.
4. Proof doc recording exact commands, issue, PR, CI, review evidence, SQLite
   state, and unproven boundaries.

## Acceptance Criteria

`prepare-issue` passes when:

- exactly one live GitHub issue is created
- labels are `odin:ready` and `safety:low-risk`
- body includes exact planned docs path
- JSON output includes issue number, URL, planned path, and zero PR/merge/deploy
  activity
- no token appears in output or artifacts

`run-once` passes when:

- exact issue is fetched and queued
- one claim is created or reused idempotently
- worker edits only the planned docs file
- diff audit passes before PR creation
- draft PR is created
- CI includes `make odin-e2e-local` and reaches success
- review evidence comments exist
- final report says `human_merge_required=true`
- merge and deploy are `not_performed`
- a second run does not duplicate worker dispatch or create a second PR

## Remaining Risks

- Live proof may leave an open issue or draft PR if CI fails.
- This proves one bounded supervised delivery, not overnight 24/7 operation.
- This does not authorize autonomous merge, deploy, higher concurrency,
  protected-path mutation, or unrestricted issue creation.

## Best Operating Rule Going Forward

This E2E promotes Odin from control-plane proof to one supervised live delivery
proof. It does not promote Odin to unrestricted 24/7 autonomy.
