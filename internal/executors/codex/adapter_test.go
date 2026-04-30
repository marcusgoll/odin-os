package codex

import (
	"context"
	"strings"
	"testing"

	"odin-os/internal/executors/contract"
)

func TestHeadlessExecutorReportsDeterministicAlphaCapabilities(t *testing.T) {
	t.Parallel()

	executor := NewHeadless()
	if executor.Key() != "codex_headless" {
		t.Fatalf("Key() = %q, want codex_headless", executor.Key())
	}
	if executor.Class() != contract.ExecutorClassPlanBackedCLI {
		t.Fatalf("Class() = %q, want %q", executor.Class(), contract.ExecutorClassPlanBackedCLI)
	}

	capabilities, err := executor.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities() error = %v", err)
	}
	for _, requirement := range []struct {
		name string
		ok   bool
	}{
		{name: "resume", ok: capabilities.SupportsResume},
		{name: "cancel", ok: capabilities.SupportsCancel},
		{name: "tools", ok: capabilities.SupportsTools},
		{name: "cost estimate", ok: capabilities.SupportsCostEstimate},
		{name: "headless plan", ok: capabilities.SupportsHeadlessPlan},
	} {
		if !requirement.ok {
			t.Fatalf("Capabilities() missing %s support: %#v", requirement.name, capabilities)
		}
	}
}

func TestRunTaskCurrentlyReturnsDeterministicOutputWithoutLaunchingCodex(t *testing.T) {
	t.Parallel()

	result, err := NewHeadless().RunTask(context.Background(), contract.TaskSpec{
		ID:     "task-42-run-7",
		Kind:   contract.TaskKindBuild,
		Scope:  "project",
		Prompt: "ship the characterization test",
		Metadata: map[string]string{
			"project_key":   "odin-core",
			"worktree_path": "/tmp/odin/worktrees/odin-core/task-42",
		},
	})
	if err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}

	if result.Status != "completed" {
		t.Fatalf("RunTask().Status = %q, want completed", result.Status)
	}
	if result.Handle.ExecutorKey != "codex_headless" {
		t.Fatalf("RunTask().Handle.ExecutorKey = %q, want codex_headless", result.Handle.ExecutorKey)
	}
	if result.Handle.ExternalID != "task-42-run-7" {
		t.Fatalf("RunTask().Handle.ExternalID = %q, want task ID", result.Handle.ExternalID)
	}
	for _, want := range []string{
		"codex_headless completed build task task-42-run-7",
		"in project scope",
		"ship the characterization test",
	} {
		if !strings.Contains(result.Output, want) {
			t.Fatalf("RunTask().Output = %q, want %q", result.Output, want)
		}
	}
	if result.Metadata["executor_class"] != string(contract.ExecutorClassPlanBackedCLI) {
		t.Fatalf("RunTask().Metadata[executor_class] = %q", result.Metadata["executor_class"])
	}
	if result.Metadata["lane"] != "local_deterministic_alpha" {
		t.Fatalf("RunTask().Metadata[lane] = %q", result.Metadata["lane"])
	}
	if _, ok := result.Metadata["worktree_path"]; ok {
		t.Fatalf("RunTask().Metadata unexpectedly echoed task metadata: %#v", result.Metadata)
	}
}

func TestResumeTaskCurrentlyReturnsSummaryOnlyMetadata(t *testing.T) {
	t.Parallel()

	result, err := NewHeadless().ResumeTask(context.Background(), contract.TaskHandle{
		ExecutorKey: "codex_headless",
		ExternalID:  "task-42-run-7",
		Status:      "waiting",
	}, contract.ResumePacket{
		Kind:    "operator_note",
		Summary: "continue with tests",
	})
	if err != nil {
		t.Fatalf("ResumeTask() error = %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("ResumeTask().Status = %q, want completed", result.Status)
	}
	if !strings.Contains(result.Output, "codex_headless resumed task-42-run-7 with continue with tests") {
		t.Fatalf("ResumeTask().Output = %q", result.Output)
	}
	if result.Metadata["resume_kind"] != "operator_note" {
		t.Fatalf("ResumeTask().Metadata[resume_kind] = %q", result.Metadata["resume_kind"])
	}
	if len(result.Metadata) != 1 {
		t.Fatalf("ResumeTask().Metadata = %#v, want only resume_kind", result.Metadata)
	}
}
