package config

import (
	"bytes"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type File struct {
	Version int             `yaml:"version"`
	Runtime RuntimeFile     `yaml:"runtime"`
	Service ServiceSettings `yaml:"service"`
}

type RuntimeFile struct {
	Root string `yaml:"root"`
}

type ServiceSettings struct {
	HTTPAddr        string `yaml:"http_addr"`
	StartupRecovery bool   `yaml:"startup_recovery"`
}

type Config struct {
	Version     int
	RuntimeRoot string
	Service     ServiceSettings
}

func Load(path string, repoRoot string, env map[string]string) (Config, error) {
	var raw File
	if err := decodeYAMLFile(path, &raw); err != nil {
		return Config{}, err
	}

	cfg := Config{
		Version: raw.Version,
		Service: raw.Service,
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Service.HTTPAddr == "" {
		cfg.Service.HTTPAddr = "127.0.0.1:9443"
	}
	if !raw.Service.StartupRecovery {
		cfg.Service.StartupRecovery = false
	} else {
		cfg.Service.StartupRecovery = true
	}

	cfg.RuntimeRoot = resolveRuntimeRoot(repoRoot, raw.Runtime.Root)
	if value := env["ODIN_ROOT"]; value != "" {
		cfg.RuntimeRoot = value
	}
	if value := env["ODIN_HTTP_ADDR"]; value != "" {
		cfg.Service.HTTPAddr = value
	}

	return cfg, nil
}

func resolveRuntimeRoot(repoRoot string, configured string) string {
	if configured == "" {
		return repoRoot
	}
	if filepath.IsAbs(configured) {
		return configured
	}
	return filepath.Clean(filepath.Join(repoRoot, configured))
}

func decodeYAMLFile(path string, target any) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	return decoder.Decode(target)
}
