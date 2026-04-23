package runs

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
	"odin-os/internal/store/sqlite"
)

func TestListFiltersRunsByScope(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openRunStore(t)
	defer store.Close()

	alphaProject, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		GitHubRepo:    "acme/alpha",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(alpha) error = %v", err)
	}
	coreProject, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "odin-core",
		GitRoot:       "/tmp/odin",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(odin-core) error = %v", err)
	}

	alphaTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   alphaProject.ID,
		Key:         "alpha-task",
		Title:       "Alpha task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(alpha) error = %v", err)
	}
	coreTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   coreProject.ID,
		Key:         "core-task",
		Title:       "Core task",
		Status:      "running",
		Scope:       "odin-core",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(core) error = %v", err)
	}

	if _, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   alphaTask.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	}); err != nil {
		t.Fatalf("StartRun(alpha) error = %v", err)
	}
	if _, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   coreTask.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	}); err != nil {
		t.Fatalf("StartRun(core) error = %v", err)
	}

	service := Service{
		DB: store.DB(),
	}

	views, err := service.List(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(views) != 1 || views[0].TaskKey != "alpha-task" {
		t.Fatalf("views = %+v, want one alpha run", views)
	}
}

func TestDetailFiltersRunsByScopeAndLoadsArtifacts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openRunStore(t)
	defer store.Close()

	alphaProject, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		GitHubRepo:    "acme/alpha",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(alpha) error = %v", err)
	}
	coreProject, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "odin-core",
		GitRoot:       "/tmp/odin",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(odin-core) error = %v", err)
	}

	alphaTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   alphaProject.ID,
		Key:         "alpha-task",
		Title:       "Alpha task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(alpha) error = %v", err)
	}
	coreTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   coreProject.ID,
		Key:         "core-task",
		Title:       "Core task",
		Status:      "running",
		Scope:       "odin-core",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(core) error = %v", err)
	}

	alphaRun, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   alphaTask.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(alpha) error = %v", err)
	}
	alphaRun, err = store.FinishRun(ctx, sqlite.FinishRunParams{
		RunID:   alphaRun.ID,
		Status:  "completed",
		Summary: "alpha complete",
	})
	if err != nil {
		t.Fatalf("FinishRun(alpha) error = %v", err)
	}
	coreRun, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   coreTask.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(core) error = %v", err)
	}

	transcript, err := store.RecordConversationTranscript(ctx, sqlite.RecordConversationTranscriptParams{
		ProjectID:   &alphaProject.ID,
		TaskID:      &alphaTask.ID,
		RunID:       &alphaRun.ID,
		Scope:       "project",
		ScopeKey:    "alpha",
		Mode:        "act",
		Prompt:      "Investigate alpha",
		Response:    "Alpha response body",
		ToolSummary: `{"executor":"codex"}`,
		Executor:    "codex",
	})
	if err != nil {
		t.Fatalf("RecordConversationTranscript() error = %v", err)
	}
	if _, err := store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		ProjectID:          &alphaProject.ID,
		SourceTranscriptID: &transcript.ID,
		TaskID:             &alphaTask.ID,
		RunID:              &alphaRun.ID,
		Scope:              "project",
		ScopeKey:           "alpha",
		MemoryType:         "episode",
		Summary:            "Alpha memory summary",
		DetailsJSON:        `{"result":"ok"}`,
	}); err != nil {
		t.Fatalf("RecordMemorySummary() error = %v", err)
	}

	service := Service{
		DB:    store.DB(),
		Store: store,
	}

	detail, err := service.Detail(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, alphaRun.ID)
	if err != nil {
		t.Fatalf("Detail(alpha) error = %v", err)
	}
	if detail.Project.Key != "alpha" || detail.Task.Key != "alpha-task" {
		t.Fatalf("detail project/task = %+v, want alpha/alpha-task", detail)
	}
	if len(detail.Transcripts) != 1 || detail.Transcripts[0].Response != "Alpha response body" {
		t.Fatalf("detail transcripts = %+v, want one alpha transcript", detail.Transcripts)
	}
	if len(detail.MemorySummaries) != 1 || detail.MemorySummaries[0].Summary != "Alpha memory summary" {
		t.Fatalf("detail memory summaries = %+v, want one alpha summary", detail.MemorySummaries)
	}

	_, err = service.Detail(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, coreRun.ID)
	if err != sql.ErrNoRows {
		t.Fatalf("Detail(core) error = %v, want sql.ErrNoRows", err)
	}
}

