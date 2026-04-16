package runs

import (
	"context"
	"database/sql"

	"odin-os/internal/cli/scope"
	"odin-os/internal/runtime/projections"
)

type Service struct {
	DB *sql.DB
}

func (service Service) List(ctx context.Context, resolved scope.Resolution) ([]projections.RunSummaryView, error) {
	if service.DB == nil {
		return nil, sql.ErrConnDone
	}

	rows, err := service.DB.QueryContext(ctx, `
		SELECT
			r.id,
			r.task_id,
			t.key,
			r.executor,
			r.status,
			r.attempt,
			r.started_at,
			r.finished_at,
			p.key,
			t.scope
		FROM runs r
		JOIN tasks t ON t.id = r.task_id
		JOIN projects p ON p.id = t.project_id
		ORDER BY r.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var views []projections.RunSummaryView
	for rows.Next() {
		var view projections.RunSummaryView
		var projectKey string
		var taskScope string
		var finishedAt sql.NullString
		if err := rows.Scan(
			&view.RunID,
			&view.TaskID,
			&view.TaskKey,
			&view.Executor,
			&view.Status,
			&view.Attempt,
			&view.StartedAt,
			&finishedAt,
			&projectKey,
			&taskScope,
		); err != nil {
			return nil, err
		}
		if finishedAt.Valid {
			view.FinishedAt = &finishedAt.String
		}
		if matchesRunScope(projectKey, taskScope, resolved) {
			views = append(views, view)
		}
	}

	return views, rows.Err()
}

func matchesRunScope(projectKey, taskScope string, resolved scope.Resolution) bool {
	switch resolved.Kind {
	case scope.ScopeGlobal:
		return true
	case scope.ScopeNewProject:
		return taskScope == string(scope.ScopeNewProject)
	case scope.ScopeProject, scope.ScopeOdinCore:
		return projectKey == resolved.ProjectKey
	default:
		return false
	}
}
