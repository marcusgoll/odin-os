package router

import (
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
	if err := yaml.Unmarshal(content, &cfg); err != nil {
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
