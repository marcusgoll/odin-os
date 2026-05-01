package commands

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/core/projects"
	"odin-os/internal/registry"
	"odin-os/internal/tracker"
)

func TestRunWorkSimulateLifecycleJSONPlansStage2WithoutTouchingGitHub(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	projectRegistry := lifecycleCommandProjectRegistry(t)
	t.Setenv("ODIN_DRY_RUN", "true")
	token := "github_pat_1234567890abcdefghijklmnopqrstuvwxyz"
	t.Setenv("GITHUB_TOKEN", token)

	var output strings.Builder
	err := RunWork(ctx, store, projectRegistry, registry.Snapshot{}, []string{"simulate-lifecycle", "--issue", "123", "--json"}, &output)
	if err != nil {
		t.Fatalf("RunWork(simulate-lifecycle --json) error = %v", err)
	}
	if strings.Contains(output.String(), token) {
		t.Fatalf("output leaked token:\n%s", output.String())
	}

	var report struct {
		Project        string `json:"project"`
		Repo           string `json:"repo"`
		Issue          int    `json:"issue"`
		DryRun         bool   `json:"dry_run"`
		GitHubWrites   int    `json:"github_writes"`
		PlannedActions []struct {
			Sequence int    `json:"sequence"`
			Action   string `json:"action"`
			Label    string `json:"label,omitempty"`
			Body     string `json:"body,omitempty"`
		} `json:"planned_actions"`
		Logs []struct {
			Level   string `json:"level"`
			Message string `json:"message"`
		} `json:"logs"`
		MethodAudit struct {
			Reads  int `json:"reads"`
			Writes int `json:"writes"`
		} `json:"method_audit"`
		Redaction struct {
			TokenEnv      string `json:"token_env"`
			TokenPresent  bool   `json:"token_present"`
			TokenRedacted bool   `json:"token_redacted"`
			TokenValue    string `json:"token_value"`
		} `json:"redaction"`
		Dispatch       string `json:"dispatch"`
		PRs            string `json:"prs"`
		CodexExecution string `json:"codex_execution"`
	}
	if err := json.Unmarshal([]byte(output.String()), &report); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\noutput:\n%s", err, output.String())
	}
	if report.Project != "odin-core" || report.Repo != "marcusgoll/odin-os" || report.Issue != 123 || !report.DryRun {
		t.Fatalf("report target = project %q repo %q issue %d dry_run %t, want odin-core/marcusgoll/odin-os#123 dry_run=true", report.Project, report.Repo, report.Issue, report.DryRun)
	}
	if report.GitHubWrites != 0 || report.MethodAudit.Reads != 0 || report.MethodAudit.Writes != 0 {
		t.Fatalf("report audit = github_writes %d method %+v, want zero GitHub HTTP requests", report.GitHubWrites, report.MethodAudit)
	}
	if report.Redaction.TokenEnv != "GITHUB_TOKEN" || !report.Redaction.TokenPresent || !report.Redaction.TokenRedacted || report.Redaction.TokenValue != "[REDACTED]" {
		t.Fatalf("redaction = %+v, want GITHUB_TOKEN present and redacted", report.Redaction)
	}
	if report.Dispatch != "not_started" || report.PRs != "not_created" || report.CodexExecution != "not_started" {
		t.Fatalf("runtime side effects = dispatch %q prs %q codex %q, want all disabled", report.Dispatch, report.PRs, report.CodexExecution)
	}

	wantActions := []struct {
		action string
		label  string
		body   string
	}{
		{action: "add_label", label: tracker.LabelRunning},
		{action: "add_label", label: tracker.LabelHumanReview},
		{action: "add_label", label: tracker.LabelFailed},
		{action: "add_comment", body: "Stage 2 dry-run lifecycle proof: simulated failure path."},
	}
	if len(report.PlannedActions) != len(wantActions) {
		t.Fatalf("planned_actions = %+v, want %d actions", report.PlannedActions, len(wantActions))
	}
	for index, want := range wantActions {
		got := report.PlannedActions[index]
		if got.Sequence != index+1 || got.Action != want.action || got.Label != want.label || got.Body != want.body {
			t.Fatalf("planned_actions[%d] = %+v, want sequence=%d action=%s label=%s body=%q", index, got, index+1, want.action, want.label, want.body)
		}
	}
	if len(report.Logs) != len(wantActions) {
		t.Fatalf("logs = %+v, want one log per planned action", report.Logs)
	}
	for _, log := range report.Logs {
		if log.Level != "info" || !strings.Contains(log.Message, "planned") || strings.Contains(log.Message, token) {
			t.Fatalf("log = %+v, want safe planned-action log", log)
		}
	}

	for _, table := range []string{"external_issues", "tasks", "runs", "approvals"} {
		var count int
		if err := store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s count = %d, want no local lifecycle side effects", table, count)
		}
	}
}

func lifecycleCommandProjectRegistry(t *testing.T) projects.Registry {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir git root: %v", err)
	}
	path := filepath.Join(root, "projects.yaml")
	if err := os.WriteFile(path, []byte(`
version: 1
projects:
  - key: odin-core
    name: Odin Core
    project_class: system_project
    system_project: true
    git_root: .
    default_branch: main
    github:
      repo: marcusgoll/odin-os
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
        require_for_system_project_changes: true
      merge_policy:
        mode: squash
        allow_direct_to_default_branch: false
      destructive_operations:
        allow_reset: false
        allow_clean: false
        allow_force_push: false
        require_explicit_approval: true
`), 0o644); err != nil {
		t.Fatalf("write projects: %v", err)
	}
	registry, diagnostics, err := projects.Register(path)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v, want none", diagnostics)
	}
	return registry
}
