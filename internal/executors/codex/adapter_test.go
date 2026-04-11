package codex

import (
	"context"
	"testing"

	"odin-os/internal/executors/contract"
)

func TestHeadlessRunTaskUsesDriverScript(t *testing.T) {
	t.Setenv("ODIN_CODEX_DRIVER_MODE", "fixture")

	executor := NewHeadless()
	result, err := executor.RunTask(context.Background(), contract.TaskSpec{
		ID:     "runtime-smoke",
		Kind:   contract.TaskKindGeneral,
		Scope:  "project",
		Prompt: "say ready",
		Metadata: map[string]string{
			"project_key": "alpha",
		},
	})
	if err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("Status = %q, want completed", result.Status)
	}
	if result.Metadata["driver"] != "codex_headless_script" {
		t.Fatalf("driver metadata = %q, want codex_headless_script", result.Metadata["driver"])
	}
}
