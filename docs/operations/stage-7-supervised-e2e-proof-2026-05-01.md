# Stage 7 Supervised E2E Proof - 2026-05-01

## Current State

Stage 7 supervised E2E is implemented as a bounded
`odin work supervise e2e run-once` path. The latest proof completed with a
disposable clone, local bare remote, fake Codex executable, fake GitHub API, and
real `bin/odin` command execution.

This proof includes a passing controlled fixture run and two controlled live
GitHub attempts. The live attempts proved issue intake, worker execution, branch
push, draft PR creation, and evidence comments, but both failed closed waiting
for Odin E2E CI because remote `main` does not currently contain
`.github/workflows/odin-e2e.yml`.

This proof did not run an overnight 24/7 daemon.

## What Already Exists

- `odin work supervise start --json`
- `odin work supervise e2e prepare-issue --project <key> --json`
- `odin work supervise e2e run-once --project <key> --issue <number> --ci-timeout <duration> --json`
- Supervision queue and dispatch claim state in SQLite.
- Exact issue fetch through the existing GitHub tracker client.
- Safe worktree creation through existing VCS/worktree helpers.
- Codex command construction through the existing runner adapter.
- Draft PR handoff through existing PR, evidence-comment, CI-wait, and deployment-audit helpers.
- Artifact output under `ODIN_ROOT/runs/supervised-e2e/<run-id>/`.

## Gaps

- Live GitHub PR creation was exercised, but live Odin E2E CI success was not
  proven because the remote default branch lacks the Odin E2E workflow.
- Overnight 24/7 daemon operation was not exercised.
- Real external review agents were not invoked; fixture-backed review evidence
  comments were created through the GitHub API seam.
- Autonomous merge, deployment, scheduler dispatch, and reviewer execution remain
  intentionally unproven.

## Reuse Plan

The implementation reuses the existing `odin work` operator surface, tracker
adapter, supervision service, SQLite claim store, worktree helpers, Codex command
builder, PR template verifier, evidence-comment helpers, Odin E2E workflow
polling, and deployment audit logic.

## New Additions

- Stage 7 `e2e prepare-issue` and `e2e run-once` command wiring.
- Exact planned-scope validation for one `docs/operations/*.md` path.
- Duplicate active-claim preservation and reused-handoff provenance validation.
- Isolated worker credential environment so GitHub and app tokens are not
  exposed to Codex.
- Draft PR handoff reporting with branch, PR, CI, review evidence, deployment
  audit, and dispatch state.
- Stage 7-specific PR body and evidence comment provenance markers:
  - `<!-- odin-stage7-supervised-e2e-review-evidence -->`
  - `<!-- odin-stage7-supervised-e2e-human-review-handoff -->`
- Handoff artifacts:
  - `pr-report.json`
  - `ci-report.json`
  - `review-evidence.json`
  - `final-report.json`

## Why New Additions Are Necessary

Stage 7 needs a supervised delivery path that can prove one exact docs-only
issue-to-draft-PR handoff without granting merge, deploy, scheduler, runner,
workspace deletion, token, or CI-secret authority.

The additional handoff artifacts are necessary because the operator needs stable,
machine-readable proof fragments for PR state, CI state, review evidence, and
boundary conditions instead of relying only on console output.

## Real Odin E2E Verification

Focused tests:

```bash
go test ./internal/store/sqlite ./internal/runtime/supervision ./internal/cli/commands ./internal/tracker/...
```

Result: passed.

Build:

```bash
make build
```

Result: passed.

Controlled real `odin` proof:

```bash
export ODIN_ROOT="/tmp/odin-stage7-final-proof-v5nw7nf3/runtime"
export ODIN_GITHUB_API_BASE_URL="http://127.0.0.1:<fake-github-port>"
export GITHUB_TOKEN="<synthetic-token>"
export PATH="/tmp/odin-stage7-final-proof-v5nw7nf3/fake-bin:$PATH"

/home/orchestrator/odin-os/bin/odin work supervise start --json

/home/orchestrator/odin-os/bin/odin work supervise e2e run-once \
  --project odin-core \
  --issue 907 \
  --ci-timeout 1s \
  --json
```

Result: both commands exited `0`.

The run directory was:

```text
/tmp/odin-stage7-final-proof-v5nw7nf3/runtime/runs/supervised-e2e/1777659621077626862
```

## Controlled Fixture Evidence

The fake GitHub API saw exactly these requests:

```text
GET  /repos/marcusgoll/odin-os/issues/907
GET  /repos/marcusgoll/odin-os/pulls?base=main&head=marcusgoll%3Aodin%2Fstage7-supervised-e2e%2Fissue-907-1777659621100297219&state=open
POST /repos/marcusgoll/odin-os/pulls
GET  /repos/marcusgoll/odin-os/issues/94/comments
POST /repos/marcusgoll/odin-os/issues/94/comments
POST /repos/marcusgoll/odin-os/issues/94/comments
GET  /repos/marcusgoll/odin-os/actions/runs?branch=odin%2Fstage7-supervised-e2e%2Fissue-907-1777659621100297219
```

Forbidden request check:

```json
[]
```

No merge endpoint, deployment endpoint, scheduler dispatch, PR creation outside
the one draft PR, or unexpected label/comment path was requested.

## Token Boundary Evidence

The fake Codex executable captured its environment and confirmed:

```json
{
  "has_github_token": false,
  "has_gh_token": false,
  "has_api_token": false,
  "has_tradeboard_token": false,
  "token_leak": false
}
```

