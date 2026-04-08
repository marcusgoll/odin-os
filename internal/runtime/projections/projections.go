package projections

import (
	"context"
	"database/sql"
	"fmt"

	runtimeevents "odin-os/internal/runtime/events"
)

type Queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type TaskStatusView struct {
	TaskID           int64
	ProjectID        int64
	ProjectKey       string
	TaskKey          string
	Title            string
	Status           string
	Scope            string
	CurrentRunID     *int64
	CurrentRunStatus string
}

type RunSummaryView struct {
	RunID      int64
	TaskID     int64
	TaskKey    string
	Executor   string
	Status     string
	Attempt    int
	StartedAt  string
	FinishedAt *string
}

type PendingApprovalView struct {
	ApprovalID  int64
	TaskID      int64
	TaskKey     string
	Status      string
	RequestedAt string
}

type ProjectTransitionView struct {
	ProjectID     int64
	ProjectKey    string
	Name          string
	Scope         string
	TaskCount     int
	OpenTaskCount int
	LastEventAt   *string
}

func ListTaskStatusViews(ctx context.Context, queryer Queryer) ([]TaskStatusView, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT
			t.id,
			p.id,
			p.key,
			t.key,
			t.title,
			t.status,
			t.scope,
			t.current_run_id,
			COALESCE(r.status, '')
		FROM tasks t
		JOIN projects p ON p.id = t.project_id
		LEFT JOIN runs r ON r.id = t.current_run_id
		ORDER BY t.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var views []TaskStatusView
	for rows.Next() {
		var view TaskStatusView
		var currentRunID sql.NullInt64
		if err := rows.Scan(
			&view.TaskID,
			&view.ProjectID,
			&view.ProjectKey,
			&view.TaskKey,
			&view.Title,
			&view.Status,
			&view.Scope,
			&currentRunID,
			&view.CurrentRunStatus,
		); err != nil {
			return nil, err
		}
		view.CurrentRunID = nullableInt64Ptr(currentRunID)
		views = append(views, view)
	}

	return views, rows.Err()
}

func ListRunSummaryViews(ctx context.Context, queryer Queryer) ([]RunSummaryView, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT
			r.id,
			r.task_id,
			t.key,
			r.executor,
			r.status,
			r.attempt,
			r.started_at,
			r.finished_at
		FROM runs r
		JOIN tasks t ON t.id = r.task_id
		ORDER BY r.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var views []RunSummaryView
	for rows.Next() {
		var view RunSummaryView
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
		); err != nil {
			return nil, err
		}
		if finishedAt.Valid {
			view.FinishedAt = &finishedAt.String
		}
		views = append(views, view)
	}

	return views, rows.Err()
}

func ListPendingApprovalViews(ctx context.Context, queryer Queryer) ([]PendingApprovalView, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT
			a.id,
			a.task_id,
			t.key,
			a.status,
			a.requested_at
		FROM approvals a
		JOIN tasks t ON t.id = a.task_id
		WHERE a.status = 'pending'
		ORDER BY a.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var views []PendingApprovalView
	for rows.Next() {
		var view PendingApprovalView
		if err := rows.Scan(
			&view.ApprovalID,
			&view.TaskID,
			&view.TaskKey,
			&view.Status,
			&view.RequestedAt,
		); err != nil {
			return nil, err
		}
		views = append(views, view)
	}

	return views, rows.Err()
}

func ListProjectTransitionViews(ctx context.Context, queryer Queryer) ([]ProjectTransitionView, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT
			p.id,
			p.key,
			p.name,
			p.scope,
			COUNT(DISTINCT t.id),
			COUNT(DISTINCT CASE WHEN t.status NOT IN ('completed', 'cancelled') THEN t.id END),
			MAX(e.occurred_at)
		FROM projects p
		LEFT JOIN tasks t ON t.project_id = p.id
		LEFT JOIN events e ON e.project_id = p.id
		GROUP BY p.id, p.key, p.name, p.scope
		ORDER BY p.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var views []ProjectTransitionView
	for rows.Next() {
		var view ProjectTransitionView
		var lastEventAt sql.NullString
		if err := rows.Scan(
			&view.ProjectID,
			&view.ProjectKey,
			&view.Name,
			&view.Scope,
			&view.TaskCount,
			&view.OpenTaskCount,
			&lastEventAt,
		); err != nil {
			return nil, err
		}
		if lastEventAt.Valid {
			view.LastEventAt = &lastEventAt.String
		}
		views = append(views, view)
	}

	return views, rows.Err()
}

