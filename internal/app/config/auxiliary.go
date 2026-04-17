package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	executorrouter "odin-os/internal/executors/router"
)

type ModelsFile struct {
	Version int           `yaml:"version"`
	Models  []ModelConfig `yaml:"models"`
}

type ModelConfig struct {
	Key      string `yaml:"key"`
	Provider string `yaml:"provider"`
	Access   string `yaml:"access"`
	Adapter  string `yaml:"adapter"`
}

// TelemetryFile is intentionally narrow: runtime telemetry behavior comes from
// explicit services and defaults, while this file only guards against stale
// undocumented config keys lingering in repo-managed state.
type TelemetryFile struct {
	Version int `yaml:"version"`
}

func ValidateRepo(repoRoot string) error {
	if _, err := LoadModels(filepath.Join(repoRoot, "config", "models.yaml")); err != nil {
		return err
	}
	if _, err := LoadTelemetry(filepath.Join(repoRoot, "config", "telemetry.yaml")); err != nil {
		return err
	}
	return nil
}

func LoadModels(path string) (ModelsFile, error) {
	var raw ModelsFile
	if err := decodeYAMLFile(path, &raw); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ModelsFile{Version: 1}, nil
		}
		return ModelsFile{}, err
	}
	if raw.Version == 0 {
		raw.Version = 1
	}

	bootstrapExecutors := executorrouter.BootstrapCatalogEntries()
	for _, model := range raw.Models {
		if model.Adapter == "" {
			return ModelsFile{}, fmt.Errorf("model %q is missing adapter", model.Key)
		}
		if _, ok := bootstrapExecutors[model.Adapter]; !ok {
			return ModelsFile{}, fmt.Errorf("model %q references unknown adapter %q", model.Key, model.Adapter)
		}
	}

	return raw, nil
}

func LoadTelemetry(path string) (TelemetryFile, error) {
	var raw TelemetryFile
	if err := decodeYAMLFile(path, &raw); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return TelemetryFile{Version: 1}, nil
		}
		return TelemetryFile{}, err
	}
	if raw.Version == 0 {
		raw.Version = 1
	}
	return raw, nil
}
