package supervision

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	runtimejobs "odin-os/internal/runtime/jobs"
	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store             *sqlite.Store
	Jobs              runtimejobs.Service
	Now               func() time.Time
	ShutdownRequested *atomic.Bool
}

type TickResult struct {
	Promoted   int
	Reconciled int
}

func (service Service) Tick(ctx context.Context) (TickResult, error) {
	if service.ShutdownRequested != nil && service.ShutdownRequested.Load() {
		return TickResult{}, nil
	}
	if service.Store == nil {
		return TickResult{}, fmt.Errorf("scheduler store is required")
	}

	now := time.Now().UTC()
	if service.Now != nil {
		now = service.Now().UTC()
	}

	result := TickResult{}
	reconciled, err := service.reconcileSwarms(ctx)
	if err != nil {
		return TickResult{}, err
	}
	result.Reconciled = reconciled

	tasks, err := service.Store.ListEligibleQueuedTasks(ctx, now)
	if err != nil {
		return TickResult{}, err
	}
	for _, task := range tasks {
		if task.NextEligibleAt.IsZero() {
			continue
		}
		if task.NextEligibleAt.After(now) {
			continue
		}

		result.Promoted++
	}

	return result, nil
}

func (service Service) reconcileSwarms(ctx context.Context) (int, error) {
	delegations, err := service.Store.ListDelegations(ctx, sqlite.ListDelegationsParams{})
	if err != nil {
		return 0, err
	}
	if len(delegations) == 0 {
		return 0, nil
	}

	parentIDs := make(map[int64]struct{}, len(delegations))
	reconciled := 0
	for _, delegation := range delegations {
		parentIDs[delegation.ParentTaskID] = struct{}{}
	}
	for parentTaskID := range parentIDs {
		changed, err := service.reconcileSwarmParent(ctx, parentTaskID)
		if err != nil {
			return reconciled, err
		}
		if changed {
			reconciled++
		}
	}
	return reconciled, nil
}

func (service Service) reconcileSwarmParent(ctx context.Context, parentTaskID int64) (bool, error) {
	parentTask, err := service.Store.GetTask(ctx, parentTaskID)
	if err != nil {
		return false, err
	}
	delegations, err := service.Store.ListDelegations(ctx, sqlite.ListDelegationsParams{
		ParentTaskID: &parentTaskID,
	})
	if err != nil {
		return false, err
	}
	if len(delegations) == 0 {
		return false, nil
	}

	changed := false
	for _, delegation := range delegations {
		delegationChanged, err := service.reconcileDelegation(ctx, parentTask, delegation)
		if err != nil {
			return changed, err
		}
		if delegationChanged {
			changed = true
		}
	}

	shouldAggregate, err := service.swarmReadyForAggregation(ctx, parentTask, delegations)
	if err != nil {
		return changed, err
	}
	if !shouldAggregate {
		return changed, nil
	}

	if _, err := service.AggregateSwarm(ctx, parentTaskID); err != nil {
		_, updateErr := service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
			TaskID:                 parentTaskID,
			Status:                 "failed",
			Summary:                fmt.Sprintf("Swarm aggregation failed: %v", err),
			TerminalReason:         "swarm_aggregation_failed",
			AllowedCurrentStatuses: []string{"queued", "running", "blocked"},
		})
		if updateErr != nil {
			return changed, updateErr
		}
		return true, nil
	}
	return true, nil
}

func (service Service) reconcileDelegation(ctx context.Context, parentTask sqlite.Task, delegation sqlite.Delegation) (bool, error) {
	if delegation.ChildTaskID == nil {
		return false, nil
	}
	childTask, err := service.Store.GetTask(ctx, *delegation.ChildTaskID)
	if err != nil {
		return false, err
	}

	changed := false
	targetStatus := delegation.Status

	switch parentTask.BlockedReason {
	case "approval_required":
		if childTask.Status == "queued" {
			childTask, err = service.Store.BlockTask(ctx, sqlite.BlockTaskParams{
				TaskID: childTask.ID,
				Reason: "approval_required",
			})
			if err != nil {
				return changed, err
			}
			changed = true
		}
		if childTask.Status == "blocked" {
			targetStatus = "blocked"
		}
	case "budget_exhausted":
		if childTask.Status != "completed" && childTask.Status != "failed" {
			childTask, err = service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
				TaskID:                 childTask.ID,
				Status:                 "failed",
				Summary:                "Swarm stopped after exhausting retry budget",
				TerminalReason:         "swarm_budget_exhausted",
				AllowedCurrentStatuses: []string{"queued", "running", "blocked"},
			})
			if err != nil {
				return changed, err
			}
			changed = true
		}
		if childTask.Status == "failed" {
			targetStatus = "failed"
		}
	}

	if parentTask.BlockedReason == "" && childTask.Status == "blocked" && childTask.BlockedReason == "approval_required" {
		childTask, err = service.Store.UpdateTaskQueueState(ctx, sqlite.UpdateTaskQueueStateParams{
			TaskID:         childTask.ID,
			Status:         "queued",
			NextEligibleAt: time.Time{},
			BlockedReason:  "",
		})
		if err != nil {
			return changed, err
		}
		changed = true
	}

	if targetStatus == delegation.Status {
		targetStatus = childTask.Status
	}
	if targetStatus == "" {
		targetStatus = delegation.Status
	}
	if targetStatus == "" {
		targetStatus = "queued"
	}
	if targetStatus != delegation.Status {
		if _, err := service.Store.UpdateDelegationStatus(ctx, sqlite.UpdateDelegationStatusParams{
			DelegationID: delegation.ID,
			Status:       targetStatus,
		}); err != nil {
			return changed, err
		}
		changed = true
	}

	return changed, nil
}

func (service Service) swarmReadyForAggregation(ctx context.Context, parentTask sqlite.Task, delegations []sqlite.Delegation) (bool, error) {
	if parentTask.BlockedReason != "" || parentTask.Status == "blocked" || parentTask.Status == "failed" || parentTask.Status == "completed" {
		return false, nil
	}
	for _, delegation := range delegations {
		if delegation.ChildTaskID != nil {
			childTask, err := service.Store.GetTask(ctx, *delegation.ChildTaskID)
			if err != nil {
				return false, err
			}
			if childTask.Status == "running" {
				return false, nil
			}
		}
		artifacts, err := service.Store.ListDelegationArtifacts(ctx, sqlite.ListDelegationArtifactsParams{
			DelegationID: delegation.ID,
			ArtifactType: "result",
		})
		if err != nil {
			return false, err
		}
		if len(artifacts) == 0 {
			return false, nil
		}
	}
	return len(delegations) > 0, nil
}
