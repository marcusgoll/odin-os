package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreworkspace "odin-os/internal/core/workspace"
	"odin-os/internal/runtime/checkpoints"
	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/store/sqlite"
)

func TestProjectWorkspaceCLIIntegration(t *testing.T) {
	ctx := context.Background()
	sourceRepoRoot := projectRoot(t)
	odinRepoRoot := createWorkspaceIntegrationOdinRoot(t, sourceRepoRoot)
	odinBinary := buildWorkspaceIntegrationBinary(t, sourceRepoRoot, odinRepoRoot)
	runtimeRoot := t.TempDir()

	managedRepo := createGitRepository(t)
	projectKey := filepath.Base(managedRepo)
	sessionName := coreworkspace.SessionName(projectKey)
	subdir := filepath.Join(managedRepo, "docs")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("MkdirAll(subdir) error = %v", err)
	}

	stubStateDir := installWorkspaceIntegrationStubs(t)

	output, err := runWorkspaceIntegrationCommand(t, managedRepo, odinBinary, runtimeRoot, nil, "", "workspace", "start", "--no-attach")
	if err == nil {
		t.Fatalf("workspace start before enroll unexpectedly succeeded\n%s", output)
	}
	if !strings.Contains(output, "not enrolled") || !strings.Contains(output, "project enroll") {
		t.Fatalf("workspace start before enroll output = %q, want enrollment guidance", output)
	}

	output, err = runWorkspaceIntegrationCommand(t, managedRepo, odinBinary, runtimeRoot, nil, "", "doctor", "--json")
	if err != nil {
		t.Fatalf("doctor --json before enroll error = %v\n%s", err, output)
	}
	beforeCheck := decodeWorkspaceDoctorCheck(t, output)
	if beforeCheck.Details["applicable"] != "false" {
		t.Fatalf("workspace doctor applicable before enroll = %q, want false", beforeCheck.Details["applicable"])
	}

	output, err = runWorkspaceIntegrationCommand(t, managedRepo, odinBinary, runtimeRoot, nil, "", "project", "enroll")
	if err != nil {
		t.Fatalf("project enroll error = %v\n%s", err, output)
	}
	for _, want := range []string{
		"project=" + projectKey,
		"class=local_git_project",
		"git_root=" + managedRepo,
		"default_branch=main",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("project enroll output = %q, want substring %q", output, want)
		}
	}

	output, err = runWorkspaceIntegrationCommand(t, managedRepo, odinBinary, runtimeRoot, nil, "", "project", "enroll")
	if err == nil {
		t.Fatalf("duplicate project enroll unexpectedly succeeded\n%s", output)
	}
	if !strings.Contains(output, "already exists") || !strings.Contains(output, "project update") {
		t.Fatalf("duplicate project enroll output = %q, want update guidance", output)
	}

	output, err = runWorkspaceIntegrationCommand(t, odinRepoRoot, odinBinary, runtimeRoot, nil, "", "project", "list", "--json")
	if err != nil {
		t.Fatalf("project list --json error = %v\n%s", err, output)
	}
	var projectList []workspaceProjectView
	if err := json.Unmarshal([]byte(output), &projectList); err != nil {
		t.Fatalf("Unmarshal(project list) error = %v\n%s", err, output)
	}
	if len(projectList) != 1 {
		t.Fatalf("project list length = %d, want 1", len(projectList))
	}
	if projectList[0].Key != projectKey || !projectList[0].WorkspaceEligible {
		t.Fatalf("project list[0] = %+v, want enrolled eligible project", projectList[0])
	}

	output, err = runWorkspaceIntegrationCommand(t, odinRepoRoot, odinBinary, runtimeRoot, nil, "", "project", "show", projectKey, "--json")
	if err != nil {
		t.Fatalf("project show --json error = %v\n%s", err, output)
	}
	var projectShow workspaceProjectView
	if err := json.Unmarshal([]byte(output), &projectShow); err != nil {
		t.Fatalf("Unmarshal(project show) error = %v\n%s", err, output)
	}
	if projectShow.Key != projectKey || projectShow.GitRoot != managedRepo || !projectShow.WorkspaceEligible {
		t.Fatalf("project show = %+v, want enrolled eligible project", projectShow)
	}

	output, err = runWorkspaceIntegrationCommand(t, managedRepo, odinBinary, runtimeRoot, nil, "", "doctor", "--json")
	if err != nil {
		t.Fatalf("doctor --json after enroll error = %v\n%s", err, output)
	}
	afterCheck := decodeWorkspaceDoctorCheck(t, output)
	if afterCheck.Status != healthsvc.StatusHealthy {
		t.Fatalf("workspace doctor status after enroll = %q, want healthy", afterCheck.Status)
	}
	if afterCheck.Details["applicable"] != "true" {
		t.Fatalf("workspace doctor applicable after enroll = %q, want true", afterCheck.Details["applicable"])
	}
	if afterCheck.Details["tmux_path"] == "" || afterCheck.Details["codex_path"] == "" {
		t.Fatalf("workspace doctor details = %+v, want tmux and codex paths", afterCheck.Details)
	}

	output, err = runWorkspaceIntegrationCommand(t, subdir, odinBinary, runtimeRoot, nil, "", "workspace", "start", "--no-attach")
	if err != nil {
		t.Fatalf("workspace start --no-attach error = %v\n%s", err, output)
	}
	for _, want := range []string{
		"project=" + projectKey,
		"session=" + sessionName,
		"state=live",
		"attach_skipped=disabled",
		"attach_command=tmux attach-session -t " + sessionName,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("workspace start output = %q, want substring %q", output, want)
		}
	}

	if got := readWorkspaceStubValue(t, stubStateDir, sessionName, "current_path"); got != subdir {
		t.Fatalf("stub current_path = %q, want %q", got, subdir)
	}
	if got := readWorkspaceStubEnv(t, stubStateDir, sessionName, coreworkspace.EnvProjectKey); got != projectKey {
		t.Fatalf("stub project env = %q, want %q", got, projectKey)
	}
	if got := readWorkspaceStubEnv(t, stubStateDir, sessionName, coreworkspace.EnvSessionName); got != sessionName {
		t.Fatalf("stub session env = %q, want %q", got, sessionName)
	}

	output, err = runWorkspaceIntegrationCommand(t, odinRepoRoot, odinBinary, runtimeRoot, nil, "", "workspace", "list", "--json")
	if err != nil {
		t.Fatalf("workspace list --json error = %v\n%s", err, output)
	}
	var liveList []coreworkspace.Status
	if err := json.Unmarshal([]byte(output), &liveList); err != nil {
		t.Fatalf("Unmarshal(workspace list live) error = %v\n%s", err, output)
	}
	if len(liveList) != 1 {
		t.Fatalf("workspace list length = %d, want 1", len(liveList))
	}
	if liveList[0].ProjectKey != projectKey || liveList[0].State != coreworkspace.StateLive || liveList[0].Branch != "main" {
		t.Fatalf("workspace list[0] = %+v, want live branch main for %s", liveList[0], projectKey)
	}

	output, err = runWorkspaceIntegrationCommand(t, subdir, odinBinary, runtimeRoot, nil, "", "workspace", "attach")
	if err != nil {
		t.Fatalf("workspace attach error = %v\n%s", err, output)
	}
	if !strings.Contains(output, "attached="+sessionName) {
		t.Fatalf("workspace attach output = %q, want attached session", output)
	}

	output, err = runWorkspaceIntegrationCommand(t, subdir, odinBinary, runtimeRoot, nil, "", "workspace", "status", "--json")
	if err != nil {
		t.Fatalf("workspace status --json error = %v\n%s", err, output)
	}
	var liveStatus coreworkspace.Status
	if err := json.Unmarshal([]byte(output), &liveStatus); err != nil {
		t.Fatalf("Unmarshal(workspace status live) error = %v\n%s", err, output)
	}
	if liveStatus.ProjectKey != projectKey || liveStatus.State != coreworkspace.StateLive {
		t.Fatalf("live workspace status = %+v, want live status for %s", liveStatus, projectKey)
	}
	if liveStatus.Branch != "main" || liveStatus.CurrentCwd != subdir || liveStatus.AttachedCount != 1 {
		t.Fatalf("live workspace status = %+v, want branch main cwd %q attached_count 1", liveStatus, subdir)
	}
	if liveStatus.FactsSource != coreworkspace.FactsSourceLive {
		t.Fatalf("live workspace facts_source = %q, want live", liveStatus.FactsSource)
	}

	handoffPayload := `{
  "objective": "Integration workspace handoff",
  "last_completed_step": "Started and inspected workspace",
  "next_steps": ["Verify handoff packet", "Stop workspace"],
  "constraints": ["Keep tmux authoritative"],
  "evidence": [{"kind":"note","summary":"Integration CLI proof"}]
}`
	output, err = runWorkspaceIntegrationCommand(t, subdir, odinBinary, runtimeRoot, nil, handoffPayload, "workspace", "handoff", "--json")
	if err != nil {
		t.Fatalf("workspace handoff --json error = %v\n%s", err, output)
	}
	for _, want := range []string{
		"project=" + projectKey,
		"trigger=handoff",
		"state=queued",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("workspace handoff output = %q, want substring %q", output, want)
		}
	}

	store := openRuntimeStore(t, runtimeRoot)
	defer store.Close()

	project, err := store.GetProjectByKey(ctx, projectKey)
	if err != nil {
		t.Fatalf("GetProjectByKey(%s) error = %v", projectKey, err)
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

	resumeState, err := (checkpoints.Service{Store: store}).LoadResumeState(ctx, project.ID, taskID)
	if err != nil {
		t.Fatalf("LoadResumeState() error = %v", err)
	}
	if resumeState.TaskKey != taskKey {
		t.Fatalf("resume task key = %q, want %q", resumeState.TaskKey, taskKey)
	}
	if resumeState.Objective != "Integration workspace handoff" {
		t.Fatalf("resume objective = %q, want handoff objective", resumeState.Objective)
	}
	if got := resumeState.ProjectContext.Facts["branch"]; got != "main" {
		t.Fatalf("resume project facts branch = %q, want main", got)
	}
	if got := resumeState.ProjectContext.Facts["current_cwd"]; got != subdir {
		t.Fatalf("resume project facts current_cwd = %q, want %q", got, subdir)
	}

	output, err = runWorkspaceIntegrationCommand(t, subdir, odinBinary, runtimeRoot, nil, "", "workspace", "handoff", "task="+taskKey, "objective=Continue integration workspace task", "last_completed_step=Verified initial handoff")
	if err != nil {
		t.Fatalf("workspace handoff existing task error = %v\n%s", err, output)
	}
	if !strings.Contains(output, "task="+taskKey) || !strings.Contains(output, "trigger=handoff") {
		t.Fatalf("workspace handoff existing task output = %q, want same task key %s", output, taskKey)
	}

	var taskCount int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM tasks
		WHERE project_id = ?
	`, project.ID).Scan(&taskCount); err != nil {
		t.Fatalf("count tasks after existing handoff error = %v", err)
	}
	if taskCount != 1 {
		t.Fatalf("task count after existing handoff = %d, want 1", taskCount)
	}

	resumeState, err = (checkpoints.Service{Store: store}).LoadResumeState(ctx, project.ID, taskID)
	if err != nil {
		t.Fatalf("LoadResumeState(existing task handoff) error = %v", err)
	}
	if resumeState.Objective != "Continue integration workspace task" {
		t.Fatalf("resume objective after existing task handoff = %q, want updated objective", resumeState.Objective)
	}

	output, err = runWorkspaceIntegrationCommand(t, subdir, odinBinary, runtimeRoot, nil, "", "workspace", "stop")
	if err == nil {
		t.Fatalf("workspace stop without force unexpectedly succeeded\n%s", output)
	}
	if !strings.Contains(output, "--force") {
		t.Fatalf("workspace stop without force output = %q, want force guidance", output)
	}

	output, err = runWorkspaceIntegrationCommand(t, subdir, odinBinary, runtimeRoot, nil, "", "workspace", "stop", "--force")
	if err != nil {
		t.Fatalf("workspace stop --force error = %v\n%s", err, output)
	}
	if !strings.Contains(output, "state=stopped") {
		t.Fatalf("workspace stop --force output = %q, want stopped state", output)
	}

	output, err = runWorkspaceIntegrationCommand(t, subdir, odinBinary, runtimeRoot, nil, "", "workspace", "status", "--json")
	if err != nil {
		t.Fatalf("workspace status after stop error = %v\n%s", err, output)
	}
	var stoppedStatus coreworkspace.Status
	if err := json.Unmarshal([]byte(output), &stoppedStatus); err != nil {
		t.Fatalf("Unmarshal(workspace status stopped) error = %v\n%s", err, output)
	}
	if stoppedStatus.State != coreworkspace.StateStopped || stoppedStatus.FactsSource != coreworkspace.FactsSourceLastKnown {
		t.Fatalf("stopped workspace status = %+v, want stopped last_known status", stoppedStatus)
	}
	if stoppedStatus.CurrentCwd != subdir || stoppedStatus.Branch != "main" || stoppedStatus.LastKnownRefreshedAt == "" {
		t.Fatalf("stopped workspace status = %+v, want last-known cwd, branch, and refreshed timestamp", stoppedStatus)
	}

	output, err = runWorkspaceIntegrationCommand(t, subdir, odinBinary, runtimeRoot, nil, "", "workspace", "attach")
	if err == nil {
		t.Fatalf("workspace attach after stop unexpectedly succeeded\n%s", output)
	}
	if !strings.Contains(output, "not live") || !strings.Contains(output, "workspace start") {
		t.Fatalf("workspace attach after stop output = %q, want restart guidance", output)
	}

	output, err = runWorkspaceIntegrationCommand(t, subdir, odinBinary, runtimeRoot, nil, "", "workspace", "handoff", "objective=Stopped workspace handoff")
	if err != nil {
		t.Fatalf("workspace handoff after stop error = %v\n%s", err, output)
	}
	if !strings.Contains(output, "trigger=handoff") || !strings.Contains(output, "project="+projectKey) {
		t.Fatalf("workspace handoff after stop output = %q, want handoff summary", output)
	}

	var stoppedTaskID int64
	var stoppedTaskKey string
	if err := store.DB().QueryRowContext(ctx, `
		SELECT id, key
		FROM tasks
		WHERE project_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, project.ID).Scan(&stoppedTaskID, &stoppedTaskKey); err != nil {
		t.Fatalf("query latest stopped handoff task error = %v", err)
	}
	stoppedResumeState, err := (checkpoints.Service{Store: store}).LoadResumeState(ctx, project.ID, stoppedTaskID)
	if err != nil {
		t.Fatalf("LoadResumeState(stopped handoff) error = %v", err)
	}
	if stoppedResumeState.TaskKey != stoppedTaskKey {
		t.Fatalf("stopped handoff task key = %q, want %q", stoppedResumeState.TaskKey, stoppedTaskKey)
	}
	if stoppedResumeState.Objective != "Stopped workspace handoff" {
		t.Fatalf("stopped handoff objective = %q, want stopped handoff objective", stoppedResumeState.Objective)
	}
	if got := stoppedResumeState.ProjectContext.Facts["facts_source"]; got != string(coreworkspace.FactsSourceLastKnown) {
		t.Fatalf("stopped handoff facts_source = %q, want %q", got, coreworkspace.FactsSourceLastKnown)
	}
	if got := stoppedResumeState.ProjectContext.Facts["workspace_state"]; got != string(coreworkspace.StateStopped) {
		t.Fatalf("stopped handoff workspace_state = %q, want %q", got, coreworkspace.StateStopped)
	}
	if got := stoppedResumeState.ProjectContext.Facts["current_cwd"]; got != subdir {
		t.Fatalf("stopped handoff current_cwd = %q, want %q", got, subdir)
	}

	output, err = runWorkspaceIntegrationCommand(t, odinRepoRoot, odinBinary, runtimeRoot, nil, "", "workspace", "list", "--json")
	if err != nil {
		t.Fatalf("workspace list after stop error = %v\n%s", err, output)
	}
	var stoppedList []coreworkspace.Status
	if err := json.Unmarshal([]byte(output), &stoppedList); err != nil {
		t.Fatalf("Unmarshal(workspace list stopped) error = %v\n%s", err, output)
	}
	if len(stoppedList) != 1 {
		t.Fatalf("stopped workspace list length = %d, want 1", len(stoppedList))
	}
	if stoppedList[0].ProjectKey != projectKey || stoppedList[0].State != coreworkspace.StateStopped || stoppedList[0].Branch != "main" {
		t.Fatalf("stopped workspace list[0] = %+v, want stopped branch main for %s", stoppedList[0], projectKey)
	}

	movedRepo := createGitRepository(t)
	runGit(t, movedRepo, "checkout", "-b", "develop")

	output, err = runWorkspaceIntegrationCommand(t, movedRepo, odinBinary, runtimeRoot, nil, "", "project", "update", projectKey, "name=Updated Project")
	if err != nil {
		t.Fatalf("project update error = %v\n%s", err, output)
	}
	for _, want := range []string{
		"project=" + projectKey,
		"git_root=" + movedRepo,
		"default_branch=develop",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("project update output = %q, want substring %q", output, want)
		}
	}

	output, err = runWorkspaceIntegrationCommand(t, odinRepoRoot, odinBinary, runtimeRoot, nil, "", "project", "show", projectKey, "--json")
	if err != nil {
		t.Fatalf("project show after update error = %v\n%s", err, output)
	}
	var updatedProjectShow workspaceProjectView
	if err := json.Unmarshal([]byte(output), &updatedProjectShow); err != nil {
		t.Fatalf("Unmarshal(project show after update) error = %v\n%s", err, output)
	}
	if updatedProjectShow.Key != projectKey || updatedProjectShow.Name != "Updated Project" {
		t.Fatalf("updated project show = %+v, want renamed project %s", updatedProjectShow, projectKey)
	}
	if updatedProjectShow.GitRoot != movedRepo || updatedProjectShow.DefaultBranch != "develop" || !updatedProjectShow.WorkspaceEligible {
		t.Fatalf("updated project show = %+v, want moved repo with develop branch", updatedProjectShow)
	}
}

