# Odin-OS Observability Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build Odin-OS observability as one backend telemetry layer with `odin serve` as the Odin Observer role, Prometheus and Loki as shared telemetry backends, Grafana as the web frontend, and a read-only Odin TUI as the terminal frontend.

**Domain Source of Truth:** `CONTEXT.md`, `docs/adr/0001-canonical-authority.md`, `docs/contracts/observability.md`, `docs/contracts/homelab-operations.md`, `docs/plans/2026-05-01-odin-os-observability-design.md`

**Context:** Odin homelab/runtime observability.

**Owns / Does Not Own:** Owns Odin runtime-derived health, readiness, metrics, structured logs, TUI read models, and repo-managed monitoring config. Does not own Prometheus, Loki, Grafana, Alloy, node_exporter, cAdvisor, or blackbox_exporter runtime internals. Does not create a separate `odin-observer` daemon.

**Invariants:**

- The **Odin Observer Role** belongs inside `odin serve`.
- Prometheus, Loki, Grafana, and the TUI must not become canonical Odin runtime authorities.
- Existing `odin_*` metrics remain compatibility metrics; new host/service/lifecycle metrics use `odin_os_*`.
- The TUI and Grafana must read the same Prometheus/Loki telemetry truth.
- Stale or missing telemetry must render as `unknown`, not healthy.
- Guarded actions must go through Odin command/action paths, not direct TUI shell-outs.

**Architecture:** Extend existing Odin metrics and operational HTTP surfaces, then add reproducible monitoring config under `monitoring/`. Add a read-only TUI command that queries Prometheus and Loki APIs. Keep actions out of scope until read-only telemetry is proven.

**Tech Stack:** Go, SQLite, Prometheus text exposition, Prometheus HTTP API, Loki HTTP API, Grafana provisioning, Grafana Alloy, Docker Compose, systemd node_exporter guidance.

---

### Task 1: Expand The Odin Metrics Contract

**Domain Goal:** Make `odin serve` expose the approved Odin-OS custom metrics without replacing the existing runtime/workflow metrics.

**Domain Rules Enforced:**

- The **Odin Observer Role** remains inside `odin serve`.
- `odin_*` compatibility metrics are not silently renamed.
- New host/service/lifecycle metrics use `odin_os_*`.
- Stale telemetry can force `unknown` status.

**Why this matters:**

- Prometheus and both frontends need one metric contract for Odin-specific state.
- Host/container metrics still come from exporters, but Odin lifecycle, backup, update, and readiness metrics belong to Odin.

**Files:**

- Modify: `docs/contracts/observability.md`
- Modify: `internal/telemetry/metrics/service.go`
- Modify: `internal/telemetry/metrics/service_test.go`
- Modify: `internal/api/http/operational_test.go`
- Modify: `internal/app/lifecycle/run.go` only if new collector dependencies must be threaded into `odin serve`

**Step 1: Write the failing metrics render test**

Add assertions to `internal/telemetry/metrics/service_test.go`:

```go
func TestRenderExportsOdinOSMetricsWithoutRenamingCompatibilityMetrics(t *testing.T) {
	t.Parallel()

	exported := Render(Snapshot{
		ActiveRuns: 3,
		OS: OSSnapshot{
			HealthScore:      87,
			Status:           "degraded",
			LifecyclePhase:   "run",
			TelemetryStale:   false,
			BackupAgeSeconds: 14400,
			RebootRequired:   false,
			CriticalServices: []CriticalServiceMetric{
				{Name: "odin", Up: true},
			},
		},
	})

	for _, want := range []string{
		"odin_active_runs 3",
		"odin_os_health_score 87",
		`odin_os_status{status="degraded"} 1`,
		`odin_os_lifecycle_phase{phase="run"} 1`,
		"odin_os_telemetry_stale 0",
		"odin_os_backup_age_seconds 14400",
		`odin_os_critical_service_up{service="odin"} 1`,
	} {
		if !strings.Contains(exported, want) {
			t.Fatalf("Render() missing %q in:\n%s", want, exported)
		}
	}
}
```

**Step 2: Run the test to verify it fails**

Run:

```bash
go test ./internal/telemetry/metrics -run TestRenderExportsOdinOSMetricsWithoutRenamingCompatibilityMetrics
```

Expected: FAIL because `OSSnapshot` and the `odin_os_*` render output do not exist.

