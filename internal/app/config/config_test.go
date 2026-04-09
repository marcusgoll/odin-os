package config

import (
	"os"
	"path/filepath"
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

func mustWriteConfig(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
