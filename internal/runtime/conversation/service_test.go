package conversation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
	"odin-os/internal/executors/contract"
	"odin-os/internal/executors/router"
	"odin-os/internal/store/sqlite"
)

func TestServiceRespondAnswersScopeQuestion(t *testing.T) {
	t.Parallel()

	service, _ := newTestService(t)

	result, err := service.Respond(context.Background(), Request{
		Scope: scope.Resolution{
			Kind:       scope.ScopeProject,
			ProjectKey: "alpha",
		},
		Mode:   "ask",
		Prompt: "what scope am i in?",
	})
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if result.Intent != "scope" {
		t.Fatalf("Intent = %q, want %q", result.Intent, "scope")
	}
	if !strings.Contains(result.Answer, "alpha") {
		t.Fatalf("Answer = %q, want project scope response", result.Answer)
	}
}

func TestServiceRespondAnswersModeQuestion(t *testing.T) {
	t.Parallel()

	service, _ := newTestService(t)

	result, err := service.Respond(context.Background(), Request{
		Scope: scope.Resolution{
			Kind:       scope.ScopeProject,
			ProjectKey: "alpha",
		},
		Mode:   "act",
		Prompt: "what mode am i in?",
	})
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if result.Intent != "mode" {
		t.Fatalf("Intent = %q, want %q", result.Intent, "mode")
	}
	if !strings.Contains(result.Answer, "act") {
		t.Fatalf("Answer = %q, want mode response", result.Answer)
	}
}

func TestServiceRespondUsesExecutorBackedFallback(t *testing.T) {
	configureConversationHarnessDriver(t)
	service, _ := newTestService(t)

	result, err := service.Respond(context.Background(), Request{
		Scope:  scope.Resolution{Kind: scope.ScopeGlobal},
		Mode:   "ask",
		Prompt: "hello there",
	})
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if result.Intent != "conversation" {
		t.Fatalf("Intent = %q, want %q", result.Intent, "conversation")
	}
	if result.ExecutorKey == "" {
		t.Fatalf("ExecutorKey = %q, want executor-backed response", result.ExecutorKey)
	}
	if !strings.Contains(result.Answer, "codex_headless") {
		t.Fatalf("Answer = %q, want executor-backed output", result.Answer)
	}
}

func TestServiceRespondFallsBackWithoutExecutors(t *testing.T) {
	t.Parallel()

	service, _ := newTestService(t)
	service.Executors = nil
	service.ExecutorConfig = router.Config{}

	result, err := service.Respond(context.Background(), Request{
		Scope:  scope.Resolution{Kind: scope.ScopeGlobal},
		Mode:   "ask",
		Prompt: "hello there",
	})
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if result.ExecutorKey != "" {
		t.Fatalf("ExecutorKey = %q, want fallback without executor", result.ExecutorKey)
	}
	if !strings.Contains(result.Answer, "Odin is listening") {
		t.Fatalf("Answer = %q, want conversational fallback", result.Answer)
	}
}

func TestConversationRespondPersistsScopedTranscriptOwnership(t *testing.T) {
	t.Parallel()

	service, store := newTestService(t)
	ctx := context.Background()

	project, err := store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}
	workspace := createConversationWorkspace(t, ctx, store)
	initiative := createConversationInitiative(t, ctx, store, workspace.ID, "alpha-initiative", &project.ID)

	_, err = service.Respond(ctx, Request{
		Scope:  scope.Resolution{Kind: scope.ScopeProject, ProjectKey: "alpha"},
		Mode:   "ask",
		Prompt: "what scope am i in?",
	})
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}

	transcripts, err := store.ListConversationTranscripts(ctx, sqlite.ListConversationTranscriptsParams{
		ProjectID:    &project.ID,
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		Scope:        "project",
		ScopeKey:     "alpha",
	})
	if err != nil {
		t.Fatalf("ListConversationTranscripts() error = %v", err)
	}
	if len(transcripts) != 1 {
		t.Fatalf("transcripts len = %d, want 1", len(transcripts))
	}
	if transcripts[0].WorkspaceID == nil || *transcripts[0].WorkspaceID != workspace.ID {
		t.Fatalf("transcript.WorkspaceID = %v, want %d", transcripts[0].WorkspaceID, workspace.ID)
	}
	if transcripts[0].InitiativeID == nil || *transcripts[0].InitiativeID != initiative.ID {
		t.Fatalf("transcript.InitiativeID = %v, want %d", transcripts[0].InitiativeID, initiative.ID)
	}
}

