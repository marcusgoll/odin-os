package bootstrap

import (
	"context"
	"crypto/sha256"
	"database/sql"
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
	"odin-os/internal/store/sqlite"
)

type App struct {
	Store               *sqlite.Store
	RepoRoot            string
	RuntimeRoot         string
	Registry            projects.Registry
	RegistryDiagnostics []projects.Diagnostic
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

	if err := bootstrapWorkspaceMemory(ctx, store, registry); err != nil {
		_ = store.Close()
		return App{}, err
	}

	if options.initializeReadiness {
		if err := initializeReadinessState(ctx, store, filepath.Join(repoRoot, "registry"), registrySnapshot, executors); err != nil {
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
		SessionStore: clistate.SessionStore{
			Path: filepath.Join(runtimeRoot, "state", "cache", "cli-session.json"),
		},
		ExecutorConfig: executorConfig,
		Executors:      executors,
	}, nil
}

func bootstrapWorkspaceMemory(ctx context.Context, store *sqlite.Store, registry projects.Registry) error {
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	workspace, err := bootstrapDefaultWorkspaceTx(ctx, tx)
	if err != nil {
		return err
	}

	initiativesByProjectID, err := reconcileManagedProjectInitiativesTx(ctx, tx, registry, workspace)
	if err != nil {
		return err
	}

	if err := migrateWorkspaceMemoryOwnership(ctx, tx, workspace, initiativesByProjectID); err != nil {
		return err
	}
	if testBootstrapHooks.beforeWorkspaceMemoryCommit != nil {
		if err := testBootstrapHooks.beforeWorkspaceMemoryCommit(); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func bootstrapDefaultWorkspaceTx(ctx context.Context, tx *sql.Tx) (sqlite.Workspace, error) {
	workspace, err := getWorkspaceByKeyTx(ctx, tx, "marcus")
	if err == nil {
		return workspace, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return sqlite.Workspace{}, err
	}
	return createWorkspaceTx(ctx, tx, sqlite.CreateWorkspaceParams{
		Key:                 "marcus",
		Name:                "Marcus",
		OwnerRef:            "marcus",
		Status:              "active",
		DefaultCompanionKey: "",
		PolicyJSON:          "{}",
	})
}

func reconcileManagedProjectInitiativesTx(ctx context.Context, tx *sql.Tx, registry projects.Registry, workspace sqlite.Workspace) (map[int64]sqlite.Initiative, error) {
	existing, err := listInitiativesTx(ctx, tx, workspace.ID)
	if err != nil {
		return nil, err
	}

	byKey := make(map[string]sqlite.Initiative, len(existing))
	byProjectID := make(map[int64]sqlite.Initiative, len(existing))
	for _, initiative := range existing {
		byKey[initiative.Key] = initiative
		if initiative.LinkedProjectID != nil {
			byProjectID[*initiative.LinkedProjectID] = initiative
		}
	}

	for _, manifest := range registry.Projects() {
		if manifest.SystemProject {
			continue
		}

		project, err := getProjectByKeyTx(ctx, tx, manifest.Key)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}

		if initiative, ok := byProjectID[project.ID]; ok {
			if err := reconcileManagedProjectInitiativeTx(ctx, tx, initiative.ID, project.ID, project.Name); err != nil {
				return nil, err
			}
			initiative.Title = project.Name
			initiative.Kind = "managed_project"
			initiative.Status = "active"
			initiative.LinkedProjectID = int64Ptr(project.ID)
			byProjectID[project.ID] = initiative
			byKey[initiative.Key] = initiative
			continue
		}

		if initiative, ok := byKey[project.Key]; ok {
			if err := reconcileManagedProjectInitiativeTx(ctx, tx, initiative.ID, project.ID, project.Name); err != nil {
				return nil, err
			}
			initiative.Title = project.Name
			initiative.LinkedProjectID = int64Ptr(project.ID)
			initiative.Kind = "managed_project"
			initiative.Status = "active"
			byKey[initiative.Key] = initiative
			byProjectID[project.ID] = initiative
			continue
		}

		initiative, err := createInitiativeTx(ctx, tx, sqlite.CreateInitiativeParams{
			WorkspaceID:     workspace.ID,
			Key:             project.Key,
			Title:           project.Name,
			Kind:            "managed_project",
			Status:          "active",
			Summary:         "",
			LinkedProjectID: int64Ptr(project.ID),
		})
		if err != nil {
			return nil, err
		}
		byKey[initiative.Key] = initiative
		byProjectID[project.ID] = initiative
	}

	return byProjectID, nil
}

func reconcileManagedProjectInitiativeTx(ctx context.Context, tx *sql.Tx, initiativeID int64, projectID int64, title string) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE initiatives
		SET linked_project_id = ?,
		    title = ?,
		    kind = 'managed_project',
		    status = 'active',
		    updated_at = STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ?
	`, projectID, title, initiativeID)
	return err
}

func migrateWorkspaceMemoryOwnership(ctx context.Context, tx *sql.Tx, workspace sqlite.Workspace, initiativesByProjectID map[int64]sqlite.Initiative) error {
	if err := migrateGlobalConversationTranscripts(ctx, tx, workspace); err != nil {
		return err
	}
	if err := migrateGlobalMemorySummaries(ctx, tx, workspace); err != nil {
		return err
	}
	for projectID, initiative := range initiativesByProjectID {
		if err := migrateProjectConversationTranscripts(ctx, tx, workspace.ID, initiative.ID, projectID); err != nil {
			return err
		}
		if err := migrateProjectMemorySummaries(ctx, tx, workspace.ID, initiative.ID, projectID); err != nil {
			return err
		}
	}
	return nil
}

func getWorkspaceByKeyTx(ctx context.Context, tx *sql.Tx, key string) (sqlite.Workspace, error) {
	var workspace sqlite.Workspace
	var defaultCompanionKey sql.NullString
	row := tx.QueryRowContext(ctx, `
		SELECT id, key, name, owner_ref, status, default_companion_key, policy_json
		FROM workspaces
		WHERE key = ?
	`, key)
	if err := row.Scan(
		&workspace.ID,
		&workspace.Key,
		&workspace.Name,
		&workspace.OwnerRef,
		&workspace.Status,
		&defaultCompanionKey,
		&workspace.PolicyJSON,
	); err != nil {
		return sqlite.Workspace{}, err
	}
	workspace.DefaultCompanionKey = defaultCompanionKey.String
	return workspace, nil
}

func createWorkspaceTx(ctx context.Context, tx *sql.Tx, params sqlite.CreateWorkspaceParams) (sqlite.Workspace, error) {
	now := formatBootstrapTime(time.Now())
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO workspaces (key, name, owner_ref, status, default_companion_key, policy_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, params.Key, params.Name, params.OwnerRef, params.Status, nullIfEmpty(params.DefaultCompanionKey), params.PolicyJSON, now, now); err != nil {
		return sqlite.Workspace{}, err
	}
	return getWorkspaceByKeyTx(ctx, tx, params.Key)
}

func listInitiativesTx(ctx context.Context, tx *sql.Tx, workspaceID int64) ([]sqlite.Initiative, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, workspace_id, key, title, kind, status, summary, linked_project_id, owner_companion_id
		FROM initiatives
		WHERE workspace_id = ?
		ORDER BY id ASC
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var initiatives []sqlite.Initiative
	for rows.Next() {
		var initiative sqlite.Initiative
		var linkedProjectID sql.NullInt64
		var ownerCompanionID sql.NullInt64
		if err := rows.Scan(
			&initiative.ID,
			&initiative.WorkspaceID,
			&initiative.Key,
			&initiative.Title,
			&initiative.Kind,
			&initiative.Status,
			&initiative.Summary,
			&linkedProjectID,
			&ownerCompanionID,
		); err != nil {
			return nil, err
		}
		initiative.LinkedProjectID = nullableInt64Ptr(linkedProjectID)
		initiative.OwnerCompanionID = nullableInt64Ptr(ownerCompanionID)
		initiatives = append(initiatives, initiative)
	}
	return initiatives, rows.Err()
}

func createInitiativeTx(ctx context.Context, tx *sql.Tx, params sqlite.CreateInitiativeParams) (sqlite.Initiative, error) {
	now := formatBootstrapTime(time.Now())
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO initiatives (workspace_id, key, title, kind, status, summary, linked_project_id, owner_companion_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, params.WorkspaceID, params.Key, params.Title, params.Kind, params.Status, params.Summary, nullInt64(params.LinkedProjectID), nullInt64(params.OwnerCompanionID), now, now); err != nil {
		return sqlite.Initiative{}, err
	}
	initiatives, err := listInitiativesTx(ctx, tx, params.WorkspaceID)
	if err != nil {
		return sqlite.Initiative{}, err
	}
	for _, initiative := range initiatives {
		if initiative.Key == params.Key {
			return initiative, nil
		}
	}
	return sqlite.Initiative{}, sql.ErrNoRows
}

func getProjectByKeyTx(ctx context.Context, tx *sql.Tx, key string) (sqlite.Project, error) {
	var project sqlite.Project
	var githubRepo sql.NullString
	row := tx.QueryRowContext(ctx, `
		SELECT id, key, name, scope, git_root, default_branch, github_repo, manifest_path
		FROM projects
		WHERE key = ?
	`, key)
	if err := row.Scan(
		&project.ID,
		&project.Key,
		&project.Name,
		&project.Scope,
		&project.GitRoot,
		&project.DefaultBranch,
		&githubRepo,
		&project.ManifestPath,
	); err != nil {
		return sqlite.Project{}, err
	}
	project.GitHubRepo = githubRepo.String
	return project, nil
}

func migrateGlobalConversationTranscripts(ctx context.Context, tx *sql.Tx, workspace sqlite.Workspace) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE conversation_transcripts
		SET workspace_id = COALESCE(workspace_id, ?),
		    scope = 'workspace',
		    scope_key = ?
		WHERE scope = 'global'
	`, workspace.ID, workspace.Key); err != nil {
		return err
	}

	_, err := tx.ExecContext(ctx, `
		UPDATE conversation_transcripts
		SET workspace_id = COALESCE(workspace_id, ?),
		    scope_key = ?
		WHERE scope = 'workspace' AND (COALESCE(TRIM(scope_key), '') = '' OR scope_key = 'global' OR workspace_id IS NULL)
	`, workspace.ID, workspace.Key)
	return err
}

