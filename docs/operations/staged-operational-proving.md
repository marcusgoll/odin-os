---
title: Staged Operational Proving
status: active
date: 2026-04-30
---

# Staged Operational Proving

After all 20 Odin implementation prompts are merged, do not promote Odin OS straight into a full autonomous 24/7 agent system.

This proving plan is the operator gate between merged implementation work and live agency operation. Each stage must preserve Odin's existing authority boundaries: no autonomous merge, no production deploy, no hidden scheduler authority, and no live external mutation unless the stage explicitly permits it and records the proof.

## Operating Rules

- Run stages in order.
- Do not skip a stage because lower-level tests passed.
- Do not infer live safety from fixture-only proof.
- Do not start unattended scheduler dispatch until a later stage explicitly authorizes it.
- Treat GitHub, Codex, PR creation, and scheduler dispatch as separate risk boundaries.
- Record proof artifacts before promotion.
- Promotion to the next stage requires an explicit human decision.

## Stage 0: Local Fixture E2E

Goal:

- No live GitHub.
- No live Codex.
- No PR creation.
- No scheduler dispatch.

Checks:

```bash
make odin-e2e-local
```

Stage 0 also requires doctor proof before exit. If doctor is not yet included in the fixture E2E command, run it separately and record the result:

```bash
./bin/odin doctor --json
```

Exit criteria:

- E2E scenarios pass.
- Doctor passes.
- Fixture-backed intake works.
- Failure analysis works.
- Prompt rendering includes brownfield guardrails.

Required artifacts:

- `.odin/e2e/run-metadata.json`
- `.odin/e2e/latest.json`
- `.odin/e2e/latest.log`

Allowed activity:

- Fixture-backed GitHub issue intake.
- Temporary `ODIN_ROOT` runtime state.
- Local prompt rendering checks.
- Local failure classification checks.
- Local workspace path safety checks.

Forbidden activity:

- Live GitHub reads or writes.
- Live Codex execution.
- Pull request creation.
- Scheduler dispatch.
- Long-running 24/7 service operation.
- Production or external-system mutation.

Promotion rule:

Stage 0 is complete only when the latest E2E artifacts and doctor output show passing results from the current checkout. Passing Stage 0 does not authorize live GitHub intake, Codex execution, PR creation, scheduler dispatch, or unattended operation.

## Stage 1: Live GitHub Read-Only Proof

Goal:

- Use a live GitHub token for read-only operations only.
- No GitHub writes.
- No GitHub comments.
- No GitHub label mutation.
- No pull request creation.
- No scheduler dispatch.

Preconditions:

- Stage 0 has passed from the current checkout.
- The `odin-core` system project is registered with GitHub intake metadata for this profile.
- `ODIN_PROFILE=github-readonly` resolves to a profile that permits GitHub issue reads only.
- `ODIN_DRY_RUN=true` is enforced as no external mutation.
- The GitHub token has the minimum read permission needed for issue intake.
- Logs and errors redact token values before writing to stdout, stderr, files, events, or reports.

Command:

```bash
ODIN_PROFILE=github-readonly \
ODIN_DRY_RUN=true \
./bin/odin work intake --project odin-core --json
```

Compatibility decision:

Stage 1 uses the existing Delivery Workflow operator surface:
`odin work intake --project <key> --json`. Do not add a parallel top-level
`odin intake github` command for Stage 1. GitHub remains the provider selected
by the enrolled GitHub-backed project manifest and `internal/tracker/intake`
service, while `odin work ...` remains the canonical operator command family
for Delivery Workflow intake.

Stage 1 targets the existing `odin-core` system project identity. Do not add a
second `odin-os` managed project key for the same repository. The reserved
system project may carry GitHub intake metadata for read-only issue sync
without weakening its self-governance constraints.

For this stage, dry-run means external-mutation dry-run, not local-runtime
dry-run. `ODIN_DRY_RUN=true` must block GitHub writes, comments, labels, pull
requests, scheduler dispatch, Codex execution, and other external-system
mutation. It must still allow local Odin runtime persistence of fetched
external issues into SQLite so idempotence can be proven.

Exit criteria:

