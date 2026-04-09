package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"odin-os/internal/cli/repl"
	"odin-os/internal/core/projects"
	"odin-os/internal/executors/contract"
	executorrouter "odin-os/internal/executors/router"
	"odin-os/internal/registry"
	registryloader "odin-os/internal/registry/loader"
	"odin-os/internal/store/sqlite"
)

type App struct {
	Store               *sqlite.Store
	RepoRoot            string
	Registry            projects.Registry
	RegistryDiagnostics []projects.Diagnostic
	SessionStore        repl.SessionStore
	ExecutorConfig      executorrouter.Config
	Executors           map[string]contract.Executor
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

	registrySnapshot, err := registryloader.LoadDir(filepath.Join(repoRoot, "registry"))
	if err != nil {
		_ = store.Close()
		return App{}, err
	}

	registry, diagnostics, err := projects.Register(filepath.Join(repoRoot, "config", "projects.yaml"))
	if err != nil {
		_ = store.Close()
		return App{}, err
	}

	for _, diagnostic := range registrySnapshot.Diagnostics {
		diagnostics = append(diagnostics, projects.Diagnostic{
			Path:    diagnostic.Path,
			Code:    diagnostic.Code,
			Message: diagnostic.Message,
		})
	}

	executorConfig, err := executorrouter.LoadConfig(filepath.Join(repoRoot, "config", "executors.yaml"))
	if err != nil {
		_ = store.Close()
		return App{}, err
	}
	executors := executorrouter.DefaultCatalog()

	if err := initializeReadinessState(ctx, store, filepath.Join(repoRoot, "registry"), registrySnapshot, executors); err != nil {
		_ = store.Close()
		return App{}, err
	}

	return App{
		Store:               store,
		RepoRoot:            repoRoot,
		Registry:            registry,
		RegistryDiagnostics: diagnostics,
		SessionStore: repl.SessionStore{
			Path: filepath.Join(runtimeRoot, "state", "cache", "cli-session.json"),
		},
		ExecutorConfig: executorConfig,
		Executors:      executors,
	}, nil
}

func initializeReadinessState(ctx context.Context, store *sqlite.Store, registryRoot string, snapshot registry.Snapshot, executors map[string]contract.Executor) error {
	if len(snapshot.Diagnostics) == 0 {
		versionHash, err := registryVersionHash(registryRoot)
		if err != nil {
			return err
		}
		if _, err := store.RecordRegistryVersion(ctx, sqlite.RecordRegistryVersionParams{
			Source:      "registry",
			VersionHash: versionHash,
			Notes:       "bootstrap load",
		}); err != nil {
			return err
		}
	}

	for key, executor := range executors {
		report, err := executor.Health(ctx)
		if err != nil {
			continue
		}
		if report.Status != contract.HealthStatusHealthy {
			continue
		}
		if _, err := store.RecordExecutorHealth(ctx, sqlite.RecordExecutorHealthParams{
			Executor:    key,
			Status:      string(report.Status),
			LatencyMS:   0,
			DetailsJSON: `{"source":"bootstrap"}`,
		}); err != nil {
			return err
		}
	}

	for _, surface := range []string{
		"doctor",
		"metrics",
		"active_runs",
		"blocked_items",
		"approvals_waiting",
		"incidents",
		"recoveries",
		"freshness",
		"project_portfolio",
	} {
		if _, err := store.RecordProjectionFreshness(ctx, sqlite.RecordProjectionFreshnessParams{
			Surface:     surface,
			Status:      "current",
			DetailsJSON: `{"source":"bootstrap"}`,
		}); err != nil {
			return err
		}
	}

	return nil
}

func registryVersionHash(root string) (string, error) {
	files, err := registryloader.ScanDir(root)
	if err != nil {
		return "", err
	}

	hasher := sha256.New()
	for _, file := range files {
		content, err := os.ReadFile(file.Path)
		if err != nil {
			return "", err
		}
		_, _ = hasher.Write([]byte(filepath.ToSlash(file.RelativePath)))
		_, _ = hasher.Write(content)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
