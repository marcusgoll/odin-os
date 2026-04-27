# Odin Orchestrator Capability Parity Audit

Date: 2026-04-24
Scope: legacy `/home/orchestrator/odin-orchestrator` runtime capabilities that must be preserved or explicitly retired before Odin OS becomes the sole operator runtime.

## Current State

Odin OS is the newer canonical target for new Odin development, but the live host is still running a mixed system:

- User service `odin-os.service` runs `/home/orchestrator/odin-os-live/bin/odin serve` on `127.0.0.1:9444`.
- `/home/orchestrator/odin-os-live` points at an older worktree, not the current `/home/orchestrator/odin-os` checkout.
- The live Odin OS binary is healthy for `healthz`, `readyz`, and `doctor`, but it does not expose the current checkout's `workspace` command.
- Root services from `odin-orchestrator` are still active: `odin-engine.service`, many `odin-worker-shim@*.service` units, `odin-dropbox.service`, `odin-slack-gateway.service`, `odin-content-fetcher.service`, and `odin-keepalive.timer`.
- Legacy `odin-orchestrator` is not fully healthy: the current strategist lease is stalling, historical `running` rows are stale, and keepalive logs show intermittent `jq` type errors.

The operating requirement is therefore parity before decommission: Odin OS should replace the older runtime only after it can cover the same useful capabilities with better observability, fewer stale states, and real operator-path proof.

## What Already Exists

### Odin OS foundations

- Canonical domain terms in `CONTEXT.md` for `Managed Project`, `Work Item`, `Run Attempt`, `Worker`, `Companion`, `Workflow`, `Policy Decision`, `Memory Record`, `Capability Catalog`, `Observability`, and `workspace`.
- Current checkout binary exposes `workspace`, `doctor`, `healthcheck`, `serve`, `jobs`, `runs`, `task`, `intake`, `transition`, `social`, `approvals`, and shell commands such as `/agent`, `/workflow`, `/skill`, `/tool`, `/jobs`, and `/runs`.
- Current checkout now exposes `odin tui` as the read-only terminal dashboard over the same canonical overview board as `/overview`.
- Current checkout registry exposes agents, workflows, skills, and tools for project intake, portal delivery, triage, browser evidence, project status, task listing, event logging, and social copilot workflows.
- Runtime store under `ODIN_ROOT=/home/orchestrator/.local/state/odin-os` contains tasks, runs, approvals, events, projects, worktree leases, executor health, and projection freshness.
- Live HTTP readiness is available at `http://127.0.0.1:9444/healthz` and `http://127.0.0.1:9444/readyz`.

### Legacy orchestrator capabilities

- Root systemd runtime: `odin-engine.service`, `odin-worker-shim@*.service`, `odin-keepalive.timer`, `odin-dropbox.service`, `odin-slack-gateway.service`, `odin-content-fetcher.service`, and legacy `odin-ohs.service` at the user level.
- Legacy `/var/odin/engine.db` owns task, run, lease, event, approval, memory, DAG, DAG step, and execution binding tables.
- Legacy task routing declares 47 task types, including PR automation, server maintenance, social workflows, analytics, n8n workflows, content creation, daily reporting, incident triage, security scans, and research.
- Legacy role routing declares 27 worker roles, including ops, devops, qa-lead, strategist, research, social-media-manager, perf, branch-ops, security, compliance, finance, marketing, and worker roles.
- Legacy scripts include concrete adapters for Slack, Dropbox, content fetching, GitHub intake, Gmail triage, Google APIs, n8n, media stack operations, finance helpers, Telegram, browser access, scheduler behavior, queue management, model routing, and self-heal/recovery.
- Legacy tmux sessions provide attachable execution lanes for worker shims, with active checkpoint and log files under `/var/odin/agents` and `/var/odin/logs`.

## Gaps

### Hard parity gaps

1. Live Odin OS binary drift: the service binary is older than the current checkout and lacks the current `workspace` operator surface.
2. Intake parity gap: legacy Slack, Dropbox, content-fetcher, and OHS services remain live; Odin OS does not yet visibly own equivalent operator-facing intake paths.
3. Scheduler parity gap: legacy has broad recurring task and maintenance scheduling; Odin OS currently shows a narrower workflow set.
4. Role and task routing gap: legacy has 47 routed task types and 27 role defaults; current Odin OS registry is much smaller.
5. Integration parity gap: Gmail, Google, n8n, media stack, finance, Telegram, and several maintenance helpers exist as legacy shell/Python adapters without clear Odin OS equivalents.
6. tmux parity gap: Odin OS has workspace tmux semantics, but legacy worker shim tmux execution is still active and not yet represented as Odin OS evidence.
7. Decommission proof gap: no real Odin OS E2E proof currently shows Slack/Dropbox/content-fetcher/OHS/maintenance/role-worker replacement.