func migrateGlobalMemorySummaries(ctx context.Context, tx *sql.Tx, workspace sqlite.Workspace) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE memory_summaries
		SET workspace_id = COALESCE(workspace_id, ?),
		    scope = 'workspace',
		    scope_key = ?,
		    visibility_scope = CASE
		        WHEN COALESCE(TRIM(visibility_scope), '') = '' THEN 'workspace'
		        ELSE visibility_scope
		    END,
		    retention_class = CASE
		        WHEN COALESCE(TRIM(retention_class), '') = '' THEN 'durable'
		        ELSE retention_class
		    END,
		    updated_at = STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE scope = 'global'
	`, workspace.ID, workspace.Key); err != nil {
		return err
	}

	_, err := tx.ExecContext(ctx, `
		UPDATE memory_summaries
		SET workspace_id = COALESCE(workspace_id, ?),
		    scope_key = ?,
		    visibility_scope = CASE
		        WHEN COALESCE(TRIM(visibility_scope), '') = '' THEN 'workspace'
		        ELSE visibility_scope
		    END,
		    retention_class = CASE
		        WHEN COALESCE(TRIM(retention_class), '') = '' THEN 'durable'
		        ELSE retention_class
		    END,
		    updated_at = STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE scope = 'workspace' AND (COALESCE(TRIM(scope_key), '') = '' OR scope_key = 'global' OR workspace_id IS NULL)
	`, workspace.ID, workspace.Key)
	return err
}

func migrateProjectConversationTranscripts(ctx context.Context, tx *sql.Tx, workspaceID int64, initiativeID int64, projectID int64) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE conversation_transcripts
		SET workspace_id = COALESCE(workspace_id, ?),
		    initiative_id = COALESCE(initiative_id, ?)
		WHERE project_id = ? AND (workspace_id IS NULL OR initiative_id IS NULL)
	`, workspaceID, initiativeID, projectID)
	return err
}

func migrateProjectMemorySummaries(ctx context.Context, tx *sql.Tx, workspaceID int64, initiativeID int64, projectID int64) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE memory_summaries
		SET workspace_id = COALESCE(workspace_id, ?),
		    initiative_id = COALESCE(initiative_id, ?),
		    visibility_scope = CASE
		        WHEN COALESCE(TRIM(visibility_scope), '') = '' THEN 'initiative'
		        ELSE visibility_scope
		    END,
		    retention_class = CASE
		        WHEN COALESCE(TRIM(retention_class), '') = '' AND memory_type = 'episode' THEN 'episodic'
		        WHEN COALESCE(TRIM(retention_class), '') = '' THEN 'durable'
		        ELSE retention_class
		    END,
		    updated_at = STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE project_id = ?
		  AND (workspace_id IS NULL OR initiative_id IS NULL OR COALESCE(TRIM(visibility_scope), '') = '' OR COALESCE(TRIM(retention_class), '') = '')
	`, workspaceID, initiativeID, projectID)
	return err
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

func int64Ptr(value int64) *int64 {
	return &value
}

func nullableInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	ptr := new(int64)
	*ptr = value.Int64
	return ptr
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func formatBootstrapTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
