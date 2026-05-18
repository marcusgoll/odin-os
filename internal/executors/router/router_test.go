package router

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"odin-os/internal/executors/contract"
)

func TestLoadConfigAndSelectPrimaryExecutor(t *testing.T) {
	t.Parallel()

	configPath := writeRouterConfig(t, `
version: 1
executors:
  - key: codex_headless
    adapter: codex_headless
    class: plan_backed_cli
    enabled: true
    priority: 10
  - key: openai_api
    adapter: openai_api
    class: api_executor
    enabled: true
    priority: 20
routes:
  - name: default
    match:
      task_kinds: [build]
      scopes: [project]
    preferred: [codex_headless]
    fallback: [openai_api]
`)

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	selector := Selector{
		Config: cfg,
		Executors: map[string]contract.Executor{
			"codex_headless": contract.NewStaticExecutor(
				"codex_headless",
				contract.ExecutorClassPlanBackedCLI,
				contract.HealthReport{Status: contract.HealthStatusHealthy, CheckedAt: time.Now().UTC()},
				contract.Capabilities{
					ExecutorClass:        contract.ExecutorClassPlanBackedCLI,
					SupportsResume:       true,
					SupportsCancel:       true,
					SupportsTools:        true,
					SupportsHeadlessPlan: true,
					TaskKinds:            []contract.TaskKind{contract.TaskKindBuild},
					Scopes:               []string{"project"},
				},
			),
			"openai_api": contract.NewStaticExecutor(
				"openai_api",
				contract.ExecutorClassAPI,
				contract.HealthReport{Status: contract.HealthStatusHealthy, CheckedAt: time.Now().UTC()},
				contract.Capabilities{
					ExecutorClass:        contract.ExecutorClassAPI,
					SupportsResume:       true,
					SupportsCancel:       true,
					SupportsTools:        true,
					SupportsCostEstimate: true,
					TaskKinds:            []contract.TaskKind{contract.TaskKindBuild},
					Scopes:               []string{"project"},
				},
			),
		},
	}

	decision, err := selector.Select(context.Background(), contract.TaskSpec{
		ID:    "task-1",
		Kind:  contract.TaskKindBuild,
		Scope: "project",
		Requirements: contract.Requirements{
			AllowedClasses:    []contract.ExecutorClass{contract.ExecutorClassPlanBackedCLI, contract.ExecutorClassAPI},
			NeedsTools:        true,
			NeedsHeadlessPlan: true,
		},
	})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}

	if decision.ExecutorKey != "codex_headless" {
		t.Fatalf("ExecutorKey = %q, want codex_headless", decision.ExecutorKey)
	}
	if decision.RouteName != "default" {
		t.Fatalf("RouteName = %q, want default", decision.RouteName)
	}
	if decision.FallbackUsed {
		t.Fatalf("FallbackUsed = true, want false")
	}
}