**Step 3: Implement the minimal metrics model**

Add types to `internal/telemetry/metrics/service.go`:

```go
type OSSnapshot struct {
	HealthScore             int
	Status                  string
	LifecyclePhase          string
	TelemetryStale          bool
	BackupAgeSeconds       int64
	RestoreTestAgeSeconds  int64
	UpdatesPending         int
	SecurityUpdatesPending int
	RebootRequired         bool
	SystemdFailedUnits     int
	CriticalServices       []CriticalServiceMetric
	CriticalContainers     []CriticalContainerMetric
}

type CriticalServiceMetric struct {
	Name string
	Up   bool
}

type CriticalContainerMetric struct {
	Name string
	Up   bool
}
```

Extend `Snapshot` with `OS OSSnapshot`.

Extend `Render` to append `odin_os_*` metrics while keeping every existing `odin_*` line.

**Step 4: Add collection defaults**

In `Service.Collect`, populate a conservative `OSSnapshot` from existing doctor/metrics inputs:

- `HealthScore`: `100` when existing snapshot has no stale executors, stale sources, stale projections, open incidents, or active recoveries; `80` for degraded; `0` only for failed collection errors.
- `Status`: `healthy`, `degraded`, or `unknown`.
- `LifecyclePhase`: `run` while `odin serve` is serving metrics.
- `TelemetryStale`: true when stale executor, source, or projection counts are greater than zero.

Do not add direct systemd, Docker, package manager, or backup shell probes in this task. Those belong to later collector tasks.

**Step 5: Re-run focused metrics tests**

Run:

```bash
go test ./internal/telemetry/metrics ./internal/api/http
```

Expected: PASS.

**Step 6: Real Odin command proof**

Run:

```bash
tmp=$(mktemp -d)
ODIN_ROOT="$tmp" ./bin/odin doctor --json
ODIN_ROOT="$tmp" ./bin/odin healthcheck
```

Expected: doctor returns JSON and healthcheck prints `ready`.

**Step 7: Commit**

```bash
git add docs/contracts/observability.md internal/telemetry/metrics/service.go internal/telemetry/metrics/service_test.go internal/api/http/operational_test.go internal/app/lifecycle/run.go
git commit -m "feat: add odin os observability metrics"
```

### Task 2: Add Monitoring Stack Configuration

**Domain Goal:** Add reproducible Prometheus, Loki, Alloy, and exporter configuration without making any of them canonical Odin runtime authorities.

**Domain Rules Enforced:**

- Prometheus scrapes `odin serve`; it does not derive Odin runtime state itself.
- Loki receives logs; it does not replace Odin runtime events.
- Monitoring config is repo-managed, not click-maintained.

**Why this matters:**

- The backend telemetry layer must be reproducible from files.

**Files:**

- Create: `monitoring/docker-compose.monitoring.yml`
- Create: `monitoring/prometheus/prometheus.yml`
- Create: `monitoring/prometheus/alerts.yml`
- Create: `monitoring/loki/loki.yml`
- Create: `monitoring/alloy/config.alloy`
- Create: `docs/operations/observability-stack.md`
- Create: `tests/monitoring/config_test.go`

**Step 1: Write failing config existence and parse tests**

Create `tests/monitoring/config_test.go`:

```go
package monitoring_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMonitoringConfigFilesExistAndAreParseable(t *testing.T) {
	root := repoRoot(t)
	for _, path := range []string{
		"monitoring/docker-compose.monitoring.yml",
		"monitoring/prometheus/prometheus.yml",
		"monitoring/prometheus/alerts.yml",
		"monitoring/loki/loki.yml",
	} {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("%s missing: %v", path, err)
		}
		var parsed any
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("%s yaml parse error: %v", path, err)
		}
	}
}

func TestPrometheusScrapesOdinServeMetrics(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "monitoring/prometheus/prometheus.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `job_name: "odin-serve"`) {
		t.Fatalf("prometheus config must scrape odin-serve")
	}
	if !strings.Contains(text, "/metrics") {
		t.Fatalf("prometheus config must use the Odin metrics endpoint")
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatal("repo root not found")
		}
		dir = next
	}
}

var _ = json.Valid
```

**Step 2: Run the test to verify it fails**

Run:

```bash
go test ./tests/monitoring
```

Expected: FAIL because monitoring files do not exist.

**Step 3: Add monitoring files**

