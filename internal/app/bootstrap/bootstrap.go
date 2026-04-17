package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"time"

	clistate "odin-os/internal/cli/state"
	"odin-os/internal/core/projects"
	"odin-os/internal/executors/contract"
	executorrouter "odin-os/internal/executors/router"
	"odin-os/internal/registry"
	registryloader "odin-os/internal/registry/loader"
	healthsvc "odin-os/internal/runtime/health"
	runtimestate "odin-os/internal/runtime/state"
	"odin-os/internal/store/sqlite"
)

type App struct {
	Store               *sqlite.Store
	RepoRoot            string
	Registry            projects.Registry
	RegistryDiagnostics []projects.Diagnostic
	SessionStore        clistate.SessionStore
	ExecutorConfig      executorrouter.Config
	Executors           map[string]contract.Executor
	BootID              string
	RuntimeState        runtimestate.Service
}

type bootIDContextKey struct{}

var bootIDKey bootIDContextKey

var migrateStoreFn = func(ctx context.Context, store *sqlite.Store) error {
	return store.Migrate(ctx)
}

var serviceOwnedProjectionSurfaces = []string{
	"doctor",
	"metrics",
	"active_runs",
	"blocked_items",
	"approvals_waiting",
	"incidents",
	"recoveries",
	"freshness",
	"project_portfolio",
}

func WithBootID(ctx context.Context, bootID string) context.Context {
	return context.WithValue(ctx, bootIDKey, bootID)
}

func ServiceOwnedProjectionSurfaces() []string {
	return append([]string(nil), serviceOwnedProjectionSurfaces...)
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

	bootID := bootIDFromContext(ctx)
	runtimeState := runtimestate.Service{Store: store}

	if err := migrateStoreFn(ctx, store); err != nil {
		if failureErr := recordBootstrapFailure(ctx, store, runtimeState, bootID, err); failureErr != nil {
			err = failureErr
		}
		_ = store.Close()
		return App{}, err
	}

	if bootID != "" {
		if _, err := runtimeState.MarkBooting(ctx, runtimestate.BootInput{
			BootID: bootID,
			PID:    os.Getpid(),
		}); err != nil {
			_ = store.Close()
			return App{}, err
		}
	}

	registrySnapshot, err := registryloader.LoadDir(filepath.Join(repoRoot, "registry"))
	if err != nil {
		if failureErr := recordBootstrapFailure(ctx, store, runtimeState, bootID, err); failureErr != nil {
			err = failureErr
		}
		_ = store.Close()
		return App{}, err
	}

	registry, diagnostics, err := projects.Register(filepath.Join(repoRoot, "config", "projects.yaml"))
	if err != nil {
		if failureErr := recordBootstrapFailure(ctx, store, runtimeState, bootID, err); failureErr != nil {
			err = failureErr
		}
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
		if failureErr := recordBootstrapFailure(ctx, store, runtimeState, bootID, err); failureErr != nil {
			err = failureErr
		}
		_ = store.Close()
		return App{}, err
	}
	executors := executorrouter.DefaultCatalog()

	if bootID != "" {
		if err := initializeReadinessState(ctx, store, filepath.Join(repoRoot, "registry"), registrySnapshot, executorConfig, executors); err != nil {
			if failureErr := recordBootstrapFailure(ctx, store, runtimeState, bootID, err); failureErr != nil {
				err = failureErr
			}
			_ = store.Close()
			return App{}, err
		}
	}

	return App{
		Store:               store,
		RepoRoot:            repoRoot,
		Registry:            registry,
		RegistryDiagnostics: diagnostics,
		SessionStore: clistate.SessionStore{
			Path: filepath.Join(runtimeRoot, "state", "cache", "cli-session.json"),
		},
		ExecutorConfig: executorConfig,
		Executors:      executors,
		BootID:         bootID,
		RuntimeState:   runtimeState,
	}, nil
}

func RefreshReadinessSamples(ctx context.Context, app App, registryHealthy bool) error {
	if registryHealthy {
		versionHash, err := registryVersionHash(filepath.Join(app.RepoRoot, "registry"))
		if err != nil {
			return err
		}
		if _, err := app.Store.RecordRegistryVersion(ctx, sqlite.RecordRegistryVersionParams{
			Source:      "registry",
			VersionHash: versionHash,
			Notes:       "doctor refresh",
		}); err != nil {
			return err
		}
	}

	if err := (healthsvc.Service{}).SampleConfiguredExecutors(ctx, app.Store, app.ExecutorConfig, app.Executors, "doctor"); err != nil {
		return err
	}

	if err := (healthsvc.Service{}).RefreshProjectionFreshness(ctx, app.Store, ServiceOwnedProjectionSurfaces(), "doctor"); err != nil {
		return err
	}

	return nil
}

func bootIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(bootIDKey).(string)
	return value
}

func recordBootstrapFailure(ctx context.Context, store *sqlite.Store, service runtimestate.Service, bootID string, cause error) error {
	if bootID == "" || service.Store == nil {
		return cause
	}

	if _, err := service.MarkStopped(ctx, runtimestate.TransitionInput{
		BootID: bootID,
		Reason: "bootstrap failed",
		Error:  cause.Error(),
	}); err != nil {
		if fallbackErr := writeBootstrapFailureState(ctx, store, bootID, cause); fallbackErr != nil {
			return errors.Join(cause, err, fallbackErr)
		}
	}
	return cause
}

func writeBootstrapFailureState(ctx context.Context, store *sqlite.Store, bootID string, cause error) error {
	now := time.Now().UTC()
	_, err := store.DB().ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS runtime_state (
		  singleton_key TEXT PRIMARY KEY,
		  boot_id TEXT NOT NULL,
		  status TEXT NOT NULL,
		  pid INTEGER NOT NULL,
		  started_at TEXT NOT NULL,
		  ready_at TEXT,
		  last_heartbeat_at TEXT NOT NULL,
		  last_shutdown_reason TEXT NOT NULL DEFAULT '',
		  last_error TEXT NOT NULL DEFAULT '',
		  updated_at TEXT NOT NULL
		)
	`)
	if err != nil {
		return err
	}

	_, err = store.DB().ExecContext(ctx, `
		INSERT INTO runtime_state (
			singleton_key,
			boot_id,
			status,
			pid,
			started_at,
			ready_at,
			last_heartbeat_at,
			last_shutdown_reason,
			last_error,
			updated_at
		)
		VALUES (?, ?, ?, ?, ?, NULL, ?, ?, ?, ?)
		ON CONFLICT(singleton_key) DO UPDATE SET
			boot_id = excluded.boot_id,
			status = excluded.status,
			pid = excluded.pid,
			started_at = excluded.started_at,
			ready_at = excluded.ready_at,
			last_heartbeat_at = excluded.last_heartbeat_at,
			last_shutdown_reason = excluded.last_shutdown_reason,
			last_error = excluded.last_error,
			updated_at = excluded.updated_at
	`, "primary", bootID, "stopped", os.Getpid(), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), "bootstrap failed", cause.Error(), now.Format(time.RFC3339Nano))
	return err
}

func initializeReadinessState(ctx context.Context, store *sqlite.Store, registryRoot string, snapshot registry.Snapshot, executorConfig executorrouter.Config, executors map[string]contract.Executor) error {
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

	if err := (healthsvc.Service{}).SampleConfiguredExecutors(ctx, store, executorConfig, executors, "bootstrap"); err != nil {
		return err
	}

	if err := (healthsvc.Service{}).RefreshProjectionFreshness(ctx, store, ServiceOwnedProjectionSurfaces(), "bootstrap"); err != nil {
		return err
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
