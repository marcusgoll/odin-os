package transfers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"odin-os/internal/core/projects"
	"odin-os/internal/runtime/checkpoints"
	"odin-os/internal/store/sqlite"
)

func TestServicePrepareCreatesApprovalWaitTransfer(t *testing.T) {
	ctx := context.Background()
	store := openTransferTestStore(t)
	registry := writeTransferRegistry(t)
	fixed := time.Date(2026, 4, 22, 3, 4, 5, 0, time.UTC)
	t.Setenv("ODIN_HUGINN_ROBINHOOD_TRANSFER_DRIVER", writeTransferDriver(t, `#!/usr/bin/env bash
printf '{"status":"completed","tool_key":"robinhood_transfer_flow","summary":"Robinhood transfer review ready","artifacts":{"session_state":"review_ready","current_url":"https://robinhood.com/transfer","next_action":"request approval"}}'
`))

	result, err := Service{
		Store:    store,
		Registry: registry,
		Now:      func() time.Time { return fixed },
	}.Prepare(ctx, PrepareParams{
		ProjectKey:         "family-ops",
		Direction:          "deposit",
		AmountUSD:          "25.00",
		SourceAccount:      "checking",
		DestinationAccount: "brokerage",
		Memo:               "household-test",
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	if result.Task.Key != "robinhood-transfer-20260422-030405" {
		t.Fatalf("Task.Key = %q, want %q", result.Task.Key, "robinhood-transfer-20260422-030405")
	}
	if result.Task.Status != "blocked" {
		t.Fatalf("Task.Status = %q, want %q", result.Task.Status, "blocked")
	}
	if result.Run.Status != "completed" {
		t.Fatalf("Run.Status = %q, want %q", result.Run.Status, "completed")
	}
	if result.Approval.Status != "pending" {
		t.Fatalf("Approval.Status = %q, want %q", result.Approval.Status, "pending")
	}
	if result.WakePacket.Trigger != "approval_wait" {
		t.Fatalf("WakePacket.Trigger = %q, want %q", result.WakePacket.Trigger, "approval_wait")
	}
	if result.Summary != "review prepared and awaiting approval" {
		t.Fatalf("Summary = %q, want %q", result.Summary, "review prepared and awaiting approval")
	}

	project, err := store.GetProjectByKey(ctx, "family-ops")
	if err != nil {
		t.Fatalf("GetProjectByKey() error = %v", err)
	}
	latestWake, err := store.GetLatestTaskWakePacket(ctx, project.ID, result.Task.ID)
	if err != nil {
		t.Fatalf("GetLatestTaskWakePacket() error = %v", err)
	}
	if latestWake.ID != result.WakePacket.ID {
		t.Fatalf("latest wake packet id = %d, want %d", latestWake.ID, result.WakePacket.ID)
	}

	resume, err := checkpoints.Service{Store: store}.LoadResumeState(ctx, project.ID, result.Task.ID)
	if err != nil {
		t.Fatalf("LoadResumeState() error = %v", err)
	}
	if resume.BlockingReason != "approval_required" {
		t.Fatalf("LoadResumeState().BlockingReason = %q, want %q", resume.BlockingReason, "approval_required")
	}
}

func TestPreparePersistsReviewReadyDriverArtifact(t *testing.T) {
	ctx := context.Background()
	store := openTransferTestStore(t)
	registry := writeTransferRegistry(t)
	t.Setenv("ODIN_HUGINN_ROBINHOOD_TRANSFER_DRIVER", writeTransferDriver(t, `#!/usr/bin/env bash
printf '{"status":"completed","tool_key":"robinhood_transfer_flow","summary":"Robinhood transfer review ready","artifacts":{"session_state":"review_ready","current_url":"https://robinhood.com/transfer","next_action":"request approval"}}'
`))

	result, err := Service{
		Store:    store,
		Registry: registry,
	}.Prepare(ctx, PrepareParams{
		ProjectKey:         "family-ops",
		Direction:          "deposit",
		AmountUSD:          "25.00",
		SourceAccount:      "checking",
		DestinationAccount: "brokerage",
		Memo:               "household-test",
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	artifacts, err := store.ListRunArtifacts(ctx, sqlite.ListRunArtifactsParams{RunID: result.Run.ID})
	if err != nil {
		t.Fatalf("ListRunArtifacts() error = %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("ListRunArtifacts() len = %d, want 1", len(artifacts))
	}
	if artifacts[0].ArtifactType != "driver_result" {
		t.Fatalf("ArtifactType = %q, want %q", artifacts[0].ArtifactType, "driver_result")
	}
	if artifacts[0].Summary != "Robinhood transfer review ready" {
		t.Fatalf("Summary = %q, want %q", artifacts[0].Summary, "Robinhood transfer review ready")
	}
	if !strings.Contains(artifacts[0].DetailsJSON, `"session_state":"review_ready"`) {
		t.Fatalf("DetailsJSON = %q, want session_state review_ready", artifacts[0].DetailsJSON)
	}
}

func TestServicePrepareRejectsInvalidTransferFacts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTransferTestStore(t)
	registry := writeTransferRegistry(t)

	_, err := Service{Store: store, Registry: registry}.Prepare(ctx, PrepareParams{
		ProjectKey:         "family-ops",
		Direction:          "sideways",
		AmountUSD:          "0",
		SourceAccount:      "",
		DestinationAccount: "brokerage",
	})
	if err == nil {
		t.Fatalf("Prepare() error = nil, want validation failure")
	}
}

func openTransferTestStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	return store
}

func writeTransferRegistry(t *testing.T) projects.Registry {
	t.Helper()

	root := t.TempDir()
	configPath := filepath.Join(root, "projects.yaml")
	familyOpsRoot := filepath.Join(root, "family-ops")
	if err := os.MkdirAll(filepath.Join(familyOpsRoot, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir git root: %v", err)
	}

	content := `version: 1
projects:
  - key: family-ops
    name: Family-Ops
    project_class: local_git_project
    git_root: ` + familyOpsRoot + `
    default_branch: main
    policy:
      allowed_commands: [status]
      branch_rules:
        protected_branches: [main]
        require_worktree: true
        require_task_branch: true
        allow_default_branch_mutation: false
      approval_gates:
        require_for_governance_changes: true
        require_for_destructive_operations: true
        require_for_system_project_changes: false
      merge_policy:
        mode: squash
        allow_direct_to_default_branch: false
      destructive_operations:
        allow_reset: false
        allow_clean: false
        allow_force_push: false
        require_explicit_approval: true
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	registry, diagnostics, err := projects.Register(configPath)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("Register() diagnostics = %#v", diagnostics)
	}

	return registry
}

func writeTransferDriver(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "robinhood-transfer-driver.sh")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("Chmod(driver) error = %v", err)
	}
	return path
}
