package router

import (
	"context"
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
