package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestMediaLoadUsesOptionalDefaultPath(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "config", "odin.yaml")
	mustWriteConfig(t, path, `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:9443
  startup_recovery: true
`)
	mustWriteConfig(t, filepath.Join(repoRoot, "config", "media-stack.yaml"), `
enabled: true
services:
  - name: plex
    kind: plex
    base_url: http://127.0.0.1:32400
policies:
  auto_allowed:
    - media_probe_cycle
`)

	cfg, err := Load(path, repoRoot, map[string]string{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.MediaConfigPath != filepath.Join(repoRoot, "config", "media-stack.yaml") {
		t.Fatalf("MediaConfigPath = %q, want %q", cfg.MediaConfigPath, filepath.Join(repoRoot, "config", "media-stack.yaml"))
	}
	if cfg.Media == nil {
		t.Fatalf("Media = nil, want loaded config")
	}
	if len(cfg.Media.Services) != 1 || cfg.Media.Services[0].Name != "plex" {
		t.Fatalf("Media.Services = %+v, want plex service", cfg.Media.Services)
	}
}

func TestMediaLoadAllowsConfigPathOverride(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "config", "odin.yaml")
	mustWriteConfig(t, path, `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:9443
  startup_recovery: true
`)
	overridePath := filepath.Join(repoRoot, "fixtures", "custom-media.yaml")
	mustWriteConfig(t, overridePath, `
enabled: true
services:
  - name: downloader
    kind: downloader
policies:
  approval_required:
    - restart_downloader
`)

	cfg, err := Load(path, repoRoot, map[string]string{
		"ODIN_MEDIA_CONFIG": overridePath,
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.MediaConfigPath != overridePath {
		t.Fatalf("MediaConfigPath = %q, want %q", cfg.MediaConfigPath, overridePath)
	}
	if cfg.Media == nil || len(cfg.Media.Services) != 1 || cfg.Media.Services[0].Name != "downloader" {
		t.Fatalf("Media = %+v, want overridden downloader service", cfg.Media)
	}
}

func TestMediaLoadAllowsMissingOptionalConfig(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "config", "odin.yaml")
	mustWriteConfig(t, path, `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:9443
  startup_recovery: true
`)

	cfg, err := Load(path, repoRoot, map[string]string{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Media != nil {
		t.Fatalf("Media = %+v, want nil when optional config is absent", cfg.Media)
	}
}

func TestMediaLoadRejectsServiceWithoutRequiredIdentifiers(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "config", "odin.yaml")
	mustWriteConfig(t, path, `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:9443
  startup_recovery: true
`)
	mustWriteConfig(t, filepath.Join(repoRoot, "config", "media-stack.yaml"), `
enabled: true
services:
  - name: ""
    kind: plex
`)

	_, err := Load(path, repoRoot, map[string]string{})
	if err == nil {
		t.Fatalf("Load() error = nil, want validation failure")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("Load() error = %v, want name validation", err)
	}
}