- Eligible issues are fetched from live GitHub.
- Labels are filtered correctly.
- External issues are persisted idempotently in Odin runtime state.
- The command performs two read-only sync passes in one Stage 1 JSON invocation.
- The JSON output reports `first_pass`, `second_pass`, `stored_before`, `stored_after`, `idempotent=true`, and `github_writes=0`.
- `github_writes=0` is proven by an Odin-owned GitHub HTTP method audit, not inferred from flags or operator observation.
- The method audit reports read count, write count, and any forbidden method/path attempted without token values.
- No Work Items, Run Attempts, approvals, worktree leases, pull request handoffs, scheduler jobs, or dispatch state were created.
- No GitHub writes occurred.
- No comments were created.
- No labels were added, removed, or changed.
- No pull requests were created or updated.
- No scheduler dispatch occurred.
- Tokens are redacted in logs, errors, reports, and events.

Required artifacts:

- Command JSON output.
- Runtime log excerpt with token redaction verified.
- External issue persistence evidence from before and after the built-in repeated sync passes.
- GitHub HTTP method audit showing zero POST, PATCH, PUT, or DELETE requests.
- Operator note identifying the token scope used without recording the token value.

Allowed activity:

- Live GitHub issue reads.
- Local filtering of labels.
- Local Odin SQLite persistence of external issue records.
- Local idempotence verification.
- Local log redaction verification.

Forbidden activity:

- GitHub POST, PATCH, PUT, or DELETE requests.
- GitHub comments.
- GitHub label mutation.
- Pull request creation or update.
- Work Item creation or reconciliation.
- Run Attempt creation.
- Approval, worktree lease, pull request handoff, scheduler job, or dispatch-state creation.
- Codex execution.
- Worker launch.
- Scheduler dispatch.
- Production or external-system mutation outside the GitHub read.

Promotion rule:

Stage 1 is complete only when the exact live read-only command proof is
recorded and the built-in repeated sync passes show stable, idempotent local
persistence with zero GitHub writes. Passing Stage 1 does not authorize Codex
execution, pull request creation, scheduler dispatch, or unattended operation.

## Stage 2: Dry-Run Lifecycle Mutation

Goal:

- Pretend to mark issues running, human-review, and failed without writing to GitHub.
- Prove lifecycle state transition planning before live issue mutation.
- Keep scheduler dispatch disabled.
- Keep PR creation disabled.
- Keep Codex worker execution disabled.

Command:

```bash
ODIN_DRY_RUN=true \
./bin/odin work simulate-lifecycle --issue 123 --json
```

Implementation note:

The binary exposes `odin work simulate-lifecycle` as the canonical Stage 2 operator path. It plans lifecycle label/comment writes through Odin's Delivery Workflow surface and reuses tracker lifecycle labels without adding a parallel top-level `odin tracker ...` operator surface.

Exit criteria:

- Planned writes are logged.
- Actual GitHub HTTP requests are zero: `reads=0`, `writes=0`.
- Token redaction is verified.
- State transitions are valid.
- No comments are created.
- No labels are changed.
- No PRs are created or updated.
- No scheduler dispatch occurs.

Required artifacts:

- Command JSON output listing planned lifecycle writes.
- Planned lifecycle writes must be exactly:
  1. Add label `odin:running`.
  2. Add label `odin:human-review`.
  3. Add label `odin:failed`.
  4. Add an issue comment with the failure reason.
- Stage 2 must not plan or execute blocked, done, issue close, follow-up issue creation, PR creation/update, scheduler dispatch, or Codex execution behavior.
- Zero-request proof from the tracker adapter or request recorder.
- Redaction proof showing no token value in output, logs, events, or reports.
- State transition validation output.

Promotion rule:

Stage 2 is complete only when dry-run lifecycle planning proves the exact intended writes and zero actual GitHub mutation.

## Stage 3: Live GitHub Controlled Mutation

Goal:

- Use one test issue.
- Use one test repository.
- Use known Odin labels.
- Require manual approval before running.

Commands:

```bash
ODIN_DRY_RUN=true \
./bin/odin work simulate-lifecycle --issue 123 --json

./bin/odin work apply-lifecycle --issue 123 --json
```

Implementation note:

`simulate-lifecycle` remains dry-run only. `apply-lifecycle` is the live-only Stage 3 mutation path and must emit a report comparable to the dry-run report.

Stage 3 uses a Stage 3-specific lifecycle plan for dry-run/live comparison:

1. Add label `odin:running`.
2. Remove label `odin:running`.
3. Add label `odin:human-review`.
4. Add one Odin comment.