func TestProjectWorkspaceCLIIntegrationInteractiveAttach(t *testing.T) {
	newWorkspaceBinary := func(t *testing.T) string {
		t.Helper()
		sourceRepoRoot := projectRoot(t)
		odinRepoRoot := createWorkspaceIntegrationOdinRoot(t, sourceRepoRoot)
		return buildWorkspaceIntegrationBinary(t, sourceRepoRoot, odinRepoRoot)
	}

	t.Run("auto attach when interactive", func(t *testing.T) {
		odinBinary := newWorkspaceBinary(t)
		runtimeRoot := t.TempDir()
		managedRepo := createGitRepository(t)
		projectKey := filepath.Base(managedRepo)
		sessionName := coreworkspace.SessionName(projectKey)
		subdir := filepath.Join(managedRepo, "docs")
		if err := os.MkdirAll(subdir, 0o755); err != nil {
			t.Fatalf("MkdirAll(subdir) error = %v", err)
		}

		stubStateDir := installWorkspaceIntegrationStubs(t)

		output, err := runWorkspaceIntegrationCommand(t, managedRepo, odinBinary, runtimeRoot, nil, "", "project", "enroll")
		if err != nil {
			t.Fatalf("project enroll error = %v\n%s", err, output)
		}

		output, err = runWorkspaceIntegrationPTYCommand(t, subdir, odinBinary, runtimeRoot, nil, "", "workspace", "start")
		if err != nil {
			t.Fatalf("interactive workspace start error = %v\n%s", err, output)
		}
		if !strings.Contains(output, "attached="+sessionName) || !strings.Contains(output, "project="+projectKey) {
			t.Fatalf("interactive workspace start output = %q, want attached session %s for %s", output, sessionName, projectKey)
		}
		if got := readWorkspaceStubValue(t, stubStateDir, sessionName, "attached"); got != "1" {
			t.Fatalf("interactive attached count = %q, want 1", got)
		}

		output, err = runWorkspaceIntegrationCommand(t, subdir, odinBinary, runtimeRoot, nil, "", "workspace", "status", "--json")
		if err != nil {
			t.Fatalf("workspace status after interactive attach error = %v\n%s", err, output)
		}
		var liveStatus coreworkspace.Status
		if err := json.Unmarshal([]byte(output), &liveStatus); err != nil {
			t.Fatalf("Unmarshal(interactive workspace status) error = %v\n%s", err, output)
		}
		if liveStatus.State != coreworkspace.StateLive || liveStatus.AttachedCount != 1 {
			t.Fatalf("interactive workspace status = %+v, want live attached_count 1", liveStatus)
		}
		if liveStatus.CurrentCwd != subdir {
			t.Fatalf("interactive workspace status cwd = %q, want %q", liveStatus.CurrentCwd, subdir)
		}
	})

	t.Run("nested tmux skips attach unless forced", func(t *testing.T) {
		odinBinary := newWorkspaceBinary(t)
		runtimeRoot := t.TempDir()
		managedRepo := createGitRepository(t)
		projectKey := filepath.Base(managedRepo)
		sessionName := coreworkspace.SessionName(projectKey)
		subdir := filepath.Join(managedRepo, "docs")
		if err := os.MkdirAll(subdir, 0o755); err != nil {
			t.Fatalf("MkdirAll(subdir) error = %v", err)
		}

		stubStateDir := installWorkspaceIntegrationStubs(t)
		nestedTMUX := "/tmp/tmux-1000/default,123,0"

		output, err := runWorkspaceIntegrationCommand(t, managedRepo, odinBinary, runtimeRoot, nil, "", "project", "enroll")
		if err != nil {
			t.Fatalf("project enroll error = %v\n%s", err, output)
		}

		output, err = runWorkspaceIntegrationPTYCommand(t, subdir, odinBinary, runtimeRoot, map[string]string{"TMUX": nestedTMUX}, "", "workspace", "start")
		if err != nil {
			t.Fatalf("nested tmux workspace start error = %v\n%s", err, output)
		}
		for _, want := range []string{
			"project=" + projectKey,
			"state=live",
			"attach_skipped=nested_tmux",
			"attach_command=tmux attach-session -t " + sessionName,
		} {
			if !strings.Contains(output, want) {
				t.Fatalf("nested tmux workspace start output = %q, want substring %q", output, want)
			}
		}
		if got := readWorkspaceStubValue(t, stubStateDir, sessionName, "attached"); got != "0" {
			t.Fatalf("nested tmux attached count = %q, want 0", got)
		}

		output, err = runWorkspaceIntegrationPTYCommand(t, subdir, odinBinary, runtimeRoot, map[string]string{"TMUX": nestedTMUX}, "", "workspace", "start", "--force-attach")
		if err != nil {
			t.Fatalf("nested tmux workspace start --force-attach error = %v\n%s", err, output)
		}
		if !strings.Contains(output, "attached="+sessionName) {
			t.Fatalf("force-attach output = %q, want attached session %s", output, sessionName)
		}
		if got := readWorkspaceStubValue(t, stubStateDir, sessionName, "attached"); got != "1" {
			t.Fatalf("force-attach attached count = %q, want 1", got)
		}
	})
}