Create `monitoring/docker-compose.monitoring.yml` with services:

- `prometheus`
- `grafana`
- `loki`
- `alloy`
- `cadvisor`
- `blackbox-exporter`

Do not include node_exporter in compose by default. Document host-level systemd installation in `docs/operations/observability-stack.md`.

Create `monitoring/prometheus/prometheus.yml` with scrape jobs:

- `odin-serve` target from an environment-supported host alias, defaulting to `host.docker.internal:9443`
- `prometheus`
- `cadvisor`
- `blackbox`

Create `monitoring/prometheus/alerts.yml` with the approved initial alerts:

- Odin health score below 70
- telemetry stale
- backup older than 24 hours
- restore test older than 30 days
- critical service down
- critical container down
- root filesystem below 10 percent free
- security updates pending over 7 days
- reboot required over 7 days
- endpoint probe failure

**Step 4: Re-run config tests**

Run:

```bash
go test ./tests/monitoring
```

Expected: PASS.

**Step 5: Optional external config validation**

If Docker is available, run:

```bash
docker run --rm -v "$PWD/monitoring/prometheus:/etc/prometheus:ro" prom/prometheus:latest promtool check config /etc/prometheus/prometheus.yml
```

Expected: PASS. If Docker is unavailable, record that this proof is unrun.

**Step 6: Commit**

```bash
git add monitoring docs/operations/observability-stack.md tests/monitoring/config_test.go
git commit -m "chore: add odin monitoring stack config"
```

### Task 3: Add Grafana Provisioning And Dashboard Validation

**Domain Goal:** Make the web frontend reproducible from version-controlled files.

**Domain Rules Enforced:**

- Grafana reads Prometheus and Loki.
- Dashboards query `odin_*` and `odin_os_*`; they do not invent status.
- Provisioned dashboards are repo state, not manual UI state.

**Why this matters:**

- Long-term observability must survive rebuilds and redeploys.

**Files:**

- Create: `monitoring/grafana/provisioning/datasources/datasources.yml`
- Create: `monitoring/grafana/provisioning/dashboards/dashboards.yml`
- Create: `monitoring/grafana/provisioning/alerting/contact-points.yml`
- Create: `monitoring/grafana/dashboards/odin-overview.json`
- Create: `monitoring/grafana/dashboards/odin-host.json`
- Create: `monitoring/grafana/dashboards/odin-containers.json`
- Create: `monitoring/grafana/dashboards/odin-services.json`
- Create: `monitoring/grafana/dashboards/odin-backups.json`
- Create: `monitoring/grafana/dashboards/odin-logs.json`
- Modify: `tests/monitoring/config_test.go`

**Step 1: Add failing dashboard validation tests**

Extend `tests/monitoring/config_test.go`:

```go
func TestGrafanaProvisioningAndDashboardsAreVersioned(t *testing.T) {
	root := repoRoot(t)
	for _, path := range []string{
		"monitoring/grafana/provisioning/datasources/datasources.yml",
		"monitoring/grafana/provisioning/dashboards/dashboards.yml",
		"monitoring/grafana/dashboards/odin-overview.json",
		"monitoring/grafana/dashboards/odin-host.json",
		"monitoring/grafana/dashboards/odin-containers.json",
		"monitoring/grafana/dashboards/odin-services.json",
		"monitoring/grafana/dashboards/odin-backups.json",
		"monitoring/grafana/dashboards/odin-logs.json",
	} {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("%s missing: %v", path, err)
		}
		if strings.HasSuffix(path, ".json") && !json.Valid(data) {
			t.Fatalf("%s is not valid JSON", path)
		}
	}
}

func TestOdinOverviewDashboardUsesApprovedTelemetrySources(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "monitoring/grafana/dashboards/odin-overview.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"odin_os_health_score", "odin_os_telemetry_stale", "odin_active_runs"} {
		if !strings.Contains(text, want) {
			t.Fatalf("overview dashboard missing %s", want)
		}
	}
}
```

**Step 2: Run tests to verify failure**

Run:

```bash
go test ./tests/monitoring
```

Expected: FAIL because Grafana provisioning and dashboards do not exist.

**Step 3: Add provisioning and dashboard JSON**

Provision two data sources:

- Prometheus UID `prometheus`
- Loki UID `loki`

Provision dashboards from `/var/lib/grafana/dashboards`.

Dashboard minimums:

