package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"odin-os/internal/core/companions"
	"odin-os/internal/core/initiatives"
	"odin-os/internal/core/workspaces"
	"odin-os/internal/store/sqlite"
)

func main() {
	runtimeRoot := flag.String("runtime-root", defaultRuntimeRoot(), "Odin runtime root containing data/odin.db")
	flag.Parse()

	dbPath := filepath.Join(*runtimeRoot, "data", "odin.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Fatal(err)
	}

	store, err := sqlite.Open(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	if err := store.Migrate(context.Background()); err != nil {
		log.Fatal(err)
	}

	report, err := bootstrapWorkspaceRuntimeState(context.Background(), store)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("workspace_id: %d\n", report.WorkspaceID)
	fmt.Printf("default_companion_id: %d\n", report.DefaultCompanionID)
	fmt.Printf("projects_reconciled: %d\n", report.ProjectsReconciled)
	fmt.Printf("tasks_bound_to_workspace: %d\n", report.TasksBoundToWorkspace)
	fmt.Printf("tasks_linked_to_initiative: %d\n", report.TasksLinkedToInitiative)
	fmt.Printf("tasks_bound_to_companion: %d\n", report.TasksBoundToCompanion)
	fmt.Printf("tasks_backfilled_work_kind: %d\n", report.TasksBackfilledWorkKind)
}

func defaultRuntimeRoot() string {
	if root := os.Getenv("ODIN_ROOT"); root != "" {
		return root
	}
	return "."
}

type workspaceBootstrapReport struct {
	WorkspaceID             int64
	DefaultCompanionID      int64
	ProjectsReconciled      int64
	TasksBoundToWorkspace   int64
	TasksLinkedToInitiative int64
	TasksBoundToCompanion   int64
	TasksBackfilledWorkKind int64
}

func bootstrapWorkspaceRuntimeState(ctx context.Context, store *sqlite.Store) (workspaceBootstrapReport, error) {
	workspace, err := workspaces.Service{Store: store}.BootstrapDefaultWorkspace(ctx)
	if err != nil {
		return workspaceBootstrapReport{}, err
	}
	companion, err := companions.Service{Store: store}.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		return workspaceBootstrapReport{}, err
	}

	report := workspaceBootstrapReport{
		WorkspaceID:        workspace.ID,
		DefaultCompanionID: companion.ID,
	}

	rows, err := store.DB().QueryContext(ctx, `SELECT id FROM projects ORDER BY id ASC`)
	if err != nil {
		return workspaceBootstrapReport{}, err
	}
	defer rows.Close()

	for rows.Next() {
		var projectID int64
		if err := rows.Scan(&projectID); err != nil {
			return workspaceBootstrapReport{}, err
		}

		project, err := store.GetProject(ctx, projectID)
		if err != nil {
			return workspaceBootstrapReport{}, err
		}

		ownerCompanionID, err := initiativeOwnerCompanionID(ctx, store, workspace.ID, project.Key, companion.ID)
		if err != nil {
			return workspaceBootstrapReport{}, err
		}
		initiative, err := initiatives.Service{Store: store}.ReconcileManagedProject(ctx, workspace.ID, project, ownerCompanionID)
		if err != nil {
			return workspaceBootstrapReport{}, err
		}
		report.ProjectsReconciled++

		boundWorkspace, err := execCount(ctx, store, `
			UPDATE tasks
			SET workspace_id = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE project_id = ? AND workspace_id IS NULL
		`, workspace.ID, project.ID)
		if err != nil {
			return workspaceBootstrapReport{}, err
		}
		report.TasksBoundToWorkspace += boundWorkspace

		linkedInitiative, err := execCount(ctx, store, `
			UPDATE tasks
			SET initiative_id = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE project_id = ? AND initiative_id IS NULL
		`, initiative.ID, project.ID)
		if err != nil {
			return workspaceBootstrapReport{}, err
		}
		report.TasksLinkedToInitiative += linkedInitiative

		if initiative.OwnerCompanionID != nil {
			boundCompanion, err := execCount(ctx, store, `
				UPDATE tasks
				SET companion_id = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				WHERE project_id = ? AND initiative_id = ? AND companion_id IS NULL
			`, *initiative.OwnerCompanionID, project.ID, initiative.ID)
			if err != nil {
				return workspaceBootstrapReport{}, err
			}
			report.TasksBoundToCompanion += boundCompanion
		}

		backfilledWorkKind, err := execCount(ctx, store, `
			UPDATE tasks
			SET work_kind = CASE
				WHEN scope = 'new-project' THEN 'new-project'
				WHEN scope = 'odin-core' THEN 'odin-core'
				ELSE 'project'
			END,
			updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE project_id = ? AND (work_kind IS NULL OR TRIM(work_kind) = '')
		`, project.ID)
		if err != nil {
			return workspaceBootstrapReport{}, err
		}
		report.TasksBackfilledWorkKind += backfilledWorkKind
	}
	if err := rows.Err(); err != nil {
		return workspaceBootstrapReport{}, err
	}

	return report, nil
}

func initiativeOwnerCompanionID(ctx context.Context, store *sqlite.Store, workspaceID int64, initiativeKey string, defaultCompanionID int64) (*int64, error) {
	initiative, err := store.GetInitiativeByKey(ctx, workspaceID, initiativeKey)
	switch {
	case err == nil:
		if initiative.OwnerCompanionID != nil {
			return initiative.OwnerCompanionID, nil
		}
	case err == sql.ErrNoRows:
	default:
		return nil, err
	}

	return &defaultCompanionID, nil
}

func execCount(ctx context.Context, store *sqlite.Store, query string, args ...any) (int64, error) {
	result, err := store.DB().ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
