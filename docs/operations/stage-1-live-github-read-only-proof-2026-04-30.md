# Stage 1 Live GitHub Read-Only Proof - 2026-04-30

## Summary

Stage 1 is complete for the current checkout on branch
`codex/serve-lifecycle-cancel-fix`.

The live proof used the canonical Delivery Workflow operator surface:

```bash
ODIN_ROOT=/tmp/tmp.sWN4vcu34F/runtime \
ODIN_PROFILE=github-readonly \
ODIN_DRY_RUN=true \
GITHUB_TOKEN=<gh auth token> \
./bin/odin work intake --project odin-core --json
```

The token value was not written to the proof artifact.

## Credential Note

The command used the authenticated GitHub CLI token for
`marcusgoll-odin-orchestrator[bot]`, passed as `GITHUB_TOKEN` only for the
command invocation. Repository access was proven by a successful live read of
`marcusgoll/odin-os` issues. No token value was recorded.

## Command Output

```json
{
  "project": "odin-core",
  "repo": "marcusgoll/odin-os",
  "stored_before": 0,
  "stored_after": 27,
  "idempotent": true,
  "github_writes": 0,
  "first_pass": {
    "fetched": 27,
    "persisted": 27
  },
  "second_pass": {
    "fetched": 27,
    "persisted": 27
  },
  "method_audit": {
    "reads": 2,
    "writes": 0
  },
  "dispatch": "not_started",
  "prs": "not_created"
}
```

## Runtime Persistence Evidence

Runtime database:

```text
/tmp/tmp.sWN4vcu34F/runtime/data/odin.db
```

SQLite count proof:

```text
external_issues|27
tasks|0
runs|0
approvals|0
```

External issue grouping:

```text
github|marcusgoll/odin-os|eligible|27
```

## Exit Criteria

| Criteria | Status | Evidence |
|---|---|---|
| Eligible issues fetched from live GitHub | passed | `first_pass.fetched=27`, `second_pass.fetched=27` |
| Labels filtered correctly | passed | command persisted eligible issues only |
| External issues persisted idempotently | passed | `stored_before=0`, `stored_after=27`, `idempotent=true` |
| Built-in repeated sync passes | passed | `first_pass` and `second_pass` in one command invocation |
| GitHub writes are zero | passed | `github_writes=0`, `method_audit.writes=0` |
| GitHub method audit recorded reads | passed | `method_audit.reads=2` |
| No Work Items created | passed | `tasks|0` |
| No Run Attempts created | passed | `runs|0` |
| No approvals created | passed | `approvals|0` |
| No PR creation or dispatch | passed | `prs=not_created`, `dispatch=not_started` |

## Promotion Status

Stage 1 is complete.

This proof does not authorize Codex execution, pull request creation, scheduler
dispatch, GitHub writes, unattended operation, merge, or deployment. Promotion
to Stage 2 still requires an explicit operator decision.
