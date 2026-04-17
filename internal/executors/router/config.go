package router

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"odin-os/internal/executors/contract"
)

type Config struct {
	Version   int              `yaml:"version"`
	Executors []ExecutorConfig `yaml:"executors"`
	Routes    []RouteConfig    `yaml:"routes"`
}

type ExecutorConfig struct {
	Key      string                 `yaml:"key"`
	Adapter  string                 `yaml:"adapter"`
	Class    contract.ExecutorClass `yaml:"class"`
	Enabled  bool                   `yaml:"enabled"`
	Priority int                    `yaml:"priority"`
	ModelRef string                 `yaml:"model_ref,omitempty"`
}

type RouteConfig struct {
	Name      string     `yaml:"name"`
	Match     RouteMatch `yaml:"match"`
	Preferred []string   `yaml:"preferred"`
	Fallback  []string   `yaml:"fallback"`
}

type RouteMatch struct {
	TaskKinds []contract.TaskKind `yaml:"task_kinds"`
	Scopes    []string            `yaml:"scopes"`
}

func LoadConfig(path string) (Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := strictUnmarshal(content, &cfg); err != nil {
		return Config{}, err
	}
	if err := validateConfig(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (config Config) ExecutorByKey(key string) (ExecutorConfig, bool) {
	for _, executor := range config.Executors {
		if executor.Key == key {
			return executor, true
		}
	}
	return ExecutorConfig{}, false
}

func validateConfig(cfg Config) error {
	bootstrap := BootstrapCatalogEntries()
	configured := make(map[string]struct{}, len(cfg.Executors))

	for _, executor := range cfg.Executors {
		if _, exists := configured[executor.Key]; exists {
			return fmt.Errorf("executor %q is declared more than once", executor.Key)
		}
		configured[executor.Key] = struct{}{}

		entry, ok := bootstrap[executor.Key]
		if !ok {
			return fmt.Errorf("executor %q is not part of the bootstrap catalog", executor.Key)
		}
		if executor.Adapter != executor.Key {
			return fmt.Errorf("executor %q adapter = %q, want %q to avoid drift from the bootstrap catalog", executor.Key, executor.Adapter, executor.Key)
		}
		if executor.Class != entry.Class {
			return fmt.Errorf("executor %q class = %q, want %q from the bootstrap catalog", executor.Key, executor.Class, entry.Class)
		}
	}

	for _, route := range cfg.Routes {
		for _, key := range append(append([]string{}, route.Preferred...), route.Fallback...) {
			if _, ok := configured[key]; !ok {
				return fmt.Errorf("route %q references unconfigured executor %q", route.Name, key)
			}
		}
	}

	return nil
}

func strictUnmarshal(content []byte, target any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	return decoder.Decode(target)
}
