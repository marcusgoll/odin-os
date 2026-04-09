package bootstrap

import (
	"context"
	"path/filepath"
	"testing"
)

func TestLoadInitializesFreshRuntimeReadinessState(t *testing.T) {
	t.Parallel()

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
