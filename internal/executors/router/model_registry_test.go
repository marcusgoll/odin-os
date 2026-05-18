package router

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadModelRegistryValidatesTypedProviderMetadata(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "models.yaml")
	if err := os.WriteFile(path, []byte(testModelRegistryYAML()), 0o644); err != nil {
		t.Fatalf("write models config: %v", err)
	}

	registry, err := LoadModelRegistry(path)
	if err != nil {
		t.Fatalf("LoadModelRegistry() error = %v", err)
	}
	model, ok := registry.ModelByKey("openrouter-kimi-k2-6")
	if !ok {
		t.Fatalf("ModelByKey(openrouter-kimi-k2-6) missing")
	}
	if model.Provider != "openrouter" || model.ProviderModelID != "fixture/openrouter-kimi-k2-6" {
		t.Fatalf("model provider metadata = %+v, want fixture OpenRouter Kimi", model)
	}
	if model.ContextWindowTokens != 128000 || model.RiskTier != "external_grunt" {
		t.Fatalf("model policy metadata = %+v, want context/risk populated", model)
	}
}

func TestLoadModelRegistryRejectsDuplicateModelKeys(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "models.yaml")
	if err := os.WriteFile(path, []byte(`
version: 1
models:
  - key: duplicate
    provider: openai
    access: plan_backed_cli
    adapter: codex_headless
    capabilities: [judgment]
    supported_task_classes: [general]
    context_window_tokens: 100000
    latency_tier: interactive
    risk_tier: premium_judgment
  - key: duplicate
    provider: openai
    access: plan_backed_cli
    adapter: codex_headless
    capabilities: [judgment]
    supported_task_classes: [general]
    context_window_tokens: 100000
    latency_tier: interactive
    risk_tier: premium_judgment
`), 0o644); err != nil {
		t.Fatalf("write models config: %v", err)
	}

	_, err := LoadModelRegistry(path)
	if err == nil || !strings.Contains(err.Error(), "declared more than once") {
		t.Fatalf("LoadModelRegistry() error = %v, want duplicate rejection", err)
	}
}

func TestLoadConfigWithModelRegistryRejectsUnknownModelRef(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	executorPath := filepath.Join(dir, "executors.yaml")
	modelsPath := filepath.Join(dir, "models.yaml")
	if err := os.WriteFile(executorPath, []byte(`
version: 1
executors:
  - key: openrouter_api
    adapter: openrouter_api
    class: broker_executor
    enabled: true
    priority: 10
    model_ref: missing-openrouter-model
routes:
  - name: default
    match:
      task_kinds: [build]
      scopes: [project]
    preferred: [openrouter_api]
`), 0o644); err != nil {
		t.Fatalf("write executors config: %v", err)
	}
	if err := os.WriteFile(modelsPath, []byte(testModelRegistryYAML()), 0o644); err != nil {
		t.Fatalf("write models config: %v", err)
	}

	_, _, err := LoadConfigWithModelRegistry(executorPath, modelsPath)
	if err == nil || !strings.Contains(err.Error(), "unknown model_ref") {
		t.Fatalf("LoadConfigWithModelRegistry() error = %v, want unknown model_ref", err)
	}
}
