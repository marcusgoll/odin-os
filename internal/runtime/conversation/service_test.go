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
	t.Parallel()

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
		Registry:       registry,
		ExecutorConfig: mustLoadExecutorConfig(t),
		Executors:      router.DefaultCatalog(),
	}, store
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
