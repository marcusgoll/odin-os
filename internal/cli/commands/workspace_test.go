package commands

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	coreprojects "odin-os/internal/core/projects"
	"odin-os/internal/runtime/checkpoints"
	"odin-os/internal/store/sqlite"
)

func withWorkspaceCommandEnv(t *testing.T, interactive bool, getenv func(string) string) {
	t.Helper()

	previousInteractive := workspaceIsInteractiveTerminal
	previousGetenv := workspaceCommandGetenv
	workspaceIsInteractiveTerminal = func() bool { return interactive }
	workspaceCommandGetenv = getenv
	t.Cleanup(func() {
		workspaceIsInteractiveTerminal = previousInteractive
		workspaceCommandGetenv = previousGetenv
	})
}

func withWorkspaceCommandStdin(t *testing.T, reader io.Reader) {
	t.Helper()

	previous := workspaceCommandStdin
	workspaceCommandStdin = reader
	t.Cleanup(func() {
		workspaceCommandStdin = previous
	})
}

func TestRunWorkspaceStartStatusAndStop(t *testing.T) {
	ctx := context.Background()
	store := openWorkspaceCommandTestStore(t)
	defer store.Close()

	repoRoot := createWorkspaceCommandGitRepo(t, "main")
	subdir := filepath.Join(repoRoot, "docs")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("MkdirAll(subdir) error = %v", err)
	}

	registry := writeWorkspaceCommandRegistry(t, map[string]string{"alpha": repoRoot})
	installTmuxStub(t)

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	defer func() {
		_ = os.Chdir(oldWD)
	}()
	if err := os.Chdir(subdir); err != nil {
		t.Fatalf("Chdir(subdir) error = %v", err)
	}

	var stdout bytes.Buffer
	if err := RunWorkspace(ctx, store, registry, []string{"start", "--no-attach"}, &stdout); err != nil {
		t.Fatalf("RunWorkspace(start) error = %v", err)
	}
	for _, want := range []string{"project=alpha", "state=live", "branch=main"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("start output = %q, want substring %q", stdout.String(), want)
		}
	}

	stdout.Reset()
	if err := RunWorkspace(ctx, store, registry, []string{"status", "--json"}, &stdout); err != nil {
		t.Fatalf("RunWorkspace(status --json) error = %v", err)
	}
	for _, want := range []string{`"project_key": "alpha"`, `"state": "live"`, `"branch": "main"`} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("status output = %q, want substring %q", stdout.String(), want)
		}
	}

	stdout.Reset()
	if err := RunWorkspace(ctx, store, registry, []string{"list", "--json"}, &stdout); err != nil {
		t.Fatalf("RunWorkspace(list --json) error = %v", err)
	}
	for _, want := range []string{`"project_key": "alpha"`, `"state": "live"`, `"branch": "main"`} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("list output = %q, want substring %q", stdout.String(), want)
		}
	}

	stdout.Reset()
	if err := RunWorkspace(ctx, store, registry, []string{"stop", "--force"}, &stdout); err != nil {
		t.Fatalf("RunWorkspace(stop --force) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "state=stopped") {
		t.Fatalf("stop output = %q, want stopped state", stdout.String())
	}
}

func TestRunWorkspaceStatusShowsDegradedRepoReason(t *testing.T) {
	ctx := context.Background()
	store := openWorkspaceCommandTestStore(t)
	defer store.Close()

	repoRoot := createWorkspaceCommandGitRepo(t, "main")
	registry := writeWorkspaceCommandRegistry(t, map[string]string{"alpha": repoRoot})
	installTmuxStub(t)

	var stdout bytes.Buffer
	if err := RunWorkspace(ctx, store, registry, []string{"start", "alpha", "--no-attach"}, &stdout); err != nil {
		t.Fatalf("RunWorkspace(start) error = %v", err)
	}
	if err := os.RemoveAll(filepath.Join(repoRoot, ".git")); err != nil {
		t.Fatalf("RemoveAll(.git) error = %v", err)
	}

	stdout.Reset()
	if err := RunWorkspace(ctx, store, registry, []string{"status", "alpha"}, &stdout); err != nil {
		t.Fatalf("RunWorkspace(status) error = %v", err)
	}
	for _, want := range []string{
		"state=live",
		"facts_source=last_known",
		"workspace_reason=git_root is not a Git repository",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("status output = %q, want substring %q", stdout.String(), want)
		}
	}
}