The Stage 3 plan must not include `odin:failed`. The Stage 2 proof artifact remains historical evidence for the broader failed-path dry-run planner.

Stage 3 idempotency means a rerun performs zero additional GitHub mutation writes once the final label state and Odin comment already exist. The live command must read current issue labels and comments, compute only missing actions, and skip all label/comment writes on an already-complete issue. The Odin comment must include the stable marker `<!-- odin-stage3-lifecycle-proof -->` so reruns can detect the existing proof comment without creating duplicates.

Stage 3 approval is command-local and target-bound. The live command must carry an explicit approved target, for example:

```bash
./bin/odin work apply-lifecycle \
  --issue 123 \
  --approved-target marcusgoll/odin-os#123 \
  --json
```

The JSON report must include an `approval` object with the approved repo, issue, planned lifecycle payload, operator source `command_flag`, and timestamp. Stage 3 must not create durable `approvals`, Work Items, Run Attempts, scheduler state, PR handoff state, or dispatch state.

Stage 3 targets the existing `odin-core` GitHub repo, `marcusgoll/odin-os`; do not add a second project key or separate test repository for this stage. The live command must fail before any write unless `--approved-target` exactly matches the resolved repo and issue number.

Stage 3 must use a pre-existing manually selected test issue. Odin must not create a GitHub issue as part of this proof.

Allowed:

- Add Odin labels on one test issue.
- Remove Odin labels on one test issue.
- Add one Odin comment.

Forbidden:

- GitHub issue creation.
- Pull request creation.
- Codex worker execution.
- Scheduler dispatch.
- Mutation of any issue other than the approved test issue.
- Mutation of any repository other than the approved test repository.

Exit criteria:

- Labels update correctly: the issue first receives `odin:running`, then moves to `odin:human-review` by removing `odin:running` and adding `odin:human-review`.
- The final issue retains `odin:human-review` and does not retain `odin:running`.
- Comment is correct.
- Idempotency works: first run writes only missing actions; second run performs zero label or comment mutation writes and creates no duplicate Odin comment.
- Dry-run and live behavior match expectations.
- Manual approval is recorded before mutation.
- Token redaction is verified in logs, errors, reports, and events.

Required artifacts:

- Manual approval record.
- Before and after issue label snapshots.
- Comment body and comment URL.
- Idempotency rerun output.
- Dry-run versus live comparison output.
- Zero PR and zero dispatch proof.

Promotion rule:

Stage 3 is complete only when a single approved live test issue is mutated exactly as planned and no other GitHub or Odin execution surface changes.

## Stage 4: Local Codex Worker Dry-Run

Goal:

- Render the worker prompt.
- Construct the Codex command in a safe workspace.
- Prove worker setup without GitHub mutation.
- Prove worker setup without PR creation.

Command:

```bash
ODIN_DRY_RUN=true \
./bin/odin work worker-dry-run --issue-fixture fixtures/issues/simple-doc-change.json --json
```

Implementation note:

The current binary does not expose `odin work worker-dry-run`. Stage 4 cannot be claimed complete until that command exists and maps to the existing prompt, worktree, and `codex_headless` worker primitives. Do not add a parallel top-level `odin worker ...` operator surface for Stage 4.

Stage 4 must not launch a local Codex process. It constructs a redacted `codex exec` command plan, renders the worker prompt, creates an isolated worktree, simulates bounded timeout behavior, and emits deterministic worker output containing `make odin-e2e-local`.

Stage 4 must create a real temporary Git worktree under the Odin worktree root using a dry-run branch name, prove the path is inside the root, and clean it up after the proof unless `--keep-worktree` is set. The JSON report must include worktree path, branch name, `inside_root=true`, creation status, and cleanup status.

The rendered prompt must include brownfield guardrails requiring:

- Audit existing repo state before editing.
- Reuse existing Odin commands, services, contracts, registries, schemas, docs, and tests.
- Do not create parallel command surfaces, registries, or sidecar tools.
- Preserve `odin work ...` as the canonical Delivery Workflow operator surface.
- Do not expose tokens, credentials, or live secrets.
- Do not create or update PRs.
- Run or report `make odin-e2e-local` before final output.

The JSON report must include guardrail coverage booleans instead of dumping the full prompt.

