package recovery

import (
	"context"
	"database/sql"
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
		lease, err := service.Store.GetActiveWorktreeLeaseByTaskRun(ctx, task.ID, run.ID)
		if err != nil && err != sql.ErrNoRows {
			return StartupResult{}, err
		}
		if err == nil {
			if _, err := service.Store.ReleaseWorktreeLease(ctx, sqlite.ReleaseWorktreeLeaseParams{
				LeaseID: lease.ID,
				State:   "released",
			}); err != nil {
				return StartupResult{}, err
			}
		}

		if _, err := service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
			TaskID: task.ID,
			Status: "queued",
		}); err != nil {
			return StartupResult{}, err
		}

		compaction, err := checkpoints.Service{Store: service.Store}.Compact(ctx, checkpoints.CompactParams{
			TaskID:         task.ID,
			RunID:          &run.ID,
			Trigger:        checkpoints.TriggerRestart,
			CheckpointKey:  fmt.Sprintf("startup-recovery-%d", run.ID),
			Objective:      task.Title,
			TaskStatus:     "queued",
			BlockingReason: "previous service instance stopped during execution",
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
			OpenTaskSummary: "task requeued after restart",
			ApprovalSummary: "none",
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
