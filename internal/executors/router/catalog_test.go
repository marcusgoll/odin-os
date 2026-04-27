package router

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"odin-os/internal/executors/contract"
)

func TestDefaultCatalogRegistersSkeletonAdapters(t *testing.T) {
	t.Parallel()

	catalog := DefaultCatalog()

	for key, wantClass := range map[string]contract.ExecutorClass{
		"codex_headless":       contract.ExecutorClassPlanBackedCLI,
		"claude_code_headless": contract.ExecutorClassPlanBackedCLI,
		"gemini_cli_headless":  contract.ExecutorClassPlanBackedCLI,
		"sandcastle_headless":  contract.ExecutorClassPlanBackedCLI,
		"openai_api":           contract.ExecutorClassAPI,
		"anthropic_api":        contract.ExecutorClassAPI,
		"google_api":           contract.ExecutorClassAPI,
		"xai_api":              contract.ExecutorClassAPI,
		"openrouter_api":       contract.ExecutorClassBroker,
	} {
		executor, ok := catalog[key]
		if !ok {
			t.Fatalf("missing executor %q", key)
		}
		if executor.Class() != wantClass {
			t.Fatalf("executor %q class = %q, want %q", key, executor.Class(), wantClass)
		}

		caps, err := executor.Capabilities(context.Background())
		if err != nil {
			t.Fatalf("Capabilities(%q) error = %v", key, err)
		}
		if caps.ExecutorClass != wantClass {
			t.Fatalf("capabilities class for %q = %q, want %q", key, caps.ExecutorClass, wantClass)
		}
	}
}

func TestRepoConfigResearchRouteUsesHarnessBackedLanesOnly(t *testing.T) {
	t.Parallel()

	cfg, err := LoadConfig(filepath.Clean(filepath.Join("..", "..", "..", "config", "executors.yaml")))
	if err != nil {
		t.Fatalf("LoadConfig(repo executors) error = %v", err)
	}

	var research RouteConfig
	found := false
	for _, route := range cfg.Routes {
		if route.Name == "research" {
			research = route
			found = true
			break
		}
	}
	if !found {
		t.Fatal("research route missing from repo config")
	}

	wantPreferred := []string{"codex_headless", "claude_code_headless"}
	if len(research.Preferred) != len(wantPreferred) {
		t.Fatalf("research.Preferred = %#v, want %#v", research.Preferred, wantPreferred)
	}
	for index, key := range wantPreferred {
		if research.Preferred[index] != key {
			t.Fatalf("research.Preferred = %#v, want %#v", research.Preferred, wantPreferred)
		}
	}
	if len(research.Fallback) != 0 {
		t.Fatalf("research.Fallback = %#v, want empty", research.Fallback)
	}
}

func TestExecutorCatalogRejectsStaleConfig(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "executors.yaml")
	if err := os.WriteFile(configPath, []byte(`
version: 1
executors:
  - key: stale_executor
    adapter: codex_headless
    class: plan_backed_cli
    enabled: true
    priority: 10
routes:
  - name: default
    match:
      task_kinds: [build]
      scopes: [project]
    preferred: [stale_executor]
`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := LoadConfig(configPath); err == nil {
		t.Fatal("LoadConfig() error = nil, want stale executor config rejection")
	}
}