func TestDetailLoadsDelegationEvidenceForParentRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openRunStore(t)
	defer store.Close()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       t.TempDir(),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(alpha) error = %v", err)
	}

	parentTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "parent-task",
		Title:       "Parent task",
		Status:      "completed",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(parent) error = %v", err)
	}
	parentRun, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   parentTask.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(parent) error = %v", err)
	}
	parentRun, err = store.FinishRun(ctx, sqlite.FinishRunParams{
		RunID:   parentRun.ID,
		Status:  "completed",
		Summary: "parent complete",
	})
	if err != nil {
		t.Fatalf("FinishRun(parent) error = %v", err)
	}

	childTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "child-task",
		Title:       "Child task",
		Status:      "completed",
		Scope:       "project",
		RequestedBy: "agent:portal-delivery-agent",
	})
	if err != nil {
		t.Fatalf("CreateTask(child) error = %v", err)
	}
	childRun, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   childTask.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(child) error = %v", err)
	}
	childRun, err = store.FinishRun(ctx, sqlite.FinishRunParams{
		RunID:   childRun.ID,
		Status:  "completed",
		Summary: "child complete",
	})
	if err != nil {
		t.Fatalf("FinishRun(child) error = %v", err)
	}

	delegation, err := store.CreateDelegation(ctx, sqlite.CreateDelegationParams{
		ParentTaskID:    parentTask.ID,
		ParentRunID:     &parentRun.ID,
		ProjectID:       project.ID,
		Scope:           "project",
		DelegationKey:   "design-direction",
		Role:            "design_direction",
		ActionClass:     "portal_delivery",
		ActionKey:       "admin-cfi:dashboard",
		MutationMode:    "read_only",
		ConvergenceMode: "parent_summary",
		ArtifactTarget:  "run_detail",
		Executor:        "codex_headless",
		DetailsJSON:     `{"skill_key":"pixel-perfect-ui-ux-designer"}`,
	})
	if err != nil {
		t.Fatalf("CreateDelegation() error = %v", err)
	}
	delegation, err = store.AttachDelegationChildTask(ctx, sqlite.AttachDelegationChildTaskParams{
		DelegationID: delegation.ID,
		ChildTaskID:  childTask.ID,
		ChildRunID:   &childRun.ID,
	})
	if err != nil {
		t.Fatalf("AttachDelegationChildTask() error = %v", err)
	}
	delegation, err = store.UpdateDelegationStatus(ctx, sqlite.UpdateDelegationStatusParams{
		DelegationID: delegation.ID,
		Status:       "completed",
	})
	if err != nil {
		t.Fatalf("UpdateDelegationStatus() error = %v", err)
	}

	if _, err := store.CreateDelegationArtifact(ctx, sqlite.CreateDelegationArtifactParams{
		DelegationID: delegation.ID,
		ArtifactType: "run_summary",
		Summary:      "effective_skill=pixel-perfect-ui-ux-designer",
		DetailsJSON:  `{"effective_skill":"pixel-perfect-ui-ux-designer"}`,
	}); err != nil {
		t.Fatalf("CreateDelegationArtifact(run_summary) error = %v", err)
	}
	if _, err := store.CreateDelegationArtifact(ctx, sqlite.CreateDelegationArtifactParams{
		DelegationID: delegation.ID,
		ArtifactType: "memory_summary",
		Summary:      "Child memory summary",
		DetailsJSON:  `{"memory_summary_id":1}`,
	}); err != nil {
		t.Fatalf("CreateDelegationArtifact(memory_summary) error = %v", err)
	}

	service := Service{
		DB:    store.DB(),
		Store: store,
	}
	detail, err := service.Detail(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, parentRun.ID)
	if err != nil {
		t.Fatalf("Detail(parent) error = %v", err)
	}
	if len(detail.Delegations) != 1 {
		t.Fatalf("detail.Delegations len = %d, want 1", len(detail.Delegations))
	}
	if detail.Delegations[0].Relation != "parent" {
		t.Fatalf("detail.Delegations[0].Relation = %q, want parent", detail.Delegations[0].Relation)
	}
	if len(detail.Delegations[0].Artifacts) != 2 {
		t.Fatalf("detail.Delegations[0].Artifacts len = %d, want 2", len(detail.Delegations[0].Artifacts))
	}
}

