package bootstrap

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"odin-os/internal/store/sqlite"
)

func TestLoadInitializesFreshRuntimeReadinessState(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	runtimeRoot := t.TempDir()

	app, err := Load(context.Background(), repoRoot, runtimeRoot)
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

func TestWorkspaceLoadBootstrapsDefaultMarcusWorkspace(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	runtimeRoot := t.TempDir()

	app, err := Load(context.Background(), repoRoot, runtimeRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	defer app.Store.Close()

	workspace, err := app.Store.GetWorkspaceByKey(context.Background(), "marcus")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(marcus) error = %v", err)
	}
	if workspace.OwnerRef != "marcus" {
		t.Fatalf("workspace.OwnerRef = %q, want marcus", workspace.OwnerRef)
	}
	if workspace.Status != "active" {
		t.Fatalf("workspace.Status = %q, want active", workspace.Status)
	}
}

func TestBootstrapLoadReconcilesManagedProjectsIntoInitiatives(t *testing.T) {
	ctx := context.Background()
	repoRoot := writeBootstrapFixtureRepo(t)
	runtimeRoot := t.TempDir()

	store := openBootstrapStore(t, runtimeRoot)
	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "local-demo",
		Name:          "Local Demo",
		Scope:         "project",
		GitRoot:       filepath.Join(repoRoot, "local-demo"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(local-demo) error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("store.Close() error = %v", err)
	}

	app, err := Load(ctx, repoRoot, runtimeRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	defer app.Store.Close()

	workspace, err := app.Store.GetWorkspaceByKey(ctx, "marcus")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(marcus) error = %v", err)
	}

	initiatives, err := app.Store.ListInitiatives(ctx, sqlite.ListInitiativesParams{WorkspaceID: workspace.ID})
	if err != nil {
		t.Fatalf("ListInitiatives() error = %v", err)
	}
	if len(initiatives) != 1 {
		t.Fatalf("initiatives len = %d, want 1", len(initiatives))
	}

	initiative := initiatives[0]
	if initiative.Key != "local-demo" {
		t.Fatalf("initiative.Key = %q, want local-demo", initiative.Key)
	}
	if initiative.Kind != "managed_project" {
		t.Fatalf("initiative.Kind = %q, want managed_project", initiative.Kind)
	}
	if initiative.LinkedProjectID == nil || *initiative.LinkedProjectID != project.ID {
		t.Fatalf("initiative.LinkedProjectID = %v, want %d", initiative.LinkedProjectID, project.ID)
	}
}

func TestBootstrapLoadReconcilesExistingManagedProjectInitiativeMetadata(t *testing.T) {
	ctx := context.Background()
	repoRoot := writeBootstrapFixtureRepo(t)
	runtimeRoot := t.TempDir()

	store := openBootstrapStore(t, runtimeRoot)
	workspace, err := store.CreateWorkspace(ctx, sqlite.CreateWorkspaceParams{
		Key:        "marcus",
		Name:       "Marcus",
		OwnerRef:   "marcus",
		Status:     "active",
		PolicyJSON: "{}",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace(marcus) error = %v", err)
	}
	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "local-demo",
		Name:          "Local Demo",
		Scope:         "project",
		GitRoot:       filepath.Join(repoRoot, "local-demo"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(local-demo) error = %v", err)
	}
	if _, err := store.CreateInitiative(ctx, sqlite.CreateInitiativeParams{
		WorkspaceID: workspace.ID,
		Key:         "local-demo",
		Title:       "Legacy Demo",
		Kind:        "delivery",
		Status:      "paused",
		Summary:     "legacy summary",
	}); err != nil {
		t.Fatalf("CreateInitiative(local-demo) error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("store.Close() error = %v", err)
	}

	app, err := Load(ctx, repoRoot, runtimeRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	defer app.Store.Close()

	initiatives, err := app.Store.ListInitiatives(ctx, sqlite.ListInitiativesParams{WorkspaceID: workspace.ID})
	if err != nil {
		t.Fatalf("ListInitiatives() error = %v", err)
	}
	if len(initiatives) != 1 {
		t.Fatalf("initiatives len = %d, want 1", len(initiatives))
	}

	initiative := initiatives[0]
	if initiative.Title != "Local Demo" {
		t.Fatalf("initiative.Title = %q, want Local Demo", initiative.Title)
	}
	if initiative.Kind != "managed_project" {
		t.Fatalf("initiative.Kind = %q, want managed_project", initiative.Kind)
	}
	if initiative.Status != "active" {
		t.Fatalf("initiative.Status = %q, want active", initiative.Status)
	}
	if initiative.LinkedProjectID == nil || *initiative.LinkedProjectID != project.ID {
		t.Fatalf("initiative.LinkedProjectID = %v, want %d", initiative.LinkedProjectID, project.ID)
	}
}

func TestBootstrapLoadDoesNotCreateWorkspaceBeforeExecutorConfigSucceeds(t *testing.T) {
	ctx := context.Background()
	repoRoot := writeBootstrapFixtureRepo(t)
	runtimeRoot := t.TempDir()

	if err := os.WriteFile(filepath.Join(repoRoot, "config", "executors.yaml"), []byte("version: ["), 0o644); err != nil {
		t.Fatalf("WriteFile(executors.yaml) error = %v", err)
	}

	if _, err := Load(ctx, repoRoot, runtimeRoot); err == nil {
		t.Fatal("Load() error = nil, want invalid executor config failure")
	}

	store := openBootstrapStore(t, runtimeRoot)
	defer store.Close()

	_, err := store.GetWorkspaceByKey(ctx, "marcus")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetWorkspaceByKey(marcus) error = %v, want sql.ErrNoRows", err)
	}
}

func TestBootstrapLoadRollsBackWorkspaceBootstrapWhenMemoryMigrationFails(t *testing.T) {
	ctx := context.Background()
	repoRoot := writeBootstrapFixtureRepo(t)
	runtimeRoot := t.TempDir()

	testBootstrapHooks.beforeWorkspaceMemoryCommit = func() error {
		return errors.New("forced workspace bootstrap failure")
	}
	t.Cleanup(func() {
		testBootstrapHooks.beforeWorkspaceMemoryCommit = nil
	})

	if _, err := Load(ctx, repoRoot, runtimeRoot); err == nil || !strings.Contains(err.Error(), "forced workspace bootstrap failure") {
		t.Fatalf("Load() error = %v, want forced workspace bootstrap failure", err)
	}

	store := openBootstrapStore(t, runtimeRoot)
	defer store.Close()

	if _, err := store.GetWorkspaceByKey(ctx, "marcus"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetWorkspaceByKey(marcus) error = %v, want sql.ErrNoRows", err)
	}
}

func TestLoadIncludesConfiguredProjectsOverlay(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeRoot := t.TempDir()

	for _, dir := range []string{
		filepath.Join(repoRoot, "config"),
		filepath.Join(repoRoot, "registry", "agents"),
		filepath.Join(repoRoot, "registry", "skills"),
		filepath.Join(repoRoot, "registry", "workflows"),
		filepath.Join(repoRoot, "registry", "commands"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", dir, err)
		}
	}
	for _, gitRoot := range []string{
		filepath.Join(repoRoot, "repo"),
		filepath.Join(repoRoot, "local-demo"),
	} {
		if err := os.MkdirAll(filepath.Join(gitRoot, ".git"), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", filepath.Join(gitRoot, ".git"), err)
		}
	}

	if err := os.WriteFile(filepath.Join(repoRoot, "config", "projects.yaml"), []byte(`
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
`), 0o644); err != nil {
		t.Fatalf("WriteFile(projects.yaml) error = %v", err)
	}

	overlayPath := filepath.Join(repoRoot, "projects.overlay.yaml")
	if err := os.WriteFile(overlayPath, []byte(`
version: 1
projects:
  - key: local-demo
    name: Local Demo
    project_class: local_git_project
    git_root: local-demo
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
`), 0o644); err != nil {
		t.Fatalf("WriteFile(overlay) error = %v", err)
	}

	for _, kind := range []string{"agents", "skills", "workflows", "commands"} {
		path := filepath.Join(repoRoot, "registry", kind, "example.md")
		if err := os.WriteFile(path, []byte(`---
kind: `+strings.TrimSuffix(kind, "s")+`
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
`), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", path, err)
		}
	}

	if err := os.WriteFile(filepath.Join(repoRoot, "config", "executors.yaml"), []byte("version: 1\nexecutors: []\nroutes: []\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(executors.yaml) error = %v", err)
	}

	t.Setenv("ODIN_PROJECTS_OVERLAY", overlayPath)

	app, err := Load(context.Background(), repoRoot, runtimeRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	defer app.Store.Close()

	if _, ok := app.Registry.Lookup("local-demo"); !ok {
		t.Fatal("Lookup(local-demo) missing from overlay")
	}
}

func TestLoadReadOnlyDoesNotInitializeReadinessState(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	runtimeRoot := t.TempDir()

	app, err := LoadReadOnly(context.Background(), repoRoot, runtimeRoot)
	if err != nil {
		t.Fatalf("LoadReadOnly() error = %v", err)
	}
	defer app.Store.Close()

	assertCountExactly(t, app.Store.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM registry_versions"), 0)
	assertCountExactly(t, app.Store.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM executor_health"), 0)
	assertCountExactly(t, app.Store.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM projection_freshness"), 0)
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

func assertCountExactly(t *testing.T, row rowScanner, want int) {
	t.Helper()

	var count int
	if err := row.Scan(&count); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if count != want {
		t.Fatalf("count = %d, want %d", count, want)
	}
}

type rowScanner interface {
	Scan(...any) error
}

func openBootstrapStore(t *testing.T, runtimeRoot string) *sqlite.Store {
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

func writeBootstrapFixtureRepo(t *testing.T) string {
	t.Helper()

	repoRoot := t.TempDir()
	for _, dir := range []string{
		filepath.Join(repoRoot, "config"),
		filepath.Join(repoRoot, "registry", "agents"),
		filepath.Join(repoRoot, "registry", "skills"),
		filepath.Join(repoRoot, "registry", "workflows"),
		filepath.Join(repoRoot, "registry", "commands"),
		filepath.Join(repoRoot, "repo", ".git"),
		filepath.Join(repoRoot, "local-demo", ".git"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", dir, err)
		}
	}

	if err := os.WriteFile(filepath.Join(repoRoot, "config", "projects.yaml"), []byte(`
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
`), 0o644); err != nil {
		t.Fatalf("WriteFile(projects.yaml) error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(repoRoot, "config", "executors.yaml"), []byte("version: 1\nexecutors: []\nroutes: []\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(executors.yaml) error = %v", err)
	}

	for _, kind := range []string{"agents", "skills", "workflows", "commands"} {
		path := filepath.Join(repoRoot, "registry", kind, "example.md")
		if err := os.WriteFile(path, []byte(`---
kind: `+strings.TrimSuffix(kind, "s")+`
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
`), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", path, err)
		}
	}

	return repoRoot
}