func TestSelectFallsBackWhenPrimaryUnavailable(t *testing.T) {
	t.Parallel()

	configPath := writeRouterConfig(t, `
version: 1
executors:
  - key: codex_headless
    adapter: codex_headless
    class: plan_backed_cli
    enabled: true
    priority: 10
  - key: openrouter_api
    adapter: openrouter_api
    class: broker_executor
    enabled: true
    priority: 30
routes:
  - name: default
    match:
      task_kinds: [research]
      scopes: [global]
    preferred: [codex_headless]
    fallback: [openrouter_api]
`)

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	selector := Selector{
		Config: cfg,
		Executors: map[string]contract.Executor{
			"codex_headless": contract.NewStaticExecutor(
				"codex_headless",
				contract.ExecutorClassPlanBackedCLI,
				contract.HealthReport{Status: contract.HealthStatusUnavailable, CheckedAt: time.Now().UTC()},
				contract.Capabilities{
					ExecutorClass:        contract.ExecutorClassPlanBackedCLI,
					SupportsResume:       true,
					SupportsCancel:       true,
					SupportsTools:        true,
					SupportsHeadlessPlan: true,
					TaskKinds:            []contract.TaskKind{contract.TaskKindResearch},
					Scopes:               []string{"global"},
				},
			),
			"openrouter_api": contract.NewStaticExecutor(
				"openrouter_api",
				contract.ExecutorClassBroker,
				contract.HealthReport{Status: contract.HealthStatusHealthy, CheckedAt: time.Now().UTC()},
				contract.Capabilities{
					ExecutorClass:         contract.ExecutorClassBroker,
					SupportsResume:        true,
					SupportsCancel:        true,
					SupportsTools:         true,
					SupportsCostEstimate:  true,
					SupportsBrokerRouting: true,
					TaskKinds:             []contract.TaskKind{contract.TaskKindResearch},
					Scopes:                []string{"global"},
				},
			),
		},
	}

	decision, err := selector.Select(context.Background(), contract.TaskSpec{
		ID:    "task-2",
		Kind:  contract.TaskKindResearch,
		Scope: "global",
		Requirements: contract.Requirements{
			AllowedClasses:      []contract.ExecutorClass{contract.ExecutorClassPlanBackedCLI, contract.ExecutorClassBroker},
			NeedsTools:          true,
			NeedsBrokerFallback: true,
		},
	})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}

	if decision.ExecutorKey != "openrouter_api" {
		t.Fatalf("ExecutorKey = %q, want openrouter_api", decision.ExecutorKey)
	}
	if !decision.FallbackUsed {
		t.Fatalf("FallbackUsed = false, want true")
	}
}

func TestSelectRejectsExecutorsMissingCapabilities(t *testing.T) {
	t.Parallel()

	configPath := writeRouterConfig(t, `
version: 1
executors:
  - key: openai_api
    adapter: openai_api
    class: api_executor
    enabled: true
    priority: 20
routes:
  - name: default
    match:
      task_kinds: [review]
      scopes: [project]
    preferred: [openai_api]
`)

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	selector := Selector{
		Config: cfg,
		Executors: map[string]contract.Executor{
			"openai_api": contract.NewStaticExecutor(
				"openai_api",
				contract.ExecutorClassAPI,
				contract.HealthReport{Status: contract.HealthStatusHealthy, CheckedAt: time.Now().UTC()},
				contract.Capabilities{
					ExecutorClass:  contract.ExecutorClassAPI,
					SupportsCancel: true,
					TaskKinds:      []contract.TaskKind{contract.TaskKindReview},
					Scopes:         []string{"project"},
				},
			),
		},
	}

	_, err = selector.Select(context.Background(), contract.TaskSpec{
		ID:    "task-3",
		Kind:  contract.TaskKindReview,
		Scope: "project",
		Requirements: contract.Requirements{
			AllowedClasses:    []contract.ExecutorClass{contract.ExecutorClassAPI},
			NeedsResume:       true,
			NeedsTools:        true,
			NeedsCancel:       true,
			NeedsCostEstimate: true,
		},
	})
	if err == nil {
		t.Fatalf("Select() error = nil, want non-nil")
	}
}

func TestRouteSelectionRejectsProvidersMissingStreamingSupport(t *testing.T) {
	t.Parallel()

	configPath := writeRouterConfig(t, `
version: 1
executors:
  - key: openai_api
    adapter: openai_api
    class: api_executor
    enabled: true
    priority: 20
routes:
  - name: default
    match:
      task_kinds: [research]
      scopes: [global]
    preferred: [openai_api]
`)

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	selector := Selector{
		Config: cfg,
		Executors: map[string]contract.Executor{
			"openai_api": contract.NewStaticExecutor(
				"openai_api",
				contract.ExecutorClassAPI,
				contract.HealthReport{Status: contract.HealthStatusHealthy, CheckedAt: time.Now().UTC()},
				contract.Capabilities{
					ExecutorClass: contract.ExecutorClassAPI,
					TaskKinds:     []contract.TaskKind{contract.TaskKindResearch},
					Scopes:        []string{"global"},
				},
			),
		},
	}

	_, err = selector.Select(context.Background(), contract.TaskSpec{
		ID:    "task-4",
		Kind:  contract.TaskKindResearch,
		Scope: "global",
		Requirements: contract.Requirements{
			AllowedClasses: []contract.ExecutorClass{contract.ExecutorClassAPI},
			NeedsStreaming: true,
		},
	})
	if err == nil {
		t.Fatalf("Select() error = nil, want streaming capability rejection")
	}
}