func TestRunWorkspaceHandoffCreatesQueuedTaskAndWakePacket(t *testing.T) {
	ctx := context.Background()
	store := openWorkspaceCommandTestStore(t)
	defer store.Close()

	repoRoot := createWorkspaceCommandGitRepo(t, "main")
	subdir := filepath.Join(repoRoot, "docs")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("MkdirAll(subdir) error = %v", err)
	}

	registry := writeWorkspaceCommandRegistry(t, map[string]string{"alpha": repoRoot})
	installTmuxStub(t)

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	defer func() {
		_ = os.Chdir(oldWD)
	}()
	if err := os.Chdir(subdir); err != nil {
		t.Fatalf("Chdir(subdir) error = %v", err)
	}

	var stdout bytes.Buffer
	if err := RunWorkspace(ctx, store, registry, []string{"start", "--no-attach"}, &stdout); err != nil {
		t.Fatalf("RunWorkspace(start) error = %v", err)
	}

	stdout.Reset()
	if err := RunWorkspace(ctx, store, registry, []string{
		"handoff",
		"objective=Implement attach behavior",
		"last_completed_step=Audited workspace session contract",
		"next_steps=Add attach command,Add doctor checks",
		"constraints=Keep tmux authoritative,Do not import transcript",
		"evidence=Validated start and status",
	}, &stdout); err != nil {
		t.Fatalf("RunWorkspace(handoff) error = %v", err)
	}
	for _, want := range []string{"trigger=handoff", "task=", "project=alpha"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("handoff output = %q, want substring %q", stdout.String(), want)
		}
	}

	project, err := store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}

	var taskID int64
	var taskKey string
	var taskTitle string
	var taskStatus string
	if err := store.DB().QueryRowContext(ctx, `
		SELECT id, key, title, status
		FROM tasks
		WHERE project_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, project.ID).Scan(&taskID, &taskKey, &taskTitle, &taskStatus); err != nil {
		t.Fatalf("query latest task error = %v", err)
	}
	if taskStatus != "queued" {
		t.Fatalf("task status = %q, want queued", taskStatus)
	}
	if taskTitle != "Implement attach behavior" {
		t.Fatalf("task title = %q, want objective", taskTitle)
	}

	resumeState, err := (checkpoints.Service{Store: store}).LoadResumeState(ctx, project.ID, taskID)
	if err != nil {
		t.Fatalf("LoadResumeState() error = %v", err)
	}
	if resumeState.TaskKey != taskKey {
		t.Fatalf("resume task key = %q, want %q", resumeState.TaskKey, taskKey)
	}
	if resumeState.Objective != "Implement attach behavior" {
		t.Fatalf("resume objective = %q, want objective", resumeState.Objective)
	}
	if got := resumeState.ProjectContext.Facts["branch"]; got != "main" {
		t.Fatalf("project facts branch = %q, want main", got)
	}
	if got := resumeState.ProjectContext.Facts["current_cwd"]; got != subdir {
		t.Fatalf("project facts current_cwd = %q, want %q", got, subdir)
	}
}

func TestRunWorkspaceHandoffCanTargetExistingTask(t *testing.T) {
	ctx := context.Background()
	store := openWorkspaceCommandTestStore(t)
	defer store.Close()

	repoRoot := createWorkspaceCommandGitRepo(t, "main")
	registry := writeWorkspaceCommandRegistry(t, map[string]string{"alpha": repoRoot})
	installTmuxStub(t)

	var stdout bytes.Buffer
	if err := RunWorkspace(ctx, store, registry, []string{"start", "alpha", "--no-attach"}, &stdout); err != nil {
		t.Fatalf("RunWorkspace(start) error = %v", err)
	}
	if err := RunWorkspace(ctx, store, registry, []string{
		"handoff",
		"alpha",
		"objective=Initial workspace handoff",
		"last_completed_step=Started session",
	}, &stdout); err != nil {
		t.Fatalf("RunWorkspace(first handoff) error = %v", err)
	}

	project, err := store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}
	var taskID int64
	var taskKey string
	if err := store.DB().QueryRowContext(ctx, `
		SELECT id, key
		FROM tasks
		WHERE project_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, project.ID).Scan(&taskID, &taskKey); err != nil {
		t.Fatalf("query latest task error = %v", err)
	}

	stdout.Reset()
	if err := RunWorkspace(ctx, store, registry, []string{
		"handoff",
		"alpha",
		"task=" + taskKey,
		"objective=Continue same task",
		"last_completed_step=Reviewed first handoff",
	}, &stdout); err != nil {
		t.Fatalf("RunWorkspace(second handoff) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "task="+taskKey) {
		t.Fatalf("second handoff output = %q, want same task key", stdout.String())
	}

	var taskCount int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM tasks
		WHERE project_id = ?
	`, project.ID).Scan(&taskCount); err != nil {
		t.Fatalf("count tasks error = %v", err)
	}
	if taskCount != 1 {
		t.Fatalf("task count = %d, want 1", taskCount)
	}

	resumeState, err := (checkpoints.Service{Store: store}).LoadResumeState(ctx, project.ID, taskID)
	if err != nil {
		t.Fatalf("LoadResumeState() error = %v", err)
	}
	if resumeState.Objective != "Continue same task" {
		t.Fatalf("resume objective = %q, want updated objective", resumeState.Objective)
	}
}

func TestRunWorkspaceHandoffAcceptsJSONStdin(t *testing.T) {
	ctx := context.Background()
	store := openWorkspaceCommandTestStore(t)
	defer store.Close()

	repoRoot := createWorkspaceCommandGitRepo(t, "main")
	registry := writeWorkspaceCommandRegistry(t, map[string]string{"alpha": repoRoot})
	installTmuxStub(t)
	withWorkspaceCommandStdin(t, strings.NewReader(`{
		"objective": "JSON workspace handoff",
		"last_completed_step": "Collected workspace facts",
		"next_steps": ["Add JSON mode", "Verify command path"],
		"constraints": ["Keep command thin"],
		"evidence": [{"kind":"note","summary":"Structured input smoke"}]
	}`))

	var stdout bytes.Buffer
	if err := RunWorkspace(ctx, store, registry, []string{"start", "alpha", "--no-attach"}, &stdout); err != nil {
		t.Fatalf("RunWorkspace(start) error = %v", err)
	}

	stdout.Reset()
	if err := RunWorkspace(ctx, store, registry, []string{"handoff", "alpha", "--json"}, &stdout); err != nil {
		t.Fatalf("RunWorkspace(handoff --json) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "trigger=handoff") {
		t.Fatalf("handoff output = %q, want trigger", stdout.String())
	}

	project, err := store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}
	var taskID int64
	if err := store.DB().QueryRowContext(ctx, `
		SELECT id
		FROM tasks
		WHERE project_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, project.ID).Scan(&taskID); err != nil {
		t.Fatalf("query latest task error = %v", err)
	}

	resumeState, err := (checkpoints.Service{Store: store}).LoadResumeState(ctx, project.ID, taskID)
	if err != nil {
		t.Fatalf("LoadResumeState() error = %v", err)
	}
	if resumeState.Objective != "JSON workspace handoff" {
		t.Fatalf("resume objective = %q, want JSON objective", resumeState.Objective)
	}
	if len(resumeState.NextSteps) != 2 {
		t.Fatalf("resume next steps = %#v, want 2 items", resumeState.NextSteps)
	}
}

