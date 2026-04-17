package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadUsesDefaultsFromFile(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "config", "odin.yaml")
	mustWriteConfig(t, path, `
version: 1
runtime:
  root: /srv/odin
service:
  http_addr: 127.0.0.1:9443
  startup_recovery: true
`)

	cfg, err := Load(path, repoRoot, map[string]string{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Version != 1 {
		t.Fatalf("Version = %d, want 1", cfg.Version)
	}
	if cfg.RuntimeRoot != "/srv/odin" {
		t.Fatalf("RuntimeRoot = %q, want %q", cfg.RuntimeRoot, "/srv/odin")
	}
	if cfg.Service.HTTPAddr != "127.0.0.1:9443" {
		t.Fatalf("Service.HTTPAddr = %q, want %q", cfg.Service.HTTPAddr, "127.0.0.1:9443")
	}
	if !cfg.Service.StartupRecovery {
		t.Fatalf("Service.StartupRecovery = false, want true")
	}
}

func TestLoadAllowsEnvironmentOverrides(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "config", "odin.yaml")
	mustWriteConfig(t, path, `
version: 1
runtime:
  root: /srv/odin
service:
  http_addr: 127.0.0.1:9443
  startup_recovery: false
`)

	cfg, err := Load(path, repoRoot, map[string]string{
		"ODIN_ROOT":      "/var/odin",
		"ODIN_HTTP_ADDR": "0.0.0.0:9000",
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.RuntimeRoot != "/var/odin" {
		t.Fatalf("RuntimeRoot = %q, want %q", cfg.RuntimeRoot, "/var/odin")
	}
	if cfg.Service.HTTPAddr != "0.0.0.0:9000" {
		t.Fatalf("Service.HTTPAddr = %q, want %q", cfg.Service.HTTPAddr, "0.0.0.0:9000")
	}
	if cfg.Service.StartupRecovery {
		t.Fatalf("Service.StartupRecovery = true, want false")
	}
}

func TestValidateRepoRejectsUnknownAuxiliaryConfigFields(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	mustWriteConfig(t, filepath.Join(repoRoot, "config", "models.yaml"), `
version: 1
models:
  - key: codex-latest
    provider: openai
    access: plan_backed_cli
    adapter: codex_headless
    stale_field: true
`)
	mustWriteConfig(t, filepath.Join(repoRoot, "config", "telemetry.yaml"), `
version: 1
stale_field: true
`)

	if err := ValidateRepo(repoRoot); err == nil {
		t.Fatal("ValidateRepo() error = nil, want unknown field rejection")
	}
}

func TestValidateRepoAllowsMissingAuxiliaryConfigFiles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if err := ValidateRepo(repoRoot); err != nil {
		t.Fatalf("ValidateRepo() error = %v, want nil when auxiliary files are absent", err)
	}
}

func TestLoadTelemetryAcceptsValidatedBootstrapMarkerOnly(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "telemetry.yaml")
	mustWriteConfig(t, path, "version: 1\n")

	telemetry, err := LoadTelemetry(path)
	if err != nil {
		t.Fatalf("LoadTelemetry() error = %v", err)
	}
	if telemetry.Version != 1 {
		t.Fatalf("LoadTelemetry().Version = %d, want 1", telemetry.Version)
	}
}

func TestCapabilityReloadDocsMatchRuntimeSurface(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))

	gatewayDocPath := filepath.Join(repoRoot, "docs", "contracts", "capability-gateway.md")
	gatewayDoc, err := os.ReadFile(gatewayDocPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", gatewayDocPath, err)
	}

	reloadDocPath := filepath.Join(repoRoot, "docs", "operations", "capability-reload.md")
	reloadDoc, err := os.ReadFile(reloadDocPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", reloadDocPath, err)
	}

	for _, route := range []string{
		"GET /capabilities",
		"GET /capabilities/{id}",
		"POST /capabilities/{id}:invoke",
		"GET /runs/{run_id}",
	} {
		if !strings.Contains(string(gatewayDoc), route) {
			t.Fatalf("capability gateway doc missing runtime route %q", route)
		}
	}

	for _, token := range []string{
		"capability.snapshot_published",
		"capability.snapshot_rejected",
		"no public CLI or REPL reload command",
		"no public HTTP reload route",
	} {
		if !strings.Contains(string(reloadDoc), token) {
			t.Fatalf("capability reload doc missing runtime surface token %q", token)
		}
	}
}

func mustWriteConfig(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
