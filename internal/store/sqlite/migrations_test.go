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
