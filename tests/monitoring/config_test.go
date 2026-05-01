package monitoring_test

import (
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