func TestSelectUsesOpenRouterKimiForLowRiskFrontendBuild(t *testing.T) {
	t.Parallel()

	cfg, registry := loadRouterConfigAndModels(t, `
version: 1
executors:
  - key: codex_headless
    adapter: codex_headless
    class: plan_backed_cli
    enabled: true
    priority: 10
    model_ref: codex-latest
  - key: openrouter_api
    adapter: openrouter_api
    class: broker_executor
    enabled: true
    priority: 20
    model_ref: broker-default
routes:
  - name: low-risk-frontend-build
    match:
      task_kinds: [build]
      scopes: [project]
      task_classes: [frontend_build]
      risk_classes: [low]
    preferred: [openrouter_api]
    fallback: [codex_headless]
    model_ref_overrides:
      openrouter_api: openrouter-kimi-k2-6
`, testModelRegistryYAML())

	decision, err := testSelector(cfg, registry).Select(context.Background(), contract.TaskSpec{
		ID:        "task-kimi",
		Kind:      contract.TaskKindBuild,
		Scope:     "project",
		TaskClass: "frontend_build",
		RiskClass: "low",
		Budget: contract.BudgetHints{
			MaxInputTokens:  20000,
			MaxOutputTokens: 8000,
			MaxCostUSD:      1.00,
		},
		Requirements: contract.Requirements{
			AllowedClasses:  []contract.ExecutorClass{contract.ExecutorClassBroker, contract.ExecutorClassPlanBackedCLI},
			CapabilityNeeds: []string{"frontend"},
			NeedsTools:      true,
		},
	})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if decision.ExecutorKey != "openrouter_api" || decision.ModelKey != "openrouter-kimi-k2-6" {
		t.Fatalf("decision = %+v, want openrouter_api/openrouter-kimi-k2-6", decision)
	}
	if decision.ProviderKey != "openrouter" || decision.ProviderModelID != "fixture/openrouter-kimi-k2-6" {
		t.Fatalf("provider metadata = %q/%q, want openrouter fixture Kimi", decision.ProviderKey, decision.ProviderModelID)
	}
	if decision.FallbackUsed {
		t.Fatalf("FallbackUsed = true, want false")
	}
}

