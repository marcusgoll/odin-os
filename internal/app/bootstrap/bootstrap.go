package bootstrap

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	clistate "odin-os/internal/cli/state"
	"odin-os/internal/core/initiatives"
	"odin-os/internal/core/projects"
	"odin-os/internal/core/workspaces"
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
	RuntimeRoot         string
	Registry            projects.Registry
	RegistrySnapshot    registry.Snapshot
	RegistryDiagnostics []projects.Diagnostic
	SessionStore        clistate.SessionStore
	ExecutorConfig      executorrouter.Config
	Executors           map[string]contract.Executor
	BootID              string
	RuntimeState        runtimestate.Service
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

	if _, err := BootstrapWorkspaceRuntimeState(ctx, store); err != nil {
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

	registryPaths, err := projectManifestPaths(repoRoot)
	if err != nil {
		if failureErr := recordBootstrapFailure(ctx, store, runtimeState, bootID, err); failureErr != nil {
			err = failureErr
		}
		_ = store.Close()
		return App{}, err
	}
	registry, diagnostics, err := projects.RegisterPaths(registryPaths...)
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

	if err := repairLegacyFollowUpTargets(ctx, store, repoRoot); err != nil {
		if failureErr := recordBootstrapFailure(ctx, store, runtimeState, bootID, err); failureErr != nil {
			err = failureErr
		}
		_ = store.Close()
		return App{}, err
	}

	executorConfig, err := executorrouter.LoadConfig(filepath.Join(repoRoot, "config", "executors.yaml"))
	if err != nil {
		if failureErr := recordBootstrapFailure(ctx, store, runtimeState, bootID, err); failureErr != nil {
			err = failureErr
		}
		_ = store.Close()
		return App{}, err
	}
	executors := executorrouter.DefaultCatalogForRepo(repoRoot)

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
		RuntimeRoot:         runtimeRoot,
		Registry:            registry,
		RegistrySnapshot:    registrySnapshot,
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

	rows, err := store.DB().QueryContext(ctx, `
		SELECT key
		FROM projects
		ORDER BY id
	`)
	if err != nil {
		return WorkspaceRuntimeReport{}, err
	}

	projectKeys := make([]string, 0, 8)
	for rows.Next() {
		var projectKey string
		if err := rows.Scan(&projectKey); err != nil {
			_ = rows.Close()
			return WorkspaceRuntimeReport{}, err
		}
		projectKeys = append(projectKeys, projectKey)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return WorkspaceRuntimeReport{}, err
	}
	if err := rows.Close(); err != nil {
		return WorkspaceRuntimeReport{}, err
	}

	for _, projectKey := range projectKeys {
		project, err := store.GetProjectByKey(ctx, projectKey)
		if err != nil {
			return WorkspaceRuntimeReport{}, err
		}

		ownerCompanionID := &defaultCompanion.ID
		if initiative, err := store.GetInitiativeByKey(ctx, workspace.ID, project.Key); err == nil && initiative.OwnerCompanionID != nil {
			ownerCompanionID = initiative.OwnerCompanionID
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return WorkspaceRuntimeReport{}, err
		}

		if _, err := (initiatives.Service{Store: store}).ReconcileManagedProject(ctx, workspace.ID, project, ownerCompanionID); err != nil {
			return WorkspaceRuntimeReport{}, err
		}
		report.ProjectsReconciled++
	}

	now := bootstrapNow(store).Format(time.RFC3339Nano)

	if report.TasksBoundToWorkspace, err = updateRowsAffected(ctx, store.DB(), `
		UPDATE tasks
		SET workspace_id = ?, updated_at = ?
		WHERE workspace_id IS NULL
	`, workspace.ID, now); err != nil {
		return WorkspaceRuntimeReport{}, err
	}

	if report.TasksLinkedToInitiative, err = updateRowsAffected(ctx, store.DB(), `
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
	`, workspace.ID, now, workspace.ID); err != nil {
		return WorkspaceRuntimeReport{}, err
	}

	if report.TasksBoundToCompanion, err = updateRowsAffected(ctx, store.DB(), `
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
	`, now); err != nil {
		return WorkspaceRuntimeReport{}, err
	}

	if report.TasksBackfilledWorkKind, err = updateRowsAffected(ctx, store.DB(), `
		UPDATE tasks
		SET work_kind = scope,
		    updated_at = ?
		WHERE TRIM(COALESCE(work_kind, '')) = ''
	`, now); err != nil {
		return WorkspaceRuntimeReport{}, err
	}

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

func repairLegacyFollowUpTargets(ctx context.Context, store *sqlite.Store, repoRoot string) error {
	if _, err := store.RepairFollowUpObligationLinkedTargets(ctx); err != nil {
		return err
	}

	if remaining, err := countFollowUpObligationsMissingTarget(ctx, store); err != nil {
		return err
	} else if remaining == 0 {
		return nil
	}

	projectID, err := ResolveFollowUpTargetProjectID(ctx, store, repoRoot)
	if err != nil {
		return err
	}
	if _, err := store.RepairFollowUpObligationTargets(ctx, projectID); err != nil {
		return err
	}
	return finalizeFollowUpTargetRepair(ctx, store)
}

func finalizeFollowUpTargetRepair(ctx context.Context, store *sqlite.Store) error {
	remaining, err := countFollowUpObligationsMissingTarget(ctx, store)
	if err != nil {
		return err
	}
	if remaining > 0 {
		return fmt.Errorf("follow-up obligations still missing target_project_id after bootstrap repair")
	}
	return nil
}

func ResolveFollowUpTargetProjectID(ctx context.Context, store *sqlite.Store, repoRoot string) (int64, error) {
	if store == nil {
		return 0, fmt.Errorf("follow-up store is required")
	}

	if project, err := store.GetProjectByKey(ctx, "odin-core"); err == nil {
		return project.ID, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	registryPaths, err := projectManifestPaths(repoRoot)
	if err != nil {
		return 0, err
	}

	project, ok, err := loadDefaultFollowUpTargetProject(registryPaths)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("default target project odin-core is not configured")
	}

	record, err := store.UpsertProject(ctx, sqlite.UpsertProjectParams{
		Key:           project.Key,
		Name:          project.Name,
		Scope:         followUpProjectScope(project),
		GitRoot:       project.GitRoot,
		DefaultBranch: project.DefaultBranch,
		GitHubRepo:    project.GitHub.Repo,
		ManifestPath:  project.SourcePath,
	})
	if err != nil {
		return 0, err
	}
	return record.ID, nil
}

func countFollowUpObligationsMissingTarget(ctx context.Context, store *sqlite.Store) (int64, error) {
	var remaining int64
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM follow_up_obligations
		WHERE target_project_id IS NULL
	`).Scan(&remaining); err != nil {
		return 0, err
	}
	return remaining, nil
}

func loadDefaultFollowUpTargetProject(registryPaths []string) (projects.Manifest, bool, error) {
	cfg, err := projects.LoadManifestFiles(registryPaths...)
	if err != nil {
		return projects.Manifest{}, false, err
	}

	var project projects.Manifest
	found := false
	for _, candidate := range cfg.Projects {
		if candidate.Key == "odin-core" {
			project = candidate
			found = true
		}
	}
	return project, found, nil
}

func followUpProjectScope(project projects.Manifest) string {
	if project.SystemProject {
		return "odin-core"
	}
	return "project"
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