func TestRunWorkspaceAttachUsesExistingSession(t *testing.T) {
	ctx := context.Background()
	store := openWorkspaceCommandTestStore(t)
	defer store.Close()

	repoRoot := createWorkspaceCommandGitRepo(t, "main")
	registry := writeWorkspaceCommandRegistry(t, map[string]string{"alpha": repoRoot})
	stateDir := installTmuxStub(t)

	var stdout bytes.Buffer
	if err := RunWorkspace(ctx, store, registry, []string{"start", "alpha", "--no-attach"}, &stdout); err != nil {
		t.Fatalf("RunWorkspace(start) error = %v", err)
	}

	stdout.Reset()
	if err := RunWorkspace(ctx, store, registry, []string{"attach", "alpha"}, &stdout); err != nil {
		t.Fatalf("RunWorkspace(attach) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "attached=odin-workspace-alpha") {
		t.Fatalf("attach output = %q, want attached session", stdout.String())
	}

	attachedBytes, err := os.ReadFile(filepath.Join(stateDir, "odin-workspace-alpha", "attached"))
	if err != nil {
		t.Fatalf("ReadFile(attached) error = %v", err)
	}
	if strings.TrimSpace(string(attachedBytes)) != "1" {
		t.Fatalf("attached count = %q, want 1", strings.TrimSpace(string(attachedBytes)))
	}
}

func TestRunWorkspaceStartAutoAttachesWhenInteractive(t *testing.T) {
	ctx := context.Background()
	store := openWorkspaceCommandTestStore(t)
	defer store.Close()

	repoRoot := createWorkspaceCommandGitRepo(t, "main")
	registry := writeWorkspaceCommandRegistry(t, map[string]string{"alpha": repoRoot})
	stateDir := installTmuxStub(t)
	withWorkspaceCommandEnv(t, true, func(string) string { return "" })

	var stdout bytes.Buffer
	if err := RunWorkspace(ctx, store, registry, []string{"start", "alpha"}, &stdout); err != nil {
		t.Fatalf("RunWorkspace(start) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "attached=odin-workspace-alpha") {
		t.Fatalf("start output = %q, want attached session", stdout.String())
	}

	attachedBytes, err := os.ReadFile(filepath.Join(stateDir, "odin-workspace-alpha", "attached"))
	if err != nil {
		t.Fatalf("ReadFile(attached) error = %v", err)
	}
	if strings.TrimSpace(string(attachedBytes)) != "1" {
		t.Fatalf("attached count = %q, want 1", strings.TrimSpace(string(attachedBytes)))
	}
}

func TestRunWorkspaceStartSkipsNestedTMuxUnlessForced(t *testing.T) {
	ctx := context.Background()
	store := openWorkspaceCommandTestStore(t)
	defer store.Close()

	repoRoot := createWorkspaceCommandGitRepo(t, "main")
	registry := writeWorkspaceCommandRegistry(t, map[string]string{"alpha": repoRoot})
	stateDir := installTmuxStub(t)
	withWorkspaceCommandEnv(t, true, func(key string) string {
		if key == "TMUX" {
			return "/tmp/tmux-1000/default,123,0"
		}
		return ""
	})

	var stdout bytes.Buffer
	if err := RunWorkspace(ctx, store, registry, []string{"start", "alpha"}, &stdout); err != nil {
		t.Fatalf("RunWorkspace(start nested tmux) error = %v", err)
	}
	for _, want := range []string{"attach_skipped=nested_tmux", "attach_command=tmux attach-session -t odin-workspace-alpha"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("start output = %q, want substring %q", stdout.String(), want)
		}
	}

	attachedBytes, err := os.ReadFile(filepath.Join(stateDir, "odin-workspace-alpha", "attached"))
	if err != nil {
		t.Fatalf("ReadFile(attached) error = %v", err)
	}
	if strings.TrimSpace(string(attachedBytes)) != "0" {
		t.Fatalf("attached count = %q, want 0", strings.TrimSpace(string(attachedBytes)))
	}

	stdout.Reset()
	if err := RunWorkspace(ctx, store, registry, []string{"start", "alpha", "--force-attach"}, &stdout); err != nil {
		t.Fatalf("RunWorkspace(start --force-attach) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "attached=odin-workspace-alpha") {
		t.Fatalf("force-attach output = %q, want attached session", stdout.String())
	}
}

func openWorkspaceCommandTestStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func createWorkspaceCommandGitRepo(t *testing.T, branch string) string {
	t.Helper()

	root := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll(root) error = %v", err)
	}
	runWorkspaceGit(t, root, "init", "-b", branch)
	runWorkspaceGit(t, root, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "init")
	return root
}

func writeWorkspaceCommandRegistry(t *testing.T, projects map[string]string) coreprojects.Registry {
	t.Helper()

	root := t.TempDir()
	configPath := filepath.Join(root, "projects.yaml")
	builder := &strings.Builder{}
	builder.WriteString("version: 1\nprojects:\n")
	for key, gitRoot := range projects {
		builder.WriteString("  - key: " + key + "\n")
		builder.WriteString("    name: " + key + "\n")
		builder.WriteString("    project_class: local_git_project\n")
		builder.WriteString("    git_root: " + gitRoot + "\n")
		builder.WriteString("    default_branch: main\n")
		builder.WriteString("    policy:\n")
		builder.WriteString("      allowed_commands: [status]\n")
		builder.WriteString("      branch_rules:\n")
		builder.WriteString("        protected_branches: [main]\n")
		builder.WriteString("        require_worktree: true\n")
		builder.WriteString("        require_task_branch: true\n")
		builder.WriteString("        allow_default_branch_mutation: false\n")
		builder.WriteString("      approval_gates:\n")
		builder.WriteString("        require_for_governance_changes: true\n")
		builder.WriteString("        require_for_destructive_operations: true\n")
		builder.WriteString("        require_for_system_project_changes: false\n")
		builder.WriteString("      merge_policy:\n")
		builder.WriteString("        mode: squash\n")
		builder.WriteString("        allow_direct_to_default_branch: false\n")
		builder.WriteString("      destructive_operations:\n")
		builder.WriteString("        allow_reset: false\n")
		builder.WriteString("        allow_clean: false\n")
		builder.WriteString("        allow_force_push: false\n")
		builder.WriteString("        require_explicit_approval: true\n")
	}
	if err := os.WriteFile(configPath, []byte(builder.String()), 0o644); err != nil {
		t.Fatalf("WriteFile(projects.yaml) error = %v", err)
	}

	registry, diagnostics, err := coreprojects.Register(configPath)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("Register() diagnostics = %#v", diagnostics)
	}
	return registry
}

