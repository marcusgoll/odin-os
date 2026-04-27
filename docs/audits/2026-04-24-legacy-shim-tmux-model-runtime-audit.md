# Legacy Shim, Tmux, and Model Runtime Audit

Date: 2026-04-24
Host: `gollahon-nas`
Audit window: approximately `2026-04-24T03:07Z` to `2026-04-24T03:10Z`
Mode: read-only live audit

## Current State

The accepted Odin OS domain mapping is now:

- legacy worker-shim behavior maps to bounded **Worker** execution under a **Run Attempt**
- legacy backend and model choices map to **Execution Lane** and **Provider Adapter** details
- tmux stays workspace attachment or adapter-local process machinery
- migrated legacy agent-status data belongs only in evidence or **Observability** views, not durable domain ownership

The live machine still runs both the legacy `odin-orchestrator` stack and the newer Odin OS sidecar:

- root `odin-engine.service` is active and running `/home/orchestrator/odin-orchestrator/engine/odin-engine`
- root `odin-worker-shim@*.service` units are active for many roles
- root `odin.service` is enabled but failed
- user `odin-os.service` is active and running `/home/orchestrator/odin-os-live/bin/odin serve`
- user `odin-ohs.service` is active but still runs `/home/orchestrator/odin-orchestrator/scripts/odin/odin-ohs.py`

The live Odin OS sidecar is healthy, but it is not the same checkout as `/home/orchestrator/odin-os`:

- live service path: `/home/orchestrator/odin-os-live`
- live symlink target: `/home/orchestrator/.config/superpowers/worktrees/odin-os/phase-23a-family-ops-shadow`
- live commit: `427c1e18dc5bc42dd2cda8996eb2571905de7e56`
- live branch: `codex/external-intake-cutover-snapshot`
- current repo path: `/home/orchestrator/odin-os`
- current repo commit: `99a339f5b3c247774660b89b1a4525ba6bb6c425`
- current repo branch: `codex/serve-lifecycle-cancel-fix`

The current repo binary exposes `odin workspace`. The live service binary does not; `/home/orchestrator/odin-os-live/bin/odin workspace status --json` returned `unknown command: workspace`.

## What Already Exists

Current Odin OS:

- `CONTEXT.md` defines **Work Item**, **Run Attempt**, **Worker**, **Execution Lane**, **Provider Adapter**, **Operator Surface**, and **Observability** language.
- `/home/orchestrator/odin-os/bin/odin doctor --json` reports healthy.
- `/home/orchestrator/odin-os/bin/odin workspace status --json` resolves `odin-core` to `odin-workspace-odin-core`, state `stopped`, workspace eligible.
- `/home/orchestrator/odin-os/bin/odin healthcheck` returns `ready`.
- live HTTP `http://127.0.0.1:9444/healthz` and `/readyz` report healthy.
- live Odin OS runtime root is `/home/orchestrator/.local/state/odin-os`.
- live Odin OS DB has tasks by status: `dead_letter=24`, `completed=13`, `failed=5`, `scheduled=1`.
- live Odin OS DB has runs by status: `interrupted=39`, `completed=20`, `failed=5`.
- recent live Odin OS runs include `social_copilot`, `codex_headless`, and one failed worktree lease conflict on `review-pbs-github-event-pull-request-opened-20260423-191715`.

Legacy `odin-orchestrator`:

- `/var/odin/engine.db` is active and being written.
- `/var/odin/routing.json` has `system_default=claude`, backends `claude`, `codex`, and `ollama-local`, and 27 role defaults.
- `/var/odin/state.json` still contains 15 `completion_unknown` dispatched tasks.
- `/var/odin/engine.db` task counts include `completed=2737`, `superseded=2220`, `dead_letter=1108`, `failed=229`, `planning=7`, `scheduled=4`, `awaiting_approval=1`, and `leased=1`.
- `/var/odin/engine.db` run counts include `failed=1538`, `completed=1401`, `queued=1180`, and `running=9`.
- one live tmux session exists: `odin-strategist-1`, attached count `0`, pane path `/home/orchestrator/odin-orchestrator`, pane command `bash`.
- one current active legacy lease exists: `strategic_review-1776996030486534326` leased to `strategist-1` through `2026-04-24T04:10:04Z`.

## Gaps