func TestConfiguredPolicyRoutesOnlyLowRiskFrontendBuildToOpenRouter(t *testing.T) {
	t.Parallel()

	cfg, registry, err := LoadConfigWithModelRegistry(filepath.Join(repoRootForTest(t), "config", "executors.yaml"), filepath.Join(repoRootForTest(t), "config", "models.yaml"))
	if err != nil {
		t.Fatalf("LoadConfigWithModelRegistry(repo config) error = %v", err)
	}

	decision, err := testSelector(cfg, registry).Select(context.Background(), contract.TaskSpec{
		ID:        "task-config-frontend",
		Kind:      contract.TaskKindGeneral,
		Scope:     "project",
		TaskClass: "frontend_build",
		RiskClass: "low",
		Budget: contract.BudgetHints{
			MaxInputTokens:  20000,
			MaxOutputTokens: 8000,
			MaxCostUSD:      1.00,
		},
		Requirements: contract.Requirements{
			AllowedClasses:  []contract.ExecutorClass{contract.ExecutorClassBroker, contract.ExecutorClassPlanBackedCLI},
			CapabilityNeeds: []string{"frontend"},
			NeedsTools:      true,
		},
	})
	if err != nil {
		t.Fatalf("Select(low risk frontend) error = %v", err)
	}
	if decision.ExecutorKey != "openrouter_api" || decision.ModelKey != "openrouter-kimi-k2-6" || decision.RouteName != "low-risk-frontend-build" {
		t.Fatalf("low risk frontend decision = %+v, want OpenRouter Kimi route", decision)
	}

	elevated, err := testSelector(cfg, registry).Select(context.Background(), contract.TaskSpec{
		ID:        "task-config-elevated",
		Kind:      contract.TaskKindBuild,
		Scope:     "project",
		TaskClass: "frontend_build",
		RiskClass: "medium",
		Requirements: contract.Requirements{
			AllowedClasses:  []contract.ExecutorClass{contract.ExecutorClassBroker, contract.ExecutorClassPlanBackedCLI},
			CapabilityNeeds: []string{"frontend"},
			NeedsTools:      true,
		},
	})
	if err != nil {
		t.Fatalf("Select(elevated frontend) error = %v", err)
	}
	if elevated.ExecutorKey != "codex_headless" || elevated.ModelKey != "codex-latest" || elevated.RouteName != "elevated-frontend-build" {
		t.Fatalf("elevated frontend decision = %+v, want Premium Codex elevated frontend route", elevated)
	}

	backend, err := testSelector(cfg, registry).Select(context.Background(), contract.TaskSpec{
		ID:        "task-config-backend",
		Kind:      contract.TaskKindBuild,
		Scope:     "project",
		TaskClass: "backend_build",
		RiskClass: "low",
		Requirements: contract.Requirements{
			AllowedClasses:  []contract.ExecutorClass{contract.ExecutorClassBroker, contract.ExecutorClassPlanBackedCLI},
			CapabilityNeeds: []string{"code"},
			NeedsTools:      true,
		},
	})
	if err != nil {
		t.Fatalf("Select(low risk backend) error = %v", err)
	}
	if backend.ExecutorKey != "openrouter_api" || backend.ModelKey != "openrouter-kimi-k2-6" || backend.RouteName != "low-risk-backend-build" {
		t.Fatalf("low risk backend decision = %+v, want OpenRouter Kimi backend route", backend)
	}

	elevatedBackend, err := testSelector(cfg, registry).Select(context.Background(), contract.TaskSpec{
		ID:        "task-config-elevated-backend",
		Kind:      contract.TaskKindBuild,
		Scope:     "project",
		TaskClass: "backend_build",
		RiskClass: "medium",
		Requirements: contract.Requirements{
			AllowedClasses:  []contract.ExecutorClass{contract.ExecutorClassBroker, contract.ExecutorClassPlanBackedCLI},
			CapabilityNeeds: []string{"backend"},
			NeedsTools:      true,
		},
	})
	if err != nil {
		t.Fatalf("Select(elevated backend) error = %v", err)
	}
	if elevatedBackend.ExecutorKey != "codex_headless" || elevatedBackend.ModelKey != "codex-latest" || elevatedBackend.RouteName != "elevated-backend-build" {
		t.Fatalf("elevated backend decision = %+v, want Premium Codex elevated backend route", elevatedBackend)
	}
}

func TestSelectBlocksHighRiskBrokerModelAndFallsBackToPlanBacked(t *testing.T) {
	t.Parallel()

	cfg, registry := loadRouterConfigAndModels(t, `
version: 1
executors:
  - key: openrouter_api
    adapter: openrouter_api
    class: broker_executor
    enabled: true
    priority: 10
    model_ref: openrouter-kimi-k2-6
  - key: codex_headless
    adapter: codex_headless
    class: plan_backed_cli
    enabled: true
    priority: 20
    model_ref: codex-latest
routes:
  - name: high-risk-review
    match:
      task_kinds: [review]
      scopes: [project]
    preferred: [openrouter_api]
    fallback: [codex_headless]
`, testModelRegistryYAML())

	decision, err := testSelector(cfg, registry).Select(context.Background(), contract.TaskSpec{
		ID:        "task-risk",
		Kind:      contract.TaskKindReview,
		Scope:     "project",
		TaskClass: "security_decision",
		RiskClass: "high",
		Requirements: contract.Requirements{
			AllowedClasses: []contract.ExecutorClass{contract.ExecutorClassBroker, contract.ExecutorClassPlanBackedCLI},
		},
	})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if decision.ExecutorKey != "codex_headless" || decision.ModelKey != "codex-latest" {
		t.Fatalf("decision = %+v, want codex fallback", decision)
	}
	if !decision.FallbackUsed {
		t.Fatalf("FallbackUsed = false, want true")
	}
	if len(decision.Considered) == 0 || decision.Considered[0].Reason != "blocked_task_class" {
		t.Fatalf("Considered = %+v, want first broker candidate blocked by task class", decision.Considered)
	}
}

