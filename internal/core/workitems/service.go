package workitems

import (
	"context"
	"fmt"

	"odin-os/internal/store/sqlite"
)

const (
	statusQueued          = "queued"
	statusRunning         = "running"
	statusBlocked         = "blocked"
	statusCompleted       = "completed"
	statusFailed          = "failed"
	statusApprovalPending = "pending"
)

type Service struct {
	Store *sqlite.Store
}

func (service Service) Queue(ctx context.Context, params sqlite.CreateTaskParams) (sqlite.Task, error) {
	if service.Store == nil {
		return sqlite.Task{}, fmt.Errorf("work item store is required")
	}
	if params.Status == "" {
		params.Status = statusQueued
	}

	task, err := service.Store.CreateTask(ctx, params)
	if err != nil {
		return sqlite.Task{}, err
	}
	return task, nil
}

func (service Service) Get(ctx context.Context, taskID int64) (WorkItem, error) {
	if service.Store == nil {
		return WorkItem{}, fmt.Errorf("work item store is required")
	}

	task, err := service.Store.GetTask(ctx, taskID)
	if err != nil {
		return WorkItem{}, err
	}
	return toDomainWorkItem(task), nil
}

func (service Service) Start(ctx context.Context, taskID int64) (sqlite.Task, error) {
	return service.setStatus(ctx, taskID, statusRunning)
}

func (service Service) Block(ctx context.Context, taskID int64) (sqlite.Task, error) {
	return service.setStatus(ctx, taskID, statusBlocked)
}

func (service Service) Complete(ctx context.Context, taskID int64) (sqlite.Task, error) {
	return service.setStatus(ctx, taskID, statusCompleted)
}

func (service Service) Fail(ctx context.Context, taskID int64) (sqlite.Task, error) {
	return service.setStatus(ctx, taskID, statusFailed)
}

func (service Service) RequestApproval(ctx context.Context, taskID int64, runID *int64, requestedBy string) (sqlite.Approval, WorkItem, error) {
	if _, err := service.Block(ctx, taskID); err != nil {
		return sqlite.Approval{}, WorkItem{}, err
	}

	if service.Store == nil {
		return sqlite.Approval{}, WorkItem{}, fmt.Errorf("work item store is required")
	}

	approval, err := service.Store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      taskID,
		RunID:       runID,
		Status:      statusApprovalPending,
		RequestedBy: requestedBy,
	})
	if err != nil {
		return sqlite.Approval{}, WorkItem{}, err
	}

	item, err := service.Get(ctx, taskID)
	if err != nil {
		return sqlite.Approval{}, WorkItem{}, err
	}

	return approval, item, nil
}

func (service Service) setStatus(ctx context.Context, taskID int64, status string) (sqlite.Task, error) {
	if service.Store == nil {
		return sqlite.Task{}, fmt.Errorf("work item store is required")
	}

	task, err := service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID: taskID,
		Status: status,
	})
	if err != nil {
		return sqlite.Task{}, err
	}

	return task, nil
}

func toDomainWorkItem(task sqlite.Task) WorkItem {
	item := WorkItem{
		ID:           task.ID,
		Key:          task.Key,
		WorkspaceID:  0,
		InitiativeID: task.InitiativeID,
		CompanionID:  task.CompanionID,
		WorkKind:     task.WorkKind,
		Status:       task.Status,
	}
	if task.WorkspaceID != nil {
		item.WorkspaceID = *task.WorkspaceID
	}
	return item
}