1. Two control planes are active. The legacy engine/shim stack and the Odin OS sidecar both run, but only Odin OS is the intended canonical future runtime.
2. The live Odin OS service is pinned to an older worktree and binary that lacks the current `workspace` operator surface.
3. Legacy `odin-orchestrator` execution state is not visible through current Odin OS observability. The active `strategist-1` lease and tmux session are outside Odin OS doctor, ready, jobs, and runs surfaces.
4. Legacy contract drift is visible: `/var/odin/agents/*/status.json` was not present even though the legacy contract describes those files, and `/var/odin/heartbeat` was absent while legacy engine/shims were still active.
5. Legacy SQLite state has stale-looking run authority: 9 runs are still marked `running` from April 1 and April 3, while their linked tasks are completed, superseded, or planning.
6. The active legacy `strategist-1` lane is repeatedly stalling. Recent journal lines show repeated `stall detected`, `stall exceeded max duration`, `agent session died`, and relaunch behavior around `strategic_review-*`.
7. Legacy watchdog behavior is noisy and partly unhealthy. `odin-engine` logs repeated watchdog tiers, shim restart signals, human alerts, and `heartbeat failed ... store: not found`; `odin-keepalive.service` logs intermittent `jq ... strptime/1 requires string inputs`.
8. Root `odin.service` remains enabled but failed, while adjacent legacy units continue to run.

## Reuse Plan

- Treat the legacy engine, shims, tmux sessions, `/var/odin` files, and `/var/odin/engine.db` as legacy Integration and Observability inputs only.
- Use the accepted Odin OS terms when discussing or migrating them:
  - legacy shim process -> **Worker** execution mechanism for one **Run Attempt**
  - legacy backend/model -> **Execution Lane** and **Provider Adapter** metadata
  - legacy tmux -> workspace attachment or adapter-local process machinery
  - legacy agent status/checkpoint/log -> **Run Attempt** evidence or **Observability** projection
- Keep current Odin OS SQLite and operator surfaces as the target runtime authority.
- Before any cutover or cleanup, classify each live legacy service as `keep_temporarily`, `migrate`, `observe_only`, `disable`, or `delete`.

## New Additions

This audit document only.

## Why New Additions Are Necessary

The live machine currently contradicts the simplified model that `odin-orchestrator` is only a migration source. It is still operationally active. A written audit is needed before proposing service cleanup, live binary promotion, observability bridging, or runtime migration work.

## Real odin E2E Verification

Commands run:

- `cd /home/orchestrator/odin-os && ./bin/odin doctor --json`
  - result: healthy
- `cd /home/orchestrator/odin-os && ./bin/odin workspace status --json`
  - result: `odin-core`, `odin-workspace-odin-core`, state `stopped`, workspace eligible
- `cd /home/orchestrator/odin-os && ./bin/odin healthcheck`
  - result: `ready`
- `curl -fsS http://127.0.0.1:9444/healthz`
  - result: healthy live Odin OS sidecar
- `curl -fsS http://127.0.0.1:9444/readyz`
  - result: healthy live Odin OS sidecar
- `cd /home/orchestrator/odin-os-live && /home/orchestrator/odin-os-live/bin/odin doctor --json`
  - result: healthy
- `cd /home/orchestrator/odin-os-live && /home/orchestrator/odin-os-live/bin/odin healthcheck`
  - result: `ready`
- `cd /home/orchestrator/odin-os-live && /home/orchestrator/odin-os-live/bin/odin workspace status --json`
  - result: `unknown command: workspace`

Additional read-only evidence commands included `systemctl list-units`, `systemctl show`, `systemctl cat`, `journalctl`, `tmux list-sessions`, `tmux list-panes`, `ps`, `find /var/odin`, `jq /var/odin/state.json`, `jq /var/odin/routing.json`, and read-only `sqlite3` queries against `/var/odin/engine.db` and `/home/orchestrator/.local/state/odin-os/data/odin.db`.

## Remaining Risks

- This audit did not stop, restart, disable, or mutate any live service.
- The legacy stack may be performing useful scheduled work; disabling it without classification could remove live operational coverage.
- The live Odin OS sidecar is healthy but not running the current repo binary, so current repo verification does not prove the deployed sidecar has the same operator surface.
- The absence of legacy heartbeat and agent status files may be intentional drift from the old contract, but it still means legacy file-state consumers cannot be trusted without further audit.
- The stale legacy `running` rows may be harmless historical drift, but they should not be treated as canonical work truth.

## Best operating rule going forward

Do not add new Odin OS behavior around legacy shim/tmux/model state until the live legacy services are explicitly classified. Any migration should either make legacy state read-only Observability evidence in Odin OS or retire it; it should not create a second durable runtime authority.