func installTmuxStub(t *testing.T) string {
	t.Helper()

	binDir := t.TempDir()
	stateDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(binDir, "codex"), []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(codex stub) error = %v", err)
	}

	script := `#!/usr/bin/env bash
set -euo pipefail
state_dir="${ODIN_TEST_TMUX_DIR:?missing ODIN_TEST_TMUX_DIR}"
mkdir -p "${state_dir}"

session_path() {
  printf '%s/%s' "${state_dir}" "$1"
}

command="$1"
shift
case "${command}" in
  has-session)
    [[ "$1" == "-t" ]]
    session_dir="$(session_path "$2")"
    [[ -d "${session_dir}" ]]
    ;;
  new-session)
    session=""
    cwd=""
    while [[ $# -gt 0 ]]; do
      case "$1" in
        -d)
          shift
          ;;
        -s)
          session="$2"
          shift 2
          ;;
        -c)
          cwd="$2"
          shift 2
          ;;
        *)
          break
          ;;
      esac
    done
    session_dir="$(session_path "${session}")"
    mkdir -p "${session_dir}/env"
    printf '%s' "${cwd}" >"${session_dir}/current_path"
    printf '0' >"${session_dir}/attached"
    ;;
  set-environment)
    [[ "$1" == "-t" ]]
    session_dir="$(session_path "$2")"
    key="$3"
    value="$4"
    mkdir -p "${session_dir}/env"
    printf '%s' "${value}" >"${session_dir}/env/${key}"
    ;;
  show-environment)
    [[ "$1" == "-t" ]]
    session_dir="$(session_path "$2")"
    key="$3"
    if [[ -f "${session_dir}/env/${key}" ]]; then
      value="$(cat "${session_dir}/env/${key}")"
      printf '%s=%s\n' "${key}" "${value}"
    else
      printf -- '-%s\n' "${key}"
    fi
    ;;
  display-message)
    [[ "$1" == "-p" ]]
    [[ "$2" == "-t" ]]
    session_dir="$(session_path "$3")"
    format="$4"
    case "${format}" in
      '#{pane_current_path}')
        cat "${session_dir}/current_path"
        ;;
      '#{session_attached}')
        cat "${session_dir}/attached"
        ;;
      *)
        exit 1
        ;;
    esac
    ;;
  kill-session)
    [[ "$1" == "-t" ]]
    rm -rf "$(session_path "$2")"
    ;;
  attach-session)
    [[ "$1" == "-t" ]]
    session_dir="$(session_path "$2")"
    attached="$(cat "${session_dir}/attached")"
    attached=$((attached + 1))
    printf '%s' "${attached}" >"${session_dir}/attached"
    ;;
  *)
    exit 1
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "tmux"), []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(tmux stub) error = %v", err)
	}

	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath)
	t.Setenv("ODIN_TEST_TMUX_DIR", stateDir)
	return stateDir
}

func runWorkspaceGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	commandArgs := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", commandArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v error = %v\n%s", args, err, output)
	}
}