- Overview: health score, status, lifecycle phase, telemetry freshness, active runs, blocked items, backups, updates, critical service/container state.
- Host: node_exporter CPU, memory, filesystem, network, and load panels.
- Containers: cAdvisor running/unhealthy/resource panels.
- Services: Odin lifecycle, systemd failed units, critical service up/down, scrape status.
- Backups: backup age, restore test age, success/failure timeline placeholders.
- Logs: Loki panels for Odin logs, systemd failures, backup/update logs.

**Step 4: Re-run validation**

Run:

```bash
go test ./tests/monitoring
```

Expected: PASS.

**Step 5: Commit**

```bash
git add monitoring/grafana tests/monitoring/config_test.go
git commit -m "chore: provision odin grafana dashboards"
```

### Task 4: Add Read-Only TUI Query Frontend

**Domain Goal:** Add a terminal cockpit that reads the same Prometheus and Loki telemetry truth as Grafana.

**Domain Rules Enforced:**

- TUI reads Prometheus/Loki APIs for observability.
- TUI does not shell out to systemctl, docker, or log files for canonical status.
- Stale or missing telemetry renders as `unknown`.
- Guarded actions are not implemented in this read-only slice.

**Why this matters:**

- The terminal frontend must be useful over SSH without becoming a parallel monitoring system.

**Files:**