## Final Report Summary

```json
{
  "mode": "supervised_e2e",
  "phase": "review_handoff",
  "status": "passed",
  "project_key": "odin-core",
  "repo": "marcusgoll/odin-os",
  "run_id": "1777659621077626862",
  "issue": {
    "number": 907,
    "url": "https://github.example/marcusgoll/odin-os/issues/907"
  },
  "planned_path": "docs/operations/stage-7-final-proof.md",
  "claim_key": "stage7_supervised_agency:odin-core:907",
  "branch": "odin/stage7-supervised-e2e/issue-907-1777659621100297219",
  "diff_file": "docs/operations/stage-7-final-proof.md",
  "diff_sha256": "4f81591c77ffef3afd952429ead4b3a17ad91e1f861d69e8deeb2e776f4133e7",
  "branch_sha": "7ea4a3bd6d8bd82ea9ea9531f0b1bc38c76bd351",
  "pr": {
    "number": 94,
    "url": "https://github.example/marcusgoll/odin-os/pull/94",
    "draft": true,
    "created": true
  },
  "ci": {
    "waited": true,
    "timed_out": false,
    "url": "https://github.example/marcusgoll/odin-os/actions/runs/10",
    "status": "completed",
    "conclusion": "success"
  },
  "codex_execution": "completed",
  "prs": "draft_created",
  "merge": "not_merged",
  "deployment": "not_started",
  "dispatch": "not_started",
  "human_merge_required": true
}
```

The command output included the Stage 7 provenance marker and the
`odin work supervise e2e run-once` command provenance. It did not include the
old Stage 6 marker or the old `odin work pr-create` provenance.

## Live GitHub Attempts

Live issue created:

```text
https://github.com/marcusgoll/odin-os/issues/104
```

First live attempt:

```text
run_id=1777659483168948475
branch=odin/stage7-supervised-e2e/issue-104-1777659483516495224
pr=https://github.com/marcusgoll/odin-os/pull/105
status=failed_closed
reason=timed out waiting for Odin E2E CI after 15m0s
```

Evidence comments:

```text
https://github.com/marcusgoll/odin-os/pull/105#issuecomment-4360897407
https://github.com/marcusgoll/odin-os/pull/105#issuecomment-4360897452
```

This attempt exposed that the branch had been based on a stale local `main`
that did not include the Odin E2E workflow. The command still failed closed and
reported `merge=not_merged`, `deployment=not_started`, and
`dispatch=not_started`.

Corrective code change:

```text
9945dea fix(stage7): base supervised branches on remote main
```

Second live attempt:

```text
run_id=1777660680152919026
branch=odin/stage7-supervised-e2e/issue-104-1777660680480814998
pr=https://github.com/marcusgoll/odin-os/pull/106
status=failed_closed
reason=timed out waiting for Odin E2E CI after 15m0s
```

Evidence comments:

```text
https://github.com/marcusgoll/odin-os/pull/106#issuecomment-4360998211
https://github.com/marcusgoll/odin-os/pull/106#issuecomment-4360998263
```

PR #106 had normal `ci` checks pass, but no Odin E2E workflow run appeared.
Direct remote verification showed:

```bash
gh api repos/marcusgoll/odin-os/contents/.github/workflows/odin-e2e.yml?ref=main
```

Result: GitHub returned `404 Not Found`. Because `.github/workflows/` is a
forbidden path for Stage 7 supervised mode, this proof did not add or modify the
workflow live.

## Artifact Evidence

The controlled fixture run wrote these handoff artifacts:

```text
queue-report.json
worker-prompt.md
worker-command.json
worker-output.txt
diff-summary.md
pr-report.json
ci-report.json
review-evidence.json
final-report.json
```

The fragment artifacts each recorded the same boundary state:

```json
{
  "merge": "not_merged",
  "deployment": "not_started",
  "dispatch": "not_started",
  "human_merge_required": true
}
```

`pr-report.json` recorded draft PR #94 as created. `ci-report.json` recorded
Odin E2E CI as completed successfully. `review-evidence.json` recorded both
Stage 7 evidence comment markers as created.

## Proven

- A supervised run can claim exactly one eligible issue.
- A duplicate active claim is preserved instead of launching a second worker.
- The worker prompt is scoped to one planned docs path.
- Codex receives no GitHub or app tokens in its environment.
- A worker diff can be audited and pushed to a branch on a local bare remote.
- A draft PR can be created through the GitHub client seam.
- Live GitHub can create draft PRs and Stage 7 evidence comments.
- Odin E2E CI can be identified and waited on by the Stage 7 path when the
  workflow exists on the target branch.
- Merge, deployment, and scheduler dispatch remain blocked by the command path.
- Handoff artifacts preserve PR, CI, review evidence, final report, and boundary
  proof.

## Unproven

- Live Odin E2E CI success for Stage 7, blocked by the missing remote workflow.
- Live external review-agent comments.
- Overnight supervised 24/7 operation.
- Any expansion beyond docs, prompts, fixtures, and non-sensitive tests.
- Any autonomous merge, deployment, scheduler dispatch, runner-code edit,
  workspace deletion, token-logic edit, CI-secret edit, or dashboard-auth edit.

## Best Operating Rule Going Forward

Run Stage 7 only as a supervised, human-gated, one-issue run-once flow. Before
the next live pass, promote `.github/workflows/odin-e2e.yml` to remote `main`
through a separate human-reviewed path; do not let supervised mode edit workflow
files to satisfy its own CI prerequisite.
