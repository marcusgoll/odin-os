package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	runtimeevents "odin-os/internal/runtime/events"
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
    git_root: ..
    default_branch: main
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

func bootstrapContextWithBootID(ctx context.Context, bootID string) context.Context {
	return WithBootID(ctx, bootID)
}