### Legacy quality gaps that should not be copied

- Stale legacy `running` rows remain in `/var/odin/engine.db`.
- Current strategist lease is active but stalling.
- Legacy keepalive emits intermittent `jq` errors.
- Several legacy counters and local files disagree about worker status.
- Root `odin.service` is enabled but failed, while the useful legacy behavior is distributed across other units.

Parity means preserving useful capabilities, not preserving these failure modes.

## Reuse Plan

### Reuse from Odin OS

- Use `Managed Project`, `Work Item`, `Run Attempt`, `Worker`, `Companion`, `Workflow`, `Policy Decision`, `Memory Record`, `Capability Catalog`, `Observability`, `Execution Lane`, and `Provider Adapter` as the canonical model.
- Use the current `odin` CLI and shell commands as the operator surface.
- Use Odin OS task/run/project/approval/event/projection tables as the canonical state model for migrated capabilities.
- Use workspace tmux only as an attachable environment, not as runtime authority.
- Use existing browser and social tools where they already cover legacy social/browser evidence behavior.

### Reuse from legacy orchestrator during migration

- Keep active legacy services running while parity is incomplete.
- Treat legacy scripts as adapter candidates, not as new canonical architecture.
- Treat `/var/odin/engine.db`, `/var/odin/state.json`, `/var/odin/routing.json`, worker checkpoints, logs, and tmux sessions as read-only migration evidence until replaced.
- Reuse legacy task and role names as source inventory for the Odin OS capability catalog, then normalize them into Odin OS domain terms.

## New Additions

This audit adds one planning artifact:

- `docs/audits/2026-04-24-odin-orchestrator-capability-parity.md`

This phase also adds the first read-only bridge:

- `internal/runtime/legacy` reads legacy systemd unit state, `/var/odin/engine.db`, `/var/odin/state.json`, `/var/odin/routing.json`, worker checkpoints, referenced checkpoint logs, and tmux sessions without mutating legacy state.
- `odin legacy status` and `/legacy status` expose that bridge through the real Odin OS operator surface.
- `odin legacy capabilities` and `/legacy capabilities` expose a read-only parity registry from legacy service units, routing, backend defaults, role defaults, task-type registry records, schedules, and tool registry entries.

This phase restores the native terminal dashboard surface:

- `odin tui` provides a read-only live dashboard over the canonical Odin OS overview board.
- `odin tui --once` renders one frame for smoke checks and scripts.
- `internal/cli/render.RenderDashboardOverview` reuses the existing overview model with dashboard-only panels, lane limits, and text truncation so the watch surface stays scannable.
- `internal/cli/overview.Service` now includes recent native runtime events in the overview model so the `Activity Log` panel is backed by Odin OS state.
- `internal/adapters/github.PullRequestLister` reads configured GitHub-backed project repos through `gh pr list` and feeds the `GitHub PRs` panel through the overview model.
- `docs/contracts/tui-overview.md` now records `odin tui` as the top-level terminal dashboard entrypoint while preserving `/overview` as the shell entrypoint.

The remaining additions should be small and staged:

1. Add higher-level capability groups so broad records can be filtered into intake, scheduler, worker, provider, and observability slices.
2. One migration adapter at a time for live intake surfaces: OHS/GitHub, Dropbox, Slack, content fetcher.
3. Scheduler migration from legacy recurring jobs to Odin OS automation triggers that create `Work Item`s.
4. Worker role migration from legacy shim defaults to Odin OS workers, companions, policies, and executor routing.

## Why New Additions Are Necessary

- Without a legacy observability bridge, operators cannot see the remaining old runtime debt through the canonical Odin OS surface.
- Without a capability parity registry, Odin OS could appear healthier while silently dropping legacy operational capabilities.
- Without staged intake adapters, disabling legacy services would break real input paths.
- Without scheduler migration, recurring maintenance, reporting, and monitoring behavior would remain stranded in legacy shell code.
- Without worker role migration, the system would lose dispatch breadth even if the newer Odin OS runtime is cleaner.

## Capability Classification

