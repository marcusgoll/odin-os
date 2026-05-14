---
title: Failure Analysis Follow-Ups
status: active
updated: 2026-05-09
---

# Failure Analysis Follow-Ups

Failed work can recommend a follow-up when retrying alone is not enough. The
operator path is review-gated and dry-run first.

Inspect the failed work item:

```bash
odin review show failed-work:<task-id> --json
```

Preview the materialization without writing state:

```bash
odin review act failed-work:<task-id> follow-up --dry-run --json
```

Approve internal follow-up creation:

```bash
odin review act failed-work:<task-id> follow-up --json
```

The approval creates an internal Follow-Up Obligation. It does not create a
GitHub issue, does not call the GitHub API, and does not mutate external tracker
state. The JSON response keeps `github_issue.status` as `not_created` so the
operator can distinguish internal follow-up creation from external issue
materialization.

After approval, inspect the persisted obligation:

```bash
odin followup list --json
```

The JSON output includes `target_project_id` and `target_project_key` so the
operator can group failed-work follow-ups by owning project before deciding
whether to complete, snooze, or plan a repair slice.

For a production-readiness backlog check:

```bash
odin overview --json | jq '.actual_use'
odin followup list --json | jq '{count:(.obligations|length), by_project:([.obligations[].target_project_key] | group_by(.) | map({project:.[0], count:length}))}'
```

Use `retry` separately when retry policy allows it:

```bash
odin review act failed-work:<task-id> retry --json
```

Do not use this flow to bypass retry limits, human review, or the GitHub tracker
mutation contract.