func TestServiceRespondScopesInitiativeMemoryToCurrentProject(t *testing.T) {
	t.Parallel()

	service, store := newTestService(t)
	ctx := context.Background()

	workspace := createConversationWorkspace(t, ctx, store)
	alphaProject, err := store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}
	betaProject, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "beta",
		Name:          "Beta",
		Scope:         "project",
		GitRoot:       "/tmp/beta",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(beta) error = %v", err)
	}
	alphaInitiative := createConversationInitiative(t, ctx, store, workspace.ID, "alpha-initiative", &alphaProject.ID)
	betaInitiative := createConversationInitiative(t, ctx, store, workspace.ID, "beta-initiative", &betaProject.ID)

	recordConversationMemorySummary(t, ctx, store, sqlite.RecordMemorySummaryParams{
		WorkspaceID:     &workspace.ID,
		InitiativeID:    &alphaInitiative.ID,
		Scope:           "initiative",
		ScopeKey:        alphaInitiative.Key,
		VisibilityScope: "initiative",
		RetentionClass:  "durable",
		MemoryType:      "summary",
		Summary:         "alpha milestone captured",
	})
	recordConversationMemorySummary(t, ctx, store, sqlite.RecordMemorySummaryParams{
		WorkspaceID:     &workspace.ID,
		InitiativeID:    &betaInitiative.ID,
		Scope:           "initiative",
		ScopeKey:        betaInitiative.Key,
		VisibilityScope: "initiative",
		RetentionClass:  "durable",
		MemoryType:      "summary",
		Summary:         "beta milestone captured",
	})

	result, err := service.Respond(ctx, Request{
		Scope:  scope.Resolution{Kind: scope.ScopeProject, ProjectKey: "alpha"},
		Mode:   "ask",
		Prompt: "show initiative memory",
	})
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if result.Intent != "memory" {
		t.Fatalf("Intent = %q, want memory", result.Intent)
	}
	if !strings.Contains(result.Answer, "alpha-initiative") {
		t.Fatalf("Answer = %q, want alpha initiative", result.Answer)
	}
	if strings.Contains(result.Answer, "beta-initiative") {
		t.Fatalf("Answer = %q, should not leak beta initiative", result.Answer)
	}
}

