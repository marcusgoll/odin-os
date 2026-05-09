package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"odin-os/internal/core/followups"
	"odin-os/internal/core/workspaces"
	"odin-os/internal/prompts"
	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/store/sqlite"
)

func TestLoadInitializesRuntimeReadinessStateForServeBootstrap(t *testing.T) {
	repoRoot := createBootstrapRepoRoot(t, true)
	runtimeRoot := t.TempDir()

	app, err := Load(bootstrapContextWithBootID(context.Background(), "boot-1"), repoRoot, runtimeRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	defer app.Store.Close()

	if len(app.RegistryDiagnostics) != 0 {
		t.Fatalf("RegistryDiagnostics = %+v, want none", app.RegistryDiagnostics)
	}

	assertCountAtLeast(t, app.Store.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM registry_versions"), 1)
	assertCountAtLeast(t, app.Store.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM executor_health"), 1)
	assertCountAtLeast(t, app.Store.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM projection_freshness"), 1)
}

func TestLoadRecordsStoppedWithLastErrorWhenMigrationFailsDuringServeBootstrap(t *testing.T) {
	repoRoot := createBootstrapRepoRoot(t, true)
	runtimeRoot := t.TempDir()

	originalMigrate := migrateStoreFn
	migrateStoreFn = func(ctx context.Context, store *sqlite.Store) error {
		return errors.New("migrate failed")
	}
	t.Cleanup(func() {
		migrateStoreFn = originalMigrate
	})

	ctx := bootstrapContextWithBootID(context.Background(), "boot-1")
	_, err := Load(ctx, repoRoot, runtimeRoot)
	if err == nil {
		t.Fatal("Load() error = nil, want migration failure")
	}

	store, openErr := sqlite.Open(filepath.Join(runtimeRoot, "data", "odin.db"))
	if openErr != nil {
		t.Fatalf("sqlite.Open() error = %v", openErr)
	}
	defer store.Close()

	got, err := store.GetRuntimeState(context.Background())
	if err != nil {
		t.Fatalf("GetRuntimeState() error = %v", err)
	}
	if got.Status != "stopped" {
		t.Fatalf("RuntimeState.Status = %q, want %q", got.Status, "stopped")
	}
	if got.LastError != "migrate failed" {
		t.Fatalf("RuntimeState.LastError = %q, want %q", got.LastError, "migrate failed")
	}
}

func TestLoadRecordsBootingWhenServeBootIDIsPresent(t *testing.T) {
	repoRoot := createBootstrapRepoRoot(t, true)
	runtimeRoot := t.TempDir()

	ctx := bootstrapContextWithBootID(context.Background(), "boot-1")
	app, err := Load(ctx, repoRoot, runtimeRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	defer app.Store.Close()

	got, err := app.Store.GetRuntimeState(context.Background())
	if err != nil {
		t.Fatalf("GetRuntimeState() error = %v", err)
	}
	if got.Status != "booting" {
		t.Fatalf("RuntimeState.Status = %q, want %q", got.Status, "booting")
	}
	if got.BootID != "boot-1" {
		t.Fatalf("RuntimeState.BootID = %q, want %q", got.BootID, "boot-1")
	}

	events, err := app.Store.ListEvents(context.Background(), sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	found := false
	for _, event := range events {
		if event.Type != runtimeevents.EventServiceLifecycleChanged {
			continue
		}
		lifecycle, err := runtimeevents.DecodePayload[runtimeevents.ServiceLifecyclePayload](event.Payload)
		if err != nil {
			t.Fatalf("DecodePayload(ServiceLifecyclePayload) error = %v", err)
		}
		if lifecycle.Status == "booting" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("events = %+v, want booting lifecycle event", events)
	}
}

func TestLoadRecordsStoppedWithLastErrorWhenBootstrapFailsAfterBooting(t *testing.T) {
	repoRoot := createBootstrapRepoRoot(t, false)
	runtimeRoot := t.TempDir()

	ctx := bootstrapContextWithBootID(context.Background(), "boot-1")
	_, err := Load(ctx, repoRoot, runtimeRoot)
	if err == nil {
		t.Fatal("Load() error = nil, want bootstrap failure")
	}

	store, openErr := sqlite.Open(filepath.Join(runtimeRoot, "data", "odin.db"))
	if openErr != nil {
		t.Fatalf("sqlite.Open() error = %v", openErr)
	}
	defer store.Close()

	got, err := store.GetRuntimeState(context.Background())
	if err != nil {
		t.Fatalf("GetRuntimeState() error = %v", err)
	}
	if got.Status != "stopped" {
		t.Fatalf("RuntimeState.Status = %q, want %q", got.Status, "stopped")
	}
	if got.LastError == "" {
		t.Fatal("RuntimeState.LastError = empty, want bootstrap error")
	}
}

func TestLoadRegistersProjectOverlayFromEnvironment(t *testing.T) {
	repoRoot := createBootstrapRepoRoot(t, true)
	runtimeRoot := t.TempDir()
	overlayRoot := filepath.Join(repoRoot, "family-ops")
	if err := os.MkdirAll(filepath.Join(overlayRoot, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir overlay git root: %v", err)
	}
	overlayPath := filepath.Join(repoRoot, "config", "projects.overlay.yaml")
	if err := os.WriteFile(overlayPath, []byte(`
version: 1
projects:
  - key: family-ops
    name: Family Ops
    project_class: local_git_project
    git_root: `+overlayRoot+`
    default_branch: main
    policy:
      allowed_commands:
        - status
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
		t.Fatalf("write overlay projects config: %v", err)
	}
	t.Setenv("ODIN_PROJECTS_OVERLAY", overlayPath)

	app, err := Load(context.Background(), repoRoot, runtimeRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	defer app.Store.Close()

	if _, ok := app.Registry.Lookup("family-ops"); !ok {
		t.Fatal("Registry.Lookup(family-ops) = false, want overlay project registered")
	}
	if len(app.RegistryDiagnostics) != 0 {
		t.Fatalf("RegistryDiagnostics = %+v, want none", app.RegistryDiagnostics)
	}
}

func TestLoadConfiguresPromptRendererFromRepoRoot(t *testing.T) {
	repoRoot := createBootstrapRepoRoot(t, true)
	writeBootstrapPromptTemplate(t, repoRoot)
	runtimeRoot := t.TempDir()

	app, err := Load(context.Background(), repoRoot, runtimeRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	defer app.Store.Close()

	renderer, ok := app.PromptRenderer.(prompts.FileRenderer)
	if !ok {
		t.Fatalf("PromptRenderer = %T, want prompts.FileRenderer", app.PromptRenderer)
	}
	wantRoot := filepath.Join(repoRoot, "prompts", "workers")
	if renderer.Root != wantRoot {
		t.Fatalf("PromptRenderer.Root = %q, want %q", renderer.Root, wantRoot)
	}
	if app.PromptTemplateName != "go-orchestrator" {
		t.Fatalf("PromptTemplateName = %q, want %q", app.PromptTemplateName, "go-orchestrator")
	}
}

func TestLoadLeavesPromptRendererUnconfiguredWhenTemplateIsMissing(t *testing.T) {
	repoRoot := createBootstrapRepoRoot(t, true)
	runtimeRoot := t.TempDir()

	app, err := Load(context.Background(), repoRoot, runtimeRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	defer app.Store.Close()

	if app.PromptRenderer != nil {
		t.Fatalf("PromptRenderer = %T, want nil", app.PromptRenderer)
	}
	if app.PromptTemplateName != "" {
		t.Fatalf("PromptTemplateName = %q, want empty", app.PromptTemplateName)
	}
}

func TestLoadRepairsLegacyFollowUpObligationTargetsBeforeMaterialization(t *testing.T) {
	ctx := context.Background()
	repoRoot := createBootstrapRepoRoot(t, true)
	runtimeRoot := t.TempDir()

	if err := os.MkdirAll(filepath.Join(runtimeRoot, "data"), 0o755); err != nil {
		t.Fatalf("MkdirAll(data) error = %v", err)
	}

	seedStore, err := sqlite.Open(filepath.Join(runtimeRoot, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open(seed) error = %v", err)
	}
	if err := seedStore.Migrate(ctx); err != nil {
		t.Fatalf("Migrate(seed) error = %v", err)
	}

	workspaceService := workspaces.Service{Store: seedStore}
	if _, err := workspaceService.BootstrapDefaultWorkspace(ctx); err != nil {
		t.Fatalf("BootstrapDefaultWorkspace() error = %v", err)
	}
	workspace, err := seedStore.GetWorkspaceByKey(ctx, workspaces.DefaultWorkspaceKey)
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}
	companion, err := seedStore.UpsertCompanion(ctx, sqlite.UpsertCompanionParams{
		WorkspaceID: workspace.ID,
		Key:         workspaces.DefaultWorkspaceCompanionKey,
		Title:       "Primary Assistant",
		Kind:        "assistant",
		Charter:     "Default companion for this workspace.",
		Status:      "active",
	})
	if err != nil {
		t.Fatalf("UpsertCompanion() error = %v", err)
	}
	initiative, err := seedStore.UpsertInitiative(ctx, sqlite.UpsertInitiativeParams{
		WorkspaceID:      workspace.ID,
		Key:              "life-admin",
		Title:            "Life Admin",
		Kind:             "routine",
		Status:           "active",
		OwnerCompanionID: &companion.ID,
	})
	if err != nil {
		t.Fatalf("UpsertInitiative() error = %v", err)
	}

	nextDueAt := time.Date(2026, 4, 17, 9, 0, 0, 0, time.UTC)
	result, err := seedStore.DB().ExecContext(ctx, `
		INSERT INTO follow_up_obligations (
			workspace_id,
			initiative_id,
			companion_id,
			target_project_id,
			title,
			status,
			cadence_json,
			next_due_at,
			last_materialized_at,
			last_completed_at,
			policy_json,
			created_at,
			updated_at
		)
		VALUES (?, ?, ?, NULL, ?, ?, ?, ?, NULL, NULL, ?, ?, ?)
	`, workspace.ID, initiative.ID, companion.ID, "Review mail", "active", `{"mode":"once"}`, nextDueAt.Format(time.RFC3339Nano), `{}`, nextDueAt.Format(time.RFC3339Nano), nextDueAt.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatalf("seed follow_up_obligations row error = %v", err)
	}
	obligationID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId() error = %v", err)
	}
	if err := seedStore.Close(); err != nil {
		t.Fatalf("seedStore.Close() error = %v", err)
	}

	app, err := Load(ctx, repoRoot, runtimeRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	defer app.Store.Close()

	obligation, err := app.Store.GetFollowUpObligation(ctx, obligationID)
	if err != nil {
		t.Fatalf("GetFollowUpObligation() error = %v", err)
	}
	defaultProject, err := app.Store.GetProjectByKey(ctx, "odin-core")
	if err != nil {
		t.Fatalf("GetProjectByKey(odin-core) error = %v", err)
	}
	if obligation.TargetProjectID != defaultProject.ID {
		t.Fatalf("TargetProjectID = %d, want %d", obligation.TargetProjectID, defaultProject.ID)
	}

	materialized, err := followups.Service{
		Store: app.Store,
		Now: func() time.Time {
			return nextDueAt.Add(time.Hour)
		},
	}.Materialize(ctx, followups.MaterializeParams{
		ObligationID: obligationID,
		TaskKey:      "review-mail-1",
		Title:        "Review mail",
		Scope:        "project",
		RequestedBy:  "operator",
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}

	task, err := app.Store.GetTask(ctx, materialized.TaskID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.ProjectID != defaultProject.ID {
		t.Fatalf("Task.ProjectID = %d, want %d", task.ProjectID, defaultProject.ID)
	}
}

func TestLoadRepairsLegacyFollowUpObligationTargetsDespiteProjectDiagnostics(t *testing.T) {
	ctx := context.Background()
	repoRoot := createBootstrapRepoRoot(t, true)
	runtimeRoot := t.TempDir()

	if err := os.WriteFile(filepath.Join(repoRoot, "config", "projects.local.yaml"), []byte(`version: 1
projects:
  - key: broken-project
    name: Broken Project
    project_class: system_project
    system_project: true
    git_root: `+filepath.Join(repoRoot, "broken-project")+`
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
`), 0o644); err != nil {
		t.Fatalf("WriteFile(projects.local.yaml) error = %v", err)
	}

	if err := os.MkdirAll(filepath.Join(runtimeRoot, "data"), 0o755); err != nil {
		t.Fatalf("MkdirAll(data) error = %v", err)
	}

	seedStore, err := sqlite.Open(filepath.Join(runtimeRoot, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open(seed) error = %v", err)
	}
	if err := seedStore.Migrate(ctx); err != nil {
		t.Fatalf("Migrate(seed) error = %v", err)
	}

	workspaceService := workspaces.Service{Store: seedStore}
	if _, err := workspaceService.BootstrapDefaultWorkspace(ctx); err != nil {
		t.Fatalf("BootstrapDefaultWorkspace() error = %v", err)
	}
	workspace, err := seedStore.GetWorkspaceByKey(ctx, workspaces.DefaultWorkspaceKey)
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}
	companion, err := seedStore.UpsertCompanion(ctx, sqlite.UpsertCompanionParams{
		WorkspaceID: workspace.ID,
		Key:         workspaces.DefaultWorkspaceCompanionKey,
		Title:       "Primary Assistant",
		Kind:        "assistant",
		Charter:     "Default companion for this workspace.",
		Status:      "active",
	})
	if err != nil {
		t.Fatalf("UpsertCompanion() error = %v", err)
	}
	initiative, err := seedStore.UpsertInitiative(ctx, sqlite.UpsertInitiativeParams{
		WorkspaceID:      workspace.ID,
		Key:              "life-admin",
		Title:            "Life Admin",
		Kind:             "routine",
		Status:           "active",
		OwnerCompanionID: &companion.ID,
	})
	if err != nil {
		t.Fatalf("UpsertInitiative() error = %v", err)
	}

	nextDueAt := time.Date(2026, 4, 17, 9, 0, 0, 0, time.UTC)
	if _, err := seedStore.DB().ExecContext(ctx, `
		INSERT INTO follow_up_obligations (
			workspace_id,
			initiative_id,
			companion_id,
			target_project_id,
			title,
			status,
			cadence_json,
			next_due_at,
			last_materialized_at,
			last_completed_at,
			policy_json,
			created_at,
			updated_at
		)
		VALUES (?, ?, ?, NULL, ?, ?, ?, ?, NULL, NULL, ?, ?, ?)
	`, workspace.ID, initiative.ID, companion.ID, "Review mail", "active", `{"mode":"once"}`, nextDueAt.Format(time.RFC3339Nano), `{}`, nextDueAt.Format(time.RFC3339Nano), nextDueAt.Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("seed follow_up_obligations row error = %v", err)
	}
	if err := seedStore.Close(); err != nil {
		t.Fatalf("seedStore.Close() error = %v", err)
	}

	app, err := Load(ctx, repoRoot, runtimeRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	defer app.Store.Close()

	obligation, err := app.Store.GetFollowUpObligation(ctx, 1)
	if err != nil {
		t.Fatalf("GetFollowUpObligation() error = %v", err)
	}
	defaultProject, err := app.Store.GetProjectByKey(ctx, "odin-core")
	if err != nil {
		t.Fatalf("GetProjectByKey(odin-core) error = %v", err)
	}
	if obligation.TargetProjectID != defaultProject.ID {
		t.Fatalf("TargetProjectID = %d, want %d", obligation.TargetProjectID, defaultProject.ID)
	}
}

func TestLoadSerializesConcurrentBootstrapForFreshRuntime(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	runtimeRoot := t.TempDir()

	var entered int32
	lockAcquired := make(chan struct{})
	release := make(chan struct{})
	testBootstrapHooks.afterLockAcquired = func() {
		if atomic.AddInt32(&entered, 1) != 1 {
			return
		}
		close(lockAcquired)
		<-release
	}
	t.Cleanup(func() {
		testBootstrapHooks.afterLockAcquired = nil
	})

	type result struct {
		app App
		err error
	}

	firstResult := make(chan result, 1)
	secondResult := make(chan result, 1)

	go func() {
		app, err := Load(context.Background(), repoRoot, runtimeRoot)
		firstResult <- result{app: app, err: err}
	}()

	<-lockAcquired

	go func() {
		app, err := Load(context.Background(), repoRoot, runtimeRoot)
		secondResult <- result{app: app, err: err}
	}()

	select {
	case result := <-secondResult:
		if result.app.Store != nil {
			_ = result.app.Store.Close()
		}
		t.Fatalf("second Load() completed before first bootstrap released the lock: err=%v", result.err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	first := <-firstResult
	if first.err != nil {
		t.Fatalf("first Load() error = %v", first.err)
	}
	defer first.app.Store.Close()

	second := <-secondResult
	if second.err != nil {
		t.Fatalf("second Load() error = %v", second.err)
	}
	defer second.app.Store.Close()
}

func TestLoadReturnsBootstrapTimeoutWhenLockWaitExceedsConfiguredLimit(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	runtimeRoot := t.TempDir()
	t.Setenv("ODIN_BOOTSTRAP_TIMEOUT", "50ms")

	lockAcquired := make(chan struct{})
	release := make(chan struct{})
	var entered int32
	testBootstrapHooks.afterLockAcquired = func() {
		if atomic.AddInt32(&entered, 1) != 1 {
			return
		}
		close(lockAcquired)
		<-release
	}
	t.Cleanup(func() {
		testBootstrapHooks.afterLockAcquired = nil
	})

	firstResult := make(chan error, 1)
	go func() {
		app, err := Load(context.Background(), repoRoot, runtimeRoot)
		if err == nil {
			_ = app.Store.Close()
		}
		firstResult <- err
	}()

	<-lockAcquired

	_, err := Load(context.Background(), repoRoot, runtimeRoot)
	close(release)

	if err == nil {
		t.Fatal("second Load() error = nil, want bootstrap timeout")
	}

	var timeoutErr *BootstrapTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("Load() error = %v, want BootstrapTimeoutError", err)
	}

	if firstErr := <-firstResult; firstErr != nil {
		t.Fatalf("first Load() error = %v", firstErr)
	}
}

func assertCountAtLeast(t *testing.T, row rowScanner, minimum int) {
	t.Helper()

	var count int
	if err := row.Scan(&count); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if count < minimum {
		t.Fatalf("count = %d, want at least %d", count, minimum)
	}
}

type rowScanner interface {
	Scan(...any) error
}

func createBootstrapRepoRoot(t *testing.T, includeProjectsConfig bool) string {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "registry"), 0o755); err != nil {
		t.Fatalf("mkdir registry: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "odin-core", ".git"), 0o755); err != nil {
		t.Fatalf("mkdir odin-core git root: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "config", "odin.yaml"), []byte(`
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:0
  startup_recovery: true
`), 0o644); err != nil {
		t.Fatalf("write odin config: %v", err)
	}
	if includeProjectsConfig {
		if err := os.WriteFile(filepath.Join(root, "config", "projects.yaml"), []byte(`
version: 1
projects:
  - key: odin-core
    name: Odin Core
    project_class: system_project
    system_project: true
    git_root: `+filepath.Join(root, "odin-core")+`
    default_branch: main
    policy:
      allowed_commands:
        - status
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
			t.Fatalf("write projects config: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "config", "executors.yaml"), []byte(`
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
`), 0o644); err != nil {
		t.Fatalf("write executors config: %v", err)
	}

	return root
}

func writeBootstrapPromptTemplate(t *testing.T, repoRoot string) {
	t.Helper()

	root := filepath.Join(repoRoot, "prompts", "workers")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir prompts root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "go-orchestrator.md"), []byte("---\nrequires_acceptance_criteria: true\n---\n"), 0o644); err != nil {
		t.Fatalf("write prompt template: %v", err)
	}
}

func bootstrapContextWithBootID(ctx context.Context, bootID string) context.Context {
	return WithBootID(ctx, bootID)
}
