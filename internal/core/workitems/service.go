package workitems

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"odin-os/internal/core/workspaces"
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

type QueueFollowUpParams struct {
	CreateTask           sqlite.CreateTaskParams
	FollowUpObligationID int64
	OccurrenceKey        string
}

func (service Service) Queue(ctx context.Context, params sqlite.CreateTaskParams) (sqlite.Task, error) {
	if service.Store == nil {
		return sqlite.Task{}, fmt.Errorf("work item store is required")
	}
	params.Status = statusQueued
	repairedParams, err := service.ensureCreateTaskWorkspace(ctx, params)
	if err != nil {
		return sqlite.Task{}, err
	}

	task, err := service.Store.CreateTask(ctx, repairedParams)
	if err != nil {
		return sqlite.Task{}, err
	}
	return task, nil
}

func (service Service) QueueFollowUp(ctx context.Context, params QueueFollowUpParams) (sqlite.Task, bool, error) {
	if service.Store == nil {
		return sqlite.Task{}, false, fmt.Errorf("work item store is required")
	}
	if params.FollowUpObligationID <= 0 {
		return sqlite.Task{}, false, fmt.Errorf("follow-up obligation ID is required")
	}
	occurrenceKey := strings.TrimSpace(params.OccurrenceKey)
	if occurrenceKey == "" {
		return sqlite.Task{}, false, fmt.Errorf("follow-up occurrence key is required")
	}

	if task, err := service.Store.GetTaskByFollowUpOccurrence(ctx, params.FollowUpObligationID, occurrenceKey); err == nil {
		return task, true, nil
	} else if !errorsIsNotFound(err) {
		return sqlite.Task{}, false, err
	}

	createTask := params.CreateTask
	createTask.FollowUpObligationID = int64Ptr(params.FollowUpObligationID)
	createTask.FollowUpOccurrenceKey = occurrenceKey
	if strings.TrimSpace(createTask.WorkKind) == "" {
		createTask.WorkKind = "follow_up"
	}
	status := strings.TrimSpace(createTask.Status)
	if status == "" {
		status = statusQueued
	}
	createTask.Status = status

	var (
		task sqlite.Task
		err  error
	)
	if status == statusQueued {
		task, err = service.Queue(ctx, createTask)
	} else {
		createTask, err = service.ensureCreateTaskWorkspace(ctx, createTask)
		if err == nil {
			task, err = service.Store.CreateTask(ctx, createTask)
		}
	}
	if err != nil {
		if existing, lookupErr := service.Store.GetTaskByFollowUpOccurrence(ctx, params.FollowUpObligationID, occurrenceKey); lookupErr == nil {
			return existing, true, nil
		}
		return sqlite.Task{}, false, err
	}
	return task, false, nil
}

func (service Service) Get(ctx context.Context, taskID int64) (WorkItem, error) {
	if service.Store == nil {
		return WorkItem{}, fmt.Errorf("work item store is required")
	}

	task, err := service.Store.GetTask(ctx, taskID)
	if err != nil {
		return WorkItem{}, err
	}
	task, err = service.ensureTaskWorkspace(ctx, task)
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
	if service.Store == nil {
		return sqlite.Task{}, fmt.Errorf("work item store is required")
	}
	current, err := service.Store.GetTask(ctx, taskID)
	if err != nil {
		return sqlite.Task{}, err
	}
	if current.Status == statusBlocked {
		pendingApprovals, err := service.pendingApprovalCount(ctx, taskID)
		if err != nil {
			return sqlite.Task{}, err
		}
		if pendingApprovals > 0 {
			return sqlite.Task{}, fmt.Errorf("task %d still has pending approval", taskID)
		}
	}
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
	current, err = service.ensureTaskWorkspace(ctx, current)
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
	current, err := service.Store.GetTask(ctx, taskID)
	if err != nil {
		return sqlite.Task{}, err
	}
	if _, err := service.ensureTaskWorkspace(ctx, current); err != nil {
		return sqlite.Task{}, err
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
	var workspaceID int64
	if task.WorkspaceID != nil {
		workspaceID = *task.WorkspaceID
	}
	item := WorkItem{
		ID:           task.ID,
		Key:          task.Key,
		WorkspaceID:  workspaceID,
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

func (service Service) ensureCreateTaskWorkspace(ctx context.Context, params sqlite.CreateTaskParams) (sqlite.CreateTaskParams, error) {
	if params.WorkspaceID != nil {
		return params, nil
	}
	workspace, err := workspaces.Service{Store: service.Store}.BootstrapDefaultWorkspace(ctx)
	if err != nil {
		return sqlite.CreateTaskParams{}, err
	}
	params.WorkspaceID = &workspace.ID
	return params, nil
}

func (service Service) ensureTaskWorkspace(ctx context.Context, task sqlite.Task) (sqlite.Task, error) {
	if task.WorkspaceID != nil {
		return task, nil
	}

	workspace, err := workspaces.Service{Store: service.Store}.BootstrapDefaultWorkspace(ctx)
	if err != nil {
		return sqlite.Task{}, err
	}
	return service.Store.AssignTaskWorkspace(ctx, task.ID, workspace.ID)
}

func (service Service) pendingApprovalCount(ctx context.Context, taskID int64) (int, error) {
	row := service.Store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM approvals
		WHERE task_id = ? AND status = 'pending'
	`, taskID)

	var count int
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func errorsIsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func int64Ptr(value int64) *int64 {
	return &value
}
