package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"odin-os/internal/app/bootstrap"
	"odin-os/internal/core/projects"
	"odin-os/internal/core/workspaces"
	memoryroot "odin-os/internal/memory"
	memorycompanions "odin-os/internal/memory/companions"
	memoryprojects "odin-os/internal/memory/projects"
	memoryworkspaces "odin-os/internal/memory/workspaces"
	"odin-os/internal/store/sqlite"
)

func TestWorkspaceRefactorAcceptance(t *testing.T) {
	ctx := context.Background()
	repoRoot := projectRoot(t)
	odinBinary := buildOdinBinary(t, repoRoot)

	t.Run("workspace exists after bootstrap", func(t *testing.T) {
		runtimeRoot := t.TempDir()

		app, err := bootstrap.Load(ctx, repoRoot, runtimeRoot)
		if err != nil {
			t.Fatalf("bootstrap.Load() error = %v", err)
		}
		defer app.Store.Close()

		workspace, err := app.Store.GetWorkspaceByKey(ctx, workspaces.DefaultWorkspaceKey)
		if err != nil {
			t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
		}
		if workspace.DefaultCompanionKey != workspaces.DefaultWorkspaceCompanionKey {
			t.Fatalf("GetWorkspaceByKey(default).DefaultCompanionKey = %q, want %q", workspace.DefaultCompanionKey, workspaces.DefaultWorkspaceCompanionKey)
		}

		companion, err := app.Store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
		if err != nil {
			t.Fatalf("GetCompanionByKey(default) error = %v", err)
		}
		if companion.Kind != workspaces.DefaultWorkspaceCompanionKind {
			t.Fatalf("GetCompanionByKey(default).Kind = %q, want %q", companion.Kind, workspaces.DefaultWorkspaceCompanionKind)
		}
	})

	t.Run("managed project becomes an initiative", func(t *testing.T) {
		runtimeRoot := t.TempDir()
		app, err := bootstrap.Load(ctx, repoRoot, runtimeRoot)
		if err != nil {
			t.Fatalf("bootstrap.Load() error = %v", err)
		}
		defer app.Store.Close()

		repoPath := createGitRepository(t)
		projectService := projects.Service{Store: app.Store}
		project, err := projectService.RegisterManagedProject(ctx, projects.Manifest{
			Key:           "marcus-admin",
			Name:          "Marcus Admin",
			ProjectClass:  projects.ProjectClassLocalGit,
			GitRoot:       repoPath,
			DefaultBranch: "main",
			SourcePath:    "config/projects.yaml",
		})
		if err != nil {
			t.Fatalf("RegisterManagedProject() error = %v", err)
		}

		workspace, err := app.Store.GetWorkspaceByKey(ctx, workspaces.DefaultWorkspaceKey)
		if err != nil {
			t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
		}
		initiative, err := app.Store.GetInitiativeByKey(ctx, workspace.ID, project.Key)
		if err != nil {
			t.Fatalf("GetInitiativeByKey(marcus-admin) error = %v", err)
		}
		if initiative.Kind != "managed_project" {
			t.Fatalf("GetInitiativeByKey(marcus-admin).Kind = %q, want managed_project", initiative.Kind)
		}
		if initiative.LinkedProjectID == nil || *initiative.LinkedProjectID != project.ID {
			t.Fatalf("GetInitiativeByKey(marcus-admin).LinkedProjectID = %v, want %d", initiative.LinkedProjectID, project.ID)
		}
	})

	t.Run("initiative lifecycle commands manage non-project work", func(t *testing.T) {
		runtimeRoot := t.TempDir()

		createOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "initiative", "create", "--kind", "routine", "--key", "life-admin", "--title", "Life Admin")
		if err != nil {
			t.Fatalf("runOdinCommand(initiative create) error = %v\n%s", err, createOutput)
		}
		if !strings.Contains(createOutput, "life-admin") {
			t.Fatalf("initiative create output = %q, want life-admin", createOutput)
		}

		listOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "initiative", "list", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(initiative list --json) error = %v\n%s", err, listOutput)
		}
		if !strings.Contains(listOutput, `"key": "life-admin"`) {
			t.Fatalf("initiative list output = %q, want life-admin entry", listOutput)
		}
		if !strings.Contains(listOutput, `"kind": "routine"`) {
			t.Fatalf("initiative list output = %q, want routine kind", listOutput)
		}
	})

	t.Run("follow-up add tolerates unrelated project diagnostics", func(t *testing.T) {
		runtimeRoot := t.TempDir()
		overlayPath := filepath.Join(t.TempDir(), "projects.overlay.yaml")
		overlay := []byte(`version: 1

projects:
  - key: diagnostics-a
    name: Diagnostics A
    project_class: system_project
    system_project: true
    git_root: ..
    default_branch: main
  - key: diagnostics-a
    name: Diagnostics A Duplicate
    project_class: system_project
    system_project: true
    git_root: ..
    default_branch: main
`)
		if err := os.WriteFile(overlayPath, overlay, 0o644); err != nil {
			t.Fatalf("WriteFile(projects overlay) error = %v", err)
		}
		env := map[string]string{"ODIN_PROJECTS_OVERLAY": overlayPath}

		createOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, env, "", "initiative", "create", "--kind", "routine", "--key", "life-admin", "--title", "Life Admin")
		if err != nil {
			t.Fatalf("runOdinCommand(initiative create) error = %v\n%s", err, createOutput)
		}

		addOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, env, "", "followup", "add", "--initiative", "life-admin", "--title", "Review mail", "--cadence", "weekly")
		if err != nil {
			t.Fatalf("runOdinCommand(followup add) error = %v\n%s", err, addOutput)
		}
		if !strings.Contains(addOutput, "created follow-up") {
			t.Fatalf("followup add output = %q, want created follow-up", addOutput)
		}

		listOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, env, "", "followup", "list", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(followup list --json) error = %v\n%s", err, listOutput)
		}
		if !strings.Contains(listOutput, `"initiative_key": "life-admin"`) {
			t.Fatalf("followup list output = %q, want life-admin entry", listOutput)
		}
		if !strings.Contains(listOutput, `"status": "active"`) {
			t.Fatalf("followup list output = %q, want active status", listOutput)
		}
	})

	t.Run("follow-up lifecycle commands complete and snooze through the root surface", func(t *testing.T) {
		runtimeRoot := t.TempDir()

		createOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "initiative", "create", "--kind", "routine", "--key", "life-admin", "--title", "Life Admin")
		if err != nil {
			t.Fatalf("runOdinCommand(initiative create) error = %v\n%s", err, createOutput)
		}

		recurringAddOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "followup", "add", "--initiative", "life-admin", "--title", "Review mail", "--cadence", "weekly")
		if err != nil {
			t.Fatalf("runOdinCommand(followup add recurring) error = %v\n%s", err, recurringAddOutput)
		}
		if !strings.Contains(recurringAddOutput, "created follow-up") {
			t.Fatalf("followup add recurring output = %q, want created follow-up", recurringAddOutput)
		}

		recurringListOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "followup", "list", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(followup list recurring) error = %v\n%s", err, recurringListOutput)
		}
		recurringID := followUpIDFromJSON(t, recurringListOutput, "Review mail")

		completeOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "followup", "complete", strconv.FormatInt(recurringID, 10))
		if err != nil {
			t.Fatalf("runOdinCommand(followup complete) error = %v\n%s", err, completeOutput)
		}
		if !strings.Contains(completeOutput, "completed follow-up") {
			t.Fatalf("followup complete output = %q, want completed follow-up", completeOutput)
		}

		snoozeAddOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "followup", "add", "--initiative", "life-admin", "--title", "Archive receipts", "--cadence", "once")
		if err != nil {
			t.Fatalf("runOdinCommand(followup add one-time) error = %v\n%s", err, snoozeAddOutput)
		}
		if !strings.Contains(snoozeAddOutput, "created follow-up") {
			t.Fatalf("followup add one-time output = %q, want created follow-up", snoozeAddOutput)
		}

		snoozeListOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "followup", "list", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(followup list one-time) error = %v\n%s", err, snoozeListOutput)
		}
		snoozeID := followUpIDFromJSON(t, snoozeListOutput, "Archive receipts")
		until := time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
		snoozeOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "followup", "snooze", strconv.FormatInt(snoozeID, 10), "--until", until)
		if err != nil {
			t.Fatalf("runOdinCommand(followup snooze) error = %v\n%s", err, snoozeOutput)
		}
		if !strings.Contains(snoozeOutput, "snoozed follow-up") {
			t.Fatalf("followup snooze output = %q, want snoozed follow-up", snoozeOutput)
		}
	})

	t.Run("follow-up serve materializes blocked work items visible in jobs", func(t *testing.T) {
		runtimeRoot := t.TempDir()

		createOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "initiative", "create", "--kind", "routine", "--key", "life-admin", "--title", "Life Admin")
		if err != nil {
			t.Fatalf("runOdinCommand(initiative create) error = %v\n%s", err, createOutput)
		}
		addOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "followup", "add", "--initiative", "life-admin", "--title", "Review mail", "--cadence", "once")
		if err != nil {
			t.Fatalf("runOdinCommand(followup add) error = %v\n%s", err, addOutput)
		}
		if !strings.Contains(addOutput, "created follow-up") {
			t.Fatalf("followup add output = %q, want created follow-up", addOutput)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, odinBinary, "serve")
		cmd.Dir = repoRoot
		cmd.Env = append(os.Environ(),
			"ODIN_ROOT="+runtimeRoot,
			"ODIN_HTTP_ADDR=127.0.0.1:0",
		)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatalf("StdoutPipe() error = %v", err)
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			t.Fatalf("StderrPipe() error = %v", err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatalf("cmd.Start() error = %v", err)
		}
		stopped := false
		defer func() {
			if stopped {
				return
			}
			_ = cmd.Process.Signal(os.Interrupt)
			_ = cmd.Wait()
		}()

		_ = waitForServeAddress(t, stdout, stderr)

		deadline := time.After(3 * time.Second)
		for {
			jobsOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "jobs", "--json")
			if err != nil {
				t.Fatalf("runOdinCommand(jobs --json) error = %v\n%s", err, jobsOutput)
			}

			var jobsView struct {
				Jobs []struct {
					ProjectKey string `json:"project_key"`
					TaskKey    string `json:"task_key"`
					Status     string `json:"status"`
				} `json:"jobs"`
			}
			if err := json.Unmarshal([]byte(jobsOutput), &jobsView); err != nil {
				t.Fatalf("json.Unmarshal(jobs output) error = %v\n%s", err, jobsOutput)
			}
			if len(jobsView.Jobs) == 1 && jobsView.Jobs[0].Status == "blocked" {
				break
			}

			select {
			case <-deadline:
				t.Fatalf("jobs output = %s, want one blocked follow-up work item", jobsOutput)
			case <-time.After(100 * time.Millisecond):
			}
		}

		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatalf("Signal(os.Interrupt) error = %v", err)
		}
		if err := cmd.Wait(); err != nil {
			t.Fatalf("cmd.Wait() error = %v", err)
		}
		stopped = true
	})

	t.Run("companion lifecycle commands manage durable companion state", func(t *testing.T) {
		runtimeRoot := t.TempDir()

		createOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "companion", "create", "--kind", "advisor", "--key", "finance", "--title", "Finance Advisor")
		if err != nil {
			t.Fatalf("runOdinCommand(companion create) error = %v\n%s", err, createOutput)
		}
		if !strings.Contains(createOutput, "finance") {
			t.Fatalf("companion create output = %q, want finance", createOutput)
		}

		listOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "companion", "list", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(companion list --json) error = %v\n%s", err, listOutput)
		}
		if !strings.Contains(listOutput, `"key": "finance"`) {
			t.Fatalf("companion list output = %q, want finance entry", listOutput)
		}
		if !strings.Contains(listOutput, `"kind": "advisor"`) {
			t.Fatalf("companion list output = %q, want advisor kind", listOutput)
		}
	})

	t.Run("companion create does not wipe durable companion fields on rerun", func(t *testing.T) {
		runtimeRoot := t.TempDir()

		createOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "companion", "create", "--kind", "advisor", "--key", "finance", "--title", "Finance Advisor")
		if err != nil {
			t.Fatalf("runOdinCommand(companion create seed) error = %v\n%s", err, createOutput)
		}

		store := openRuntimeStore(t, runtimeRoot)
		defer store.Close()

		workspace, err := store.GetWorkspaceByKey(ctx, workspaces.DefaultWorkspaceKey)
		if err != nil {
			t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
		}
		if _, err := store.DB().ExecContext(ctx, `
			UPDATE companions
			SET charter = ?, status = ?, initiative_scope_json = ?, tool_policy_json = ?, memory_policy_json = ?, planning_policy_json = ?
			WHERE workspace_id = ? AND key = ?
		`, "Keep finance decisions clear.", "disabled", `{"initiatives":["finance"]}`, `{"allow":["budget_review"]}`, `{"mode":"project"}`, `{"mode":"guided"}`, workspace.ID, "finance"); err != nil {
			t.Fatalf("seed companion customization error = %v", err)
		}

		rerunOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "companion", "create", "--kind", "advisor", "--key", "finance", "--title", "Finance Advisor")
		if err != nil {
			t.Fatalf("runOdinCommand(companion create rerun) error = %v\n%s", err, rerunOutput)
		}

		reloaded, err := store.GetCompanionByKey(ctx, workspace.ID, "finance")
		if err != nil {
			t.Fatalf("GetCompanionByKey(finance) error = %v", err)
		}
		if reloaded.Charter != "Keep finance decisions clear." {
			t.Fatalf("reloaded.Charter = %q, want preserved charter", reloaded.Charter)
		}
		if reloaded.Status != "disabled" {
			t.Fatalf("reloaded.Status = %q, want preserved status", reloaded.Status)
		}
		if reloaded.ToolPolicyJSON != `{"allow":["budget_review"]}` {
			t.Fatalf("reloaded.ToolPolicyJSON = %q, want preserved policy", reloaded.ToolPolicyJSON)
		}
		if reloaded.MemoryPolicyJSON != `{"mode":"project"}` {
			t.Fatalf("reloaded.MemoryPolicyJSON = %q, want preserved policy", reloaded.MemoryPolicyJSON)
		}
		if reloaded.PlanningPolicyJSON != `{"mode":"guided"}` {
			t.Fatalf("reloaded.PlanningPolicyJSON = %q, want preserved policy", reloaded.PlanningPolicyJSON)
		}
	})

	t.Run("a companion can own a work item", func(t *testing.T) {
		runtimeRoot := t.TempDir()
		output, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "/project odin-core\n/mode act\nworkspace acceptance work item\n/quit\n", "repl")
		if err != nil {
			t.Fatalf("runOdinCommand(interactive act) error = %v\n%s", err, output)
		}
		if !strings.Contains(output, "created task") {
			t.Fatalf("interactive output = %q, want created task", output)
		}

		store := openRuntimeStore(t, runtimeRoot)
		defer store.Close()

		workspace, err := store.GetWorkspaceByKey(ctx, workspaces.DefaultWorkspaceKey)
		if err != nil {
			t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
		}
		companion, err := store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
		if err != nil {
			t.Fatalf("GetCompanionByKey(default) error = %v", err)
		}

		var taskID int64
		if err := store.DB().QueryRowContext(ctx, `SELECT id FROM tasks ORDER BY id DESC LIMIT 1`).Scan(&taskID); err != nil {
			t.Fatalf("latest task query error = %v", err)
		}
		task, err := store.GetTask(ctx, taskID)
		if err != nil {
			t.Fatalf("GetTask(latest) error = %v", err)
		}
		if task.WorkspaceID == nil || *task.WorkspaceID != workspace.ID {
			t.Fatalf("GetTask(latest).WorkspaceID = %v, want %d", task.WorkspaceID, workspace.ID)
		}
		if task.InitiativeID == nil {
			t.Fatalf("GetTask(latest).InitiativeID = nil, want linked initiative")
		}
		if task.CompanionID == nil || *task.CompanionID != companion.ID {
			t.Fatalf("GetTask(latest).CompanionID = %v, want %d", task.CompanionID, companion.ID)
		}
	})

	t.Run("memory writes remain scoped", func(t *testing.T) {
		runtimeRoot := t.TempDir()
		app, err := bootstrap.Load(ctx, repoRoot, runtimeRoot)
		if err != nil {
			t.Fatalf("bootstrap.Load() error = %v", err)
		}
		defer app.Store.Close()

		workspace, err := workspaces.Service{Store: app.Store}.BootstrapDefaultWorkspace(ctx)
		if err != nil {
			t.Fatalf("BootstrapDefaultWorkspace() error = %v", err)
		}
		companion, err := app.Store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
		if err != nil {
			t.Fatalf("GetCompanionByKey(default) error = %v", err)
		}

		repoPath := createGitRepository(t)
		project, err := (projects.Service{Store: app.Store}).RegisterManagedProject(ctx, projects.Manifest{
			Key:           "life-admin",
			Name:          "Life Admin",
			ProjectClass:  projects.ProjectClassLocalGit,
			GitRoot:       repoPath,
			DefaultBranch: "main",
			SourcePath:    "config/projects.yaml",
		})
		if err != nil {
			t.Fatalf("RegisterManagedProject() error = %v", err)
		}
		initiative, err := app.Store.GetInitiativeByKey(ctx, workspace.ID, project.Key)
		if err != nil {
			t.Fatalf("GetInitiativeByKey(life-admin) error = %v", err)
		}

		workspaceMemory := memoryworkspaces.Service{Store: app.Store}
		companionMemory := memorycompanions.Service{Store: app.Store}
		initiativeMemory := memoryprojects.Service{Store: app.Store}

		if _, err := workspaceMemory.Record(ctx, workspace.ID, memoryroot.WriteInput{
			EntryType:       memoryroot.EntryTypeNote,
			VisibilityScope: memoryroot.VisibilityWorkspace,
			RetentionClass:  memoryroot.RetentionDurable,
			Summary:         "workspace preference",
			Content:         "Marcus prefers concise morning briefings.",
		}); err != nil {
			t.Fatalf("workspace memory Record() error = %v", err)
		}
		if _, err := companionMemory.Record(ctx, workspace.ID, companion.ID, memoryroot.WriteInput{
			EntryType:       memoryroot.EntryTypeNote,
			VisibilityScope: memoryroot.VisibilityCompanion,
			RetentionClass:  memoryroot.RetentionWorking,
			Summary:         "companion preference",
			Content:         "Operator companion owns life admin follow-ups.",
		}); err != nil {
			t.Fatalf("companion memory Record() error = %v", err)
		}
		if _, err := initiativeMemory.Record(ctx, workspace.ID, initiative.ID, memoryroot.WriteInput{
			EntryType:       memoryroot.EntryTypeSummary,
			VisibilityScope: memoryroot.VisibilityInitiative,
			RetentionClass:  memoryroot.RetentionDurable,
			Summary:         "initiative summary",
			Content:         "Life Admin tracks recurring paperwork.",
		}); err != nil {
			t.Fatalf("initiative memory Record() error = %v", err)
		}

		workspaceEntries, err := workspaceMemory.Recall(ctx, workspace.ID, 10)
		if err != nil {
			t.Fatalf("workspace memory Recall() error = %v", err)
		}
		if len(workspaceEntries) != 1 || workspaceEntries[0].VisibilityScope != string(memoryroot.VisibilityWorkspace) {
			t.Fatalf("workspace memory Recall() = %+v, want only workspace-visible entry", workspaceEntries)
		}

		companionEntries, err := companionMemory.Recall(ctx, workspace.ID, companion.ID, 10)
		if err != nil {
			t.Fatalf("companion memory Recall() error = %v", err)
		}
		if !containsMemoryContent(companionEntries, "Operator companion owns life admin follow-ups.") {
			t.Fatalf("companion memory Recall() = %+v, want companion entry", companionEntries)
		}
		if !containsMemoryContent(companionEntries, "Marcus prefers concise morning briefings.") {
			t.Fatalf("companion memory Recall() = %+v, want workspace fallback", companionEntries)
		}
		if containsMemoryContent(companionEntries, "Life Admin tracks recurring paperwork.") {
			t.Fatalf("companion memory Recall() leaked initiative entry: %+v", companionEntries)
		}

		initiativeEntries, err := initiativeMemory.Recall(ctx, workspace.ID, initiative.ID, 10)
		if err != nil {
			t.Fatalf("initiative memory Recall() error = %v", err)
		}
		if !containsMemoryContent(initiativeEntries, "Life Admin tracks recurring paperwork.") {
			t.Fatalf("initiative memory Recall() = %+v, want initiative entry", initiativeEntries)
		}
		if !containsMemoryContent(initiativeEntries, "Marcus prefers concise morning briefings.") {
			t.Fatalf("initiative memory Recall() = %+v, want workspace fallback", initiativeEntries)
		}
		if containsMemoryContent(initiativeEntries, "Operator companion owns life admin follow-ups.") {
			t.Fatalf("initiative memory Recall() leaked companion entry: %+v", initiativeEntries)
		}
	})

	t.Run("project governance still blocks unsafe mutation", func(t *testing.T) {
		runtimeRoot := t.TempDir()
		app, err := bootstrap.Load(ctx, repoRoot, runtimeRoot)
		if err != nil {
			t.Fatalf("bootstrap.Load() error = %v", err)
		}
		defer app.Store.Close()

		systemManifest, ok := app.Registry.SystemProject()
		if !ok {
			t.Fatal("SystemProject() missing odin-core")
		}
		project, err := (projects.Service{Store: app.Store}).RegisterManagedProject(ctx, systemManifest)
		if err != nil {
			t.Fatalf("RegisterManagedProject(odin-core) error = %v", err)
		}

		err = (projects.Service{Store: app.Store}).AuthorizeExecutionMutation(ctx, projects.ExecutionAuthorizationInput{
			ProjectID:   project.ID,
			Manifest:    systemManifest,
			Actor:       projects.TransitionControllerOdinOS,
			ActionClass: projects.ActionClassIsolatedMutation,
			ActionKey:   "apply_patch",
		})
		if err == nil {
			t.Fatal("AuthorizeExecutionMutation() error = nil, want system project mutation rejection")
		}
		if !strings.Contains(err.Error(), "requires explicit approval") {
			t.Fatalf("AuthorizeExecutionMutation() error = %v, want explicit approval rejection", err)
		}
	})
}

func containsMemoryContent(entries []sqlite.MemoryEntry, target string) bool {
	for _, entry := range entries {
		if entry.Content == target {
			return true
		}
	}
	return false
}

func followUpIDFromJSON(t *testing.T, payload string, title string) int64 {
	t.Helper()

	var parsed struct {
		Obligations []struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
		} `json:"obligations"`
	}
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		t.Fatalf("json.Unmarshal(follow-up list) error = %v\npayload=%s", err, payload)
	}
	for _, obligation := range parsed.Obligations {
		if obligation.Title == title {
			return obligation.ID
		}
	}
	t.Fatalf("follow-up list payload missing title %q: %s", title, payload)
	return 0
}
