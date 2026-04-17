package integration_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestMemoryMigrationBootstrapScriptBackfillsScopedOwnership(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repoRoot := projectRoot(t)
	fixtureRepo := createWorkspaceMemoryBootstrapRepoRoot(t)
	runtimeRoot := t.TempDir()

	store := openWorkspaceMemoryBootstrapStore(t, runtimeRoot)
	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "local-demo",
		Name:          "Local Demo",
		Scope:         "project",
		GitRoot:       filepath.Join(fixtureRepo, "local-demo"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(local-demo) error = %v", err)
	}

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "bootstrap-memory",
		Title:       "Bootstrap memory",
		ActionKey:   "docs_audit_note",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "test",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	transcript, err := store.RecordConversationTranscript(ctx, sqlite.RecordConversationTranscriptParams{
		ProjectID:   &project.ID,
		TaskID:      &task.ID,
		RunID:       &run.ID,
		Scope:       "project",
		ScopeKey:    project.Key,
		Mode:        "plan",
		Prompt:      "Capture project state",
		Response:    "Recorded current project memory",
		ToolSummary: "none",
		Executor:    "codex",
	})
	if err != nil {
		t.Fatalf("RecordConversationTranscript(project) error = %v", err)
	}

	globalTranscript, err := store.RecordConversationTranscript(ctx, sqlite.RecordConversationTranscriptParams{
		Scope:       "global",
		ScopeKey:    "global",
		Mode:        "assist",
		Prompt:      "Remember a workspace preference",
		Response:    "Preference captured",
		ToolSummary: "none",
		Executor:    "codex",
	})
	if err != nil {
		t.Fatalf("RecordConversationTranscript(global) error = %v", err)
	}

	if _, err := store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		Scope:      "global",
		ScopeKey:   "global",
		MemoryType: "user_preference",
		Summary:    "Prefer concise replies.",
		DetailsJSON: `{"source":"test"}`,
	}); err != nil {
		t.Fatalf("RecordMemorySummary(global) error = %v", err)
	}

	if _, err := store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		ProjectID:          &project.ID,
		SourceTranscriptID: &transcript.ID,
		TaskID:             &task.ID,
		RunID:              &run.ID,
		Scope:              "project",
		ScopeKey:           project.Key,
		MemoryType:         "episode",
		Summary:            "Captured project bootstrap state.",
		DetailsJSON:        `{"source":"test"}`,
	}); err != nil {
		t.Fatalf("RecordMemorySummary(project) error = %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("store.Close() error = %v", err)
	}

	cmd := exec.Command("go", "run", "./scripts/migrate/bootstrap_workspace.go", "-repo-root", fixtureRepo, "-runtime-root", runtimeRoot)
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run ./scripts/migrate/bootstrap_workspace.go error = %v\n%s", err, string(output))
	}

	reopened := openWorkspaceMemoryBootstrapStore(t, runtimeRoot)
	defer reopened.Close()

	workspace, err := reopened.GetWorkspaceByKey(ctx, "marcus")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(marcus) error = %v", err)
	}

	initiatives, err := reopened.ListInitiatives(ctx, sqlite.ListInitiativesParams{WorkspaceID: workspace.ID})
	if err != nil {
		t.Fatalf("ListInitiatives() error = %v", err)
	}
	if len(initiatives) != 1 {
		t.Fatalf("initiatives len = %d, want 1", len(initiatives))
	}
	initiative := initiatives[0]
	if initiative.LinkedProjectID == nil || *initiative.LinkedProjectID != project.ID {
		t.Fatalf("initiative.LinkedProjectID = %v, want %d", initiative.LinkedProjectID, project.ID)
	}

	workspaceID := workspace.ID
	workspaceSummaries, err := reopened.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		WorkspaceID: &workspaceID,
		Scope:       "workspace",
		ScopeKey:    workspace.Key,
		MemoryType:  "user_preference",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries(workspace) error = %v", err)
	}
	if len(workspaceSummaries) != 1 {
		t.Fatalf("workspaceSummaries len = %d, want 1", len(workspaceSummaries))
	}
	workspaceSummary := workspaceSummaries[0]
	if workspaceSummary.ProjectID != nil {
		t.Fatalf("workspaceSummary.ProjectID = %v, want nil", workspaceSummary.ProjectID)
	}
	if workspaceSummary.WorkspaceID == nil || *workspaceSummary.WorkspaceID != workspace.ID {
		t.Fatalf("workspaceSummary.WorkspaceID = %v, want %d", workspaceSummary.WorkspaceID, workspace.ID)
	}
	if workspaceSummary.VisibilityScope != "workspace" {
		t.Fatalf("workspaceSummary.VisibilityScope = %q, want workspace", workspaceSummary.VisibilityScope)
	}
	if workspaceSummary.RetentionClass != "durable" {
		t.Fatalf("workspaceSummary.RetentionClass = %q, want durable", workspaceSummary.RetentionClass)
	}

	workspaceTranscripts, err := reopened.ListConversationTranscripts(ctx, sqlite.ListConversationTranscriptsParams{
		WorkspaceID: &workspaceID,
		Scope:       "workspace",
		ScopeKey:    workspace.Key,
		Mode:        "assist",
	})
	if err != nil {
		t.Fatalf("ListConversationTranscripts(workspace) error = %v", err)
	}
	if len(workspaceTranscripts) != 1 {
		t.Fatalf("workspaceTranscripts len = %d, want 1", len(workspaceTranscripts))
	}
	workspaceTranscript := workspaceTranscripts[0]
	if workspaceTranscript.ID != globalTranscript.ID {
		t.Fatalf("workspaceTranscript.ID = %d, want %d", workspaceTranscript.ID, globalTranscript.ID)
	}
	if workspaceTranscript.WorkspaceID == nil || *workspaceTranscript.WorkspaceID != workspace.ID {
		t.Fatalf("workspaceTranscript.WorkspaceID = %v, want %d", workspaceTranscript.WorkspaceID, workspace.ID)
	}
	if workspaceTranscript.Scope != "workspace" {
		t.Fatalf("workspaceTranscript.Scope = %q, want workspace", workspaceTranscript.Scope)
	}
	if workspaceTranscript.ScopeKey != workspace.Key {
		t.Fatalf("workspaceTranscript.ScopeKey = %q, want %q", workspaceTranscript.ScopeKey, workspace.Key)
	}

	projectID := project.ID
	taskID := task.ID
	runID := run.ID
	projectTranscripts, err := reopened.ListConversationTranscripts(ctx, sqlite.ListConversationTranscriptsParams{
		ProjectID: &projectID,
		TaskID:    &taskID,
		RunID:     &runID,
		Scope:     "project",
		ScopeKey:  project.Key,
	})
	if err != nil {
		t.Fatalf("ListConversationTranscripts(project) error = %v", err)
	}
	if len(projectTranscripts) != 1 {
		t.Fatalf("projectTranscripts len = %d, want 1", len(projectTranscripts))
	}
	projectTranscript := projectTranscripts[0]
	if projectTranscript.WorkspaceID == nil || *projectTranscript.WorkspaceID != workspace.ID {
		t.Fatalf("projectTranscript.WorkspaceID = %v, want %d", projectTranscript.WorkspaceID, workspace.ID)
	}
	if projectTranscript.InitiativeID == nil || *projectTranscript.InitiativeID != initiative.ID {
		t.Fatalf("projectTranscript.InitiativeID = %v, want %d", projectTranscript.InitiativeID, initiative.ID)
	}
	if projectTranscript.TaskID == nil || *projectTranscript.TaskID != task.ID {
		t.Fatalf("projectTranscript.TaskID = %v, want %d", projectTranscript.TaskID, task.ID)
	}
	if projectTranscript.RunID == nil || *projectTranscript.RunID != run.ID {
		t.Fatalf("projectTranscript.RunID = %v, want %d", projectTranscript.RunID, run.ID)
	}

	projectEpisodes, err := reopened.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		ProjectID: &projectID,
		TaskID:    &taskID,
		RunID:     &runID,
		Scope:     "project",
		ScopeKey:  project.Key,
		MemoryType: "episode",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries(project episode) error = %v", err)
	}
	if len(projectEpisodes) != 1 {
		t.Fatalf("projectEpisodes len = %d, want 1", len(projectEpisodes))
	}
	projectEpisode := projectEpisodes[0]
	if projectEpisode.WorkspaceID == nil || *projectEpisode.WorkspaceID != workspace.ID {
		t.Fatalf("projectEpisode.WorkspaceID = %v, want %d", projectEpisode.WorkspaceID, workspace.ID)
	}
	if projectEpisode.InitiativeID == nil || *projectEpisode.InitiativeID != initiative.ID {
		t.Fatalf("projectEpisode.InitiativeID = %v, want %d", projectEpisode.InitiativeID, initiative.ID)
	}
	if projectEpisode.SourceTranscriptID == nil || *projectEpisode.SourceTranscriptID != transcript.ID {
		t.Fatalf("projectEpisode.SourceTranscriptID = %v, want %d", projectEpisode.SourceTranscriptID, transcript.ID)
	}
	if projectEpisode.RetentionClass != "episodic" {
		t.Fatalf("projectEpisode.RetentionClass = %q, want episodic", projectEpisode.RetentionClass)
	}
	if projectEpisode.VisibilityScope != "initiative" {
		t.Fatalf("projectEpisode.VisibilityScope = %q, want initiative", projectEpisode.VisibilityScope)
	}
}

