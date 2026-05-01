# Stage 7 Supervised E2E Proof - 2026-05-01

## Current State

Stage 7 supervised E2E is implemented as a bounded `odin work supervise e2e`
run-once path. The local proof completed with fixture-backed GitHub behavior and
real `./bin/odin` command execution. No live GitHub PR mutation was performed in
this proof pass.

## What Already Exists

- `odin work supervise e2e prepare-issue --project <key> --json`
- `odin work supervise e2e run-once --project <key> --issue <number> --ci-timeout <duration> --json`
- Supervision queue and dispatch claim state in SQLite.
- Exact issue fetch through the existing GitHub tracker seam.
- Safe worktree creation through existing VCS/worktree helpers.
- Draft PR handoff through existing PR, evidence-comment, CI-wait, and deployment-audit helpers.
- Artifact output under `ODIN_ROOT/runs/supervised-e2e/<run-id>/`.

## Gaps

- Live GitHub PR creation was not exercised in this proof pass.
- Overnight 24/7 daemon operation was not exercised.
- Autonomous merge, deployment, scheduler dispatch, and reviewer execution remain intentionally unproven.

## Reuse Plan

The implementation reuses the existing `odin work` operator surface, tracker
adapter, supervision service, SQLite claim store, worktree helpers, Codex command
builder, PR template verifier, evidence-comment helpers, Odin E2E workflow
polling, and deployment audit logic.

## New Additions

- Stage 7 `e2e prepare-issue` and `e2e run-once` command wiring.
- Exact planned-scope validation for one `docs/operations/*.md` path.
- Draft PR handoff reporting with branch, PR, CI, review evidence, deployment audit, and dispatch state.
- Stage 7-specific PR body provenance validation for new and reused draft PRs.
- Handoff artifacts:
  - `pr-report.json`
  - `ci-report.json`
  - `review-evidence.json`
  - `final-report.json`

## Why New Additions Are Necessary

Stage 7 needs a supervised delivery path that can prove one exact docs-only
issue-to-draft-PR handoff without granting merge, deploy, scheduler, runner,
workspace deletion, token, or CI-secret authority.

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

Local E2E:

```bash
make odin-e2e-local
make odin-e2e-contract
```

Result: both passed. The contract command was rerun after `make odin-e2e-local`
completed, because running it too early fails when `latest.log` has not been
created yet.

Guarded real `odin` command smoke:

```bash
export ODIN_ROOT="/tmp/tmp.1r2i2OoUTg/runtime"
export ODIN_GITHUB_API_BASE_URL="http://127.0.0.1:<fake-github-port>"
export GITHUB_TOKEN="<synthetic-token>"

./bin/odin work supervise e2e run-once \
  --project odin-core \
  --issue 904 \
  --ci-timeout 1ms \
  --json
```

Result: exited non-zero by design before worker behavior because the supervision
kill switch was active.

Observed final report summary:

```json
{
  "phase": "queued",
  "status": "refused",
  "codex_execution": "not_started",
  "prs": "not_created",
  "merge": "not_merged",
  "deployment": "not_started",
  "dispatch": "not_started",
  "human_merge_required": true,
  "queue": [
    {
      "project_key": "odin-core",
      "repo": "marcusgoll/odin-os",
      "issue_number": 904,
      "decision": "refused",
      "eligible": true,
      "refusal_reason": "kill_switch_active"
    }
  ],
  "claims": null,
  "ci": {
    "waited": false,
    "timed_out": false
  },
  "pr": {
    "number": 0,
    "url": "",
    "draft": false,
    "created": false,
    "reused": false
  },
  "evidence_comments": null
}
```

Fake GitHub request log:

```text
GET /repos/marcusgoll/odin-os/issues/904 auth=[REDACTED]
```

Token scan result:

```text
no synthetic token leak
```

## Remaining Risks

- The full live handoff path can push a branch, create a draft PR, and add PR
  comments. That live mutation was not run in this final proof pass.
- Real Codex worker behavior is still bounded by prompt, worktree, diff, and
  token audits, but only fixture-backed tests exercised the successful handoff.
- The first overnight supervised mode is not proven and should not be treated as
  a daemon-ready 24/7 rollout.

## Best Operating Rule Going Forward

Run Stage 7 only as a supervised, human-gated, one-issue run-once flow until a
separate live proof records one draft PR, successful Odin E2E CI, review
evidence comments, and explicit no-merge/no-deploy evidence.