| Legacy capability | Current evidence | Odin OS parity state | Classification | Required parity proof |
| --- | --- | --- | --- | --- |
| Engine queue, leases, DAGs | `odin-engine.service`, `/var/odin/engine.db` | Odin OS has tasks/runs/worktree leases but not legacy breadth | Migrate | Real `odin` shows equivalent work item, run attempt, lease, event, and recovery lifecycle |
| Worker shims and role workers | Active `odin-worker-shim@*.service`, 27 role defaults | Odin OS has workers/companions but smaller registry | Migrate | Real `odin` dispatches representative role work and records attempts/evidence |
| Model/backend routing | `/var/odin/routing.json`, role/task backend defaults | Odin OS has executor/provider concepts but no full parity matrix | Migrate | Real `odin` routes equivalent task classes through declared provider adapters |
| tmux execution lanes | Active `odin-strategist-1` tmux session and checkpoints | Odin OS has workspace tmux but not legacy worker-shim status | Bridge then migrate | Real `odin workspace status` or observability command reports legacy and native lanes |
| Slack intake | `odin-slack-gateway.service` | No proven Odin OS replacement | Keep until migrated | Real Slack-like intake creates Odin OS work item through `odin` path |
| Dropbox intake | `odin-dropbox.service` | No proven Odin OS replacement | Keep until migrated | File-drop event creates Odin OS work item through `odin` path |
| Content fetching | `odin-content-fetcher.service` | No proven Odin OS replacement | Keep until migrated | Content fetch creates Odin OS event/work item and durable evidence |
| OHS/webhook intake | `odin-ohs.service` still points to legacy script | Odin OS has HTTP serve but not proven OHS parity | Keep until migrated | Webhook/API event enters Odin OS intake and appears in `odin jobs/runs` |
| Scheduler/maintenance | legacy scheduler, keepalive, maintenance scripts | Odin OS has doctor/projections but not full scheduled parity | Migrate | Recurring task is created, executed, observed, and recovered by Odin OS |
| GitHub/PR automation | legacy task types and scripts for PR review/fix/rebase/merge | Odin OS has project intake and event logs, but not full PR task parity | Migrate | GitHub event or fixture runs through Odin OS work item lifecycle |
| Social/browser workflows | legacy social scripts; Odin OS social tools/workflow exist | Partial parity | Extend | Real `odin` social or browser evidence flow proves current intended surface |
| Gmail/Google/n8n/media/finance/Telegram | legacy helper scripts and configs | No full Odin OS parity shown | Inventory before migration | Each adapter has owner, policy boundary, and real fixture/live proof |
| Health/self-heal/keepalive | `odin-keepalive.timer`, engine watchdog logs | Odin OS doctor/projections exist; mixed-stack health not canonical | Bridge then migrate | Real `odin doctor/status` reports both native and remaining legacy risk |

## Real odin E2E Verification

Commands exercised during this audit:

```bash
cd /home/orchestrator/odin-os
./bin/odin doctor --json
./bin/odin healthcheck
./bin/odin workspace status --json
./bin/odin legacy status
./bin/odin legacy status --json
./bin/odin legacy capabilities
./bin/odin legacy capabilities --json
make install-local
/home/orchestrator/.local/bin/odin legacy status
/home/orchestrator/.local/bin/odin legacy capabilities
/home/orchestrator/.local/bin/odin tui --once
/home/orchestrator/.local/bin/odin help
/home/orchestrator/.local/bin/odin doctor --json
timeout 3s /home/orchestrator/.local/bin/odin tui --interval=1s --no-clear
printf '/agent list\n/workflow list\n/skill list\n/tool list\n/jobs\n/runs\nexit\n' | ./bin/odin
printf '/legacy status\nexit\n' | ./bin/odin
printf '/legacy capabilities\nexit\n' | ./bin/odin
```

Observed result:

- Current checkout `./bin/odin doctor --json` reported healthy.
- Current checkout `./bin/odin healthcheck` reported ready.
- Current checkout `./bin/odin workspace status --json` returned a valid `odin-core` workspace status.
- Current checkout shell listed registered agents, workflows, skills, tools, jobs, and runs.
- Current checkout `./bin/odin legacy status` reported legacy state as degraded with `services=31`, `failed_services=1`, `active_leases=3`, `checkpoints=3`, `tmux_sessions=3`, and legacy run counts including `running:9`.
- Current checkout `./bin/odin legacy capabilities` reported `capabilities=302`, `services=31`, `task_routes=46`, `role_defaults=27`, `backends=3`, `task_types=150`, `active_task_types=131`, `schedules=60`, `enabled_schedules=43`, and `tools=4`.
- `make install-local` updated `/home/orchestrator/.local/bin/odin` to `/home/orchestrator/odin-os/bin/odin`; the installed operator path also reported the same legacy degraded state.
- Installed `/home/orchestrator/.local/bin/odin tui --once` rendered the native Odin OS terminal dashboard with ASCII panels for `Odin OS`, `Initiatives`, `Work Items`, `Run Attempts`, `Companions`, `Intake Inbox`, `Approvals`, `Incidents`, `GitHub PRs`, `Activity Log`, `Memory`, and `Automation Triggers`.
- The panelled dashboard reported `health=healthy`, `initiatives=4`, `work_items=176`, bounded work item rows, overflow summary `... 164 more work_items`, and native Activity Log events such as `executor_health.recorded` and `registry_version.recorded`.
- The installed `GitHub PRs` panel reported `status=ok repositories=1 open=2` from `github.repo=marcusgoll/pbs`, including `marcusgoll/pbs#60` and `marcusgoll/pbs#56`.
- Direct verification with `gh pr list --repo marcusgoll/pbs --state open --limit 5 --json number,title,state,author,isDraft,headRefName,reviewDecision` returned the same two open PR records.
- Installed `/home/orchestrator/.local/bin/odin help` advertised `odin tui [--once]`.
- Installed `/home/orchestrator/.local/bin/odin doctor --json` reported `status=healthy`.
- Bounded live watch proof with `timeout 3s /home/orchestrator/.local/bin/odin tui --interval=1s --no-clear` produced repeated panelled dashboard frames with the live PR panel at `23:17:30Z`, `23:17:32Z`, and `23:17:33Z`; exit code `124` came from the deliberate timeout.
- Intake panel slice proof used an isolated runtime root with normalized JSON piped to `/home/orchestrator/.local/bin/odin intake enqueue`; the command recorded `intake_item=1 status=received workspace=default source=n8n kind=ci_failure dedupe_key=default:n8n:pbs-ci-main`.
- The same isolated runtime root proved `/home/orchestrator/.local/bin/odin intake list` returned `intake_items total=1` and the `project/pbs` intake row.
- The same isolated runtime root proved `/home/orchestrator/.local/bin/odin tui --once` rendered `Intake Inbox` as `wiring=live total=1 received=1` and Activity Log included `event=3 type=intake.item_created scope=project`.
- Current checkout `/legacy status` rendered the same bridge inside the interactive shell.
- Current checkout `/legacy capabilities` rendered the same parity registry inside the interactive shell.
- The installed operator path reported checkpoint evidence for `research-1`, `strategist-1`, and `tl-1`; all three referenced logs ended with `Your organization does not have access to Claude. Please login again or contact your administrator.`
- The installed operator path classified `slack_intake`, `dropbox_intake`, `content_fetcher`, and `webhook_intake` as `keep_until_migrated`; task routes, active task types, and roles as `migrate`; non-active task types and legacy tools as `inventory_before_migration`; and `keepalive_watchdog` as `bridge_then_migrate`.

Live-service checks:

```bash
curl -fsS http://127.0.0.1:9444/healthz
curl -fsS http://127.0.0.1:9444/readyz
/home/orchestrator/odin-os-live/bin/odin doctor --json
/home/orchestrator/odin-os-live/bin/odin workspace status --json
```

Observed result:

- Live Odin OS HTTP health and readiness were healthy.
- Live Odin OS doctor was healthy.
- Live Odin OS binary rejected `workspace` as an unknown command, confirming live binary drift from the current checkout.

Legacy verification was read-only through systemd, tmux, SQLite, and logs. No legacy services were stopped, restarted, or modified during this parity audit.

## Remaining Risks

- Some legacy capabilities are represented by scripts and configs rather than clear service boundaries; a script inventory alone does not prove current use.
- Some legacy database rows are stale or contradictory, so legacy state must be normalized before migration.
- Promoting the current Odin OS binary to the live service could improve operator surface parity, but it must be done as a separate deployment step with rollback.
- Keeping both runtimes alive avoids breaking inputs but increases cognitive load until Odin OS can report the mixed state canonically.
- Decommissioning root services before intake, scheduler, worker, and adapter parity is proven would risk losing operational behavior.

## Best operating rule going forward

Do not disable or delete a legacy `odin-orchestrator` unit, script, route, role, task type, or tmux execution path until it is classified in the parity registry and one of these is true:

1. Odin OS has a real `odin` E2E proof for the equivalent capability.
2. The capability is explicitly retired with an operator decision and no remaining live dependency.
3. The capability is legacy-only evidence and has been replaced by read-only Odin OS observability.

The migration target is not feature-for-feature preservation of implementation details. The target is capability parity or better: same useful operational coverage, canonical Odin OS state, real operator-surface proof, and fewer stale or contradictory runtime states.
