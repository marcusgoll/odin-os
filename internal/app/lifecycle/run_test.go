package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/app/bootstrap"
	"odin-os/internal/core/capabilities"
	"odin-os/internal/core/initiatives"
	"odin-os/internal/runtime/supervision"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/vcs/worktrees"
)

const testProjectKey = "alpha-cli"

func TestRunReplStartsInteractiveShell(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)

	stdin := strings.NewReader("/help\n")
	var stdout bytes.Buffer

	err := Run(context.Background(), root, []string{"repl"}, stdin, &stdout)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "scope=") {
		t.Fatalf("Run() output = %q, want header", output)
	}
	if !strings.Contains(output, "/help") {
		t.Fatalf("Run() output = %q, want help", output)
	}
}

func TestRunWithoutArgsPrintsUsageInsteadOfStartingShell(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	var stdout bytes.Buffer

	err := Run(context.Background(), root, nil, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "Usage: odin") {
		t.Fatalf("stdout = %q, want usage banner", output)
	}
	if strings.Contains(output, "odin>") {
		t.Fatalf("stdout = %q, should not contain repl prompt", output)
	}
}

func TestRunStatusJSON(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	seedStatusCompanionSwarms(t, ctx, store)
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	var stdout bytes.Buffer

	err = Run(context.Background(), root, []string{"status", "--json"}, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var payload struct {
		Health           string `json:"health"`
		PendingApprovals int    `json:"pending_approvals"`
		RegistryHealthy  bool   `json:"registry_healthy"`
		CompanionSwarms  []struct {
			ParentTaskKey       string `json:"parent_task_key"`
			Status              string `json:"status"`
			BlockedReason       string `json:"blocked_reason"`
			BacklogCount        int    `json:"backlog_count"`
			ActiveChildRunCount int    `json:"active_child_run_count"`
		} `json:"companion_swarms"`
		CompanionSwarmCounts struct {
			Active  int `json:"active"`
			Blocked int `json:"blocked"`
			Backlog int `json:"backlog"`
		} `json:"companion_swarm_counts"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("status json = %v", err)
	}
	if payload.Health == "" {
		t.Fatalf("Health = %q, want non-empty", payload.Health)
	}
	if !payload.RegistryHealthy {
		t.Fatalf("RegistryHealthy = false, want true")
	}
	if len(payload.CompanionSwarms) != 3 {
		t.Fatalf("CompanionSwarms len = %d, want 3", len(payload.CompanionSwarms))
	}
	if payload.CompanionSwarmCounts.Active != 1 {
		t.Fatalf("CompanionSwarmCounts.Active = %d, want 1", payload.CompanionSwarmCounts.Active)
	}
	if payload.CompanionSwarmCounts.Blocked != 2 {
		t.Fatalf("CompanionSwarmCounts.Blocked = %d, want 2", payload.CompanionSwarmCounts.Blocked)
	}
	if payload.CompanionSwarmCounts.Backlog < 1 {
		t.Fatalf("CompanionSwarmCounts.Backlog = %d, want backlog", payload.CompanionSwarmCounts.Backlog)
	}

	activeFound := false
	for _, swarm := range payload.CompanionSwarms {
		if swarm.ParentTaskKey != "status-swarm-active" {
			continue
		}
		activeFound = true
		if swarm.Status != "running" {
			t.Fatalf("active swarm status = %q, want running", swarm.Status)
		}
		if swarm.ActiveChildRunCount != 1 {
			t.Fatalf("active swarm active_child_run_count = %d, want 1", swarm.ActiveChildRunCount)
		}
		if swarm.BacklogCount != 0 {
			t.Fatalf("active swarm backlog_count = %d, want 0", swarm.BacklogCount)
		}
	}
	if !activeFound {
		t.Fatal("status-swarm-active missing from companion_swarms")
	}
}

func TestRunCompanionGetJSON(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := testRepoRoot(t)
	runtimeRoot := t.TempDir()

	app, err := bootstrap.Load(ctx, root, runtimeRoot)
	if err != nil {
		t.Fatalf("bootstrap.Load() error = %v", err)
	}
	defer app.Store.Close()

	if err := runCompanion(ctx, app, []string{"create", "--kind", "advisor", "--key", "finance", "--title", "Finance Advisor"}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runCompanion(create) error = %v", err)
	}

	var stdout bytes.Buffer
	if err := runCompanion(ctx, app, []string{"get", "finance", "--json"}, &stdout); err != nil {
		t.Fatalf("runCompanion(get --json) error = %v", err)
	}

	var payload struct {
		Key                string `json:"key"`
		Title              string `json:"title"`
		Kind               string `json:"kind"`
		Status             string `json:"status"`
		ToolPolicyJSON     string `json:"tool_policy_json"`
		MemoryPolicyJSON   string `json:"memory_policy_json"`
		PlanningPolicyJSON string `json:"planning_policy_json"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(get output) error = %v\n%s", err, stdout.String())
	}
	if payload.Key != "finance" {
		t.Fatalf("Key = %q, want finance", payload.Key)
	}
	if payload.Title != "Finance Advisor" {
		t.Fatalf("Title = %q, want Finance Advisor", payload.Title)
	}
	if payload.Kind != "advisor" {
		t.Fatalf("Kind = %q, want advisor", payload.Kind)
	}
	if payload.Status != "active" {
		t.Fatalf("Status = %q, want active", payload.Status)
	}
	if payload.ToolPolicyJSON != "{}" {
		t.Fatalf("ToolPolicyJSON = %q, want {}", payload.ToolPolicyJSON)
	}
	if payload.MemoryPolicyJSON != "{}" {
		t.Fatalf("MemoryPolicyJSON = %q, want {}", payload.MemoryPolicyJSON)
	}
	if payload.PlanningPolicyJSON != "{}" {
		t.Fatalf("PlanningPolicyJSON = %q, want {}", payload.PlanningPolicyJSON)
	}
}

func TestRunCompanionStateJSON(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := testRepoRoot(t)
	runtimeRoot := t.TempDir()

	app, err := bootstrap.Load(ctx, root, runtimeRoot)
	if err != nil {
		t.Fatalf("bootstrap.Load() error = %v", err)
	}
	defer app.Store.Close()

	if err := runCompanion(ctx, app, []string{"create", "--kind", "advisor", "--key", "finance", "--title", "Finance Advisor"}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runCompanion(create) error = %v", err)
	}

	var stdout bytes.Buffer
	if err := runCompanion(ctx, app, []string{"state", "finance", "--json"}, &stdout); err != nil {
		t.Fatalf("runCompanion(state --json) error = %v", err)
	}

	var payload struct {
		Key       string `json:"key"`
		Title     string `json:"title"`
		Kind      string `json:"kind"`
		Status    string `json:"status"`
		TaskState struct {
			CompanionKey         string `json:"companion_key"`
			OwnedInitiativeCount int    `json:"owned_initiative_count"`
			OpenWorkItemCount    int    `json:"open_work_item_count"`
			ActiveRunCount       int    `json:"active_run_count"`
			PendingApprovalCount int    `json:"pending_approval_count"`
			BlockedWorkItemCount int    `json:"blocked_work_item_count"`
			OverdueFollowUpCount int    `json:"overdue_follow_up_count"`
		} `json:"task_state"`
		Swarms []struct {
			ParentTaskKey string `json:"parent_task_key"`
			Status        string `json:"status"`
		} `json:"swarms"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(state output) error = %v\n%s", err, stdout.String())
	}
	if payload.Key != "finance" {
		t.Fatalf("Key = %q, want finance", payload.Key)
	}
	if payload.TaskState.CompanionKey != "finance" {
		t.Fatalf("TaskState.CompanionKey = %q, want finance", payload.TaskState.CompanionKey)
	}
	if payload.TaskState.OwnedInitiativeCount != 0 || payload.TaskState.OpenWorkItemCount != 0 || payload.TaskState.ActiveRunCount != 0 || payload.TaskState.PendingApprovalCount != 0 || payload.TaskState.BlockedWorkItemCount != 0 || payload.TaskState.OverdueFollowUpCount != 0 {
		t.Fatalf("TaskState counts = %+v, want zeros for a fresh companion", payload.TaskState)
	}
	if len(payload.Swarms) != 0 {
		t.Fatalf("Swarms len = %d, want 0 for a fresh companion", len(payload.Swarms))
	}
}

func TestRunCompanionCapabilitiesJSON(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := testRepoRoot(t)
	runtimeRoot := t.TempDir()

	app, err := bootstrap.Load(ctx, root, runtimeRoot)
	if err != nil {
		t.Fatalf("bootstrap.Load() error = %v", err)
	}
	defer app.Store.Close()

	if err := runCompanion(ctx, app, []string{"create", "--kind", "advisor", "--key", "finance", "--title", "Finance Advisor"}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runCompanion(create) error = %v", err)
	}

	workspace, err := app.Store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}
	if _, err := app.Store.DB().ExecContext(ctx, `
		UPDATE companions
		SET tool_policy_json = ?, memory_policy_json = ?, planning_policy_json = ?
		WHERE workspace_id = ? AND key = ?
	`, `{"allow":["branch_proposal","repo_read"]}`, `{"mode":"initiative"}`, `{"mode":"planning","swarm":{"max_children":2}}`, workspace.ID, "finance"); err != nil {
		t.Fatalf("seed companion policy update error = %v", err)
	}

	var stdout bytes.Buffer
	if err := runCompanion(ctx, app, []string{"capabilities", "finance", "--json"}, &stdout); err != nil {
		t.Fatalf("runCompanion(capabilities --json) error = %v", err)
	}

	var payload struct {
		Key        string `json:"key"`
		ToolPolicy struct {
			Allow []string `json:"allow"`
		} `json:"tool_policy"`
		MemoryPolicy struct {
			Mode string `json:"mode"`
		} `json:"memory_policy"`
		PlanningPolicy struct {
			Mode  string `json:"mode"`
			Swarm struct {
				MaxChildren int `json:"max_children"`
			} `json:"swarm"`
		} `json:"planning_policy"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(capabilities output) error = %v\n%s", err, stdout.String())
	}
	if payload.Key != "finance" {
		t.Fatalf("Key = %q, want finance", payload.Key)
	}
	if len(payload.ToolPolicy.Allow) != 2 || payload.ToolPolicy.Allow[0] != "branch_proposal" || payload.ToolPolicy.Allow[1] != "repo_read" {
		t.Fatalf("ToolPolicy.Allow = %+v, want branch_proposal and repo_read", payload.ToolPolicy.Allow)
	}
	if payload.MemoryPolicy.Mode != "initiative" {
		t.Fatalf("MemoryPolicy.Mode = %q, want initiative", payload.MemoryPolicy.Mode)
	}
	if payload.PlanningPolicy.Mode != "planning" {
		t.Fatalf("PlanningPolicy.Mode = %q, want planning", payload.PlanningPolicy.Mode)
	}
	if payload.PlanningPolicy.Swarm.MaxChildren != 2 {
		t.Fatalf("PlanningPolicy.Swarm.MaxChildren = %d, want 2", payload.PlanningPolicy.Swarm.MaxChildren)
	}
}

func TestRunCompanionRejectsUnsupportedSubcommand(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := testRepoRoot(t)
	runtimeRoot := t.TempDir()

	app, err := bootstrap.Load(ctx, root, runtimeRoot)
	if err != nil {
		t.Fatalf("bootstrap.Load() error = %v", err)
	}
	defer app.Store.Close()

	if err := runCompanion(ctx, app, []string{"delete", "finance"}, &bytes.Buffer{}); err == nil {
		t.Fatal("runCompanion(delete) error = nil, want unsupported companion subcommand error")
	}
}

func TestCompanionRunCreatesOwnedTaskInDefaultScope(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := testRepoRoot(t)
	runtimeRoot := t.TempDir()

	app, err := bootstrap.Load(ctx, root, runtimeRoot)
	if err != nil {
		t.Fatalf("bootstrap.Load() error = %v", err)
	}
	defer app.Store.Close()

	if err := runCompanion(ctx, app, []string{"create", "--kind", "advisor", "--key", "finance", "--title", "Finance Advisor"}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runCompanion(create) error = %v", err)
	}

	var stdout bytes.Buffer
	if err := runCompanion(ctx, app, []string{"run", "finance", "--objective", "review April budget", "--json"}, &stdout); err != nil {
		t.Fatalf("runCompanion(run --json) error = %v\n%s", err, stdout.String())
	}

	var payload struct {
		CompanionKey          string `json:"companion_key"`
		Objective             string `json:"objective"`
		RequestedSwarmTrigger string `json:"requested_swarm_trigger,omitempty"`
		Task                  struct {
			ID     int64  `json:"id"`
			Key    string `json:"key"`
			Status string `json:"status"`
			Scope  string `json:"scope"`
		} `json:"task"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(runCompanion output) error = %v\n%s", err, stdout.String())
	}
	if payload.CompanionKey != "finance" {
		t.Fatalf("CompanionKey = %q, want finance", payload.CompanionKey)
	}
	if payload.Objective != "review April budget" {
		t.Fatalf("Objective = %q, want review April budget", payload.Objective)
	}
	if payload.RequestedSwarmTrigger != "" {
		t.Fatalf("RequestedSwarmTrigger = %q, want empty", payload.RequestedSwarmTrigger)
	}
	if payload.Task.Status != "queued" {
		t.Fatalf("Task.Status = %q, want queued", payload.Task.Status)
	}

	task, err := app.Store.GetTask(ctx, payload.Task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.CompanionID == nil {
		t.Fatal("Task.CompanionID = nil, want companion ownership")
	}
	if task.RequestedBy != "companion" {
		t.Fatalf("Task.RequestedBy = %q, want companion", task.RequestedBy)
	}
	if task.ActionKey != "" {
		t.Fatalf("Task.ActionKey = %q, want empty without trigger", task.ActionKey)
	}

	project, err := app.Store.GetProjectByKey(ctx, "odin-core")
	if err != nil {
		t.Fatalf("GetProjectByKey(odin-core) error = %v", err)
	}
	if task.ProjectID != project.ID {
		t.Fatalf("Task.ProjectID = %d, want odin-core project %d", task.ProjectID, project.ID)
	}
	if task.WorkspaceID == nil {
		t.Fatal("Task.WorkspaceID = nil, want workspace ownership")
	}
	if task.InitiativeID == nil {
		t.Fatal("Task.InitiativeID = nil, want initiative ownership")
	}
}

func TestRunDoctorIgnoresInvalidOdinNowForNonAgendaCommands(t *testing.T) {
	root := testRepoRoot(t)
	t.Setenv("ODIN_NOW", "definitely-not-a-timestamp")

	var stdout bytes.Buffer
	err := Run(context.Background(), root, []string{"doctor", "--json"}, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run(doctor --json) error = %v", err)
	}

	var payload struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("doctor json = %v\n%s", err, stdout.String())
	}
	if payload.Status == "" {
		t.Fatalf("Status = %q, want non-empty", payload.Status)
	}
}

func TestRunProjectListText(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	var stdout bytes.Buffer

	err := Run(context.Background(), root, []string{"project", "list"}, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "odin-core") {
		t.Fatalf("stdout = %q, want project key", stdout.String())
	}
}

func TestRunProjectSelectPersistsSession(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	var stdout bytes.Buffer

	err := Run(context.Background(), root, []string{"project", "select", testProjectKey}, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "project="+testProjectKey) {
		t.Fatalf("stdout = %q, want selection confirmation", stdout.String())
	}

	sessionBytes, err := os.ReadFile(filepath.Join(root, "state", "cache", "cli-session.json"))
	if err != nil {
		t.Fatalf("ReadFile(cli-session.json) error = %v", err)
	}
	if !strings.Contains(string(sessionBytes), "\"project_key\": \""+testProjectKey+"\"") {
		t.Fatalf("session = %q, want alpha project selection", string(sessionBytes))
	}
}

func TestRunTransitionSetUsesSelectedProject(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	if err := Run(context.Background(), root, []string{"project", "select", testProjectKey}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(project select) error = %v", err)
	}

	var stdout bytes.Buffer
	err := Run(
		context.Background(),
		root,
		[]string{"transition", "set", "cutover", "confirm", "because", "cli smoke"},
		strings.NewReader(""),
		&stdout,
	)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "project="+testProjectKey) || !strings.Contains(output, "state=cutover") {
		t.Fatalf("stdout = %q, want transition status for alpha cutover", output)
	}
}

func TestRunTaskCreateJSON(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	var stdout bytes.Buffer

	err := Run(
		context.Background(),
		root,
		[]string{"task", "create", "--project", testProjectKey, "--title", "cutover smoke"},
		strings.NewReader(""),
		&stdout,
	)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var payload struct {
		ID     int64  `json:"id"`
		Key    string `json:"key"`
		Status string `json:"status"`
		Scope  string `json:"scope"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("task create json = %v", err)
	}
	if payload.Status != "queued" {
		t.Fatalf("Status = %q, want queued", payload.Status)
	}
	if payload.Scope != "project" {
		t.Fatalf("Scope = %q, want project", payload.Scope)
	}
	if payload.ID == 0 || payload.Key == "" {
		t.Fatalf("payload = %+v, want populated task identity", payload)
	}
}

func TestRunTaskRunJSON(t *testing.T) {
	configureLifecycleHarnessDriver(t)
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)
	cleanupTaskRunWorktree(t, testProjectKey)
	if err := Run(context.Background(), root, []string{"project", "select", testProjectKey}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(project select) error = %v", err)
	}
	if err := Run(
		context.Background(),
		root,
		[]string{"transition", "set", "cutover", "confirm", "because", "allow cli run"},
		strings.NewReader(""),
		&bytes.Buffer{},
	); err != nil {
		t.Fatalf("Run(transition set) error = %v", err)
	}

	var stdout bytes.Buffer
	err := Run(
		context.Background(),
		root,
		[]string{"task", "run", "--project", testProjectKey, "--title", "run from cli", "--json"},
		strings.NewReader(""),
		&stdout,
	)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var payload struct {
		Task struct {
			Status string `json:"status"`
		} `json:"task"`
		Run struct {
			Status string `json:"status"`
		} `json:"run"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("task run json = %v", err)
	}
	if payload.Task.Status != "completed" {
		t.Fatalf("Task.Status = %q, want completed", payload.Task.Status)
	}
	if payload.Run.Status != "completed" {
		t.Fatalf("Run.Status = %q, want completed", payload.Run.Status)
	}
}

func TestRunSkillsCrudAndInvokeJSON(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	scriptPath := filepath.Join(root, "scripts", "skills", "echo-skill.sh")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(script dir) error = %v", err)
	}
	if err := os.WriteFile(scriptPath, []byte(`#!/usr/bin/env bash
cat >/dev/null
printf '%s\n' '{"status":"ok","summary":"echo complete","output":{"message":"hello"}}'
`), 0o755); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}

	createSpecPath := filepath.Join(root, "echo-skill.json")
	if err := os.WriteFile(createSpecPath, []byte(`{
  "key": "echo-skill",
  "title": "Echo Skill",
  "summary": "Echoes a structured response.",
  "status": "active",
  "version": "1.0.0",
  "enabled": true,
  "tags": ["testing"],
  "owners": ["odin-core"],
  "strictness": "rigid",
  "applies_to": ["testing"],
  "scopes": ["project"],
  "permissions": ["repo.read"],
  "handler_type": "command",
  "handler_ref": "scripts/skills/echo-skill.sh",
  "timeout_seconds": 15,
  "input_schema": {"type":"object"},
  "output_schema": {"type":"object"},
  "sections": {
    "Purpose": "Echo input.",
    "When to Use": "When testing.",
    "Inputs": "A message.",
    "Procedure": "Read and echo.",
    "Outputs": "A JSON response.",
    "Constraints": "Stay deterministic.",
    "Success Criteria": "The caller gets a stable response."
  }
}`), 0o644); err != nil {
		t.Fatalf("WriteFile(spec) error = %v", err)
	}

	updateSpecPath := filepath.Join(root, "echo-skill-v2.json")
	if err := os.WriteFile(updateSpecPath, []byte(`{
  "key": "echo-skill",
  "title": "Echo Skill",
  "summary": "Updated summary.",
  "status": "active",
  "version": "1.0.1",
  "enabled": true,
  "tags": ["testing"],
  "owners": ["odin-core"],
  "strictness": "rigid",
  "applies_to": ["testing"],
  "scopes": ["project"],
  "permissions": ["repo.read"],
  "handler_type": "command",
  "handler_ref": "scripts/skills/echo-skill.sh",
  "timeout_seconds": 15,
  "input_schema": {"type":"object"},
  "output_schema": {"type":"object"},
  "sections": {
    "Purpose": "Echo input.",
    "When to Use": "When testing.",
    "Inputs": "A message.",
    "Procedure": "Read and echo.",
    "Outputs": "A JSON response.",
    "Constraints": "Stay deterministic.",
    "Success Criteria": "The caller gets a stable response."
  }
}`), 0o644); err != nil {
		t.Fatalf("WriteFile(update spec) error = %v", err)
	}

	var stdout bytes.Buffer
	if err := Run(context.Background(), root, []string{"skills", "create", "--spec", createSpecPath, "--json"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(skills create) error = %v", err)
	}

	stdout.Reset()
	if err := Run(context.Background(), root, []string{"skills", "list", "--json"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(skills list) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "echo-skill") {
		t.Fatalf("skills list output = %q, want echo-skill", stdout.String())
	}

	stdout.Reset()
	if err := Run(context.Background(), root, []string{"skills", "get", "echo-skill", "--json"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(skills get) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "\"version\": \"1.0.0\"") {
		t.Fatalf("skills get output = %q, want version 1.0.0", stdout.String())
	}

	stdout.Reset()
	if err := Run(context.Background(), root, []string{"skills", "invoke", "echo-skill", "--input", `{"message":"hello"}`, "--json"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(skills invoke) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "echo complete") {
		t.Fatalf("skills invoke output = %q, want echo summary", stdout.String())
	}

	stdout.Reset()
	if err := Run(context.Background(), root, []string{"skills", "update", "echo-skill", "--spec", updateSpecPath, "--json"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(skills update) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "\"version\": \"1.0.1\"") {
		t.Fatalf("skills update output = %q, want version 1.0.1", stdout.String())
	}

	stdout.Reset()
	if err := Run(context.Background(), root, []string{"skills", "delete", "echo-skill", "--json"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(skills delete) error = %v", err)
	}

	stdout.Reset()
	if err := Run(context.Background(), root, []string{"skills", "list", "--json"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(skills list after delete) error = %v", err)
	}
	if strings.Contains(stdout.String(), "echo-skill") {
		t.Fatalf("skills list after delete output = %q, should not contain echo-skill", stdout.String())
	}
}

func TestRunSkillsInvokeUsesSelectedProjectTransitionState(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	scriptPath := filepath.Join(root, "scripts", "skills", "audit-note.sh")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(script dir) error = %v", err)
	}
	if err := os.WriteFile(scriptPath, []byte(`#!/usr/bin/env bash
cat >/dev/null
printf '%s\n' '{"status":"ok","summary":"audit note recorded"}'
`), 0o755); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}

	createSpecPath := filepath.Join(root, "audit-note.json")
	if err := os.WriteFile(createSpecPath, []byte(`{
  "key": "audit-note",
  "title": "Audit Note",
  "summary": "Writes a limited action note.",
  "status": "active",
  "version": "1.0.0",
  "enabled": true,
  "tags": ["testing"],
  "owners": ["odin-core"],
  "strictness": "rigid",
  "applies_to": ["testing"],
  "scopes": ["project"],
  "permissions": ["repo.mutate.isolated:docs_audit_note"],
  "handler_type": "command",
  "handler_ref": "scripts/skills/audit-note.sh",
  "timeout_seconds": 15,
  "input_schema": {"type":"object"},
  "output_schema": {"type":"object"},
  "sections": {
    "Purpose": "Write an audit note.",
    "When to Use": "When testing transition policy.",
    "Inputs": "A structured note.",
    "Procedure": "Record the note.",
    "Outputs": "A JSON response.",
    "Constraints": "Stay deterministic.",
    "Success Criteria": "The caller gets a stable response."
  }
}`), 0o644); err != nil {
		t.Fatalf("WriteFile(spec) error = %v", err)
	}

	if err := Run(context.Background(), root, []string{"skills", "create", "--spec", createSpecPath, "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(skills create) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"project", "select", testProjectKey}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(project select) error = %v", err)
	}
	if err := Run(
		context.Background(),
		root,
		[]string{"transition", "set", "limited_action", "allow=docs_audit_note", "confirm", "because", "cli transition smoke"},
		strings.NewReader(""),
		&bytes.Buffer{},
	); err != nil {
		t.Fatalf("Run(transition set) error = %v", err)
	}

	var stdout bytes.Buffer
	err := Run(
		context.Background(),
		root,
		[]string{"skills", "invoke", "audit-note", "--json"},
		strings.NewReader(""),
		&stdout,
	)
	if err != nil {
		t.Fatalf("Run(skills invoke) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "audit note recorded") {
		t.Fatalf("skills invoke output = %q, want audit note summary", stdout.String())
	}
}

func TestNewJobServiceIncludesSupervisor(t *testing.T) {
	t.Parallel()

	service := newJobService(bootstrap.App{})
	if service.Supervisor == nil {
		t.Fatal("newJobService() Supervisor = nil, want concrete supervisor")
	}
	if _, ok := service.Supervisor.(supervision.Service); !ok {
		t.Fatalf("newJobService() Supervisor = %T, want supervision.Service", service.Supervisor)
	}
}

func TestInvokeServedProjectStatusFallsBackToScopeAndMode(t *testing.T) {
	t.Parallel()

	response, err := invokeServedProjectStatus(context.Background(), bootstrap.App{}, capabilities.InvokeRequest{
		Scope: capabilities.ScopeRef{
			Kind: "global",
		},
		Execution: capabilities.ExecutionRequest{
			Mode: "local",
		},
	})
	if err != nil {
		t.Fatalf("invokeServedProjectStatus() error = %v", err)
	}
	if string(response.Output) != "scope=global mode=local\n" {
		t.Fatalf("response output = %q, want %q", response.Output, "scope=global mode=local\n")
	}
}

func TestInvokeServedProjectStatusFallsBackToProjectScopeLabel(t *testing.T) {
	t.Parallel()

	response, err := invokeServedProjectStatus(context.Background(), bootstrap.App{}, capabilities.InvokeRequest{
		Scope: capabilities.ScopeRef{
			Kind:       "project",
			ProjectKey: "alpha",
		},
		Execution: capabilities.ExecutionRequest{
			Mode: "local",
		},
	})
	if err != nil {
		t.Fatalf("invokeServedProjectStatus() error = %v", err)
	}
	if string(response.Output) != "scope=alpha mode=local\n" {
		t.Fatalf("response output = %q, want %q", response.Output, "scope=alpha mode=local\n")
	}
}

func testRepoRoot(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	mustMkdirAll := func(path string) {
		t.Helper()
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
	mustWriteFile := func(path string, contents string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	mustMkdirAll(filepath.Join(root, "config"))
	mustMkdirAll(filepath.Join(root, "data"))
	mustMkdirAll(filepath.Join(root, "registry"))
	mustMkdirAll(filepath.Join(root, "state", "cache"))
	mustMkdirAll(filepath.Join(root, "alpha"))

	mustWriteFile(filepath.Join(root, "config", "projects.yaml"), `
version: 1
projects:
  - key: alpha-cli
    name: Alpha
    project_class: github_backed_project
    git_root: ../alpha
    default_branch: main
    github:
      repo: acme/alpha
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
  - key: odin-core
    name: Odin Core
    project_class: system_project
    system_project: true
    git_root: ..
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
        require_for_system_project_changes: true
      merge_policy:
        mode: squash
        allow_direct_to_default_branch: false
      destructive_operations:
        allow_reset: false
        allow_clean: false
        allow_force_push: false
        require_explicit_approval: true
`)
	mustWriteFile(filepath.Join(root, "config", "executors.yaml"), `
version: 1
executors:
  - key: codex_headless
    adapter: codex_headless
    class: plan_backed_cli
    enabled: true
    priority: 10
routes:
  - name: default
    match:
      task_kinds: [general, plan, build, review, qa, research]
      scopes: [global, odin-core, project, new-project]
    preferred: [codex_headless]
`)
	mustWriteFile(filepath.Join(root, "config", "odin.yaml"), `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:9443
  startup_recovery: true
`)

	mustWriteFile(filepath.Join(root, "README.md"), "alpha test repo\n")
	mustWriteFile(filepath.Join(root, "alpha", "README.md"), "alpha nested repo\n")
	runGitIn := func(dir string, args ...string) {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = dir
		command.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test User",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test User",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}

	runGitIn(root, "init", "-b", "main")
	runGitIn(root, "add", ".")
	runGitIn(root, "commit", "-m", "test fixture")

	runGitIn(filepath.Join(root, "alpha"), "init", "-b", "main")
	runGitIn(filepath.Join(root, "alpha"), "add", ".")
	runGitIn(filepath.Join(root, "alpha"), "commit", "-m", "alpha fixture")

	return root
}

func cleanupTaskRunWorktree(t *testing.T, projectKey string) {
	t.Helper()

	path := worktrees.ResolvePath(worktrees.PathParams{
		Root:       worktrees.DefaultRoot(),
		ProjectKey: projectKey,
		TaskID:     1,
		RunID:      1,
		Try:        1,
	})
	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("RemoveAll(%s) error = %v", path, err)
	}
}

func seedStatusCompanionSwarms(t *testing.T, ctx context.Context, store *sqlite.Store) {
	t.Helper()

	workspace, err := store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}
	companion, err := store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey(default) error = %v", err)
	}
	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "swarm-project",
		Name:          "Swarm Project",
		Scope:         "project",
		GitRoot:       t.TempDir(),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	initiative, err := store.UpsertInitiative(ctx, sqlite.UpsertInitiativeParams{
		WorkspaceID:      workspace.ID,
		Key:              project.Key,
		Title:            project.Name,
		Kind:             string(initiatives.KindManagedProject),
		Status:           "active",
		Summary:          "Swarm initiative",
		OwnerCompanionID: &companion.ID,
		LinkedProjectID:  &project.ID,
	})
	if err != nil {
		t.Fatalf("UpsertInitiative() error = %v", err)
	}

	activeTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "status-swarm-active",
		Title:        "Active swarm",
		Status:       "running",
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "delivery",
	})
	if err != nil {
		t.Fatalf("CreateTask(active) error = %v", err)
	}
	activeDelegation, err := store.CreateDelegation(ctx, sqlite.CreateDelegationParams{
		ParentTaskID:    activeTask.ID,
		ProjectID:       project.ID,
		Scope:           activeTask.Scope,
		DelegationKey:   "status-swarm-active-child",
		Role:            "builder",
		ActionClass:     "mutation",
		ActionKey:       "implement",
		MutationMode:    "read_only",
		Status:          "queued",
		ConvergenceMode: "merge",
		ArtifactTarget:  "branch",
		Executor:        "codex",
		DetailsJSON:     `{"objective":"active","swarm":{"requested_budget":2,"max_children":2}}`,
	})
	if err != nil {
		t.Fatalf("CreateDelegation(active) error = %v", err)
	}
	activeChild, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "status-swarm-active-child",
		Title:       "Active child",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "supervisor",
	})
	if err != nil {
		t.Fatalf("CreateTask(active child) error = %v", err)
	}
	activeRun, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:     activeChild.ID,
		Executor:   "codex",
		Attempt:    1,
		Status:     "running",
		TaskStatus: "running",
	})
	if err != nil {
		t.Fatalf("StartRun(active child) error = %v", err)
	}
	if _, err := store.AttachDelegationChildTask(ctx, sqlite.AttachDelegationChildTaskParams{
		DelegationID: activeDelegation.ID,
		ChildTaskID:  activeChild.ID,
		ChildRunID:   &activeRun.ID,
	}); err != nil {
		t.Fatalf("AttachDelegationChildTask(active) error = %v", err)
	}
	if _, err := store.CreateDelegationArtifact(ctx, sqlite.CreateDelegationArtifactParams{
		DelegationID: activeDelegation.ID,
		ArtifactType: "result",
		Summary:      "Active child completed",
		DetailsJSON:  `{"status":"completed","confidence":0.9,"evidence_refs":["status/active"],"unresolved_risks":[],"proposed_next_actions":[],"proposed_memory_candidates":[]}`,
	}); err != nil {
		t.Fatalf("CreateDelegationArtifact(active) error = %v", err)
	}

	approvalTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "status-swarm-approval",
		Title:        "Approval swarm",
		Status:       "running",
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "delivery",
	})
	if err != nil {
		t.Fatalf("CreateTask(approval) error = %v", err)
	}
	approvalDelegation, err := store.CreateDelegation(ctx, sqlite.CreateDelegationParams{
		ParentTaskID:    approvalTask.ID,
		ProjectID:       project.ID,
		Scope:           approvalTask.Scope,
		DelegationKey:   "status-swarm-approval-child",
		Role:            "builder",
		ActionClass:     "mutation",
		ActionKey:       "implement",
		MutationMode:    "read_only",
		Status:          "queued",
		ConvergenceMode: "review_gate",
		ArtifactTarget:  "branch",
		Executor:        "codex",
		DetailsJSON:     `{"objective":"approval","swarm":{"requested_budget":1,"max_children":1}}`,
	})
	if err != nil {
		t.Fatalf("CreateDelegation(approval) error = %v", err)
	}
	approvalChild, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "status-swarm-approval-child",
		Title:       "Approval child",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "supervisor",
	})
	if err != nil {
		t.Fatalf("CreateTask(approval child) error = %v", err)
	}
	if _, _, err := store.BlockTaskAndRequestApproval(ctx, sqlite.BlockTaskAndRequestApprovalParams{
		TaskID:      approvalChild.ID,
		RunID:       nil,
		RequestedBy: "system",
	}); err != nil {
		t.Fatalf("BlockTaskAndRequestApproval(approval child) error = %v", err)
	}
	if _, err := store.AttachDelegationChildTask(ctx, sqlite.AttachDelegationChildTaskParams{
		DelegationID: approvalDelegation.ID,
		ChildTaskID:  approvalChild.ID,
	}); err != nil {
		t.Fatalf("AttachDelegationChildTask(approval) error = %v", err)
	}

	budgetTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "status-swarm-budget",
		Title:        "Budget swarm",
		Status:       "blocked",
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "delivery",
	})
	if err != nil {
		t.Fatalf("CreateTask(budget) error = %v", err)
	}
	if _, err := store.BlockTask(ctx, sqlite.BlockTaskParams{
		TaskID: budgetTask.ID,
		Reason: "budget_exhausted",
	}); err != nil {
		t.Fatalf("BlockTask(budget) error = %v", err)
	}
	budgetDelegation, err := store.CreateDelegation(ctx, sqlite.CreateDelegationParams{
		ParentTaskID:    budgetTask.ID,
		ProjectID:       project.ID,
		Scope:           budgetTask.Scope,
		DelegationKey:   "status-swarm-budget-child",
		Role:            "reviewer",
		ActionClass:     "analysis",
		ActionKey:       "review",
		MutationMode:    "read_only",
		Status:          "queued",
		ConvergenceMode: "merge",
		ArtifactTarget:  "report",
		Executor:        "codex",
		DetailsJSON:     `{"objective":"budget","swarm":{"requested_budget":3,"max_children":1}}`,
	})
	if err != nil {
		t.Fatalf("CreateDelegation(budget) error = %v", err)
	}
	budgetChild, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "status-swarm-budget-child",
		Title:       "Budget child",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "supervisor",
	})
	if err != nil {
		t.Fatalf("CreateTask(budget child) error = %v", err)
	}
	if _, err := store.AttachDelegationChildTask(ctx, sqlite.AttachDelegationChildTaskParams{
		DelegationID: budgetDelegation.ID,
		ChildTaskID:  budgetChild.ID,
	}); err != nil {
		t.Fatalf("AttachDelegationChildTask(budget) error = %v", err)
	}
}

func configureLifecycleHarnessDriver(t *testing.T) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "codex-driver.sh")
	if err := os.WriteFile(path, []byte(`#!/usr/bin/env bash
payload="$(cat)"
PAYLOAD="$payload" python3 - <<'PY'
import json
import os

request = json.loads(os.environ["PAYLOAD"])
action = request.get("action")
if action == "health":
    print(json.dumps({"status":"healthy","details":"lifecycle test driver healthy"}))
else:
    print(json.dumps({"status":"completed","output":"driver test ok","handle":{"external_id":"fixture-driver"}}))
PY
`), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("Chmod(driver) error = %v", err)
	}
	t.Setenv("ODIN_CODEX_DRIVER", path)
}