func openWorkspaceMemoryBootstrapStore(t *testing.T, runtimeRoot string) *sqlite.Store {
	t.Helper()

	dbPath := filepath.Join(runtimeRoot, "data", "odin.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(dbPath), err)
	}

	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("store.Migrate() error = %v", err)
	}
	return store
}

func createWorkspaceMemoryBootstrapRepoRoot(t *testing.T) string {
	t.Helper()

	repoRoot := t.TempDir()
	for _, dir := range []string{
		filepath.Join(repoRoot, "config"),
		filepath.Join(repoRoot, "registry", "agents"),
		filepath.Join(repoRoot, "registry", "skills"),
		filepath.Join(repoRoot, "registry", "workflows"),
		filepath.Join(repoRoot, "registry", "commands"),
		filepath.Join(repoRoot, "repo"),
		filepath.Join(repoRoot, "local-demo"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", dir, err)
		}
	}

	writeTextFile(t, filepath.Join(repoRoot, "repo", "README.md"), "# repo fixture\n")
	writeTextFile(t, filepath.Join(repoRoot, "local-demo", "README.md"), "# local demo fixture\n")
	initializeGitRepo(t, filepath.Join(repoRoot, "repo"))
	initializeGitRepo(t, filepath.Join(repoRoot, "local-demo"))

	writeTextFile(t, filepath.Join(repoRoot, "config", "projects.yaml"), `
version: 1
projects:
  - key: odin-core
    name: Odin Core
    project_class: system_project
    system_project: true
    git_root: ../repo
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
  - key: local-demo
    name: Local Demo
    project_class: local_git_project
    git_root: ../local-demo
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
`)
	writeTextFile(t, filepath.Join(repoRoot, "config", "executors.yaml"), "version: 1\nexecutors: []\nroutes: []\n")

	for _, kind := range []string{"agents", "skills", "workflows", "commands"} {
		writeTextFile(t, filepath.Join(repoRoot, "registry", kind, "example.md"), `---
kind: `+kind[:len(kind)-1]+`
key: example
title: Example
summary: Example
---

## Purpose
Example

## When to Use
Example

## Inputs
Example

## Procedure
Example

## Outputs
Example

## Constraints
Example

## Success Criteria
Example
`)
	}

	return repoRoot
}
