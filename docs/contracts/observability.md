# Observability Contract

## Purpose

Phase 10 defines the structured observability surfaces Odin uses for logs, health, metrics, and operator-facing projections. The approved homelab observability model adds the **Odin Observer Role** inside `odin serve`; it does not create a separate `odin-observer` daemon or a second runtime authority.

## Logging

Structured logs are JSON lines with:

- timestamp
- level
- component
- message
- correlation identifier
- scope
- optional project, task, and run identifiers
- structured fields

## Doctor

Doctor reports are structured and machine-parseable. They must distinguish:

- `healthy`
- `degraded`
- `failed`

Doctor is diagnostic reporting, not the readiness gate itself. `odin healthcheck` and `/readyz` own fail-closed Runtime Readiness decisions. `/overview` may summarize health and freshness cues through its Observability lane, but it must not become a second readiness authority.

## Metrics

Metrics are derived from canonical runtime state and exported in machine-readable text form by `odin serve`.

The existing `odin_*` metrics are compatibility metrics for Odin runtime and workflow state. They must remain available under their current names:

- `odin_active_runs`
- `odin_blocked_items`
- `odin_approvals_waiting`
- `odin_open_incidents`
- `odin_escalated_incidents`
- `odin_active_recoveries`
- `odin_queued_tasks`
- `odin_stale_executors`
- `odin_stale_sources`
- `odin_stale_projections`

New Odin host, service, lifecycle, backup, update, and readiness metrics use the `odin_os_*` family:

- `odin_os_health_score`
- `odin_os_status{status="healthy|degraded|critical|unknown"}`
- `odin_os_lifecycle_phase{phase="boot|initialize|run|maintain|backup|update|recover"}`
- `odin_os_telemetry_stale`
- `odin_os_backup_age_seconds`
- `odin_os_restore_test_age_seconds`
- `odin_os_updates_pending_total`
- `odin_os_security_updates_pending_total`
- `odin_os_reboot_required`
- `odin_os_systemd_failed_units_total`
- `odin_os_critical_service_up{service="..."}`
- `odin_os_critical_container_up{container="..."}`

The `odin_*` family remains the compatibility contract for runtime/workflow observability. The `odin_os_*` family is the additive contract for Odin-OS host/service/lifecycle observability. Do not silently rename compatibility metrics into the new family.

Task 1 collection defaults are conservative:

- `odin_os_health_score` is `100` when existing runtime-derived metrics show no degradation signals.
- `odin_os_health_score` is `80` when runtime-derived signals are degraded or telemetry samples are missing.
- `odin_os_status` is `healthy`, `degraded`, or `unknown` for the initial collection model.
- `odin_os_lifecycle_phase` is `run` while `odin serve` is serving metrics.
- `odin_os_telemetry_stale` is true when stale executor, source, or projection counts are nonzero, or when required telemetry samples are missing.
- Stale or missing telemetry forces `odin_os_status{status="unknown"}`.
- Fresh telemetry with active incidents (`open` or `escalated`) or running recoveries may render `odin_os_status{status="degraded"}`.
- Resolved incidents do not degrade Odin-OS health.
- Task 1 always renders the core OS metrics: health score, status, lifecycle phase, and telemetry stale.
- Task 1 does not perform direct systemd, Docker, package manager, or backup shell probes. Host fact metrics such as backup age, restore-test age, update counts, reboot-required, systemd-failed-unit counts, and critical service/container status render only when a snapshot or later approved collector explicitly populates them.

Prometheus, Loki, Grafana, and the Odin TUI are telemetry consumers or backends. They do not become canonical Odin runtime authorities. Stale or missing telemetry must render as `unknown`, not healthy.

## Odin TUI

The read-only terminal observability surface is:

```bash
odin tui --prometheus-url http://127.0.0.1:9090 --loki-url http://127.0.0.1:3100
```

`odin tui` refreshes continuously by default. Use `--once` for deterministic smoke checks and scripts, `--interval <duration>` to control the refresh interval, and `--no-clear` to render repeated frames without clearing the terminal. The command renders a boxed terminal cockpit with separate overview and recent-log panels so live SSH use stays scannable. It reads Prometheus instant query data for Odin status, health score, telemetry staleness, lifecycle phase, and active runs, and reads recent Odin-related entries from Loki's current `docker-containers` source when Loki is available. It must not shell out to host, systemd, Docker, node_exporter, or log files for canonical status.

Prometheus being unavailable, malformed, or missing any required Odin metric is controlled unavailable telemetry. The TUI must fail or render `UNKNOWN`; it must never silently report healthy. Loki being empty or unavailable is reported in the logs section without inventing host log data.

## Projections

Operator projections remain read-only and must not mutate runtime state.

Projection consumers may render derived status for readability, but canonical lifecycle ownership stays with the underlying domain object. Observability must not mint new Work Item, Run Attempt, Approval Request, or Follow-Up Obligation states.

`GET /mobile/operator-snapshot` is an authenticated, read-only `odin serve` projection for command-center frontends. It composes existing health/readiness, overview Observability lane, mobile review queue, approval, run, event, and browser handoff read models into one snapshot with `action_required`, `odin_health`, `live_execution`, `activity`, and `browser` sections. Snapshot rows may include labels, summaries, severity, source detail payloads, read-only command hints such as `odin review list`, `odin approvals show <id>`, `odin runs show <id>`, and web deep links. The snapshot is not a second authority and must not decide approvals, resolve review items, fire triggers, retry work, or create/update runtime state.

The Observability lane includes an `Activity Log` readback derived from the SQLite runtime event stream. It summarizes recent events with source identifiers such as event ID, event type, scope, project, work item, run, approval, timestamp, and a human-readable summary. It reuses the same provenance summarization used by `odin logs trail`.

`odin logs show <event-id>` and `odin logs trail --task <id|key>`, `--run <id>`, or `--approval <id>` are read-only operator views over canonical runtime events. They may make event payloads easier to inspect, but they do not replace events, mutate state, or introduce a parallel log authority.
