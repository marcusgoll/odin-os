package router

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type ModelRegistry struct {
	Version int           `yaml:"version"`
	Models  []ModelConfig `yaml:"models"`
}

type ModelConfig struct {
	Key                           string   `yaml:"key"`
	Provider                      string   `yaml:"provider"`
	ProviderModelID               string   `yaml:"provider_model_id,omitempty"`
	LiveProviderModelID           string   `yaml:"live_provider_model_id,omitempty"`
	Access                        string   `yaml:"access"`
	Adapter                       string   `yaml:"adapter"`
	Enabled                       *bool    `yaml:"enabled,omitempty"`
	Capabilities                  []string `yaml:"capabilities"`
	SupportedTaskClasses          []string `yaml:"supported_task_classes"`
	SupportedFeatures             []string `yaml:"supported_features,omitempty"`
	ContextWindowTokens           int      `yaml:"context_window_tokens"`
	MaxInputTokens                int      `yaml:"max_input_tokens,omitempty"`
	MaxOutputTokens               int      `yaml:"max_output_tokens,omitempty"`
	InputCostPerMillionTokensUSD  float64  `yaml:"input_cost_per_million_tokens_usd,omitempty"`
	OutputCostPerMillionTokensUSD float64  `yaml:"output_cost_per_million_tokens_usd,omitempty"`
	LatencyTier                   string   `yaml:"latency_tier"`
	RiskTier                      string   `yaml:"risk_tier"`
	BlockedTaskClasses            []string `yaml:"blocked_task_classes,omitempty"`
	AllowHighRisk                 bool     `yaml:"allow_high_risk,omitempty"`
}

func LoadModelRegistry(path string) (ModelRegistry, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return ModelRegistry{}, err
	}

	var registry ModelRegistry
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(&registry); err != nil {
		return ModelRegistry{}, err
	}
	if err := ValidateModelRegistry(registry); err != nil {
		return ModelRegistry{}, err
	}
	return registry, nil
}

func ValidateModelRegistry(registry ModelRegistry) error {
	if registry.Version == 0 {
		registry.Version = 1
	}
	seen := make(map[string]struct{}, len(registry.Models))
	bootstrap := BootstrapCatalogEntries()
	for _, model := range registry.Models {
		key := strings.TrimSpace(model.Key)
		if key == "" {
			return fmt.Errorf("model key is required")
		}
		if _, ok := seen[key]; ok {
			return fmt.Errorf("model %q is declared more than once", key)
		}
		seen[key] = struct{}{}
		if strings.TrimSpace(model.Provider) == "" {
			return fmt.Errorf("model %q is missing provider", key)
		}
		if strings.TrimSpace(model.Access) == "" {
			return fmt.Errorf("model %q is missing access", key)
		}
		if strings.TrimSpace(model.Adapter) == "" {
			return fmt.Errorf("model %q is missing adapter", key)
		}
		if _, ok := bootstrap[model.Adapter]; !ok {
			return fmt.Errorf("model %q references unknown adapter %q", key, model.Adapter)
		}
		if len(normalizeTokens(model.Capabilities)) == 0 {
			return fmt.Errorf("model %q is missing capabilities", key)
		}
		if len(normalizeTokens(model.SupportedTaskClasses)) == 0 {
			return fmt.Errorf("model %q is missing supported_task_classes", key)
		}
		if model.ContextWindowTokens <= 0 {
			return fmt.Errorf("model %q context_window_tokens must be positive", key)
		}
		if strings.TrimSpace(model.LatencyTier) == "" {
			return fmt.Errorf("model %q is missing latency_tier", key)
		}
		if strings.TrimSpace(model.RiskTier) == "" {
			return fmt.Errorf("model %q is missing risk_tier", key)
		}
		if model.InputCostPerMillionTokensUSD < 0 || model.OutputCostPerMillionTokensUSD < 0 {
			return fmt.Errorf("model %q token costs must be non-negative", key)
		}
	}
	return nil
}

func (registry ModelRegistry) HasModels() bool {
	return len(registry.Models) > 0
}

func (registry ModelRegistry) ModelByKey(key string) (ModelConfig, bool) {
	key = strings.TrimSpace(key)
	for _, model := range registry.Models {
		if strings.TrimSpace(model.Key) == key {
			return model, true
		}
	}
	return ModelConfig{}, false
}

func (model ModelConfig) IsEnabled() bool {
	return model.Enabled == nil || *model.Enabled
}

func (model ModelConfig) EstimatedCostUSD(inputTokens int, outputTokens int) float64 {
	if inputTokens < 0 {
		inputTokens = 0
	}
	if outputTokens < 0 {
		outputTokens = 0
	}
	return (float64(inputTokens)/1_000_000)*model.InputCostPerMillionTokensUSD +
		(float64(outputTokens)/1_000_000)*model.OutputCostPerMillionTokensUSD
}

func normalizeTokens(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}
