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
	Name              string            `yaml:"name"`
	Match             RouteMatch        `yaml:"match"`
	Preferred         []string          `yaml:"preferred"`
	Fallback          []string          `yaml:"fallback"`
	ModelRefOverrides map[string]string `yaml:"model_ref_overrides,omitempty"`
}

type RouteMatch struct {
	TaskKinds   []contract.TaskKind `yaml:"task_kinds"`
	Scopes      []string            `yaml:"scopes"`
	TaskClasses []string            `yaml:"task_classes,omitempty"`
	RiskClasses []string            `yaml:"risk_classes,omitempty"`
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

func LoadConfigWithModelRegistry(configPath string, modelRegistryPath string) (Config, ModelRegistry, error) {
	modelRegistry, err := LoadModelRegistry(modelRegistryPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return Config{}, ModelRegistry{}, err
		}
	}
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return Config{}, ModelRegistry{}, err
	}
	if modelRegistry.HasModels() {
		if err := ValidateConfigModelRefs(cfg, modelRegistry); err != nil {
			return Config{}, ModelRegistry{}, err
		}
	}
	return cfg, modelRegistry, nil
}

func (config Config) ExecutorByKey(key string) (ExecutorConfig, bool) {
	for _, executor := range config.Executors {
		if executor.Key == key {
			return executor, true
		}
	}
	return ExecutorConfig{}, false
}

func ValidateConfigModelRefs(cfg Config, registry ModelRegistry) error {
	if err := ValidateModelRegistry(registry); err != nil {
		return err
	}
	for _, executor := range cfg.Executors {
		if executor.ModelRef == "" {
			return fmt.Errorf("executor %q is missing model_ref", executor.Key)
		}
		model, ok := registry.ModelByKey(executor.ModelRef)
		if !ok {
			return fmt.Errorf("executor %q references unknown model_ref %q", executor.Key, executor.ModelRef)
		}
		if model.Adapter != executor.Adapter {
			return fmt.Errorf("executor %q model_ref %q adapter = %q, want %q", executor.Key, executor.ModelRef, model.Adapter, executor.Adapter)
		}
	}
	configured := make(map[string]struct{}, len(cfg.Executors))
	for _, executor := range cfg.Executors {
		configured[executor.Key] = struct{}{}
	}
	for _, route := range cfg.Routes {
		for executorKey, modelRef := range route.ModelRefOverrides {
			if _, ok := configured[executorKey]; !ok {
				return fmt.Errorf("route %q model_ref_overrides references unconfigured executor %q", route.Name, executorKey)
			}
			model, ok := registry.ModelByKey(modelRef)
			if !ok {
				return fmt.Errorf("route %q references unknown model_ref %q", route.Name, modelRef)
			}
			executor, _ := cfg.ExecutorByKey(executorKey)
			if model.Adapter != executor.Adapter {
				return fmt.Errorf("route %q model_ref %q adapter = %q, want %q", route.Name, modelRef, model.Adapter, executor.Adapter)
			}
		}
	}
	return nil
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