func TestSelectFallsBackWhenPreferredModelExceedsBudget(t *testing.T) {
	t.Parallel()

	cfg, registry := loadRouterConfigAndModels(t, `
version: 1
executors:
  - key: openrouter_api
    adapter: openrouter_api
    class: broker_executor
    enabled: true
    priority: 10
    model_ref: openrouter-kimi-k2-6
  - key: codex_headless
    adapter: codex_headless
    class: plan_backed_cli
    enabled: true
    priority: 20
    model_ref: codex-latest
routes:
  - name: low-risk-build
    match:
      task_kinds: [build]
      scopes: [project]
    preferred: [openrouter_api]
    fallback: [codex_headless]
`, testModelRegistryYAML())

	decision, err := testSelector(cfg, registry).Select(context.Background(), contract.TaskSpec{
		ID:        "task-budget",
		Kind:      contract.TaskKindBuild,
		Scope:     "project",
		TaskClass: "frontend_build",
		RiskClass: "low",
		Budget: contract.BudgetHints{
			MaxInputTokens:  90000,
			MaxOutputTokens: 30000,
			MaxCostUSD:      0.01,
		},
		Requirements: contract.Requirements{
			AllowedClasses: []contract.ExecutorClass{contract.ExecutorClassBroker, contract.ExecutorClassPlanBackedCLI},
		},
	})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if decision.ExecutorKey != "codex_headless" {
		t.Fatalf("ExecutorKey = %q, want codex_headless", decision.ExecutorKey)
	}
	if len(decision.Considered) == 0 || decision.Considered[0].Reason != "budget_exceeded" {
		t.Fatalf("Considered = %+v, want budget_exceeded for preferred model", decision.Considered)
	}
}

func TestSelectFailsClosedWhenExecutorModelConfigMissing(t *testing.T) {
	t.Parallel()

	cfg, registry := loadRouterConfigAndModels(t, `
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
`, `
version: 1
models:
  - key: codex-latest
    provider: openai
    access: plan_backed_cli
    adapter: codex_headless
    capabilities: [judgment]
    supported_task_classes: [build]
    context_window_tokens: 100000
    latency_tier: interactive
    risk_tier: premium_judgment
    allow_high_risk: true
`)

	decision, err := testSelector(cfg, registry).Select(context.Background(), contract.TaskSpec{
		ID:        "task-missing",
		Kind:      contract.TaskKindBuild,
		Scope:     "project",
		TaskClass: "frontend_build",
		RiskClass: "low",
	})
	if err == nil {
		t.Fatalf("Select() error = nil, want missing model config failure")
	}
	if len(decision.Considered) != 1 || decision.Considered[0].Reason != "missing_model_config" {
		t.Fatalf("Considered = %+v, want missing_model_config", decision.Considered)
	}
}

func writeRouterConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "executors.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func repoRootForTest(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repo root not found from %q", dir)
		}
		dir = parent
	}
}

func loadRouterConfigAndModels(t *testing.T, executorConfig string, modelsConfig string) (Config, ModelRegistry) {
	t.Helper()

	dir := t.TempDir()
	executorPath := filepath.Join(dir, "executors.yaml")
	modelsPath := filepath.Join(dir, "models.yaml")
	if err := os.WriteFile(executorPath, []byte(executorConfig), 0o644); err != nil {
		t.Fatalf("write executors config: %v", err)
	}
	if err := os.WriteFile(modelsPath, []byte(modelsConfig), 0o644); err != nil {
		t.Fatalf("write models config: %v", err)
	}
	cfg, registry, err := LoadConfigWithModelRegistry(executorPath, modelsPath)
	if err != nil && !strings.Contains(err.Error(), "unknown model_ref") {
		t.Fatalf("LoadConfigWithModelRegistry() error = %v", err)
	}
	if err != nil {
		cfg, loadErr := LoadConfig(executorPath)
		if loadErr != nil {
			t.Fatalf("LoadConfig() error = %v", loadErr)
		}
		registry, loadErr = LoadModelRegistry(modelsPath)
		if loadErr != nil {
			t.Fatalf("LoadModelRegistry() error = %v", loadErr)
		}
		return cfg, registry
	}
	return cfg, registry
}