- Create: `internal/cli/tui/client.go`
- Create: `internal/cli/tui/client_test.go`
- Create: `internal/cli/tui/model.go`
- Create: `internal/cli/tui/render.go`
- Create: `internal/cli/tui/render_test.go`
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/app/lifecycle/run_test.go`
- Modify: `docs/contracts/observability.md`

**Step 1: Write failing Prometheus client tests**

Create `internal/cli/tui/client_test.go` with an `httptest.Server` that returns Prometheus `/api/v1/query` JSON for:

- `odin_os_health_score`
- `odin_os_telemetry_stale`
- `odin_os_lifecycle_phase`
- `odin_active_runs`

Expected client model:

```go
if model.HealthScore != 87 || model.Status != "degraded" || model.LifecyclePhase != "run" {
	t.Fatalf("model = %+v", model)
}
```

**Step 2: Write failing stale telemetry render test**

Create `internal/cli/tui/render_test.go`:

```go
func TestRenderOverviewShowsUnknownWhenTelemetryIsStale(t *testing.T) {
	output := RenderOverview(Model{
		Status:         "healthy",
		HealthScore:    99,
		TelemetryStale: true,
	})
	if !strings.Contains(output, "HEALTH: UNKNOWN") {
		t.Fatalf("output = %q, want UNKNOWN", output)
	}
}
```

**Step 3: Run tests to verify failure**

Run:

```bash
go test ./internal/cli/tui
```

Expected: FAIL because the TUI package is empty.

**Step 4: Implement read-only client and renderer**

Add:

- `Client.QueryOverview(ctx)` for Prometheus instant queries.
- `Client.QueryRecentLogs(ctx)` for Loki query range or instant log queries.
- `RenderOverview(Model)` for stable text output.

Add a top-level command:

```bash
odin tui --once --prometheus-url http://127.0.0.1:9090 --loki-url http://127.0.0.1:3100
```

`--once` is required for the first slice so tests and SSH smoke checks are deterministic.

If Prometheus is missing or `odin_os_telemetry_stale == 1`, render `HEALTH: UNKNOWN`.

**Step 5: Add lifecycle command tests**

In `internal/app/lifecycle/run_test.go`, assert that `odin tui --once` calls the TUI runner and that missing Prometheus renders a controlled error instead of panicking.

**Step 6: Re-run focused tests**

Run:

```bash
go test ./internal/cli/tui ./internal/app/lifecycle
```

Expected: PASS.

**Step 7: Real Odin command proof**

Run:

```bash
go build -o ./bin/odin ./cmd/odin
./bin/odin tui --once --prometheus-url http://127.0.0.1:9090 --loki-url http://127.0.0.1:3100
```

Expected: If Prometheus/Loki are not running, command returns a clear unavailable-telemetry error. It must not silently report healthy.

**Step 8: Commit**

```bash
git add internal/cli/tui internal/app/lifecycle/run.go internal/app/lifecycle/run_test.go docs/contracts/observability.md
git commit -m "feat: add read-only odin observability tui"
```

### Task 5: Prove End-To-End Observability

**Domain Goal:** Prove that Odin, Prometheus, Loki, Grafana, and the TUI share one telemetry truth.

**Domain Rules Enforced:**

- Real `odin` command paths are part of proof.
- Prometheus/Grafana/TUI read exported Odin telemetry.
- Missing or stale telemetry is not reported as healthy.

**Why this matters:**

- Internal tests do not prove the operator can run the observability stack.

**Files:**

- Create: `docs/operations/observability-proof-2026-05-01.md`
- Modify: `README.md`
- Modify: `docs/operations/observability-stack.md`

**Step 1: Build Odin**

Run:

```bash
go build -o ./bin/odin ./cmd/odin
```

Expected: PASS.

**Step 2: Start Odin service locally**

Run:

```bash
tmp=$(mktemp -d)
ODIN_ROOT="$tmp" ODIN_HTTP_ADDR=127.0.0.1:19443 ./bin/odin serve
```

Run this in a controlled session and stop it after proof.

Expected: stdout includes `serving on 127.0.0.1:19443`.

**Step 3: Verify Odin endpoints directly**

Run:

```bash
curl -fsS http://127.0.0.1:19443/healthz
curl -fsS http://127.0.0.1:19443/readyz
curl -fsS http://127.0.0.1:19443/metrics | rg 'odin_active_runs|odin_os_health_score'
```

Expected: health and readiness JSON return; metrics include both compatibility and `odin_os_*` metrics.

**Step 4: Start monitoring stack**

Run:

```bash
docker compose -f monitoring/docker-compose.monitoring.yml up -d
```

Expected: Prometheus, Grafana, Loki, Alloy, cAdvisor, and blackbox_exporter start.

**Step 5: Verify Prometheus scrape**

Run:

```bash
curl -fsS 'http://127.0.0.1:9090/api/v1/query?query=odin_os_health_score'
curl -fsS 'http://127.0.0.1:9090/api/v1/query?query=odin_active_runs'
```

Expected: Prometheus returns success results for both queries.

**Step 6: Verify Loki ingestion**

Run:

```bash
curl -G -fsS 'http://127.0.0.1:3100/loki/api/v1/query' --data-urlencode 'query={service="odin"}'
```

Expected: Loki query succeeds. If no recent Odin logs exist, record the empty result and then generate an Odin health request to create fresh log activity if the logger is wired to the service path.

**Step 7: Verify Grafana provisioning**

Run:

```bash
curl -fsS http://127.0.0.1:3000/api/health
```

Expected: Grafana API health returns a healthy database status.

Then verify dashboard files are mounted through the Grafana UI or API. If auth blocks API inspection, record browser/manual verification requirements in the proof doc.

**Step 8: Verify TUI uses Prometheus/Loki**

Run:

```bash
./bin/odin tui --once --prometheus-url http://127.0.0.1:9090 --loki-url http://127.0.0.1:3100
```

Expected: TUI renders the same health score and lifecycle phase as Prometheus queries. If Prometheus is stopped, TUI renders `UNKNOWN` or returns a controlled unavailable-telemetry error.

**Step 9: Record proof**

Create `docs/operations/observability-proof-2026-05-01.md` with:

- exact commands run
- command output summaries
- Prometheus query results
- Loki query result
- Grafana provisioning proof
- TUI output
- remaining unproven items

**Step 10: Final verification**

Run:

```bash
go test ./internal/telemetry/... ./internal/runtime/health ./internal/api/http ./internal/cli/tui ./tests/monitoring
go test ./...
```

Expected: PASS.

**Step 11: Commit**

```bash
git add README.md docs/operations/observability-stack.md docs/operations/observability-proof-2026-05-01.md
git commit -m "docs: record odin observability proof"
```

## Review Checklist

- Domain naming matches `CONTEXT.md`, especially **Odin Observer Role**.
- No separate `odin-observer` service or daemon was introduced.
- Existing `odin_*` metrics still exist.
- New Odin-OS metrics use `odin_os_*`.
- Prometheus and Loki are query backends, not runtime authorities.
- Grafana dashboards are provisioned from files.
- TUI reads Prometheus/Loki APIs and treats stale telemetry as `unknown`.
- Real `odin` commands prove `doctor`, `healthcheck`, `serve`, `/metrics`, and `tui --once`.
- Any guarded TUI actions remain out of scope until read-only truth is proven.
