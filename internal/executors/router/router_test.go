package router

import (
	"context"
	"os"
	"path/filepath"
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

func writeRouterConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "executors.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