func TestProjectWorkspaceCLIIntegrationExplicitProjectTarget(t *testing.T) {
	ctx := context.Background()
	sourceRepoRoot := projectRoot(t)
	odinRepoRoot := createWorkspaceIntegrationOdinRoot(t, sourceRepoRoot)
	odinBinary := buildWorkspaceIntegrationBinary(t, sourceRepoRoot, odinRepoRoot)
	runtimeRoot := t.TempDir()

	managedRepo := createGitRepository(t)
	projectKey := filepath.Base(managedRepo)
	sessionName := coreworkspace.SessionName(projectKey)
	outsideDir := t.TempDir()
	stubStateDir := installWorkspaceIntegrationStubs(t)

	output, err := runWorkspaceIntegrationCommand(t, managedRepo, odinBinary, runtimeRoot, nil, "", "project", "enroll")
	if err != nil {
		t.Fatalf("project enroll error = %v\n%s", err, output)
	}

	output, err = runWorkspaceIntegrationCommand(t, outsideDir, odinBinary, runtimeRoot, nil, "", "workspace", "status", "--json")
	if err == nil {
		t.Fatalf("workspace status without explicit project outside repo unexpectedly succeeded\n%s", output)
	}
	if !strings.Contains(output, "workspace target required") {
		t.Fatalf("workspace status outside repo output = %q, want explicit target guidance", output)
	}

	output, err = runWorkspaceIntegrationCommand(t, outsideDir, odinBinary, runtimeRoot, nil, "", "workspace", "start", projectKey, "--no-attach")
	if err != nil {
		t.Fatalf("workspace start explicit project outside repo error = %v\n%s", err, output)
	}
	for _, want := range []string{
		"project=" + projectKey,
		"session=" + sessionName,
		"state=live",
		"attach_skipped=disabled",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("workspace start explicit project output = %q, want substring %q", output, want)
		}
	}
	if got := readWorkspaceStubValue(t, stubStateDir, sessionName, "current_path"); got != managedRepo {
		t.Fatalf("explicit project stub current_path = %q, want repo root %q", got, managedRepo)
	}

	output, err = runWorkspaceIntegrationCommand(t, outsideDir, odinBinary, runtimeRoot, nil, "", "workspace", "attach", projectKey)
	if err != nil {
		t.Fatalf("workspace attach explicit project outside repo error = %v\n%s", err, output)
	}
	if !strings.Contains(output, "attached="+sessionName) {
		t.Fatalf("workspace attach explicit project output = %q, want attached session", output)
	}

	output, err = runWorkspaceIntegrationCommand(t, outsideDir, odinBinary, runtimeRoot, nil, "", "workspace", "status", projectKey, "--json")
	if err != nil {
		t.Fatalf("workspace status explicit project outside repo error = %v\n%s", err, output)
	}
	var liveStatus coreworkspace.Status
	if err := json.Unmarshal([]byte(output), &liveStatus); err != nil {
		t.Fatalf("Unmarshal(explicit project status) error = %v\n%s", err, output)
	}
	if liveStatus.ProjectKey != projectKey || liveStatus.State != coreworkspace.StateLive {
		t.Fatalf("explicit project live status = %+v, want live status for %s", liveStatus, projectKey)
	}
	if liveStatus.CurrentCwd != managedRepo || liveStatus.LaunchCwd != managedRepo {
		t.Fatalf("explicit project live status = %+v, want repo-root cwd for explicit launch", liveStatus)
	}
	if liveStatus.AttachedCount != 1 {
		t.Fatalf("explicit project attached_count = %d, want 1", liveStatus.AttachedCount)
	}

	output, err = runWorkspaceIntegrationCommand(t, outsideDir, odinBinary, runtimeRoot, nil, "", "workspace", "handoff", projectKey, "objective=Explicit project handoff")
	if err != nil {
		t.Fatalf("workspace handoff explicit project outside repo error = %v\n%s", err, output)
	}
	if !strings.Contains(output, "project="+projectKey) || !strings.Contains(output, "trigger=handoff") {
		t.Fatalf("workspace handoff explicit project output = %q, want handoff summary", output)
	}

	store := openRuntimeStore(t, runtimeRoot)
	defer store.Close()

	project, err := store.GetProjectByKey(ctx, projectKey)
	if err != nil {
		t.Fatalf("GetProjectByKey(%s) error = %v", projectKey, err)
	}

	var taskID int64
	if err := store.DB().QueryRowContext(ctx, `
		SELECT id
		FROM tasks
		WHERE project_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, project.ID).Scan(&taskID); err != nil {
		t.Fatalf("query latest explicit project task error = %v", err)
	}
	resumeState, err := (checkpoints.Service{Store: store}).LoadResumeState(ctx, project.ID, taskID)
	if err != nil {
		t.Fatalf("LoadResumeState(explicit project handoff) error = %v", err)
	}
	if resumeState.Objective != "Explicit project handoff" {
		t.Fatalf("explicit project handoff objective = %q, want explicit project handoff", resumeState.Objective)
	}
	if got := resumeState.ProjectContext.Facts["current_cwd"]; got != managedRepo {
		t.Fatalf("explicit project handoff current_cwd = %q, want repo root %q", got, managedRepo)
	}

	output, err = runWorkspaceIntegrationCommand(t, outsideDir, odinBinary, runtimeRoot, nil, "", "workspace", "stop", projectKey, "--force")
	if err != nil {
		t.Fatalf("workspace stop explicit project outside repo error = %v\n%s", err, output)
	}
	if !strings.Contains(output, "state=stopped") {
		t.Fatalf("workspace stop explicit project output = %q, want stopped state", output)
	}
}

func TestProjectCLIIntegrationExplicitEnrollAndHintedUpdate(t *testing.T) {
	sourceRepoRoot := projectRoot(t)
	odinRepoRoot := createWorkspaceIntegrationOdinRoot(t, sourceRepoRoot)
	odinBinary := buildWorkspaceIntegrationBinary(t, sourceRepoRoot, odinRepoRoot)
	runtimeRoot := t.TempDir()

	outsideDir := t.TempDir()
	initialRepo := createGitRepository(t)
	projectKey := "family-ops"

	output, err := runWorkspaceIntegrationCommand(t, outsideDir, odinBinary, runtimeRoot, nil, "", "project", "enroll", projectKey, "git_root="+initialRepo, "default_branch=main", "name=Family-Ops")
	if err != nil {
		t.Fatalf("project enroll explicit args error = %v\n%s", err, output)
	}
	for _, want := range []string{
		"project=" + projectKey,
		"class=local_git_project",
		"git_root=" + initialRepo,
		"default_branch=main",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("project enroll explicit args output = %q, want substring %q", output, want)
		}
	}

	output, err = runWorkspaceIntegrationCommand(t, outsideDir, odinBinary, runtimeRoot, nil, "", "project", "show", projectKey, "--json")
	if err != nil {
		t.Fatalf("project show after explicit enroll error = %v\n%s", err, output)
	}
	var enrolledProject workspaceProjectView
	if err := json.Unmarshal([]byte(output), &enrolledProject); err != nil {
		t.Fatalf("Unmarshal(project show explicit enroll) error = %v\n%s", err, output)
	}
	if enrolledProject.Key != projectKey || enrolledProject.Name != "Family-Ops" {
		t.Fatalf("enrolled project = %+v, want key %s name Family-Ops", enrolledProject, projectKey)
	}
	if enrolledProject.GitRoot != initialRepo || enrolledProject.DefaultBranch != "main" || !enrolledProject.WorkspaceEligible {
		t.Fatalf("enrolled project = %+v, want explicit repo root and main branch", enrolledProject)
	}

	movedRepo := createGitRepository(t)
	runGit(t, movedRepo, "checkout", "-b", "develop")

	output, err = runWorkspaceIntegrationCommand(t, movedRepo, odinBinary, runtimeRoot, nil, "", "project", "update", projectKey)
	if err != nil {
		t.Fatalf("project update hinted error = %v\n%s", err, output)
	}
	for _, want := range []string{
		"project=" + projectKey,
		"git_root=" + movedRepo,
		"default_branch=develop",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("project update hinted output = %q, want substring %q", output, want)
		}
	}

	output, err = runWorkspaceIntegrationCommand(t, outsideDir, odinBinary, runtimeRoot, nil, "", "project", "show", projectKey, "--json")
	if err != nil {
		t.Fatalf("project show after hinted update error = %v\n%s", err, output)
	}
	var updatedProject workspaceProjectView
	if err := json.Unmarshal([]byte(output), &updatedProject); err != nil {
		t.Fatalf("Unmarshal(project show hinted update) error = %v\n%s", err, output)
	}
	if updatedProject.Key != projectKey || updatedProject.Name != "Family-Ops" {
		t.Fatalf("updated project = %+v, want key %s name Family-Ops", updatedProject, projectKey)
	}
	if updatedProject.GitRoot != movedRepo || updatedProject.DefaultBranch != "develop" || !updatedProject.WorkspaceEligible {
		t.Fatalf("updated project = %+v, want moved repo with develop branch", updatedProject)
	}
}

func TestProjectCLIIntegrationHumanReadableListAndShow(t *testing.T) {
	ctx := context.Background()
	sourceRepoRoot := projectRoot(t)
	odinRepoRoot := createWorkspaceIntegrationOdinRoot(t, sourceRepoRoot)
	odinBinary := buildWorkspaceIntegrationBinary(t, sourceRepoRoot, odinRepoRoot)
	runtimeRoot := t.TempDir()

	managedRepo := createGitRepository(t)
	projectKey := filepath.Base(managedRepo)

	output, err := runWorkspaceIntegrationCommand(t, managedRepo, odinBinary, runtimeRoot, nil, "", "project", "enroll")
	if err != nil {
		t.Fatalf("project enroll error = %v\n%s", err, output)
	}

	store := openRuntimeStore(t, runtimeRoot)
	defer store.Close()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           projectKey,
		Name:          projectKey,
		Scope:         "project",
		GitRoot:       managedRepo,
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(%s) error = %v", projectKey, err)
	}
	if _, err := store.SetProjectTransition(ctx, sqlite.SetProjectTransitionParams{
		ProjectID:  project.ID,
		State:      "shadow",
		Controller: "legacy_odin",
		ChangedBy:  "integration-test",
		Notes:      "project read-path proof",
	}); err != nil {
		t.Fatalf("SetProjectTransition(%s) error = %v", projectKey, err)
	}

	output, err = runWorkspaceIntegrationCommand(t, odinRepoRoot, odinBinary, runtimeRoot, nil, "", "project", "list")
	if err != nil {
		t.Fatalf("project list text error = %v\n%s", err, output)
	}
	for _, want := range []string{
		"project=" + projectKey,
		"class=local_git_project",
		"transition=shadow",
		"workspace=eligible",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("project list output = %q, want substring %q", output, want)
		}
	}

	output, err = runWorkspaceIntegrationCommand(t, odinRepoRoot, odinBinary, runtimeRoot, nil, "", "project", "show", projectKey)
	if err != nil {
		t.Fatalf("project show text error = %v\n%s", err, output)
	}
	for _, want := range []string{
		"key=" + projectKey,
		"name=" + projectKey,
		"transition_state=shadow",
		"transition_controller=legacy_odin",
		"workspace_eligible=true",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("project show output = %q, want substring %q", output, want)
		}
	}

	if err := os.RemoveAll(filepath.Join(managedRepo, ".git")); err != nil {
		t.Fatalf("RemoveAll(.git) error = %v", err)
	}

	output, err = runWorkspaceIntegrationCommand(t, odinRepoRoot, odinBinary, runtimeRoot, nil, "", "project", "list")
	if err != nil {
		t.Fatalf("project list text after repo loss error = %v\n%s", err, output)
	}
	if !strings.Contains(output, "workspace=ineligible") || !strings.Contains(output, "workspace_reason=git_root is not a Git repository") {
		t.Fatalf("project list after repo loss output = %q, want ineligibility reason", output)
	}

	output, err = runWorkspaceIntegrationCommand(t, odinRepoRoot, odinBinary, runtimeRoot, nil, "", "project", "show", projectKey)
	if err != nil {
		t.Fatalf("project show text after repo loss error = %v\n%s", err, output)
	}
	if !strings.Contains(output, "workspace_eligible=false") || !strings.Contains(output, "workspace_reason=git_root is not a Git repository") {
		t.Fatalf("project show after repo loss output = %q, want ineligibility reason", output)
	}
}

func TestProjectWorkspaceCLIIntegrationDegradedRepoStatusAndHandoff(t *testing.T) {
	ctx := context.Background()
	sourceRepoRoot := projectRoot(t)
	odinRepoRoot := createWorkspaceIntegrationOdinRoot(t, sourceRepoRoot)
	odinBinary := buildWorkspaceIntegrationBinary(t, sourceRepoRoot, odinRepoRoot)
	runtimeRoot := t.TempDir()

	managedRepo := createGitRepository(t)
	projectKey := filepath.Base(managedRepo)
	subdir := filepath.Join(managedRepo, "docs")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("MkdirAll(subdir) error = %v", err)
	}

	installWorkspaceIntegrationStubs(t)

	output, err := runWorkspaceIntegrationCommand(t, managedRepo, odinBinary, runtimeRoot, nil, "", "project", "enroll")
	if err != nil {
		t.Fatalf("project enroll error = %v\n%s", err, output)
	}
	output, err = runWorkspaceIntegrationCommand(t, subdir, odinBinary, runtimeRoot, nil, "", "workspace", "start", "--no-attach")
	if err != nil {
		t.Fatalf("workspace start --no-attach error = %v\n%s", err, output)
	}

	if err := os.RemoveAll(filepath.Join(managedRepo, ".git")); err != nil {
		t.Fatalf("RemoveAll(.git) error = %v", err)
	}

	output, err = runWorkspaceIntegrationCommand(t, odinRepoRoot, odinBinary, runtimeRoot, nil, "", "workspace", "status", projectKey)
	if err != nil {
		t.Fatalf("workspace status text after repo loss error = %v\n%s", err, output)
	}
	for _, want := range []string{
		"project=" + projectKey,
		"state=live",
		"facts_source=last_known",
		"workspace_reason=git_root is not a Git repository",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("workspace status text after repo loss output = %q, want substring %q", output, want)
		}
	}

	output, err = runWorkspaceIntegrationCommand(t, odinRepoRoot, odinBinary, runtimeRoot, nil, "", "workspace", "status", projectKey, "--json")
	if err != nil {
		t.Fatalf("workspace status --json after repo loss error = %v\n%s", err, output)
	}
	var degradedStatus coreworkspace.Status
	if err := json.Unmarshal([]byte(output), &degradedStatus); err != nil {
		t.Fatalf("Unmarshal(workspace status after repo loss) error = %v\n%s", err, output)
	}
	if degradedStatus.ProjectKey != projectKey || degradedStatus.State != coreworkspace.StateLive {
		t.Fatalf("degraded workspace status = %+v, want live status for %s", degradedStatus, projectKey)
	}
	if degradedStatus.FactsSource != coreworkspace.FactsSourceLastKnown {
		t.Fatalf("degraded workspace facts_source = %q, want last_known", degradedStatus.FactsSource)
	}
	if degradedStatus.WorkspaceEligible {
		t.Fatalf("degraded workspace eligible = %t, want false", degradedStatus.WorkspaceEligible)
	}
	if degradedStatus.WorkspaceReason != "git_root is not a Git repository" {
		t.Fatalf("degraded workspace reason = %q, want git_root error", degradedStatus.WorkspaceReason)
	}
	if degradedStatus.Branch != "main" || degradedStatus.CurrentCwd != subdir {
		t.Fatalf("degraded workspace status = %+v, want branch main cwd %q", degradedStatus, subdir)
	}

	output, err = runWorkspaceIntegrationCommand(t, odinRepoRoot, odinBinary, runtimeRoot, nil, "", "workspace", "list", "--json")
	if err != nil {
		t.Fatalf("workspace list --json after repo loss error = %v\n%s", err, output)
	}
	var degradedList []coreworkspace.Status
	if err := json.Unmarshal([]byte(output), &degradedList); err != nil {
		t.Fatalf("Unmarshal(workspace list after repo loss) error = %v\n%s", err, output)
	}
	if len(degradedList) != 1 {
		t.Fatalf("workspace list after repo loss length = %d, want 1", len(degradedList))
	}
	if degradedList[0].ProjectKey != projectKey || degradedList[0].State != coreworkspace.StateLive {
		t.Fatalf("workspace list after repo loss[0] = %+v, want live status for %s", degradedList[0], projectKey)
	}
	if degradedList[0].FactsSource != coreworkspace.FactsSourceLastKnown {
		t.Fatalf("workspace list after repo loss facts_source = %q, want last_known", degradedList[0].FactsSource)
	}
	if degradedList[0].WorkspaceEligible {
		t.Fatalf("workspace list after repo loss eligible = %t, want false", degradedList[0].WorkspaceEligible)
	}
	if degradedList[0].WorkspaceReason != "git_root is not a Git repository" {
		t.Fatalf("workspace list after repo loss reason = %q, want git_root error", degradedList[0].WorkspaceReason)
	}

	output, err = runWorkspaceIntegrationCommand(t, odinRepoRoot, odinBinary, runtimeRoot, nil, "", "workspace", "stop", projectKey, "--force")
	if err != nil {
		t.Fatalf("workspace stop after repo loss error = %v\n%s", err, output)
	}
	for _, want := range []string{
		"state=stopped",
		"facts_source=last_known",
		"workspace_reason=git_root is not a Git repository",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("workspace stop after repo loss output = %q, want substring %q", output, want)
		}
	}

	output, err = runWorkspaceIntegrationCommand(t, odinRepoRoot, odinBinary, runtimeRoot, nil, "", "workspace", "handoff", projectKey, "objective=Repo loss handoff")
	if err != nil {
		t.Fatalf("workspace handoff after repo loss error = %v\n%s", err, output)
	}
	if !strings.Contains(output, "trigger=handoff") || !strings.Contains(output, "project="+projectKey) {
		t.Fatalf("workspace handoff after repo loss output = %q, want handoff summary", output)
	}

	store := openRuntimeStore(t, runtimeRoot)
	defer store.Close()

	project, err := store.GetProjectByKey(ctx, projectKey)
	if err != nil {
		t.Fatalf("GetProjectByKey(%s) error = %v", projectKey, err)
	}

	var taskID int64
	if err := store.DB().QueryRowContext(ctx, `
		SELECT id
		FROM tasks
		WHERE project_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, project.ID).Scan(&taskID); err != nil {
		t.Fatalf("query latest repo-loss handoff task error = %v", err)
	}
	resumeState, err := (checkpoints.Service{Store: store}).LoadResumeState(ctx, project.ID, taskID)
	if err != nil {
		t.Fatalf("LoadResumeState(repo loss handoff) error = %v", err)
	}
	if resumeState.Objective != "Repo loss handoff" {
		t.Fatalf("resume objective = %q, want repo loss handoff", resumeState.Objective)
	}
	if got := resumeState.ProjectContext.Facts["facts_source"]; got != string(coreworkspace.FactsSourceLastKnown) {
		t.Fatalf("resume facts_source = %q, want %q", got, coreworkspace.FactsSourceLastKnown)
	}
	if got := resumeState.ProjectContext.Facts["workspace_state"]; got != string(coreworkspace.StateStopped) {
		t.Fatalf("resume workspace_state = %q, want %q", got, coreworkspace.StateStopped)
	}
	if got := resumeState.ProjectContext.Facts["branch"]; got != "main" {
		t.Fatalf("resume branch = %q, want main", got)
	}
	if got := resumeState.ProjectContext.Facts["current_cwd"]; got != subdir {
		t.Fatalf("resume current_cwd = %q, want %q", got, subdir)
	}
}

