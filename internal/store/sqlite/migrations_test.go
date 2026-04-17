package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMigrateBackfillsManagedProjectInitiativesForExistingProjects(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openMigrationBackfillStore(t)
	defer store.Close()

	for _, version := range []int{1, 11} {
		migration, err := loadMigrationByVersion(version)
		if err != nil {
			t.Fatalf("loadMigrationByVersion(%d) error = %v", version, err)
		}
		if err := store.applyMigration(ctx, migration); err != nil {
			t.Fatalf("applyMigration(%d) error = %v", version, err)
		}
	}

	workspace, err := store.CreateWorkspace(ctx, CreateWorkspaceParams{
		Key:                 "default",
		Name:                "Default Workspace",
		OwnerRef:            "operator",
		DefaultCompanionKey: "primary",
		Status:              "active",
		PolicyJSON:          `{"allow":["branch_proposal"]}`,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace(default) error = %v", err)
	}

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       filepath.Join(t.TempDir(), "alpha"),
		DefaultBranch: "main",
		GitHubRepo:    "acme/alpha",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	initiative, err := store.GetInitiativeByKey(ctx, workspace.ID, project.Key)
	if err != nil {
		t.Fatalf("GetInitiativeByKey(alpha) error = %v", err)
	}
	if initiative.Kind != "managed_project" {
		t.Fatalf("initiative.Kind = %q, want %q", initiative.Kind, "managed_project")
	}
	if initiative.Title != project.Name {
		t.Fatalf("initiative.Title = %q, want %q", initiative.Title, project.Name)
	}
	if initiative.LinkedProjectID == nil || *initiative.LinkedProjectID != project.ID {
		t.Fatalf("initiative.LinkedProjectID = %v, want %d", initiative.LinkedProjectID, project.ID)
	}
}

func TestMigrateBackfillsManagedProjectInitiativesWithoutPreseededDefaultWorkspace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openMigrationBackfillStore(t)
	defer store.Close()

	for _, version := range []int{1, 11} {
		migration, err := loadMigrationByVersion(version)
		if err != nil {
			t.Fatalf("loadMigrationByVersion(%d) error = %v", version, err)
		}
		if err := store.applyMigration(ctx, migration); err != nil {
			t.Fatalf("applyMigration(%d) error = %v", version, err)
		}
	}

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       filepath.Join(t.TempDir(), "alpha"),
		DefaultBranch: "main",
		GitHubRepo:    "acme/alpha",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	workspace, err := store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}
	if workspace.PolicyJSON != `{}` {
		t.Fatalf("workspace.PolicyJSON = %q, want %q", workspace.PolicyJSON, `{}`)
	}

	initiative, err := store.GetInitiativeByKey(ctx, workspace.ID, project.Key)
	if err != nil {
		t.Fatalf("GetInitiativeByKey(alpha) error = %v", err)
	}
	if initiative.Kind != "managed_project" {
		t.Fatalf("initiative.Kind = %q, want %q", initiative.Kind, "managed_project")
	}
	if initiative.Title != project.Name {
		t.Fatalf("initiative.Title = %q, want %q", initiative.Title, project.Name)
	}
	if initiative.LinkedProjectID == nil || *initiative.LinkedProjectID != project.ID {
		t.Fatalf("initiative.LinkedProjectID = %v, want %d", initiative.LinkedProjectID, project.ID)
	}
}

