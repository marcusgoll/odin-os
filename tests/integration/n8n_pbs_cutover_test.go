package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"odin-os/internal/app/lifecycle"
	"odin-os/internal/core/projects"
	"odin-os/internal/store/sqlite"
)

func TestN8NPBSCutoverFlow(t *testing.T) {
	ctx := context.Background()
	sourceRepoRoot := projectRoot(t)
	repoRoot := createPBSCutoverRepoRoot(t)
	odinBinary := buildOdinBinary(t, sourceRepoRoot)
	runtimeRoot := t.TempDir()
	extraEnv := acceptanceHarnessDriverEnv(t)
	serveAddr := reserveLoopbackAddr(t)

	homePath := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(homePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(home) error = %v", err)
	}
	extraEnv["HOME"] = homePath
	extraEnv["ODIN_HTTP_ADDR"] = serveAddr
	for key, value := range extraEnv {
		t.Setenv(key, value)
	}
	t.Setenv("ODIN_ROOT", runtimeRoot)

	routerScriptPath := filepath.Join(sourceRepoRoot, "scripts", "ops", "odin-n8n-ssh-dispatch.sh")
	routerScript, err := os.ReadFile(routerScriptPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", routerScriptPath, err)
	}
	if strings.Contains(string(routerScript), "/var/odin/inbox") {
		t.Fatalf("%s contains /var/odin/inbox, want no legacy inbox dependency in the router path", routerScriptPath)
	}

	normalizedEnvelope := `{
  "schema_version": 1,
  "source": "n8n",
  "type": "ci_failure",
  "project_key": "pbs",
  "title": "Investigate PBS CI failure",
  "action_key": "",
  "dedup_key": "ci_failure:pbs:4242",
  "requested_by": "n8n",
  "payload": {
    "run_id": "4242",
    "workflow_name": "PBS CI",
    "html_url": "https://github.com/marcusgoll/pbs/actions/runs/4242"
  }
}`

	routerCmd := exec.Command("bash", routerScriptPath)
	routerCmd.Dir = repoRoot
	routerCmd.Env = append(
		append([]string{}, os.Environ()...),
		"ODIN_BIN="+odinBinary,
		"ODIN_ROOT="+runtimeRoot,
		"SSH_ORIGINAL_COMMAND=",
		"HOME="+homePath,
	)
	routerCmd.Stdin = strings.NewReader(normalizedEnvelope)

	outputBytes, err := routerCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("router intake dispatch error = %v\n%s", err, string(outputBytes))
	}
	output := string(outputBytes)

	var intakeResult struct {
		Task struct {
			ID     int64  `json:"id"`
			Status string `json:"status"`
		} `json:"task"`
		Intake struct {
			Source   string `json:"source"`
			Type     string `json:"type"`
			DedupKey string `json:"dedup_key"`
		} `json:"intake"`
	}
	if err := json.Unmarshal([]byte(output), &intakeResult); err != nil {
		t.Fatalf("unmarshal intake enqueue output = %v\n%s", err, output)
	}
	if intakeResult.Task.ID == 0 {
		t.Fatalf("intake result = %+v, want populated task id", intakeResult)
	}
	if intakeResult.Task.Status != "queued" {
		t.Fatalf("task status = %q, want queued", intakeResult.Task.Status)
	}
	if intakeResult.Intake.Source != "n8n" {
		t.Fatalf("intake source = %q, want n8n", intakeResult.Intake.Source)
	}
	if intakeResult.Intake.Type != "ci_failure" {
		t.Fatalf("intake type = %q, want ci_failure", intakeResult.Intake.Type)
	}
	if intakeResult.Intake.DedupKey != "ci_failure:pbs:4242" {
		t.Fatalf("intake dedup key = %q, want ci_failure:pbs:4242", intakeResult.Intake.DedupKey)
	}

	store := openRuntimeStore(t, runtimeRoot)
	defer store.Close()

	project, err := store.GetProjectByKey(ctx, "pbs")
	if err != nil {
		t.Fatalf("GetProjectByKey(pbs) error = %v", err)
	}
	if _, err := (projects.Service{Store: store}).SetTransitionState(ctx, projects.TransitionStateInput{
		ProjectID:   project.ID,
		Actor:       projects.TransitionControllerOdinOS,
		TargetState: projects.TransitionStateCutover,
		ChangedBy:   "test",
		Notes:       "pbs n8n cutover proof",
	}); err != nil {
		t.Fatalf("SetTransitionState(cutover) error = %v", err)
	}

	serveCtx, cancelServe := context.WithCancel(ctx)
	defer cancelServe()

	var serveOutput bytes.Buffer
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- lifecycle.Run(serveCtx, repoRoot, []string{"serve"}, bytes.NewBufferString(""), &serveOutput)
	}()

	waitForServeReady(t, "http://"+serveAddr+"/readyz", 5*time.Second)
	waitForTaskStatus(t, ctx, store, intakeResult.Task.ID, "completed", 10*time.Second)
	cancelServe()

	if err := <-serveErr; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("lifecycle.Run(serve) error = %v\n%s", err, serveOutput.String())
	}

	task, err := store.GetTask(ctx, intakeResult.Task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.Status != "completed" {
		t.Fatalf("task status after serve = %q, want completed", task.Status)
	}

	completedRuns, err := store.ListRunsByStatus(ctx, "completed")
	if err != nil {
		t.Fatalf("ListRunsByStatus(completed) error = %v", err)
	}
	if !hasCompletedRunForTask(completedRuns, intakeResult.Task.ID) {
		t.Fatalf("completed runs = %+v, want completed run for task %d", completedRuns, intakeResult.Task.ID)
	}

	inboxPath := filepath.Join(runtimeRoot, "inbox")
	if _, err := os.Stat(inboxPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy inbox-style path %s exists, want no legacy inbox handoff", inboxPath)
	}

	assertFileContains(t, filepath.Join(sourceRepoRoot, "docs/operations/odin-os-cutover.md"), []string{
		"pbs",
		"n8n",
		"activate only `pbs` workflows first",
		"dedicated odin-os pilot ingress key",
		"verify `odin status --json`, `odin jobs --json`, `odin runs --json`",
	})
	assertFileContains(t, filepath.Join(sourceRepoRoot, "docs/operations/n8n-cutover.md"), []string{
		"pbs",
		"import `odin-os` workflow exports into n8n",
		"activate only `pbs` workflows first",
		"dedicated odin-os pilot ingress key",
		"trigger manual smoke events",
		"verify no new `/var/odin/inbox/*.json` files appear for `pbs`",
	})
}

