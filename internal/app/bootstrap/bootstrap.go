package bootstrap

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	appconfig "odin-os/internal/app/config"
	clistate "odin-os/internal/cli/state"
	"odin-os/internal/core/capabilities"
	"odin-os/internal/core/initiatives"
	"odin-os/internal/core/projects"
	"odin-os/internal/core/workspaces"
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

type WorkspaceRuntimeReport struct {
	WorkspaceID             int64
	DefaultCompanionID      int64
	ProjectsReconciled      int
	TasksBoundToWorkspace   int64
	TasksLinkedToInitiative int64
	TasksBoundToCompanion   int64
	TasksBackfilledWorkKind int64
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
	if _, err := BootstrapWorkspaceRuntimeState(ctx, store); err != nil {
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

	if len(diagnostics) == 0 {
		if err := repairLegacyFollowUpTargets(ctx, store, registry); err != nil {
			_ = store.Close()
			return App{}, err
		}
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

func BootstrapWorkspaceRuntimeState(ctx context.Context, store *sqlite.Store) (WorkspaceRuntimeReport, error) {
	if store == nil {
		return WorkspaceRuntimeReport{}, fmt.Errorf("workspace runtime store is required")
	}

	workspace, err := workspaces.Service{Store: store}.BootstrapDefaultWorkspace(ctx)
	if err != nil {
		return WorkspaceRuntimeReport{}, err
	}

	defaultCompanion, err := store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		return WorkspaceRuntimeReport{}, err
	}

	report := WorkspaceRuntimeReport{
		WorkspaceID:        workspace.ID,
		DefaultCompanionID: defaultCompanion.ID,
	}

	projectKeys, err := listProjectKeys(ctx, store)
	if err != nil {
		return WorkspaceRuntimeReport{}, err
	}
	for _, projectKey := range projectKeys {
		project, err := store.GetProjectByKey(ctx, projectKey)
		if err != nil {
			return WorkspaceRuntimeReport{}, err
		}

		ownerCompanionID, err := bootstrapInitiativeOwner(ctx, store, workspace.ID, project.Key, defaultCompanion.ID)
		if err != nil {
			return WorkspaceRuntimeReport{}, err
		}

		if _, err := (initiatives.Service{Store: store}).ReconcileManagedProject(ctx, workspace.ID, project, ownerCompanionID); err != nil {
			return WorkspaceRuntimeReport{}, err
		}
		report.ProjectsReconciled++
	}

	now := bootstrapNow(store).Format(time.RFC3339Nano)

	boundToWorkspace, err := updateRowsAffected(ctx, store.DB(), `
		UPDATE tasks
		SET workspace_id = ?, updated_at = ?
		WHERE workspace_id IS NULL
	`, workspace.ID, now)
	if err != nil {
		return WorkspaceRuntimeReport{}, err
	}
	report.TasksBoundToWorkspace = boundToWorkspace

	linkedToInitiative, err := updateRowsAffected(ctx, store.DB(), `
		UPDATE tasks
		SET initiative_id = (
			SELECT initiatives.id
			FROM initiatives
			WHERE initiatives.workspace_id = ?
				AND initiatives.linked_project_id = tasks.project_id
			ORDER BY initiatives.id DESC
			LIMIT 1
		),
		    updated_at = ?
		WHERE initiative_id IS NULL
		  AND EXISTS (
			SELECT 1
			FROM initiatives
			WHERE initiatives.workspace_id = ?
			  AND initiatives.linked_project_id = tasks.project_id
		  )
	`, workspace.ID, now, workspace.ID)
	if err != nil {
		return WorkspaceRuntimeReport{}, err
	}
	report.TasksLinkedToInitiative = linkedToInitiative

	boundToCompanion, err := updateRowsAffected(ctx, store.DB(), `
		UPDATE tasks
		SET companion_id = (
			SELECT initiatives.owner_companion_id
			FROM initiatives
			WHERE initiatives.id = tasks.initiative_id
			LIMIT 1
		),
		    updated_at = ?
		WHERE companion_id IS NULL
		  AND initiative_id IS NOT NULL
		  AND EXISTS (
			SELECT 1
			FROM initiatives
			WHERE initiatives.id = tasks.initiative_id
			  AND initiatives.owner_companion_id IS NOT NULL
		  )
	`, now)
	if err != nil {
		return WorkspaceRuntimeReport{}, err
	}
	report.TasksBoundToCompanion = boundToCompanion

	backfilledWorkKind, err := updateRowsAffected(ctx, store.DB(), `
		UPDATE tasks
		SET work_kind = scope,
		    updated_at = ?
		WHERE TRIM(COALESCE(work_kind, '')) = ''
	`, now)
	if err != nil {
		return WorkspaceRuntimeReport{}, err
	}
	report.TasksBackfilledWorkKind = backfilledWorkKind

	return report, nil
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

func listProjectKeys(ctx context.Context, store *sqlite.Store) ([]string, error) {
	rows, err := store.DB().QueryContext(ctx, `
		SELECT key
		FROM projects
		ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return keys, nil
}

func repairLegacyFollowUpTargets(ctx context.Context, store *sqlite.Store, registry projects.Registry) error {
	if _, err := store.RepairFollowUpObligationLinkedTargets(ctx); err != nil {
		return err
	}

	var remaining int64
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM follow_up_obligations
		WHERE target_project_id IS NULL
	`).Scan(&remaining); err != nil {
		return err
	}
	if remaining == 0 {
		return nil
	}

	project, ok := registry.Lookup("odin-core")
	if !ok {
		return fmt.Errorf("default target project odin-core is not configured")
	}

	scopeValue := "project"
	if project.SystemProject {
		scopeValue = "odin-core"
	}

	record, err := store.UpsertProject(ctx, sqlite.UpsertProjectParams{
		Key:           project.Key,
		Name:          project.Name,
		Scope:         scopeValue,
		GitRoot:       project.GitRoot,
		DefaultBranch: project.DefaultBranch,
		GitHubRepo:    project.GitHub.Repo,
		ManifestPath:  project.SourcePath,
	})
	if err != nil {
		return err
	}

	_, err = store.RepairFollowUpObligationTargets(ctx, record.ID)
	return err
}

func bootstrapInitiativeOwner(ctx context.Context, store *sqlite.Store, workspaceID int64, projectKey string, defaultCompanionID int64) (*int64, error) {
	initiative, err := store.GetInitiativeByKey(ctx, workspaceID, projectKey)
	if err == nil {
		return initiative.OwnerCompanionID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	return &defaultCompanionID, nil
}

func updateRowsAffected(ctx context.Context, db *sql.DB, query string, args ...any) (int64, error) {
	result, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return affected, nil
}

func bootstrapNow(store *sqlite.Store) time.Time {
	if store.Now != nil {
		return store.Now().UTC()
	}
	return time.Now().UTC()
}