func testSelector(cfg Config, registry ModelRegistry) Selector {
	return Selector{
		Config:        cfg,
		ModelRegistry: registry,
		Executors: map[string]contract.Executor{
			"codex_headless": testExecutor("codex_headless", contract.ExecutorClassPlanBackedCLI, []contract.TaskKind{contract.TaskKindGeneral, contract.TaskKindBuild, contract.TaskKindReview, contract.TaskKindQA}, []string{"project", "global"}),
			"openrouter_api": testExecutor("openrouter_api", contract.ExecutorClassBroker, []contract.TaskKind{contract.TaskKindGeneral, contract.TaskKindBuild, contract.TaskKindReview, contract.TaskKindQA, contract.TaskKindResearch}, []string{"project", "global"}),
		},
	}
}

func testExecutor(key string, class contract.ExecutorClass, taskKinds []contract.TaskKind, scopes []string) contract.Executor {
	return contract.NewStaticExecutor(
		key,
		class,
		contract.HealthReport{Status: contract.HealthStatusHealthy, CheckedAt: time.Now().UTC()},
		contract.Capabilities{
			ExecutorClass:         class,
			SupportsResume:        true,
			SupportsCancel:        true,
			SupportsTools:         true,
			SupportsCostEstimate:  true,
			SupportsHeadlessPlan:  class == contract.ExecutorClassPlanBackedCLI,
			SupportsBrokerRouting: class == contract.ExecutorClassBroker,
			TaskKinds:             taskKinds,
			Scopes:                scopes,
		},
	)
}

func testModelRegistryYAML() string {
	return `
version: 1
models:
  - key: codex-latest
    provider: openai
    access: plan_backed_cli
    adapter: codex_headless
    capabilities: [judgment, code, frontend]
    supported_task_classes: [general, build, frontend_build, backend_build, review, security_decision]
    supported_features: [headless_plan, tools]
    context_window_tokens: 200000
    max_input_tokens: 160000
    max_output_tokens: 40000
    input_cost_per_million_tokens_usd: 0
    output_cost_per_million_tokens_usd: 0
    latency_tier: interactive
    risk_tier: premium_judgment
    allow_high_risk: true
  - key: broker-default
    provider: openrouter
    access: broker
    adapter: openrouter_api
    capabilities: [code, summarization]
    supported_task_classes: [general, build, frontend_build, backend_build]
    supported_features: [broker_routing, tools]
    context_window_tokens: 128000
    max_input_tokens: 96000
    max_output_tokens: 32000
    input_cost_per_million_tokens_usd: 0.25
    output_cost_per_million_tokens_usd: 0.75
    latency_tier: batch
    risk_tier: external_grunt
    blocked_task_classes: [finance, legal, medical, security_decision, approval_resolution, production_deploy, public_publish]
  - key: openrouter-kimi-k2-6
    provider: openrouter
    provider_model_id: fixture/openrouter-kimi-k2-6
    access: broker
    adapter: openrouter_api
    capabilities: [code, frontend, backend, test_writing]
    supported_task_classes: [build, frontend_build, backend_build, refactor, test_writing, qa]
    supported_features: [broker_routing, tools]
    context_window_tokens: 128000
    max_input_tokens: 96000
    max_output_tokens: 32000
    input_cost_per_million_tokens_usd: 0.25
    output_cost_per_million_tokens_usd: 1.00
    latency_tier: batch
    risk_tier: external_grunt
    blocked_task_classes: [finance, legal, medical, security_decision, approval_resolution, production_deploy, public_publish]
`
}