type LifecycleReplay struct {
	Tasks     map[int64]TaskReplay
	Runs      map[int64]RunReplay
	Approvals map[int64]ApprovalReplay
}

type TaskReplay struct {
	ID           int64
	Key          string
	Title        string
	Status       string
	Scope        string
	CurrentRunID *int64
}

type RunReplay struct {
	ID       int64
	TaskID   int64
	Executor string
	Attempt  int
	Status   string
	Summary  string
}

type ApprovalReplay struct {
	ID          int64
	TaskID      int64
	RunID       *int64
	Status      string
	RequestedBy string
	DecisionBy  string
	Reason      string
}

func ReplayLifecycle(records []runtimeevents.Record) (LifecycleReplay, error) {
	replay := LifecycleReplay{
		Tasks:     make(map[int64]TaskReplay),
		Runs:      make(map[int64]RunReplay),
		Approvals: make(map[int64]ApprovalReplay),
	}

	for _, record := range records {
		switch record.Type {
		case runtimeevents.EventTaskCreated:
			payload, err := runtimeevents.DecodePayload[runtimeevents.TaskCreatedPayload](record.Payload)
			if err != nil {
				return LifecycleReplay{}, fmt.Errorf("decode %s payload: %w", record.Type, err)
			}
			replay.Tasks[record.StreamID] = TaskReplay{
				ID:     record.StreamID,
				Key:    payload.Key,
				Title:  payload.Title,
				Status: payload.Status,
				Scope:  payload.Scope,
			}
		case runtimeevents.EventTaskStatusChanged:
			payload, err := runtimeevents.DecodePayload[runtimeevents.TaskStatusChangedPayload](record.Payload)
			if err != nil {
				return LifecycleReplay{}, fmt.Errorf("decode %s payload: %w", record.Type, err)
			}
			task := replay.Tasks[record.StreamID]
			task.ID = record.StreamID
			task.Status = payload.Status
			if record.RunID != nil {
				task.CurrentRunID = record.RunID
			}
			replay.Tasks[record.StreamID] = task
		case runtimeevents.EventRunStarted:
			payload, err := runtimeevents.DecodePayload[runtimeevents.RunStartedPayload](record.Payload)
			if err != nil {
				return LifecycleReplay{}, fmt.Errorf("decode %s payload: %w", record.Type, err)
			}
			replay.Runs[record.StreamID] = RunReplay{
				ID:       record.StreamID,
				TaskID:   payload.TaskID,
				Executor: payload.Executor,
				Attempt:  payload.Attempt,
				Status:   payload.Status,
			}
			task := replay.Tasks[payload.TaskID]
			task.ID = payload.TaskID
			runID := record.StreamID
			task.CurrentRunID = &runID
			replay.Tasks[payload.TaskID] = task
		case runtimeevents.EventRunFinished:
			payload, err := runtimeevents.DecodePayload[runtimeevents.RunFinishedPayload](record.Payload)
			if err != nil {
				return LifecycleReplay{}, fmt.Errorf("decode %s payload: %w", record.Type, err)
			}
			run := replay.Runs[record.StreamID]
			run.ID = record.StreamID
			run.Status = payload.Status
			run.Summary = payload.Summary
			replay.Runs[record.StreamID] = run
		case runtimeevents.EventApprovalRequested:
			payload, err := runtimeevents.DecodePayload[runtimeevents.ApprovalRequestedPayload](record.Payload)
			if err != nil {
				return LifecycleReplay{}, fmt.Errorf("decode %s payload: %w", record.Type, err)
			}
			replay.Approvals[record.StreamID] = ApprovalReplay{
				ID:          record.StreamID,
				TaskID:      payload.TaskID,
				RunID:       payload.RunID,
				Status:      payload.Status,
				RequestedBy: payload.RequestedBy,
			}
		case runtimeevents.EventApprovalResolved:
			payload, err := runtimeevents.DecodePayload[runtimeevents.ApprovalResolvedPayload](record.Payload)
			if err != nil {
				return LifecycleReplay{}, fmt.Errorf("decode %s payload: %w", record.Type, err)
			}
			approval := replay.Approvals[record.StreamID]
			approval.ID = record.StreamID
			approval.Status = payload.Status
			approval.DecisionBy = payload.DecisionBy
			approval.Reason = payload.Reason
			replay.Approvals[record.StreamID] = approval
		}
	}

	return replay, nil
}

func nullableInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	ptr := new(int64)
	*ptr = value.Int64
	return ptr
}