func TestServiceRespondScopesWorkspaceAndCompanionMemoryToMarcus(t *testing.T) {
	t.Parallel()

	service, store := newTestService(t)
	ctx := context.Background()

	marcus := createConversationWorkspace(t, ctx, store)
	other, err := store.CreateWorkspace(ctx, sqlite.CreateWorkspaceParams{
		Key:        "other",
		Name:       "Other",
		OwnerRef:   "other",
		Status:     "active",
		PolicyJSON: "{}",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace(other) error = %v", err)
	}

	recordConversationMemorySummary(t, ctx, store, sqlite.RecordMemorySummaryParams{
		WorkspaceID:     &marcus.ID,
		Scope:           "workspace",
		ScopeKey:        marcus.Key,
		VisibilityScope: "workspace",
		RetentionClass:  "durable",
		MemoryType:      "summary",
		Summary:         "marcus workspace summary",
	})
	recordConversationMemorySummary(t, ctx, store, sqlite.RecordMemorySummaryParams{
		WorkspaceID:     &other.ID,
		Scope:           "workspace",
		ScopeKey:        other.Key,
		VisibilityScope: "workspace",
		RetentionClass:  "durable",
		MemoryType:      "summary",
		Summary:         "other workspace summary",
	})

	strategist, err := store.CreateCompanion(ctx, sqlite.CreateCompanionParams{
		WorkspaceID:         marcus.ID,
		Key:                 "strategist",
		Title:               "Strategist",
		Kind:                "advisor",
		Status:              "active",
		InitiativeScopeJSON: "[]",
		MemoryPolicyJSON:    "{}",
		PlanningPolicyJSON:  "{}",
		ToolPolicyJSON:      "{}",
	})
	if err != nil {
		t.Fatalf("CreateCompanion(strategist) error = %v", err)
	}
	outsider, err := store.CreateCompanion(ctx, sqlite.CreateCompanionParams{
		WorkspaceID:         other.ID,
		Key:                 "outsider",
		Title:               "Outsider",
		Kind:                "advisor",
		Status:              "active",
		InitiativeScopeJSON: "[]",
		MemoryPolicyJSON:    "{}",
		PlanningPolicyJSON:  "{}",
		ToolPolicyJSON:      "{}",
	})
	if err != nil {
		t.Fatalf("CreateCompanion(outsider) error = %v", err)
	}

	recordConversationMemorySummary(t, ctx, store, sqlite.RecordMemorySummaryParams{
		WorkspaceID:     &marcus.ID,
		CompanionID:     &strategist.ID,
		Scope:           "companion",
		ScopeKey:        strategist.Key,
		VisibilityScope: "companion",
		RetentionClass:  "durable",
		MemoryType:      "summary",
		Summary:         "strategist remembered planning cadence",
	})
	recordConversationMemorySummary(t, ctx, store, sqlite.RecordMemorySummaryParams{
		WorkspaceID:     &other.ID,
		CompanionID:     &outsider.ID,
		Scope:           "companion",
		ScopeKey:        outsider.Key,
		VisibilityScope: "companion",
		RetentionClass:  "durable",
		MemoryType:      "summary",
		Summary:         "outsider memory should stay hidden",
	})

	workspaceResult, err := service.Respond(ctx, Request{
		Scope:  scope.Resolution{Kind: scope.ScopeGlobal},
		Mode:   "ask",
		Prompt: "show workspace memory",
	})
	if err != nil {
		t.Fatalf("Respond(workspace) error = %v", err)
	}
	if !strings.Contains(workspaceResult.Answer, "Workspace marcus") {
		t.Fatalf("workspace answer = %q, want Marcus workspace", workspaceResult.Answer)
	}
	if strings.Contains(workspaceResult.Answer, "other") {
		t.Fatalf("workspace answer = %q, should not leak other workspace", workspaceResult.Answer)
	}

	companionResult, err := service.Respond(ctx, Request{
		Scope:  scope.Resolution{Kind: scope.ScopeGlobal},
		Mode:   "ask",
		Prompt: "show companion memory",
	})
	if err != nil {
		t.Fatalf("Respond(companion) error = %v", err)
	}
	if !strings.Contains(companionResult.Answer, "strategist") {
		t.Fatalf("companion answer = %q, want strategist", companionResult.Answer)
	}
	if strings.Contains(companionResult.Answer, "outsider") {
		t.Fatalf("companion answer = %q, should not leak outsider", companionResult.Answer)
	}
}

func TestServiceRespondSurfacesExecutorFailure(t *testing.T) {
	t.Parallel()

	service, _ := newTestService(t)
	service.Executors = map[string]contract.Executor{
		"failing": contract.NewStaticExecutor(
			"failing",
			contract.ExecutorClassPlanBackedCLI,
			contract.HealthReport{Status: contract.HealthStatusHealthy, CheckedAt: time.Now().UTC()},
			contract.Capabilities{
				ExecutorClass:        contract.ExecutorClassPlanBackedCLI,
				SupportsHeadlessPlan: true,
				TaskKinds:            []contract.TaskKind{contract.TaskKindGeneral},
				Scopes:               []string{"global", "project", "odin-core", "new-project"},
			},
		),
	}
	service.ExecutorConfig = router.Config{
		Version: 1,
		Executors: []router.ExecutorConfig{
			{
				Key:     "failing",
				Class:   contract.ExecutorClassPlanBackedCLI,
				Enabled: true,
			},
		},
		Routes: []router.RouteConfig{
			{
				Name: "default",
				Match: router.RouteMatch{
					TaskKinds: []contract.TaskKind{contract.TaskKindGeneral},
					Scopes:    []string{"global", "project", "odin-core", "new-project"},
				},
				Preferred: []string{"failing"},
			},
		},
	}

	result, err := service.Respond(context.Background(), Request{
		Scope:  scope.Resolution{Kind: scope.ScopeGlobal},
		Mode:   "ask",
		Prompt: "hello there",
	})
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if result.Warning == "" {
		t.Fatalf("Warning = %q, want surfaced executor failure", result.Warning)
	}
	if !strings.Contains(strings.ToLower(result.Answer), "unavailable") {
		t.Fatalf("Answer = %q, want degraded executor notice", result.Answer)
	}
	if !strings.Contains(result.Answer, "Odin is listening") {
		t.Fatalf("Answer = %q, want conversational fallback content", result.Answer)
	}
}

func TestServiceRespondHandlesJobsRunsAndDoctor(t *testing.T) {
	t.Parallel()

	service, store := newTestService(t)
	ctx := context.Background()

	project, err := store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}
	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "alpha-task",
		Title:       "seeded task",
		Status:      "completed",
		Scope:       string(scope.ScopeProject),
		RequestedBy: "test",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "completed",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	if _, err := store.FinishRun(ctx, sqlite.FinishRunParams{
		RunID:   run.ID,
		Status:  "completed",
		Summary: "seeded run",
	}); err != nil {
		t.Fatalf("FinishRun() error = %v", err)
	}

	for _, tc := range []struct {
		name   string
		prompt string
		intent string
	}{
		{name: "jobs", prompt: "show me jobs", intent: "jobs"},
		{name: "runs", prompt: "show me runs", intent: "runs"},
		{name: "doctor", prompt: "doctor please", intent: "doctor"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := service.Respond(ctx, Request{
				Scope:  scope.Resolution{Kind: scope.ScopeProject, ProjectKey: "alpha"},
				Mode:   "ask",
				Prompt: tc.prompt,
			})
			if err != nil {
				t.Fatalf("Respond() error = %v", err)
			}
			if result.Intent != tc.intent {
				t.Fatalf("Intent = %q, want %q", result.Intent, tc.intent)
			}
			if strings.TrimSpace(result.Answer) == "" {
				t.Fatalf("Answer is empty")
			}
		})
	}
}