type workspaceProjectView struct {
	Key               string `json:"key"`
	Name              string `json:"name"`
	GitRoot           string `json:"git_root"`
	DefaultBranch     string `json:"default_branch"`
	WorkspaceEligible bool   `json:"workspace_eligible"`
}

func createWorkspaceIntegrationOdinRoot(t *testing.T, sourceRepoRoot string) string {
	t.Helper()

	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(configDir) error = %v", err)
	}
	copyWorkspaceIntegrationFile(t, filepath.Join(sourceRepoRoot, "config", "odin.yaml"), filepath.Join(configDir, "odin.yaml"))
	copyWorkspaceIntegrationFile(t, filepath.Join(sourceRepoRoot, "config", "executors.yaml"), filepath.Join(configDir, "executors.yaml"))
	if err := os.WriteFile(filepath.Join(configDir, "projects.yaml"), []byte("version: 1\nprojects: []\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(projects.yaml) error = %v", err)
	}
	copyWorkspaceIntegrationTree(t, filepath.Join(sourceRepoRoot, "registry"), filepath.Join(root, "registry"))
	return root
}

func buildWorkspaceIntegrationBinary(t *testing.T, sourceRepoRoot string, runtimeRepoRoot string) string {
	t.Helper()

	binaryPath := filepath.Join(runtimeRepoRoot, "bin", "odin")
	buildOdinBinaryAt(t, sourceRepoRoot, binaryPath)
	return binaryPath
}

