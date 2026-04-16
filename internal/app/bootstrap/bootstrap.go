package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	clistate "odin-os/internal/cli/state"
	"odin-os/internal/core/capabilities"
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
	CapabilityService   *capabilities.Service
	SessionStore        clistate.SessionStore
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

	lock, err := acquireBootstrapLock(ctx, runtimeRoot)
	if err != nil {
		return App{}, err
	}
	defer lock.Release()

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

	digest, err := snapshotDigest(registrySnapshot)
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

	capabilityService, err := capabilities.NewService(capabilities.Snapshot{
		Digest:       digest,
		Diagnostics:  registrySnapshot.Diagnostics,
		Capabilities: registrySnapshot.ByKey,
	})
	if err != nil {
		_ = store.Close()
		return App{}, err
	}

	if err := initializeReadinessState(ctx, store, digest, registrySnapshot, executors); err != nil {
		_ = store.Close()
		return App{}, err
	}

	return App{
		Store:               store,
		RepoRoot:            repoRoot,
		Registry:            registry,
		RegistryDiagnostics: diagnostics,
		CapabilityService:   capabilityService,
		SessionStore: clistate.SessionStore{
			Path: filepath.Join(runtimeRoot, "state", "cache", "cli-session.json"),
		},
		ExecutorConfig: executorConfig,
		Executors:      executors,
	}, nil
}

func initializeReadinessState(ctx context.Context, store *sqlite.Store, versionHash string, snapshot registry.Snapshot, executors map[string]contract.Executor) error {
	if len(snapshot.Diagnostics) == 0 {
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

func snapshotDigest(snapshot registry.Snapshot) (string, error) {
	payload := struct {
		Items       []registry.Item       `json:"items"`
		Diagnostics []registry.Diagnostic `json:"diagnostics"`
	}{
		Items:       snapshot.Items,
		Diagnostics: snapshot.Diagnostics,
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
