package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestRunWorkApplyLifecycleMutatesOnlyApprovedStage3Issue(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	projectRegistry := lifecycleCommandProjectRegistry(t)
	t.Setenv("GITHUB_TOKEN", "github_pat_1234567890abcdefghijklmnopqrstuvwxyz")

	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		requests = append(requests, request.Method+" "+request.URL.Path+" "+string(body))
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/repos/marcusgoll/odin-os/issues/123":
			fmt.Fprint(response, `{"number":123,"title":"Stage 3 test","html_url":"https://github.example/marcusgoll/odin-os/issues/123","state":"open","labels":[]}`)
		case request.Method == http.MethodGet && request.URL.Path == "/repos/marcusgoll/odin-os/issues/123/comments":
			fmt.Fprint(response, `[]`)
		case request.Method == http.MethodPost && request.URL.Path == "/repos/marcusgoll/odin-os/issues/123/labels":
			fmt.Fprint(response, `{}`)
		case request.Method == http.MethodDelete && request.URL.Path == "/repos/marcusgoll/odin-os/issues/123/labels/odin:running":
			fmt.Fprint(response, `{}`)
		case request.Method == http.MethodPost && request.URL.Path == "/repos/marcusgoll/odin-os/issues/123/comments":
			fmt.Fprint(response, `{"html_url":"https://github.example/marcusgoll/odin-os/issues/123#issuecomment-1","body":"ok"}`)
		default:
			t.Fatalf("unexpected request: %s %s body=%s", request.Method, request.URL.Path, string(body))
		}
	}))
	defer server.Close()
	t.Setenv("ODIN_GITHUB_API_BASE_URL", server.URL)

	var output strings.Builder
	err := RunWork(ctx, store, projectRegistry, registry.Snapshot{}, []string{
		"apply-lifecycle",
		"--issue", "123",
		"--approved-target", "marcusgoll/odin-os#123",
		"--json",
	}, &output)
	if err != nil {
		t.Fatalf("RunWork(apply-lifecycle) error = %v", err)
	}

	var report struct {
		Project  string `json:"project"`
		Repo     string `json:"repo"`
		Issue    int    `json:"issue"`
		DryRun   bool   `json:"dry_run"`
		Approval struct {
			ApprovedTarget string `json:"approved_target"`
			OperatorSource string `json:"operator_source"`
		} `json:"approval"`
		Before struct {
			Labels []string `json:"labels"`
		} `json:"before"`
		After struct {
			Labels []string `json:"labels"`
		} `json:"after"`
		AppliedActions []struct {
			Action string `json:"action"`
			Label  string `json:"label,omitempty"`
			Body   string `json:"body,omitempty"`
		} `json:"applied_actions"`
		Comment struct {
			Created bool   `json:"created"`
			URL     string `json:"url"`
			Marker  string `json:"marker"`
		} `json:"comment"`
		MethodAudit struct {
			Reads  int `json:"reads"`
			Writes int `json:"writes"`
		} `json:"method_audit"`
		PRs      string `json:"prs"`
		Dispatch string `json:"dispatch"`
	}
	if err := json.Unmarshal([]byte(output.String()), &report); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\noutput:\n%s", err, output.String())
	}
	if report.Project != "odin-core" || report.Repo != "marcusgoll/odin-os" || report.Issue != 123 || report.DryRun {
		t.Fatalf("target = %+v, want live odin-core marcusgoll/odin-os#123", report)
	}
	if report.Approval.ApprovedTarget != "marcusgoll/odin-os#123" || report.Approval.OperatorSource != "command_flag" {
		t.Fatalf("approval = %+v, want command flag target binding", report.Approval)
	}
	if containsString(report.After.Labels, tracker.LabelRunning) || !containsString(report.After.Labels, tracker.LabelHumanReview) {
		t.Fatalf("after labels = %#v, want human-review only among Stage 3 labels", report.After.Labels)
	}
	wantActions := []string{"add_label", "remove_label", "add_label", "add_comment"}
	if len(report.AppliedActions) != len(wantActions) {
		t.Fatalf("applied actions = %+v, want %d actions", report.AppliedActions, len(wantActions))
	}
	for index, want := range wantActions {
		if report.AppliedActions[index].Action != want {
			t.Fatalf("applied_actions[%d] = %+v, want action %s", index, report.AppliedActions[index], want)
		}
	}
	if !report.Comment.Created || report.Comment.Marker != "<!-- odin-stage3-lifecycle-proof -->" || !strings.Contains(report.Comment.URL, "issuecomment-1") {
		t.Fatalf("comment = %+v, want created proof comment with marker", report.Comment)
	}
	if report.MethodAudit.Reads != 2 || report.MethodAudit.Writes != 4 {
		t.Fatalf("audit = %+v, want reads=2 writes=4", report.MethodAudit)
	}
	if report.PRs != "not_created" || report.Dispatch != "not_started" {
		t.Fatalf("prs/dispatch = %s/%s, want disabled", report.PRs, report.Dispatch)
	}
	for _, table := range []string{"external_issues", "tasks", "runs", "approvals"} {
		var count int
		if err := store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s count = %d, want no durable Stage 3 state", table, count)
		}
	}
	for _, want := range []string{
		`GET /repos/marcusgoll/odin-os/issues/123 `,
		`GET /repos/marcusgoll/odin-os/issues/123/comments `,
		`POST /repos/marcusgoll/odin-os/issues/123/labels {"labels":["odin:running"]}`,
		`DELETE /repos/marcusgoll/odin-os/issues/123/labels/odin:running `,
		`POST /repos/marcusgoll/odin-os/issues/123/labels {"labels":["odin:human-review"]}`,
		`POST /repos/marcusgoll/odin-os/issues/123/comments `,
	} {
		if !containsRequestPrefix(requests, want) {
			t.Fatalf("requests = %#v, want %q", requests, want)
		}
	}
}