func runWorkspaceIntegrationCommand(t *testing.T, workDir string, binaryPath string, runtimeRoot string, extraEnv map[string]string, stdin string, args ...string) (string, error) {
	t.Helper()
	return runOdinCommandInDir(t, workDir, binaryPath, runtimeRoot, extraEnv, stdin, args...)
}

func runWorkspaceIntegrationPTYCommand(t *testing.T, workDir string, binaryPath string, runtimeRoot string, extraEnv map[string]string, stdin string, args ...string) (string, error) {
	t.Helper()
	return runOdinPTYCommandInDir(t, workDir, binaryPath, runtimeRoot, extraEnv, stdin, args...)
}

func installWorkspaceIntegrationStubs(t *testing.T) string {
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

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ODIN_TEST_TMUX_DIR", stateDir)
	return stateDir
}

func decodeWorkspaceDoctorCheck(t *testing.T, output string) healthsvc.Check {
	t.Helper()

	var report healthsvc.Report
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("Unmarshal(doctor report) error = %v\n%s", err, output)
	}
	for _, check := range report.Checks {
		if check.Name == "workspace_prerequisites" {
			return check
		}
	}
	t.Fatalf("doctor report missing workspace_prerequisites check\n%s", output)
	return healthsvc.Check{}
}

func readWorkspaceStubEnv(t *testing.T, stateDir string, sessionName string, key string) string {
	t.Helper()
	return strings.TrimSpace(readWorkspaceStubValue(t, filepath.Join(stateDir, sessionName, "env"), "", key))
}

func readWorkspaceStubValue(t *testing.T, base string, sessionName string, name string) string {
	t.Helper()

	path := base
	if sessionName != "" {
		path = filepath.Join(path, sessionName)
	}
	if name != "" {
		path = filepath.Join(path, name)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	return strings.TrimSpace(string(content))
}

func copyWorkspaceIntegrationTree(t *testing.T, src string, dst string) {
	t.Helper()

	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("ReadDir(%s) error = %v", src, err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", dst, err)
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			copyWorkspaceIntegrationTree(t, srcPath, dstPath)
			continue
		}
		copyWorkspaceIntegrationFile(t, srcPath, dstPath)
	}
}

func copyWorkspaceIntegrationFile(t *testing.T, src string, dst string) {
	t.Helper()

	info, err := os.Stat(src)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", src, err)
	}
	input, err := os.Open(src)
	if err != nil {
		t.Fatalf("Open(%s) error = %v", src, err)
	}
	defer input.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", filepath.Dir(dst), err)
	}
	output, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		t.Fatalf("OpenFile(%s) error = %v", dst, err)
	}
	defer output.Close()

	if _, err := io.Copy(output, input); err != nil {
		t.Fatalf("Copy(%s -> %s) error = %v", src, dst, err)
	}
}