func TestCancelMarksRunAndTaskCancelledAndReleasesLease(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openRunStore(t)
	defer store.Close()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       t.TempDir(),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(alpha) error = %v", err)
	}
	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "alpha-task",
		Title:       "Alpha task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(alpha) error = %v", err)
	}
	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(alpha) error = %v", err)
	}

	worktreePath := filepath.Join(t.TempDir(), "task-1")
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(worktreePath) error = %v", err)
	}
	lease, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       task.ID,
		RunID:        run.ID,
		Mode:         "mutable",
		BranchName:   "odin/alpha/task-1/run-1/try-1",
		WorktreePath: worktreePath,
		RepoRoot:     project.GitRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease() error = %v", err)
	}

	sleepCmd := exec.Command("sleep", "30")
	sleepCmd.Dir = worktreePath
	if err := sleepCmd.Start(); err != nil {
		t.Fatalf("sleepCmd.Start() error = %v", err)
	}
	waitCh := make(chan error, 1)
	exited := false
	go func() {
		waitCh <- sleepCmd.Wait()
	}()
	defer func() {
		if exited {
			return
		}
		select {
		case <-waitCh:
			exited = true
		default:
			_ = sleepCmd.Process.Kill()
			<-waitCh
			exited = true
		}
	}()

	service := Service{
		DB:    store.DB(),
		Store: store,
	}
	detail, err := service.Cancel(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, run.ID)
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}

	if detail.Run.Status != "cancelled" {
		t.Fatalf("Run.Status = %q, want cancelled", detail.Run.Status)
	}
	if detail.Task.Status != "cancelled" {
		t.Fatalf("Task.Status = %q, want cancelled", detail.Task.Status)
	}
	if detail.Run.Summary == "" || detail.Run.Summary == "running" {
		t.Fatalf("Run.Summary = %q, want cancellation summary", detail.Run.Summary)
	}

	updatedLease, err := store.GetWorktreeLease(ctx, lease.ID)
	if err != nil {
		t.Fatalf("GetWorktreeLease() error = %v", err)
	}
	if updatedLease.State != "released" {
		t.Fatalf("Lease.State = %q, want released", updatedLease.State)
	}
	if updatedLease.ReleasedAt == nil {
		t.Fatalf("Lease.ReleasedAt = nil, want value")
	}

	select {
	case <-waitCh:
		exited = true
		return
	case <-time.After(3 * time.Second):
	}
	t.Fatalf("sleep process still alive after cancel")
}

func openRunStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func writeRegistry(t *testing.T) projects.Registry {
	t.Helper()

	root := t.TempDir()
	configPath := filepath.Join(root, "projects.yaml")

	for _, key := range []string{"odin-core", "alpha"} {
		gitRoot := filepath.Join(root, key)
		if err := os.MkdirAll(filepath.Join(gitRoot, ".git"), 0o755); err != nil {
			t.Fatalf("mkdir git root: %v", err)
		}
	}

	if err := os.WriteFile(configPath, []byte(`
version: 1
projects:
  - key: odin-core
    name: Odin Core
    project_class: system_project
    system_project: true
    git_root: odin-core
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
  - key: alpha
    name: Alpha
    project_class: github_backed_project
    git_root: alpha
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
`), 0o644); err != nil {
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