func TestSnapshotIncludesApprovalsActiveRunsStalledRunsAndProjectTransitions(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	now := time.Now().UTC()
	store.Now = func() time.Time { return now.Add(-2 * time.Hour) }

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	runningTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "running-task",
		Title:       "Running task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(running) error = %v", err)
	}
	if _, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   runningTask.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	}); err != nil {
		t.Fatalf("StartRun(running) error = %v", err)
	}

	approvalTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "approval-task",
		Title:       "Approval task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(approval) error = %v", err)
	}
	approvalRun, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   approvalTask.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(approval) error = %v", err)
	}
	if _, _, _, err := store.AwaitApproval(ctx, sqlite.AwaitApprovalParams{
		TaskID:         approvalTask.ID,
		RunID:          approvalRun.ID,
		RequestedBy:    "operator",
		Summary:        "waiting on approval",
		TerminalReason: "waiting on approval",
		ArtifactsJSON:  `[]`,
	}); err != nil {
		t.Fatalf("AwaitApproval() error = %v", err)
	}

	if _, err := store.SetProjectTransition(ctx, sqlite.SetProjectTransitionParams{
		ProjectID:          project.ID,
		State:              "cutover",
		Controller:         "odin_os",
		LimitedActionsJSON: "[]",
		Notes:              "primary controller",
		ChangedBy:          "operator",
	}); err != nil {
		t.Fatalf("SetProjectTransition() error = %v", err)
	}

	snapshot, err := Service{
		DB:             store.DB(),
		Now:            func() time.Time { return now },
		StalledTimeout: 30 * time.Minute,
	}.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	if len(snapshot.ApprovalsWaiting) != 1 {
		t.Fatalf("approvals waiting = %d, want 1", len(snapshot.ApprovalsWaiting))
	}
	if len(snapshot.ActiveRuns) != 1 {
		t.Fatalf("active runs = %d, want 1", len(snapshot.ActiveRuns))
	}
	if len(snapshot.StalledRuns) != 1 {
		t.Fatalf("stalled runs = %d, want 1", len(snapshot.StalledRuns))
	}
	if len(snapshot.ProjectTransitions) != 1 {
		t.Fatalf("project transitions = %d, want 1", len(snapshot.ProjectTransitions))
	}
	if snapshot.ProjectTransitionOwnership.OdinOS != 1 {
		t.Fatalf("odin_os ownership = %d, want 1", snapshot.ProjectTransitionOwnership.OdinOS)
	}
}

