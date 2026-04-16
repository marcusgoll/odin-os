package recovery

import (
	"context"
	"fmt"

	"odin-os/internal/runtime/checkpoints"
	"odin-os/internal/store/sqlite"
)

const startupRecoveryFaultKey = "service_restart"

type StartupResult struct {
	RecoveredRuns int
	RecoveryIDs   []int64
	WakePacketIDs []int64
}

func (service Service) RunStartupRecovery(ctx context.Context) (StartupResult, error) {
	if service.Store == nil {
		return StartupResult{}, fmt.Errorf("self-heal store is required")
	}

	runs, err := service.Store.ListRunsByStatus(ctx, "running")
	if err != nil {
		return StartupResult{}, err
	}

	result := StartupResult{}
	for _, run := range runs {
		task, err := service.Store.GetTask(ctx, run.TaskID)
		if err != nil {
			return StartupResult{}, err
		}
		pendingApprovals, err := service.pendingApprovalCount(ctx, task.ID)
		if err != nil {
			return StartupResult{}, err
		}
		requeueable := task.Status != "blocked" && pendingApprovals == 0

		recoveryRecord, err := service.Store.StartRecovery(ctx, sqlite.StartRecoveryParams{
			RunID:       &run.ID,
			Status:      "running",
			Strategy:    "startup_recovery",
			DetailsJSON: `{"trigger":"restart"}`,
		})
		if err != nil {
			return StartupResult{}, err
		}

		if _, err := service.Store.FinishRun(ctx, sqlite.FinishRunParams{
			RunID:   run.ID,
			Status:  "interrupted",
			Summary: "interrupted by startup recovery",
		}); err != nil {
			return StartupResult{}, err
		}

		if requeueable {
			if _, err := service.workItemService().Requeue(ctx, task.ID); err != nil {
				return StartupResult{}, err
			}
		} else if pendingApprovals > 0 && task.Status != "blocked" {
			// Repair legacy or partially written approval-gated state so blocked-task
			// projections and resume state agree with the pending approval.
			if _, err := service.workItemService().Block(ctx, task.ID); err != nil {
				return StartupResult{}, err
			}
		}

		blockingReason := "previous service instance stopped during execution"
		openTaskSummary := "task requeued after restart"
		approvalSummary := "none"
		nextSteps := []string{
			"Review the restart wake packet",
			"Resume the queued task when the runtime is healthy",
		}
		recoveryDescription := "interrupted running run, requeued task, and created restart wake packet"
		if !requeueable {
			blockingReason = "pending approval before restart"
			openTaskSummary = "task remains blocked pending approval"
			approvalSummary = pendingApprovalSummary(pendingApprovals)
			nextSteps = []string{
				"Review the pending approval",
				"Resume the task after approval is resolved and the runtime is healthy",
			}
			recoveryDescription = "interrupted running run, preserved blocked task, and created restart wake packet"
		}

		compaction, err := checkpoints.Service{Store: service.Store}.Compact(ctx, checkpoints.CompactParams{
			TaskID:               task.ID,
			RunID:                &run.ID,
			Trigger:              checkpoints.TriggerRestart,
			CheckpointKey:        fmt.Sprintf("startup-recovery-%d", run.ID),
			Objective:            task.Title,
			BlockingReason:       blockingReason,
			NextSteps:            nextSteps,
			Constraints:          []string{"previous run was interrupted by restart"},
			SelectedCapabilities: []string{"startup_recovery"},
			Evidence: []checkpoints.Evidence{{
				Kind:    "restart",
				Summary: fmt.Sprintf("run %d was still marked running at startup", run.ID),
			}},
			ManifestSummary: "managed project",
			PolicySummary:   "bounded startup recovery",
			OpenTaskSummary: openTaskSummary,
			ApprovalSummary: approvalSummary,
		})
		if err != nil {
			return StartupResult{}, err
		}

		if err := service.Store.RecordRecoveryAction(ctx, sqlite.RecordRecoveryActionParams{
			RecoveryID:  recoveryRecord.ID,
			Playbook:    "startup_recovery",
			FaultKey:    startupRecoveryFaultKey,
			ActionName:  "interrupt_run_and_checkpoint",
			Attempt:     1,
			Result:      "completed",
			Description: recoveryDescription,
		}); err != nil {
			return StartupResult{}, err
		}

		completed, err := service.Store.CompleteRecovery(ctx, sqlite.CompleteRecoveryParams{
			RecoveryID:  recoveryRecord.ID,
			Status:      "completed",
			DetailsJSON: fmt.Sprintf(`{"wake_packet_id":%d}`, compaction.WakePacket.ID),
		})
		if err != nil {
			return StartupResult{}, err
		}

		result.RecoveredRuns++
		result.RecoveryIDs = append(result.RecoveryIDs, completed.ID)
		result.WakePacketIDs = append(result.WakePacketIDs, compaction.WakePacket.ID)
	}

	return result, nil
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

func pendingApprovalSummary(count int) string {
	switch count {
	case 0:
		return "none"
	case 1:
		return "1 pending approval"
	default:
		return fmt.Sprintf("%d pending approvals", count)
	}
}
