package recovery

import (
	"context"
	"database/sql"
	"fmt"
	"time"

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

	runningRuns, err := service.Store.ListRunsByStatus(ctx, "running")
	if err != nil {
		return StartupResult{}, err
	}
	preparingRuns, err := service.Store.ListRunsByStatus(ctx, "preparing")
	if err != nil {
		return StartupResult{}, err
	}
	runs := append(runningRuns, preparingRuns...)

	result := StartupResult{}
	for _, run := range runs {
		task, err := service.Store.GetTask(ctx, run.TaskID)
		if err != nil {
			return StartupResult{}, err
		}
		targetStatus, blockedReason, approvalSummary, err := service.recoveryTargetState(ctx, task)
		if err != nil {
			return StartupResult{}, err
		}

		recoveredTask, _, err := service.Store.InterruptRunAndRequeueTask(ctx, sqlite.InterruptRunAndRequeueTaskParams{
			RunID:   run.ID,
			Summary: "interrupted by startup recovery",
		})
		if err != nil {
			return StartupResult{}, err
		}
		recoveredTask, err = service.restoreRecoveredTaskState(ctx, recoveredTask, targetStatus, blockedReason)
		if err != nil {
			return StartupResult{}, err
		}

		compaction, err := checkpoints.Service{Store: service.Store}.Compact(ctx, checkpoints.CompactParams{
			TaskID:         task.ID,
			RunID:          &run.ID,
			Trigger:        checkpoints.TriggerRestart,
			CheckpointKey:  fmt.Sprintf("startup-recovery-%d", run.ID),
			Objective:      task.Title,
			TaskStatus:     recoveredTask.Status,
			BlockingReason: startupRecoveryBlockingReason(recoveredTask.Status, blockedReason),
			NextSteps: []string{
				"Review the restart wake packet",
				"Resume the queued task when the runtime is healthy",
			},
			Constraints:          []string{"previous run was interrupted by restart"},
			SelectedCapabilities: []string{"startup_recovery"},
			Evidence: []checkpoints.Evidence{{
				Kind:    "restart",
				Summary: fmt.Sprintf("run %d was still marked %s at startup", run.ID, run.Status),
			}},
			ManifestSummary: "managed project",
			PolicySummary:   "bounded startup recovery",
			OpenTaskSummary: startupRecoveryOpenTaskSummary(recoveredTask.Status),
			ApprovalSummary: approvalSummary,
		})
		if err != nil {
			return StartupResult{}, err
		}

		recoveryRecord, err := service.Store.StartRecovery(ctx, sqlite.StartRecoveryParams{
			RunID:       &run.ID,
			Status:      "running",
			Strategy:    "startup_recovery",
			DetailsJSON: fmt.Sprintf(`{"trigger":"restart","wake_packet_id":%d}`, compaction.WakePacket.ID),
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
			Description: "interrupted running run, requeued task, and created restart wake packet",
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

func (service Service) recoveryTargetState(ctx context.Context, task sqlite.Task) (string, string, string, error) {
	approval, err := service.Store.GetLatestTaskApproval(ctx, task.ID)
	switch {
	case err == nil:
		switch approval.Status {
		case "pending":
			return "blocked", "approval_required", "approval pending", nil
		case "rejected":
			return "blocked", task.BlockedReason, "approval rejected", nil
		case "approved":
			return task.Status, task.BlockedReason, "approval approved", nil
		default:
			return task.Status, task.BlockedReason, approval.Status, nil
		}
	case err == sql.ErrNoRows:
	default:
		return "", "", "", err
	}

	switch task.Status {
	case "blocked":
		if task.BlockedReason != "" {
			return task.Status, task.BlockedReason, "none", nil
		}
		return "queued", "", "none", nil
	case "completed", "failed", "dead_letter", "timeout":
		return task.Status, task.BlockedReason, "none", nil
	default:
		return "queued", "", "none", nil
	}
}

func (service Service) restoreRecoveredTaskState(ctx context.Context, task sqlite.Task, targetStatus string, blockedReason string) (sqlite.Task, error) {
	if targetStatus == "" || targetStatus == task.Status {
		return task, nil
	}

	return service.Store.UpdateTaskQueueState(ctx, sqlite.UpdateTaskQueueStateParams{
		TaskID:         task.ID,
		Status:         targetStatus,
		NextEligibleAt: time.Time{},
		Priority:       task.Priority,
		LastError:      task.LastError,
		RetryCount:     task.RetryCount,
		MaxAttempts:    task.MaxAttempts,
		BlockedReason:  blockedReason,
	})
}

func startupRecoveryBlockingReason(status string, blockedReason string) string {
	if status == "blocked" && blockedReason != "" {
		return blockedReason
	}
	return "previous service instance stopped during execution"
}

func startupRecoveryOpenTaskSummary(status string) string {
	switch status {
	case "blocked":
		return "task remains blocked after restart"
	case "completed", "failed", "dead_letter", "timeout":
		return fmt.Sprintf("task remains %s after restart", status)
	default:
		return "task requeued after restart"
	}
}
