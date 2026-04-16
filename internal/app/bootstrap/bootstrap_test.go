package bootstrap

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"odin-os/internal/registry"
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

func TestBootstrapRetainsCapabilityService(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	runtimeRoot := t.TempDir()

	app, err := Load(context.Background(), repoRoot, runtimeRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	defer app.Store.Close()

	if app.CapabilityService == nil {
		t.Fatal("CapabilityService = nil, want live service")
	}

	active := app.CapabilityService.Active()
	if active.Digest == "" {
		t.Fatal("Active().Digest = empty, want snapshot digest")
	}
	if len(active.Diagnostics) != 0 {
		t.Fatalf("Active().Diagnostics = %+v, want none", active.Diagnostics)
	}
}

func TestSnapshotDigestIgnoresAbsoluteSourcePaths(t *testing.T) {
	base := registry.Snapshot{
		Items: []registry.Item{
			{
				Kind:   registry.KindSkill,
				Key:    "skill.example",
				Title:  "Example Skill",
				Scopes: []string{"project"},
				Source: registry.SourceInfo{
					Path:         "/tmp/a/registry/skills/example.md",
					RelativePath: "skills/example.md",
				},
			},
		},
	}

	other := registry.Snapshot{
		Items: []registry.Item{
			{
				Kind:   registry.KindSkill,
				Key:    "skill.example",
				Title:  "Example Skill",
				Scopes: []string{"project"},
				Source: registry.SourceInfo{
					Path:         "/opt/alt/registry/skills/example.md",
					RelativePath: "skills/example.md",
				},
			},
		},
	}

	baseDigest, err := snapshotDigest(base)
	if err != nil {
		t.Fatalf("snapshotDigest(base) error = %v", err)
	}
	otherDigest, err := snapshotDigest(other)
	if err != nil {
		t.Fatalf("snapshotDigest(other) error = %v", err)
	}

	if baseDigest != otherDigest {
		t.Fatalf("snapshotDigest() = %q and %q, want equal", baseDigest, otherDigest)
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