func TestMigrateDeduplicatesPendingApprovalsBeforeAddingUniquenessIndex(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openMigrationBackfillStore(t)
	defer store.Close()

	for version := 1; version <= 14; version++ {
		migration, err := loadMigrationByVersion(version)
		if err != nil {
			t.Fatalf("loadMigrationByVersion(%d) error = %v", version, err)
		}
		if err := store.applyMigration(ctx, migration); err != nil {
			t.Fatalf("applyMigration(%d) error = %v", version, err)
		}
	}

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       filepath.Join(t.TempDir(), "alpha"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "approval-task",
		Title:       "Await approval",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO approvals (task_id, run_id, status, requested_at, resolved_at, decision_by, reason)
		VALUES
		  (?, NULL, 'pending', '2026-04-09T12:00:00.000Z', NULL, '', ''),
		  (?, NULL, 'pending', '2026-04-09T12:05:00.000Z', NULL, '', '')
	`, task.ID, task.ID); err != nil {
		t.Fatalf("seed duplicate approvals error = %v", err)
	}

	if err := store.applyMigration(ctx, mustLoadMigrationByVersion(t, 15)); err != nil {
		t.Fatalf("applyMigration(15) error = %v", err)
	}

	var approvalCount int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM approvals
		WHERE task_id = ?
	`, task.ID).Scan(&approvalCount); err != nil {
		t.Fatalf("count approvals error = %v", err)
	}
	if approvalCount != 2 {
		t.Fatalf("approval count = %d, want 2", approvalCount)
	}

	var pendingCount int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM approvals
		WHERE task_id = ? AND status = 'pending'
	`, task.ID).Scan(&pendingCount); err != nil {
		t.Fatalf("count pending approvals error = %v", err)
	}
	if pendingCount != 1 {
		t.Fatalf("pending approval count = %d, want 1", pendingCount)
	}
}

func TestProfileMigrationCreatesWorkspaceProfileTable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	var tableName string
	if err := store.DB().QueryRowContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type = 'table' AND name = 'workspace_profile'
	`).Scan(&tableName); err != nil {
		t.Fatalf("workspace_profile table query error = %v", err)
	}

	var migrationCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = 17`).Scan(&migrationCount); err != nil {
		t.Fatalf("schema_migrations query error = %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("schema_migrations version 17 count = %d, want 1", migrationCount)
	}
}

func TestFollowUpMigrationCreatesObligationTableAndTaskProvenance(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	var tableName string
	if err := store.DB().QueryRowContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type = 'table' AND name = 'follow_up_obligations'
	`).Scan(&tableName); err != nil {
		t.Fatalf("follow_up_obligations table query error = %v", err)
	}

	taskColumns, err := taskColumnNames(ctx, store)
	if err != nil {
		t.Fatalf("taskColumnNames() error = %v", err)
	}
	for _, want := range []string{"follow_up_obligation_id", "follow_up_occurrence_key"} {
		if !containsTaskColumn(taskColumns, want) {
			t.Fatalf("tasks columns = %v, want %q", taskColumns, want)
		}
	}

	var migrationCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = 18`).Scan(&migrationCount); err != nil {
		t.Fatalf("schema_migrations query error = %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("schema_migrations version 18 count = %d, want 1", migrationCount)
	}
}

func TestFollowUpTargetProjectMigrationCreatesTargetProjectColumn(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	var columnName string
	if err := store.DB().QueryRowContext(ctx, `
		SELECT name
		FROM pragma_table_info('follow_up_obligations')
		WHERE name = 'target_project_id'
	`).Scan(&columnName); err != nil {
		t.Fatalf("target_project_id column query error = %v", err)
	}

	var migrationCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = 19`).Scan(&migrationCount); err != nil {
		t.Fatalf("schema_migrations query error = %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("schema_migrations version 19 count = %d, want 1", migrationCount)
	}
}

func TestCreateTaskFailsClosedWhenFollowUpColumnsAreMissing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openMigrationBackfillStore(t)
	defer store.Close()

	for version := 1; version <= 17; version++ {
		migration, err := loadMigrationByVersion(version)
		if err != nil {
			t.Fatalf("loadMigrationByVersion(%d) error = %v", version, err)
		}
		if err := store.applyMigration(ctx, migration); err != nil {
			t.Fatalf("applyMigration(%d) error = %v", version, err)
		}
	}

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       filepath.Join(t.TempDir(), "alpha"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	obligationID := int64(42)

	_, err = store.CreateTask(ctx, CreateTaskParams{
		ProjectID:             project.ID,
		Key:                   "follow-up-item",
		Title:                 "Create follow-up task",
		Status:                "queued",
		Scope:                 "project",
		RequestedBy:           "operator",
		FollowUpObligationID:  &obligationID,
		FollowUpOccurrenceKey: "2026-04-18T09:00:00Z",
	})
	if err == nil {
		t.Fatal("CreateTask() error = nil, want fail-closed provenance error")
	}

	var taskCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE key = ?`, "follow-up-item").Scan(&taskCount); err != nil {
		t.Fatalf("task count query error = %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("task count = %d, want 0", taskCount)
	}
}

func openMigrationBackfillStore(t *testing.T) *Store {
	t.Helper()

	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TEXT NOT NULL
		)
	`); err != nil {
		t.Fatalf("create schema_migrations error = %v", err)
	}
	return store
}

func mustLoadMigrationByVersion(t *testing.T, version int) migration {
	t.Helper()

	migration, err := loadMigrationByVersion(version)
	if err != nil {
		t.Fatalf("loadMigrationByVersion(%d) error = %v", version, err)
	}
	return migration
}

func taskColumnNames(ctx context.Context, store *Store) ([]string, error) {
	rows, err := store.DB().QueryContext(ctx, `PRAGMA table_info(tasks)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var (
			cid        int
			name       string
			typ        string
			notNull    int
			defaultVal any
			pk         int
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultVal, &pk); err != nil {
			return nil, err
		}
		columns = append(columns, name)
	}
	return columns, rows.Err()
}

func containsTaskColumn(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
