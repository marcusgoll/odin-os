package monitoring_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMonitoringConfigFilesExistAndAreParseable(t *testing.T) {
	t.Parallel()

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

func TestComposeDefinesMonitoringServices(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(repoRoot(t), "monitoring/docker-compose.monitoring.yml"))
	if err != nil {
		t.Fatal(err)
	}

	var compose struct {
		Services map[string]any `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &compose); err != nil {
		t.Fatalf("compose yaml parse error: %v", err)
	}
	for _, service := range []string{
		"prometheus",
		"grafana",
		"loki",
		"alloy",
		"cadvisor",
		"blackbox-exporter",
	} {
		if _, ok := compose.Services[service]; !ok {
			t.Fatalf("compose missing %s service", service)
		}
	}
	if _, ok := compose.Services["node_exporter"]; ok {
		t.Fatal("compose must not include node_exporter by default")
	}
	if _, ok := compose.Services["node-exporter"]; ok {
		t.Fatal("compose must not include node-exporter by default")
	}
}

func TestPrometheusScrapesOdinServeMetrics(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(repoRoot(t), "monitoring/prometheus/prometheus.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `job_name: "odin-serve"`) {
		t.Fatal("prometheus config must scrape odin-serve")
	}
	if !strings.Contains(text, "/metrics") {
		t.Fatal("prometheus config must use the Odin metrics endpoint")
	}
	if !strings.Contains(text, "host.docker.internal:19443") {
		t.Fatal("prometheus config must target odin serve through the Docker host alias")
	}
}

func TestPrometheusScrapesOdinServeMetricsOnMonitoringPort(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(repoRoot(t), "monitoring/prometheus/prometheus.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "host.docker.internal:9443") {
		t.Fatal("prometheus config must not use the default Odin service port for the local monitoring stack")
	}
	if !strings.Contains(text, "host.docker.internal:19443") {
		t.Fatal("prometheus config must target odin serve through the Docker host alias")
	}
}

func TestPrometheusDefinesRequiredScrapeJobsAndAlertsFile(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(repoRoot(t), "monitoring/prometheus/prometheus.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, job := range []string{
		`job_name: "odin-serve"`,
		`job_name: "prometheus"`,
		`job_name: "cadvisor"`,
		`job_name: "blackbox"`,
	} {
		if !strings.Contains(text, job) {
			t.Fatalf("prometheus config missing %s scrape job", job)
		}
	}
	if !strings.Contains(text, "alerts.yml") {
		t.Fatal("prometheus config must load alerts.yml")
	}
}

func TestInitialAlertsUseApprovedTelemetryBoundaries(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(repoRoot(t), "monitoring/prometheus/alerts.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"odin_os_health_score",
		"odin_os_telemetry_stale",
		"odin_os_backup_age_seconds",
		"absent(odin_os_backup_age_seconds)",
		"odin_os_restore_test_age_seconds",
		"absent(odin_os_restore_test_age_seconds)",
		"odin_os_critical_service_up",
		"absent(odin_os_critical_service_up)",
		"odin_os_critical_container_up",
		"absent(odin_os_critical_container_up)",
		"node_filesystem_avail_bytes",
		"absent(node_filesystem_size_bytes",
		"odin_os_security_updates_pending_total",
		"absent(odin_os_security_updates_pending_total)",
		"odin_os_reboot_required",
		"absent(odin_os_reboot_required)",
		"probe_success",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("alerts missing approved telemetry expression %s", want)
		}
	}
}

func TestAlloyConfigAndDocsExist(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	for _, path := range []string{
		"monitoring/alloy/config.alloy",
		"docs/operations/observability-stack.md",
	} {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("%s missing: %v", path, err)
		}
		if len(strings.TrimSpace(string(data))) == 0 {
			t.Fatalf("%s must not be empty", path)
		}
	}
}

func TestGrafanaProvisioningAndDashboardsAreVersioned(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	for _, path := range []string{
		"monitoring/grafana/provisioning/datasources/datasources.yml",
		"monitoring/grafana/provisioning/dashboards/dashboards.yml",
		"monitoring/grafana/provisioning/alerting/contact-points.yml",
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
		if strings.HasSuffix(path, ".yml") {
			var parsed any
			if err := yaml.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("%s yaml parse error: %v", path, err)
			}
		}
	}
}

func TestGrafanaProvisioningUsesPrometheusAndLokiDataSources(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(repoRoot(t), "monitoring/grafana/provisioning/datasources/datasources.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"uid: prometheus",
		"type: prometheus",
		"url: http://prometheus:9090",
		"uid: loki",
		"type: loki",
		"url: http://loki:3100",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("grafana datasource provisioning missing %q", want)
		}
	}
}

func TestGrafanaDashboardProviderLoadsRepoManagedDashboards(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(repoRoot(t), "monitoring/grafana/provisioning/dashboards/dashboards.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"path: /var/lib/grafana/dashboards",
		"disableDeletion: false",
		"allowUiUpdates: false",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("grafana dashboard provisioning missing %q", want)
		}
	}
}

func TestOdinOverviewDashboardUsesApprovedTelemetrySources(t *testing.T) {
	t.Parallel()

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

func TestComposeMountsGrafanaProvisioningReadOnly(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(repoRoot(t), "monitoring/docker-compose.monitoring.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"./grafana/provisioning:/etc/grafana/provisioning:ro",
		"./grafana/dashboards:/var/lib/grafana/dashboards:ro",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("compose must mount grafana path %q", want)
		}
	}
}

func TestGrafanaDashboardScopeDocumentsOptionalCollectors(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	doc, err := os.ReadFile(filepath.Join(root, "docs/operations/observability-stack.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(doc)
	for _, want := range []string{
		"The host dashboard queries `node_*` metrics and will be empty until a host-level `node_exporter` is installed",
		"The logs dashboards query Loki's `docker-containers` job",
		"The provisioned `odin-local-loopback` Grafana contact point is a non-delivering placeholder",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("observability docs missing dashboard scope note %q", want)
		}
	}

	hostDashboard, err := os.ReadFile(filepath.Join(root, "monitoring/grafana/dashboards/odin-host.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hostDashboard), "node_cpu_seconds_total") {
		t.Fatal("host dashboard should remain explicit about its node_exporter metric dependency")
	}

	logDashboard, err := os.ReadFile(filepath.Join(root, "monitoring/grafana/dashboards/odin-logs.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logDashboard), "docker-containers") {
		t.Fatal("logs dashboard should match the documented Docker log source")
	}
}

func TestObservabilityDocsProtectDockerHostScrapeAssumptions(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(repoRoot(t), "docs/operations/observability-stack.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"ODIN_HTTP_ADDR=0.0.0.0:19443",
		"host.docker.internal:19443",
		"--entrypoint promtool",
		"prom/prometheus:v2.54.1",
		"127.0.0.1",
		"cAdvisor needs read access",
		"does not mount the Docker socket",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("observability docs missing %q", want)
		}
	}
}

func TestComposePublishesLocalhostOnlyAndAvoidsUnusedDockerSocket(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(repoRoot(t), "monitoring/docker-compose.monitoring.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"127.0.0.1:9090:9090",
		"127.0.0.1:3000:3000",
		"127.0.0.1:3100:3100",
		"127.0.0.1:12345:12345",
		"127.0.0.1:8080:8080",
		"127.0.0.1:9115:9115",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("compose must bind %q", want)
		}
	}
	if strings.Contains(text, "/var/run/docker.sock") {
		t.Fatal("alloy must not mount the Docker socket in the baseline config")
	}
}

func TestExternalMonitoringConfigValidators(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("docker not available for external monitoring validators: %v", err)
	}

	root := repoRoot(t)
	runCommand(t, root, "docker", "compose", "-f", "monitoring/docker-compose.monitoring.yml", "config")
	runCommand(t, root, "docker", "run", "--rm", "--entrypoint", "promtool", "-v", root+"/monitoring/prometheus:/etc/prometheus:ro", "prom/prometheus:v2.54.1", "check", "config", "/etc/prometheus/prometheus.yml")
	runCommand(t, root, "docker", "run", "--rm", "-v", root+"/monitoring/loki:/etc/loki:ro", "grafana/loki:3.2.1", "-config.file=/etc/loki/loki.yml", "-verify-config")
	runCommand(t, root, "docker", "run", "--rm", "-v", root+"/monitoring/alloy:/etc/alloy:ro", "grafana/alloy:v1.4.3", "fmt", "-t", "/etc/alloy/config.alloy")
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

func runCommand(t *testing.T, dir string, name string, args ...string) {
	t.Helper()

	command := exec.Command(name, args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, string(output))
	}
}