func newTestService(t *testing.T) (Service, *sqlite.Store) {
	t.Helper()

	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	dataDir := filepath.Join(root, "data")
	for _, dir := range []string{configDir, dataDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	registry := writeRegistry(t, root)
	store, err := sqlite.Open(filepath.Join(dataDir, "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	for _, manifest := range registry.Projects() {
		scopeValue := "project"
		if manifest.SystemProject {
			scopeValue = "odin-core"
		}
		if _, err := store.CreateProject(context.Background(), sqlite.CreateProjectParams{
			Key:           manifest.Key,
			Name:          manifest.Name,
			Scope:         scopeValue,
			GitRoot:       manifest.GitRoot,
			DefaultBranch: manifest.DefaultBranch,
			GitHubRepo:    manifest.GitHub.Repo,
			ManifestPath:  manifest.SourcePath,
		}); err != nil {
			t.Fatalf("CreateProject(%s) error = %v", manifest.Key, err)
		}
	}

	return Service{
		Store:          store,
		ExecutorConfig: mustLoadExecutorConfig(t),
		Executors:      router.DefaultCatalog(),
		Registry:       registry,
	}, store
}

func createConversationWorkspace(t *testing.T, ctx context.Context, store *sqlite.Store) sqlite.Workspace {
	t.Helper()

	workspace, err := store.CreateWorkspace(ctx, sqlite.CreateWorkspaceParams{
		Key:        "marcus",
		Name:       "Marcus",
		OwnerRef:   "marcus",
		Status:     "active",
		PolicyJSON: "{}",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	return workspace
}

func createConversationInitiative(t *testing.T, ctx context.Context, store *sqlite.Store, workspaceID int64, key string, linkedProjectID *int64) sqlite.Initiative {
	t.Helper()

	initiative, err := store.CreateInitiative(ctx, sqlite.CreateInitiativeParams{
		WorkspaceID:     workspaceID,
		Key:             key,
		Title:           key,
		Kind:            "managed_project",
		Status:          "active",
		Summary:         "runtime test initiative",
		LinkedProjectID: linkedProjectID,
	})
	if err != nil {
		t.Fatalf("CreateInitiative() error = %v", err)
	}
	return initiative
}

func recordConversationMemorySummary(t *testing.T, ctx context.Context, store *sqlite.Store, params sqlite.RecordMemorySummaryParams) sqlite.MemorySummary {
	t.Helper()

	summary, err := store.RecordMemorySummary(ctx, params)
	if err != nil {
		t.Fatalf("RecordMemorySummary() error = %v", err)
	}
	return summary
}

func configureConversationHarnessDriver(t *testing.T) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "codex-driver.sh")
	if err := os.WriteFile(path, []byte(`#!/usr/bin/env bash
cat >/dev/null
printf '{"status":"completed","output":"codex_headless says hello","external_id":"fixture-driver"}'
`), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("Chmod(driver) error = %v", err)
	}
	t.Setenv("ODIN_CODEX_DRIVER", path)
}

func mustLoadExecutorConfig(t *testing.T) router.Config {
	t.Helper()

	cfg, err := router.LoadConfig(filepath.Clean(filepath.Join("..", "..", "..", "config", "executors.yaml")))
	if err != nil {
		t.Fatalf("LoadConfig(executors) error = %v", err)
	}
	return cfg
}

func writeRegistry(t *testing.T, root string) projects.Registry {
	t.Helper()

	configPath := filepath.Join(root, "projects.yaml")
	for _, key := range []string{"odin-core", "alpha"} {
		gitRoot := filepath.Join(root, key)
		if err := os.MkdirAll(filepath.Join(gitRoot, ".git"), 0o755); err != nil {
			t.Fatalf("mkdir git root: %v", err)
		}
	}

	content := `
version: 1
projects:
  - key: odin-core
    name: Odin Core
    project_class: system_project
    system_project: true
    git_root: ` + filepath.Join(root, "odin-core") + `
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
    git_root: ` + filepath.Join(root, "alpha") + `
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
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	registry, diagnostics, err := projects.Register(configPath)
	if err != nil {
		t.Fatalf("projects.Register() error = %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("projects.Register() diagnostics = %+v", diagnostics)
	}
	return registry
}
