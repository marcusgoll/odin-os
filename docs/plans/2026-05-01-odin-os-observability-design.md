---
title: Odin-OS Observability Design
status: approved
date: 2026-05-01
---

# Odin-OS Observability Design

## Domain Source Of Truth

- `CONTEXT.md`
- `docs/adr/0001-canonical-authority.md`
- `docs/contracts/observability.md`
- `docs/contracts/homelab-operations.md`
- `docs/plans/2026-04-09-phase-10-observability-design.md`
- `docs/plans/2026-04-09-phase-15-homelab-design.md`

## Current State

Odin-OS already has a first-generation observability spine:

- `odin serve` exposes `/healthz`, `/readyz`, and `/metrics`.
- `odin doctor --json` and `odin healthcheck` expose command-line readiness proof.
- `internal/api/http/operational.go` serves the operational HTTP endpoints.
- `internal/telemetry/metrics` renders Prometheus text metrics.
- `internal/telemetry/logs` writes structured JSON logs.
- `internal/runtime/health` owns typed doctor checks.
- `internal/runtime/projections` exposes read-only runtime views.

The repo does not yet contain a provisioned monitoring stack, Grafana dashboards, Loki/Alloy config, host/container exporters, or a real TUI frontend over Prometheus and Loki.

## Approved Approach

Use **Approach A: Native Odin Observer Role + Provisioned Monitoring Stack**.

The **Odin Observer Role** is a role inside `odin serve`, not a separate `odin-observer` process. Prometheus scrapes Odin's existing operational HTTP surface. Loki receives logs through Grafana Alloy. Grafana and the Odin TUI query Prometheus and Loki as their shared telemetry backends.

Rejected alternatives:

- A separate `odin-observer` service, because it would add a second runtime boundary and risk duplicate health derivation.
- A metrics-only MVP, because the target product requires both metrics and logs across terminal and web frontends.

## Architecture

```text
Odin runtime authority
  SQLite + runtime events + projections
        |
        v
odin serve
  /healthz
  /readyz
  /metrics
  structured logs
        |
        +--> Prometheus scrapes metrics
        +--> Alloy ships logs to Loki
                  |
                  v
        Shared telemetry query layer
        Prometheus API + Loki API
                  |
        +---------+---------+
        |                   |
        v                   v
  Odin TUI             Grafana
  terminal cockpit     web dashboard
```

Prometheus is the metrics backend. Loki is the logs backend. Grafana is the long-term web dashboard. The Odin TUI is the fast local terminal cockpit. Both frontends must read the same Prometheus and Loki sources for telemetry status.

## Metrics Contract

Keep the existing `odin_*` metrics as compatibility metrics for runtime workflow state:

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

Add an `odin_os_*` metric family for host, service, lifecycle, backup, update, and recovery readiness:

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

Rule: `odin_*` is runtime/workflow observability; `odin_os_*` is host/service/lifecycle observability. Do not silently rename current metrics.

## Logs Contract

Odin already writes structured JSON logs. Grafana Alloy should collect:

- Odin service logs from the configured runtime log path.
- systemd journal logs for the installed Odin service.
- Docker/container logs for the monitoring stack and critical services.

Loki stores logs. Grafana and the TUI query Loki; neither frontend tails log files directly as its normal data path.

## Freshness Contract

Freshness is part of the status model:

- Fresh telemetry allows normal status rendering.
- Stale telemetry forces overall status to `unknown`.
- Missing scrapes or missing log streams must never render as healthy.

The TUI and Grafana should show `unknown` when Prometheus or Loki data is stale or missing.

## Frontends

Grafana owns:

- trends
- historical charts
- dashboard sharing
- alert summaries
- logs drilldown
- long-form troubleshooting views

Initial dashboards:

- Odin-OS Overview
- Host Health
- Containers
- Services And Lifecycle
- Backups And Recovery
- Logs

The Odin TUI owns:

- current health
- lifecycle phase
- failed services
- container health
- backup status
- update status
- recent logs/events
- alerts
- suggested next action
- guarded operator actions

Initial TUI views:

- Overview
- Lifecycle
- Host
- Services
- Containers
- Backups
- Updates
- Logs
- Alerts
- Actions

The TUI should query Prometheus instant/range APIs and Loki log APIs. Guarded actions still go through Odin command/action paths and require explicit confirmation.

## Deployment Layout

Add reproducible monitoring assets under:

```text
monitoring/
  docker-compose.monitoring.yml
  prometheus/
    prometheus.yml
    alerts.yml
  grafana/
    provisioning/
      datasources/
      dashboards/
      alerting/
    dashboards/
      odin-overview.json
      odin-host.json
      odin-containers.json
      odin-services.json
      odin-backups.json
      odin-logs.json
  loki/
    loki.yml
  alloy/
    config.alloy
```

The target local startup path is:

```bash
docker compose -f monitoring/docker-compose.monitoring.yml up -d
```

Node exporter should run as a host-level systemd service by default. The compose stack should include Prometheus, Grafana, Loki, Alloy, cAdvisor, and blackbox_exporter.

## Alerts

Initial alerts:

- Odin health score below 70.
- Telemetry stale.
- Backup older than 24 hours.
- Restore test older than 30 days.
- Critical service down.
- Critical container down.
- Root filesystem below 10 percent free.
- Security updates pending over 7 days.
- Reboot required over 7 days.
- Endpoint probe failure.

Use one notification authority at first. For the homelab baseline, prefer Grafana Alerting so dashboarding and notification management stay together.

## Implementation Order

1. Expand and document the Odin metrics contract.
2. Add Prometheus scrape and alert config.
3. Add Grafana, Loki, and Alloy provisioning.
4. Add dashboard JSON.
5. Add the TUI read-only Prometheus and Loki client.
6. Add guarded TUI actions only after read-only truth is proven.

## Verification Requirements

Implementation is incomplete until it proves:

- `odin doctor --json` and `odin healthcheck` still work.
- `odin serve` exposes `/metrics`, `/healthz`, and `/readyz`.
- Prometheus can scrape Odin metrics.
- Grafana can load provisioned Prometheus and Loki data sources.
- Grafana can load provisioned dashboards.
- Loki receives Odin logs through Alloy.
- The TUI reads Prometheus and Loki rather than local ad hoc probes.
- Any action exposed in the TUI goes through a guarded Odin command/action path.

## External Documentation Used

- Prometheus HTTP API: `https://prometheus.io/docs/prometheus/latest/querying/api/`
- Grafana provisioning: `https://grafana.com/docs/grafana/latest/administration/provisioning/`
- Grafana Loki: `https://grafana.com/oss/loki/`
- Grafana Alloy migration: `https://grafana.com/docs/enterprise-logs/latest/setup/migrate/migrate-to-alloy/`
