package bootstrap

import (
	"context"
	"os"
	"path/filepath"

	"odin-os/internal/cli/repl"
	"odin-os/internal/core/projects"
	"odin-os/internal/store/sqlite"
)

type App struct {
	Store               *sqlite.Store
	Registry            projects.Registry
	RegistryDiagnostics []projects.Diagnostic
	SessionStore        repl.SessionStore
}

func Load(ctx context.Context, repoRoot string, runtimeRoot string) (App, error) {
	if err := os.MkdirAll(filepath.Join(runtimeRoot, "data"), 0o755); err != nil {
		return App{}, err
	}
	if err := os.MkdirAll(filepath.Join(runtimeRoot, "state", "cache"), 0o755); err != nil {
		return App{}, err
	}

	store, err := sqlite.Open(filepath.Join(runtimeRoot, "data", "odin.db"))
	if err != nil {
		return App{}, err
	}

	if err := store.Migrate(ctx); err != nil {
		_ = store.Close()
		return App{}, err
	}

	registry, diagnostics, err := projects.Register(filepath.Join(repoRoot, "config", "projects.yaml"))
	if err != nil {
		_ = store.Close()
		return App{}, err
	}

	return App{
		Store:               store,
		Registry:            registry,
		RegistryDiagnostics: diagnostics,
		SessionStore: repl.SessionStore{
			Path: filepath.Join(runtimeRoot, "state", "cache", "cli-session.json"),
		},
	}, nil
}
