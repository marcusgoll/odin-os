package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	appconfig "odin-os/internal/app/config"
	clistate "odin-os/internal/cli/state"
	"odin-os/internal/core/capabilities"
	"odin-os/internal/core/projects"
	"odin-os/internal/executors/contract"
	executorrouter "odin-os/internal/executors/router"
	"odin-os/internal/registry"
	registryloader "odin-os/internal/registry/loader"
	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/store/sqlite"
)

type App struct {
	Store               *sqlite.Store
	RepoRoot            string
	RuntimeRoot         string
	Registry            projects.Registry
	RegistryDiagnostics []projects.Diagnostic
	CapabilityService   *capabilities.Service
	SessionStore        clistate.SessionStore
	ExecutorConfig      executorrouter.Config
	Executors           map[string]contract.Executor
}

func Load(ctx context.Context, repoRoot string, runtimeRoot string) (App, error) {
	return load(ctx, repoRoot, runtimeRoot, loadOptions{initializeReadiness: true, acquireLock: true, migrate: true})
}

func LoadReadOnly(ctx context.Context, repoRoot string, runtimeRoot string) (App, error) {
	return load(ctx, repoRoot, runtimeRoot, loadOptions{initializeReadiness: false, acquireLock: true, migrate: true})
}

type loadOptions struct {
	initializeReadiness bool
	acquireLock         bool
	migrate             bool
}

func load(ctx context.Context, repoRoot string, runtimeRoot string, options loadOptions) (App, error) {
	if err := os.MkdirAll(filepath.Join(runtimeRoot, "data"), 0o755); err != nil {
		return App{}, err
	}
	if err := os.MkdirAll(filepath.Join(runtimeRoot, "state", "cache"), 0o755); err != nil {
		return App{}, err
	}
	if err := appconfig.ValidateRepo(repoRoot); err != nil {
		return App{}, err
	}

	var lock *bootstrapLock
	if options.acquireLock {
		var err error
		lock, err = acquireBootstrapLock(ctx, runtimeRoot)
		if err != nil {
			return App{}, err
		}
		defer lock.Release()
	}

	store, err := sqlite.Open(filepath.Join(runtimeRoot, "data", "odin.db"))
	if err != nil {
		return App{}, err
	}

	if options.migrate {
		if err := store.Migrate(ctx); err != nil {
			_ = store.Close()
			return App{}, err
		}
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

	registryPaths, err := projectManifestPaths(repoRoot)
	if err != nil {
		_ = store.Close()
		return App{}, err
	}

	registry, diagnostics, err := projects.RegisterPaths(registryPaths...)
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

	if options.initializeReadiness {
		if err := initializeReadinessState(ctx, store, filepath.Join(repoRoot, "registry"), registrySnapshot, executors, healthsvc.DefaultExpectedExecutors()); err != nil {
			_ = store.Close()
			return App{}, err
		}
	}

	return App{
		Store:               store,
		RepoRoot:            repoRoot,
		RuntimeRoot:         runtimeRoot,
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

func projectManifestPaths(repoRoot string) ([]string, error) {
	paths := []string{filepath.Join(repoRoot, "config", "projects.yaml")}

	if overlay := os.Getenv("ODIN_PROJECTS_OVERLAY"); overlay != "" {
		paths = append(paths, overlay)
		return paths, nil
	}

	localOverlay := filepath.Join(repoRoot, "config", "projects.local.yaml")
	if _, err := os.Stat(localOverlay); err == nil {
		paths = append(paths, localOverlay)
		return paths, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	return paths, nil
}

func initializeReadinessState(ctx context.Context, store *sqlite.Store, registryRoot string, snapshot registry.Snapshot, executors map[string]contract.Executor, expectedExecutors []string) error {
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

	for _, key := range expectedExecutors {
		executor, ok := executors[key]
		if !ok {
			if err := recordExecutorReadiness(ctx, store, key, contract.HealthReport{
				Status:  contract.HealthStatusUnavailable,
				Details: "executor is not configured",
			}); err != nil {
				return err
			}
			continue
		}

		report, err := executor.Health(ctx)
		if err != nil {
			report = contract.HealthReport{
				Status:  contract.HealthStatusUnavailable,
				Details: err.Error(),
			}
		}
		if err := recordExecutorReadiness(ctx, store, key, report); err != nil {
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
	type digestItem struct {
		Kind       registry.Kind     `json:"kind"`
		Key        string            `json:"key"`
		Title      string            `json:"title"`
		Summary    string            `json:"summary"`
		Status     string            `json:"status"`
		Tags       []string          `json:"tags"`
		Owners     []string          `json:"owners"`
		Role       string            `json:"role"`
		Scopes     []string          `json:"scopes"`
		Tools      []string          `json:"tools"`
		Strictness string            `json:"strictness"`
		AppliesTo  []string          `json:"applies_to"`
		Entrypoint string            `json:"entrypoint"`
		Composes   []string          `json:"composes"`
		Command    string            `json:"command"`
		Aliases    []string          `json:"aliases"`
		Sections   map[string]string `json:"sections"`
		Source     struct {
			RelativePath string `json:"relative_path"`
		} `json:"source"`
	}

	payload := struct {
		Items []digestItem `json:"items"`
	}{}

	for _, item := range snapshot.Items {
		sections := make(map[string]string, len(item.Sections))
		for key, value := range item.Sections {
			sections[key] = value
		}
		payload.Items = append(payload.Items, digestItem{
			Kind:       item.Kind,
			Key:        item.Key,
			Title:      item.Title,
			Summary:    item.Summary,
			Status:     item.Status,
			Tags:       append([]string(nil), item.Tags...),
			Owners:     append([]string(nil), item.Owners...),
			Role:       item.Role,
			Scopes:     append([]string(nil), item.Scopes...),
			Tools:      append([]string(nil), item.Tools...),
			Strictness: item.Strictness,
			AppliesTo:  append([]string(nil), item.AppliesTo...),
			Entrypoint: item.Entrypoint,
			Composes:   append([]string(nil), item.Composes...),
			Command:    item.Command,
			Aliases:    append([]string(nil), item.Aliases...),
			Sections:   sections,
			Source: struct {
				RelativePath string `json:"relative_path"`
			}{
				RelativePath: item.Source.RelativePath,
			},
		})
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func recordExecutorReadiness(ctx context.Context, store *sqlite.Store, executorKey string, report contract.HealthReport) error {
	details := map[string]string{
		"source": "bootstrap",
	}
	if report.Details != "" {
		details["details"] = report.Details
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return err
	}

	_, err = store.RecordExecutorHealth(ctx, sqlite.RecordExecutorHealthParams{
		Executor:    executorKey,
		Status:      string(report.Status),
		LatencyMS:   0,
		DetailsJSON: string(detailsJSON),
	})
	return err
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