func TestRunWorkApplyLifecycleIdempotentRerunPerformsZeroWrites(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	projectRegistry := lifecycleCommandProjectRegistry(t)

	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		requests = append(requests, request.Method+" "+request.URL.Path+" "+string(body))
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/repos/marcusgoll/odin-os/issues/123":
			fmt.Fprint(response, `{"number":123,"title":"Stage 3 test","html_url":"https://github.example/marcusgoll/odin-os/issues/123","state":"open","labels":[{"name":"odin:human-review"}]}`)
		case request.Method == http.MethodGet && request.URL.Path == "/repos/marcusgoll/odin-os/issues/123/comments":
			fmt.Fprint(response, `[{"html_url":"https://github.example/marcusgoll/odin-os/issues/123#issuecomment-1","body":"<!-- odin-stage3-lifecycle-proof -->\nStage 3 controlled lifecycle proof."}]`)
		default:
			t.Fatalf("unexpected write or request: %s %s body=%s", request.Method, request.URL.Path, string(body))
		}
	}))
	defer server.Close()
	t.Setenv("ODIN_GITHUB_API_BASE_URL", server.URL)

	var output strings.Builder
	err := RunWork(ctx, store, projectRegistry, registry.Snapshot{}, []string{
		"apply-lifecycle",
		"--issue", "123",
		"--approved-target", "marcusgoll/odin-os#123",
		"--json",
	}, &output)
	if err != nil {
		t.Fatalf("RunWork(apply-lifecycle idempotent) error = %v", err)
	}

	var report struct {
		AppliedActions []struct {
			Action string `json:"action"`
		} `json:"applied_actions"`
		Comment struct {
			Created bool `json:"created"`
		} `json:"comment"`
		MethodAudit struct {
			Reads  int `json:"reads"`
			Writes int `json:"writes"`
		} `json:"method_audit"`
	}
	if err := json.Unmarshal([]byte(output.String()), &report); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\noutput:\n%s", err, output.String())
	}
	if len(report.AppliedActions) != 0 {
		t.Fatalf("applied actions = %+v, want idempotent zero writes", report.AppliedActions)
	}
	if report.Comment.Created {
		t.Fatalf("comment created = true, want existing marker reused")
	}
	if report.MethodAudit.Reads != 2 || report.MethodAudit.Writes != 0 {
		t.Fatalf("audit = %+v, want reads=2 writes=0", report.MethodAudit)
	}
	for _, request := range requests {
		if !strings.HasPrefix(request, "GET ") {
			t.Fatalf("request = %q, want only reads on rerun", request)
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

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsRequestPrefix(values []string, want string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, want) {
			return true
		}
	}
	return false
}
