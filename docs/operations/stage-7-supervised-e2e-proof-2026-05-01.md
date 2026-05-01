# Stage 7 Supervised E2E Proof - 2026-05-01

## Current State

Stage 7 supervised E2E is implemented as a bounded
`odin work supervise e2e run-once` path. The latest proof completed with a
disposable clone, local bare remote, fake Codex executable, fake GitHub API, and
real `bin/odin` command execution.

This proof intentionally did not mutate live GitHub and did not run an overnight
24/7 daemon.

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

- Live GitHub PR creation was not exercised in this proof pass.
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

## Artifact Evidence

The run wrote these handoff artifacts:

```text
queue-report.json
worker-report.json
diff-report.json
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
- Stage 7 review evidence and human handoff comments can be posted.
- Odin E2E CI can be identified and waited on by the Stage 7 path.
- Merge, deployment, and scheduler dispatch remain blocked by the command path.
- Handoff artifacts preserve PR, CI, review evidence, final report, and boundary
  proof.

## Unproven

- Live GitHub mutation for Stage 7.
- Live external review-agent comments.
- Overnight supervised 24/7 operation.
- Any expansion beyond docs, prompts, fixtures, and non-sensitive tests.
- Any autonomous merge, deployment, scheduler dispatch, runner-code edit,
  workspace deletion, token-logic edit, CI-secret edit, or dashboard-auth edit.

## Best Operating Rule Going Forward

Run Stage 7 only as a supervised, human-gated, one-issue run-once flow until a
separate live proof records one low-risk draft PR, successful Odin E2E CI, review
evidence comments, and explicit no-merge/no-deploy evidence against real GitHub.
