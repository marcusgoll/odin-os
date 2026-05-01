# Odin Observability Proof - 2026-05-01

## Scope

This proof covers the Odin observability stack added in the 2026-05-01 implementation slice:

- `odin serve` as the Odin Observer Role
- Prometheus scrape and query path
- Loki Docker-log ingestion path
- Grafana file-provisioned data sources and dashboards
- `odin tui --once` as the read-only terminal frontend

The proof used branch `codex/odin-observability-impl` and commit `54742d6` for the TUI slice.

## Existing State Found

The host already had services bound to ports `3000` and `8080`, so the default Grafana and cAdvisor host bindings could not be used for the live proof without disrupting unrelated containers.

The host also blocked Docker bridge traffic from the monitoring network back to `host-gateway:19443`. Direct host-local `curl http://127.0.0.1:19443/metrics` passed, but Prometheus scraping `http://host.docker.internal:19443/metrics` timed out from the container network. The proof therefore used a temporary compose-only override that ran the real `./bin/odin serve` binary as a service alias for `host.docker.internal`.

## Commands Run

Build:

```bash
go build -o ./bin/odin ./cmd/odin
```

Direct host Odin proof:

```bash
tmp=$(mktemp -d)
ODIN_ROOT="$tmp" ODIN_HTTP_ADDR=0.0.0.0:19443 ./bin/odin serve
curl -fsS http://127.0.0.1:19443/healthz
curl -fsS http://127.0.0.1:19443/readyz
curl -fsS http://127.0.0.1:19443/metrics | rg 'odin_active_runs|odin_os_health_score|odin_os_status|odin_os_lifecycle_phase|odin_os_telemetry_stale'
```

Result:

- `healthz` returned `status=healthy`.
- `readyz` returned `status=healthy`.
- `/metrics` included `odin_active_runs 0`, `odin_os_health_score 100`, `odin_os_status{status="healthy"} 1`, `odin_os_lifecycle_phase{phase="run"} 1`, and `odin_os_telemetry_stale 0`.

Monitoring proof override:

```yaml
services:
  odin-serve-proof:
    image: debian:bookworm-slim
    working_dir: /repo
    command: ["/repo/bin/odin", "serve"]
    environment:
      ODIN_ROOT: /runtime
      ODIN_HTTP_ADDR: 0.0.0.0:19443
    volumes:
      - /home/orchestrator/odin-os/.worktrees/odin-observability-impl:/repo:ro
      - /tmp/odin-observability-proof-container-root:/runtime
    networks:
      default:
        aliases:
          - host.docker.internal
  prometheus:
    extra_hosts: !override []
    depends_on:
      - odin-serve-proof
  blackbox-exporter:
    extra_hosts: !override []
  grafana:
    ports: !override
      - "127.0.0.1:13000:3000"
  cadvisor:
    ports: !override
      - "127.0.0.1:18080:8080"
```

Stack start:

```bash
docker compose -f monitoring/docker-compose.monitoring.yml -f /tmp/odin-monitoring-proof.override.yml up -d
```

Result:

- Prometheus, Grafana, Loki, Alloy, cAdvisor, blackbox_exporter, and `odin-serve-proof` started.
- Prometheus target health reported `odin-serve`, `prometheus`, `cadvisor`, and `blackbox` as `up`.

Prometheus queries:

```bash
curl -fsS 'http://127.0.0.1:9090/api/v1/query?query=odin_os_health_score'
curl -fsS 'http://127.0.0.1:9090/api/v1/query?query=odin_os_status'
curl -fsS 'http://127.0.0.1:9090/api/v1/query?query=odin_os_lifecycle_phase'
curl -fsS 'http://127.0.0.1:9090/api/v1/query?query=odin_active_runs'
```

Result:

- `odin_os_health_score` returned `100`.
- `odin_os_status{status="healthy"}` returned `1`.
- `odin_os_lifecycle_phase{phase="run"}` returned `1`.
- `odin_active_runs` returned `0`.

Loki query:

```bash
curl -G -fsS 'http://127.0.0.1:3100/loki/api/v1/query_range' \
  --data-urlencode 'query={job="docker-containers"}' \
  --data-urlencode 'limit=3' \
  --data-urlencode 'direction=BACKWARD'
```

Result:

- Loki returned `status=success`.
- The stream labels included `job="docker-containers"`.
- Odin-specific host logs are not proven in this slice because Alloy currently tails Docker JSON logs only and does not collect the host-running `odin serve` log file or systemd journal.

Grafana provisioning:

```bash
curl -fsS http://127.0.0.1:13000/api/health
curl -fsS -u admin:admin http://127.0.0.1:13000/api/datasources
curl -fsS -u admin:admin 'http://127.0.0.1:13000/api/search?query=Odin'
```

Result:

- Grafana health returned `database=ok`.
- Data sources loaded with UID `prometheus` and UID `loki`.
- Provisioned dashboards loaded under folder `Odin`: `Odin Overview`, `Odin Host`, `Odin Containers`, `Odin Services`, `Odin Backups`, and `Odin Logs`.

TUI proof:

```bash
./bin/odin tui --once --prometheus-url http://127.0.0.1:9090 --loki-url http://127.0.0.1:3100
```

Result:

- Rendered `HEALTH: HEALTHY`.
- Rendered `HEALTH_SCORE: 100`.
- Rendered `TELEMETRY_STALE: false`.
- Rendered `LIFECYCLE_PHASE: run`.
- Rendered `ACTIVE_RUNS: 0`.

Fail-closed TUI proof:

```bash
ODIN_ROOT="$(mktemp -d)" ./bin/odin tui --once --prometheus-url http://127.0.0.1:1 --loki-url http://127.0.0.1:1
```

Result:

- Command exited nonzero.
- Error began with `unavailable telemetry: prometheus query "odin_os_health_score" failed`.
- The TUI did not report healthy when Prometheus was unavailable.

Focused tests:

```bash
go test -count=1 ./internal/cli/tui ./internal/app/lifecycle ./tests/monitoring
```

Result: passed.

## Proven

- The real `odin` command builds.
- The real `odin serve` surface exports healthy `/healthz`, `/readyz`, and Prometheus metrics.
- Prometheus can scrape a real `odin serve` process when the proof stack provides a reachable `host.docker.internal` target.
- Grafana loads provisioned Prometheus and Loki data sources.
- Grafana loads the six repo-provisioned Odin dashboards.
- Loki ingests Docker container logs through Alloy.
- `odin tui --once` reads Prometheus/Loki APIs and renders the same core Odin metrics returned by Prometheus.
- `odin tui --once` fails closed when Prometheus is unavailable.

## Unproven

- The default host-gateway scrape path is not proven on this host because Docker bridge traffic to host port `19443` timed out.
- Host-running `odin serve` logs and systemd journal logs are not collected by the baseline Alloy config.
- The Loki proof validates Docker-log ingestion, not a dedicated Odin service log stream.
- Grafana alert delivery is not proven. The provisioned `odin-local-loopback` contact point is intentionally non-delivering.
- Host `node_exporter` metrics are not proven because node_exporter remains an explicit host-level install step outside the default compose stack.

## Best Operating Rule

Keep the Odin TUI and Grafana as frontends over Prometheus and Loki. If Prometheus cannot provide the required Odin metrics, the TUI must fail closed or render `UNKNOWN`; it must never infer healthy state from missing telemetry.
