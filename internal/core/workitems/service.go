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
	params.Status = statusQueued

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
	return service.transitionStatus(ctx, taskID, statusRunning, statusQueued)
}

func (service Service) Block(ctx context.Context, taskID int64) (sqlite.Task, error) {
	return service.transitionStatus(ctx, taskID, statusBlocked, statusQueued, statusRunning)
}

func (service Service) Requeue(ctx context.Context, taskID int64) (sqlite.Task, error) {
	return service.transitionStatus(ctx, taskID, statusQueued, statusRunning, statusBlocked)
}

func (service Service) Complete(ctx context.Context, taskID int64) (sqlite.Task, error) {
	return service.transitionStatus(ctx, taskID, statusCompleted, statusQueued, statusRunning)
}

func (service Service) Fail(ctx context.Context, taskID int64) (sqlite.Task, error) {
	return service.transitionStatus(ctx, taskID, statusFailed, statusQueued, statusRunning)
}

func (service Service) Finalize(ctx context.Context, taskID int64, executorStatus string) (sqlite.Task, error) {
	if executorStatus == "" || executorStatus == statusCompleted {
		return service.transitionStatus(ctx, taskID, statusCompleted, statusRunning)
	}
	return service.transitionStatus(ctx, taskID, statusFailed, statusRunning)
}

func (service Service) RequestApproval(ctx context.Context, taskID int64, runID *int64, requestedBy string) (sqlite.Approval, WorkItem, error) {
	if service.Store == nil {
		return sqlite.Approval{}, WorkItem{}, fmt.Errorf("work item store is required")
	}

	current, err := service.Store.GetTask(ctx, taskID)
	if err != nil {
		return sqlite.Approval{}, WorkItem{}, err
	}
	if isTerminalStatus(current.Status) {
		return sqlite.Approval{}, WorkItem{}, fmt.Errorf("task %d is already %s", taskID, current.Status)
	}
	if current.Status == statusBlocked {
		return sqlite.Approval{}, WorkItem{}, fmt.Errorf("task %d is already %s", taskID, current.Status)
	}

	task, approval, err := service.Store.BlockTaskAndRequestApproval(ctx, sqlite.BlockTaskAndRequestApprovalParams{
		TaskID:      taskID,
		RunID:       runID,
		RequestedBy: requestedBy,
	})
	if err != nil {
		return sqlite.Approval{}, WorkItem{}, err
	}

	return approval, toDomainWorkItem(task), nil
}

func (service Service) transitionStatus(ctx context.Context, taskID int64, status string, allowedCurrentStatuses ...string) (sqlite.Task, error) {
	if service.Store == nil {
		return sqlite.Task{}, fmt.Errorf("work item store is required")
	}

	task, err := service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID:                 taskID,
		Status:                 status,
		AllowedCurrentStatuses: allowedCurrentStatuses,
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
		WorkspaceID:  task.WorkspaceID,
		InitiativeID: task.InitiativeID,
		CompanionID:  task.CompanionID,
		WorkKind:     task.WorkKind,
		Status:       task.Status,
	}
	return item
}

func isTerminalStatus(status string) bool {
	return status == statusCompleted || status == statusFailed
}
