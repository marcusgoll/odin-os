package config

import (
	"fmt"
	"os"
	"path/filepath"

	coremedia "odin-os/internal/core/media"

	"gopkg.in/yaml.v3"
)

func loadMediaConfig(repoRoot string, env map[string]string) (string, *coremedia.Config, error) {
	path := filepath.Join(repoRoot, "config", "media-stack.yaml")
	explicit := false
	if value := env["ODIN_MEDIA_CONFIG"]; value != "" {
		if filepath.IsAbs(value) {
			path = value
		} else {
			path = filepath.Clean(filepath.Join(repoRoot, value))
		}
		explicit = true
	}

	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) && !explicit {
			return path, nil, nil
		}
		return path, nil, err
	}

	var config coremedia.Config
	if err := yaml.Unmarshal(content, &config); err != nil {
		return path, nil, err
	}
	if err := (coremedia.Service{}).Validate(config); err != nil {
		return path, nil, fmt.Errorf("media config %s invalid: %w", path, err)
	}

	return path, &config, nil
}
