# Odin Observability Stack

This stack is the repo-managed local telemetry backend for Odin OS. `odin serve` remains the Odin Observer Role and the source of Odin runtime health, readiness, metrics, and structured logs. Prometheus scrapes `odin serve`; Loki receives logs; Grafana reads telemetry; none of them replace Odin runtime events or derive canonical Odin state.

## Startup

Build the Odin binary and start the existing service surface first:

```bash
go build -o ./bin/odin ./cmd/odin
ODIN_HTTP_ADDR=0.0.0.0:19443 ./bin/odin serve
```

In another shell, start the monitoring stack:

```bash
docker compose -f monitoring/docker-compose.monitoring.yml up -d
```

Prometheus uses the Docker Linux host alias `host.docker.internal:19443` to scrape the host-running `odin serve` metrics endpoint. The compose file sets `extra_hosts: host.docker.internal:host-gateway` where the stack needs that alias.

`odin serve` must bind to an address reachable from Docker's host-gateway path. Binding only to `127.0.0.1:19443` is useful for host-local curl checks, but it is not generally reachable from Prometheus running on the Docker bridge. Binding to `0.0.0.0:19443` exposes Odin's operational endpoints on host interfaces; keep this endpoint constrained by host firewall or local-network policy. Port `19443` is intentionally used for this local monitoring stack to avoid colliding with the default Odin service port or unrelated HTTPS listeners; verify the port is free before starting the stack.

Default local endpoints:

- Odin metrics: `http://127.0.0.1:19443/metrics`
- Odin readiness: `http://127.0.0.1:19443/readyz`
- Prometheus: `http://127.0.0.1:9090`
- Grafana: `http://127.0.0.1:3000`
- Loki: `http://127.0.0.1:3100`
- Alloy: `http://127.0.0.1:12345`
- cAdvisor: `http://127.0.0.1:8080`
- blackbox_exporter: `http://127.0.0.1:9115`

## Data Flow

- `odin serve` exports `/metrics`, `/readyz`, and `/healthz` from Odin-owned runtime state.
- Prometheus scrapes `odin serve`, Prometheus itself, cAdvisor, and blackbox_exporter.
- blackbox_exporter probes the host-running Odin readiness endpoint through `host.docker.internal`.
- cAdvisor exports container metrics for Prometheus.
- Alloy tails Docker container JSON logs and sends them to Loki. Task 2 does not yet collect host-running `odin serve` logs or systemd journal logs.
- Loki stores Docker logs for query and correlation. It does not replace Odin runtime events.
- Grafana reads Prometheus and Loki as telemetry backends. Grafana dashboards are presentation, not runtime authority.

## Grafana Dashboards

Grafana is provisioned from files under `monitoring/grafana/`. Prometheus and Loki data sources use fixed UIDs so dashboards can be versioned and reviewed with the rest of the stack.

The baseline dashboards are intentionally honest about the current collectors:

- Odin overview, services, containers, and backups panels use Odin, Prometheus, cAdvisor, blackbox_exporter, and Loki signals from the repo-managed stack.
- The host dashboard queries `node_*` metrics and will be empty until a host-level `node_exporter` is installed and a reviewed Prometheus scrape job is added.
- The logs dashboards query Loki's `docker-containers` job. They do not show host-running `odin serve` logs or systemd journal logs until a later Alloy/journal collection change proves those sources.
- The provisioned `odin-local-loopback` Grafana contact point is a non-delivering placeholder. Do not attach actionable alert policies to it; replace it with an operator-approved receiver before treating Grafana notifications as real alert delivery.

## Host-Level node_exporter

`node_exporter` is intentionally not included in `monitoring/docker-compose.monitoring.yml` by default. Host filesystem, CPU, memory, and systemd-adjacent signals should be installed as a host-level systemd service so they describe the host directly rather than a container view.

Example host-level installation outline:

```bash
sudo useradd --system --no-create-home --shell /usr/sbin/nologin node_exporter
sudo install -m 0755 node_exporter /usr/local/bin/node_exporter
sudo tee /etc/systemd/system/node_exporter.service >/dev/null <<'UNIT'
[Unit]
Description=Prometheus Node Exporter
After=network-online.target

[Service]
User=node_exporter
Group=node_exporter
ExecStart=/usr/local/bin/node_exporter --collector.filesystem.mount-points-exclude='^/(dev|proc|sys|run|var/lib/docker/.+)($|/)'
Restart=on-failure

[Install]
WantedBy=multi-user.target
UNIT
sudo systemctl daemon-reload
sudo systemctl enable --now node_exporter.service
```

After installing it, add a Prometheus scrape job for the host-level endpoint in a later reviewed change. The Task 2 compose stack leaves that out on purpose.

## Host Access And Local Binding

The compose stack binds its web ports to `127.0.0.1` by default so Prometheus, Grafana, Loki, Alloy, cAdvisor, and blackbox_exporter are local-only unless an operator deliberately changes the bindings.

cAdvisor needs read access to host container and filesystem metadata to export container metrics. The compose service uses read-only host mounts and `privileged: true`, which is a meaningful host-observability permission. Run this stack only on a trusted host and treat cAdvisor as an operator-approved local monitoring component.

Alloy tails Docker JSON log files from `/var/lib/docker/containers` and does not mount the Docker socket in this baseline. If a later task adds Docker discovery or host journal collection, it must document the extra permission and prove the new source explicitly.

## Verification

Check the Odin-owned surface:

```bash
curl -fsS http://127.0.0.1:19443/readyz
curl -fsS http://127.0.0.1:19443/metrics | rg 'odin_os_|odin_active_runs'
```

Check Prometheus config locally:

```bash
go test ./tests/monitoring
docker run --rm --entrypoint promtool -v "$PWD/monitoring/prometheus:/etc/prometheus:ro" prom/prometheus:v2.54.1 check config /etc/prometheus/prometheus.yml
```

Check the running stack:

```bash
docker compose -f monitoring/docker-compose.monitoring.yml ps
curl -fsS http://127.0.0.1:9090/-/ready
curl -fsS 'http://127.0.0.1:9090/api/v1/targets?state=active'
curl -fsS 'http://127.0.0.1:3100/ready'
```

Prometheus target health proves scrape reachability. It does not prove Odin runtime correctness by itself; runtime correctness still comes from the real `odin serve` endpoints and Odin-owned command paths.