Stage 4 must both redact token-like values from JSON, logs, reports, and artifacts and construct a sanitized Codex environment plan that excludes token-bearing environment variables. The JSON report must include `redaction.token_values_exposed=false` and `environment.excluded_token_env=["GITHUB_TOKEN","GH_TOKEN","API_TOKEN","ODIN_TRADEBOARD_API_TOKEN"]`.

Stage 4 is command-local proof. It must not persist Work Items, Run Attempts, approvals, scheduler jobs, PR handoff state, or durable worktree lease rows. Durable worker/dispatch state is deferred to a later stage.

Exit criteria:

- Worktree is created safely.
- Prompt is rendered.
- Codex command is constructed safely.
- No token exposure occurs.
- Timeout behavior works.
- E2E command appears in worker final output.
- No GitHub mutation occurs.
- No PR is created or updated.
- No Work Items, Run Attempts, approvals, scheduler jobs, PR handoff state, or durable worktree lease rows are created.

Required artifacts:

- Command JSON output.
- Worktree path and inside-root proof.
- Rendered prompt metadata or redacted prompt excerpt.
- Redacted Codex command plan.
- Timeout proof.
- Worker final output containing the required E2E command.

Promotion rule:

Stage 4 is complete only when local worker dry-run proves prompt rendering, safe command construction, timeout handling, and zero external mutation.

## Stage 5: PR Creation Dry-Run

Goal:

- Let a worker produce branch changes.
- Let Odin plan a Human Review Handoff draft.
- Do not push.
- Do not create or update a live PR.

Command:

```bash
ODIN_DRY_RUN=true \
./bin/odin work pr-dry-run --worktree <path> --base <branch> --json
```

Implementation note:

Stage 5 plans a read-only Human Review Handoff draft from local branch changes; it is not live PR creation. The command must generate a diff summary, PR body draft, human checklist, zero-push proof, zero-merge proof, and zero GitHub PR API write proof without pushing, creating or updating a live pull request, merging, or persisting durable PR handoff state.

The PR body draft must follow the repo PR body contract and include these headings exactly:

```markdown
## Summary
## Proven
## Unproven
## Commands Run
```

Stage 5 must validate the generated PR body with `scripts/ci/verify-pr-template.sh` before reporting success.

The human checklist must include at least:

```markdown
- [ ] Review diff summary
- [ ] Confirm tests listed under Commands Run are sufficient
- [ ] Confirm Unproven items are acceptable
- [ ] Confirm no push occurred during dry-run
- [ ] Confirm no live PR was created or updated
- [ ] Confirm no merge occurred
```

Stage 5 may persist local disposable draft artifacts under the Odin runtime artifact area, for example:

```text
<ODIN_ROOT>/runs/pr-dry-run/<id>/pr-body.md
<ODIN_ROOT>/runs/pr-dry-run/<id>/handoff-checklist.md
<ODIN_ROOT>/runs/pr-dry-run/<id>/diff-summary.md
```

The JSON report must include artifact paths and SHA-256 hashes. Each artifact must be labeled "draft artifact, not durable PR handoff state" and must not be treated as a live pull request.

Stage 5 consumes an existing local worktree and branch with changes against the requested base branch. The command must inspect that worktree, generate the diff summary and PR draft from its current diff, and fail clearly when there is no diff. Fixture-backed tests may create temporary changed worktrees, but `pr-dry-run` must not synthesize branch changes itself.

Exit criteria:

- Diff summary is generated.
- PR body is generated.
- Human checklist is included.
- PR body passes `scripts/ci/verify-pr-template.sh`.
- Local draft artifact paths and hashes are reported.
- No push happens in dry-run.
- No merge happens in dry-run.
- No live GitHub PR is created or updated.
- Scheduler dispatch remains disabled.

Required artifacts:

- Diff summary.
- PR body draft.
- Human review checklist.
- Local draft artifact paths and hashes.
- Git push zero-call proof.
- Merge zero-call proof.
- PR API zero-write proof.

Promotion rule:

Stage 5 is complete only when Odin can plan a PR handoff from local branch changes without pushing or mutating GitHub.

## Stage 6: Live PR Creation With Toy Issue

Goal:

- Use one low-risk docs-only issue.
- Create one live PR.
- Do not merge autonomously.
- Require human approval before any merge.

Allowed:

- Push one task branch for the approved toy issue.
- Create one PR for the approved toy issue.
- Add Odin-authored Stage 6 review evidence comments on the live PR.
- Run CI.

Forbidden:

- Autonomous merge.
- Production deploy.
- Codex reviewer or QA worker execution.
- Scheduler dispatch beyond the approved toy issue.
- Durable Run Attempt creation.
- Mutation of protected areas unless the approved toy issue explicitly includes them and a human approves.

Exit criteria:

- PR is created.
- Odin-authored Stage 6 review evidence comments exist on the PR.
- Human approval is required before merge.
- CI runs `make odin-e2e-local`.
- No merge occurs without human approval.
- No deploy occurs.

Required artifacts:

- Issue URL.
- Branch name.
- PR URL.
- Odin-authored Stage 6 review evidence comments.
- CI run URL showing `make odin-e2e-local`.
- Human approval gate evidence.

Promotion rule:

Stage 6 is complete only when a toy docs-only issue reaches live PR handoff with CI and Odin-authored review evidence while merge and deploy remain human-gated. Stage 6 review evidence comments are not full autonomous reviewer or QA Run Attempts and do not approve merge or deployment.

## Stage 7: 24/7 Supervised Mode

Goal:

- Run Odin continuously.
- Only process low-risk labeled issues.
- Keep concurrency at one task.
- Keep human approval required.
- Preserve no autonomous merge or deploy.

Config:

```yaml
orchestrator:
  maxConcurrentTasks: 1
  dryRun: false
  requireHumanApproval: true

scheduler:
  allowedLabels:
    - odin:ready
    - safety:low-risk

forbiddenAreas:
  - deploy/
  - internal/runner/
  - internal/workspace/
  - internal/security/
  - .github/workflows/
```

Exit criteria:

- Runs overnight without duplicate dispatch.
- Kill switch works.
- State recovers after restart.
- CI catches failures.
- No autonomous merge occurs.
- No autonomous deploy occurs.
- Forbidden areas remain untouched unless a later human-approved stage changes the policy.

Required artifacts:

- Runtime config snapshot with secrets redacted.
- Overnight run log.
- Duplicate-dispatch check.
- Kill-switch proof.
- Restart recovery proof.
- CI failure-detection proof.
- Merge and deploy zero-action proof.

Promotion rule:

Stage 7 is supervised operation, not full autonomy. Passing Stage 7 does not authorize autonomous merge, autonomous deploy, unrestricted labels, higher concurrency, or protected-area mutation.

## Reserved Later Stages

Later stages beyond Stage 7 must be added as explicit amendments to this document before use. Each later stage must define:

- goal
- allowed live surfaces
- forbidden live surfaces
- exact commands
- proof artifacts
- exit criteria
- rollback condition
- human approval required for promotion

No later stage is implied by this Stage 0 through Stage 7 plan.

## Stop Conditions

Stop the proving process immediately if any of these occur:

- `make odin-e2e-local` fails.
- Doctor reports an unexplained unhealthy state.
- Fixture-backed intake performs or attempts a live GitHub operation.
- A local check invokes live Codex without an explicit later-stage configuration.
- A workflow creates or mutates a pull request during Stage 0.
- Scheduler dispatch starts during Stage 0.
- E2E artifacts are missing or stale.
- Stage 1 performs any GitHub write request.
- Stage 1 persists a token value to logs, reports, runtime state, or artifacts.
- Stage 2 performs any GitHub write request.
- Stage 3 touches any issue or repository outside the approved test target.
- Stage 4 exposes a token value to Codex, logs, reports, runtime state, or artifacts.
- Stage 5 pushes a branch or creates a live pull request.
- Stage 6 merges or deploys without explicit human approval.
- Stage 7 dispatches duplicate work, ignores the kill switch, mutates a forbidden area, merges, or deploys autonomously.

## Relationship To Existing Readiness Gates

This document does not replace alpha readiness, cutover readiness, phase exit criteria, or the verification model. It adds the operational promotion order for proving the agency workflow after all 20 implementation prompts are merged.

Use the existing gates as supporting evidence:

- [Alpha Readiness](alpha-readiness.md)
- [Cutover Readiness Checklist](cutover-readiness.md)
- [Phase Exit Criteria](../contracts/phase-exit-criteria.md)
- [Verification Model](../contracts/verification-model.md)
