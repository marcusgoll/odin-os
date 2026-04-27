# Marcus Social Copilot Loop Operations

Use this runbook to operate the supported Marcus Social Copilot loop in `odin-os`.

## Current State

Supported path: `odin-os` Social Copilot loop through `/workflow social`, `/jobs`, `/runs`, and existing `/memory` commands.

Legacy path: any `odin-orchestrator` social media manager or worker is retired/reference-only. Do not repair or restart it as the future social runtime.

Default service state: `service.social_copilot.enabled=false` unless an operator intentionally enables it in `config/odin.yaml` or the deployed service config.

## Daily Check

From `/home/orchestrator/odin-os`:

```bash
./bin/odin
```

Then run:

```text
/workflow validate marcus-social-growth-workflow
/workflow use marcus-social-growth-workflow
/workflow social status
/jobs
/runs
```

Expected readback:

- `/workflow validate ...` reports `workflow=marcus-social-growth-workflow status=ready`.
- `/workflow social status` reports either `status=not_configured` or one scheduled job named `workflow-marcus-social-growth-workflow-social-copilot-loop`.
- `/jobs` must not show overlapping peer social jobs for the same workflow.
- `/runs` may show bounded `executor=social_copilot status=completed` runs.

Stop condition:
If `/jobs` shows multiple Marcus social polling jobs, stop and inspect SQLite/job metadata before waking the loop again.

## Configure Watch Scope

Whole-scope replacement is the v1 operator model:

```text
/workflow social scope replace marcus_own=timeline,mentions target=https://x.com/example/status/123 account=@AviationDaily
```

Supported target inputs:

- `marcus_own=timeline,mentions`
- `target=<x-or-twitter-status-url>`
- `account=<x-handle-or-profile-url>`
- `thread=<x-or-twitter-status-url>`

Forbidden target inputs:

- `marcus_own_*` as an operator-entered target outside `marcus_own=...`
- non-X URLs
- generic keyword monitoring
- hidden inferred audiences

## Manual Wake

Manual wake is explicit and bounded:

```text
/workflow social wake reason=<short-token>
```

Expected readback:

- `wake=manual status=completed`
- `executor=social_copilot status=completed` appears in `/runs`
- `account_actions=none`

Manual wake does not bypass cooldown. V1 has no `force`, `cooldown_bypass`, `like`, `repost`, `follow`, `dm`, or `publish` option.

## Approval Queue

Review queued Social Copilot recommendations through existing memory:

```text
/memory list type=social_research field.status=pending order=desc limit=5
/memory list type=social_draft field.approval=pending order=desc limit=5
/memory show <memory-id>
```

Approve or reject drafts through the existing approval path:

```text
/memory resolve <draft-id> result=approved
/memory resolve <draft-id> result=rejected reason=<kebab-case-tag>
```

Publishing remains operator-attended:

```text
/memory publish <outcome-id> via=huginn_x
```

LinkedIn remains manual. Odin may draft or record outcomes, but it must not automate LinkedIn browser actions.

## Forbidden Actions

The Social Copilot loop must not:

- autonomously publish
- autonomously reply
- like
- repost
- follow
- unfollow
- DM
- schedule content
- bypass approval
- bypass cooldown
- create a parallel social queue

If any wake creates a `social_outcome` with `publish_status=published`, treat it as a defect and stop the service.

## Service Enablement

The repo default is disabled:

```yaml
service:
  social_copilot:
    enabled: false
    workflow_key: marcus-social-growth-workflow
    cadence_seconds: 1800
```

Only enable after the operator has accepted the watch scope and confirmed that no legacy social worker owns the same responsibility.

After enabling and restarting the deployed `odin-os.service`, prove the supported path:

```text
/workflow use marcus-social-growth-workflow
/workflow social status
/jobs
/runs
/memory list type=social_outcome field.publish_status=published order=desc limit=5
```

Expected readback:

- one scheduled Social Copilot job
- bounded `social_copilot` runs only
- no newly published outcome unless an operator explicitly approved and published through `/memory publish`

## Cutover Rule

Do not claim the old social media manager is fixed. The supported replacement is the `odin-os` Social Copilot loop, and the proof path is the real `odin` command surface above.