func createPBSCutoverRepoRoot(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	for _, dir := range []string{
		filepath.Join(root, "config"),
		filepath.Join(root, "data"),
		filepath.Join(root, "pbs"),
		filepath.Join(root, "registry"),
		filepath.Join(root, "state", "cache"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", dir, err)
		}
	}

	writeTextFile(t, filepath.Join(root, "config", "projects.yaml"), `
version: 1
projects:
  - key: odin-core
    name: Odin Core
    project_class: system_project
    system_project: true
    git_root: ..
    default_branch: main
    policy:
      allowed_commands: [status, test, build]
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
  - key: pbs
    name: PBS
    project_class: github_backed_project
    git_root: ../pbs
    default_branch: main
    github:
      repo: marcusgoll/pbs
    scheduler:
      max_concurrent_runs: 1
      max_starts_per_cycle: 1
      stalled_run_retry_limit: 2
    policy:
      allowed_commands: [status, test, build]
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
cutover:
  pilot_projects:
    - key: pbs
      runtime_owner: odin_os
      primary_controller: odin_os
      stage: cutover
      comparison_context: odin-orchestrator
      legacy_primary_required: false
      shadow_graduation:
        - legacy and Odin readouts agree on project scope and ownership
      limited_action_graduation:
        - allowlisted isolated mutations complete successfully under Odin ownership
      cutover_graduation:
        - routine queued work completes under Odin OS ownership
      legacy_duties_to_retire_in_order:
        - routine queue intake and run selection
`)

	writeTextFile(t, filepath.Join(root, "config", "executors.yaml"), `
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
	writeTextFile(t, filepath.Join(root, "config", "odin.yaml"), `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:9443
  startup_recovery: true
`)
	writeTextFile(t, filepath.Join(root, "README.md"), "pbs cutover fixture\n")
	writeTextFile(t, filepath.Join(root, "pbs", "README.md"), "pbs fixture repo\n")

	initializeGitRepo(t, root)
	initializeGitRepo(t, filepath.Join(root, "pbs"))

	return root
}

func waitForTaskStatus(t *testing.T, ctx context.Context, store *sqlite.Store, taskID int64, want string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		task, err := store.GetTask(ctx, taskID)
		if err != nil {
			t.Fatalf("GetTask(%d) error = %v", taskID, err)
		}
		if task.Status == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	task, err := store.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask(%d) error after timeout = %v", taskID, err)
	}
	t.Fatalf("task %d status = %q after timeout, want %q", taskID, task.Status, want)
}

func waitForServeReady(t *testing.T, readyURL string, timeout time.Duration) {
	t.Helper()

	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		response, err := client.Get(readyURL)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("serve never became ready at %s within %s", readyURL, timeout)
}

func reserveLoopbackAddr(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer listener.Close()
	return listener.Addr().String()
}

func hasCompletedRunForTask(runs []sqlite.Run, taskID int64) bool {
	for _, run := range runs {
		if run.TaskID == taskID && run.Status == "completed" {
			return true
		}
	}
	return false
}
