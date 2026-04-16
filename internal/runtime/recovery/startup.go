package recovery

import (
	"context"
	"database/sql"
	"errors"
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

		resumeState, resumeErr := (checkpoints.Service{Store: service.Store}).LoadResumeState(ctx, task.ProjectID, task.ID)
		if resumeErr != nil && !errors.Is(resumeErr, sql.ErrNoRows) {
			return StartupResult{}, resumeErr
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

		if _, err := service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
			TaskID: task.ID,
			Status: "queued",
		}); err != nil {
			return StartupResult{}, err
		}

		objective := task.Title
		taskStatus := "queued"
		blockingReason := "previous service instance stopped during execution"
		nextSteps := []string{
			"Review the restart wake packet",
			"Resume the queued task when the runtime is healthy",
		}
		constraints := []string{"previous run was interrupted by restart"}
		selectedCapabilities := []string{"startup_recovery"}
		evidence := []checkpoints.Evidence{{
			Kind:    "restart",
			Summary: fmt.Sprintf("run %d was still marked running at startup", run.ID),
		}}
		manifestSummary := "managed project"
		policySummary := "bounded startup recovery"
		openTaskSummary := "task requeued after restart"
		approvalSummary := "none"
		var invocation *checkpoints.InvocationContext

		if resumeErr == nil {
			if resumeState.Objective != "" {
				objective = resumeState.Objective
			}
			if resumeState.BlockingReason != "" {
				blockingReason = resumeState.BlockingReason
			}
			if len(resumeState.NextSteps) > 0 {
				nextSteps = append([]string(nil), resumeState.NextSteps...)
			}
			if len(resumeState.Constraints) > 0 {
				constraints = append([]string(nil), resumeState.Constraints...)
			}
			if len(resumeState.Capabilities) > 0 {
				selectedCapabilities = append([]string(nil), resumeState.Capabilities...)
			}
			if resumeState.ProjectContext != nil {
				if resumeState.ProjectContext.ManifestSummary != "" {
					manifestSummary = resumeState.ProjectContext.ManifestSummary
				}
				if resumeState.ProjectContext.PolicySummary != "" {
					policySummary = resumeState.ProjectContext.PolicySummary
				}
				if resumeState.ProjectContext.OpenTaskSummary != "" {
					openTaskSummary = resumeState.ProjectContext.OpenTaskSummary
				}
			}
			if resumeState.RunContext != nil {
				invocation = resumeState.RunContext.Invocation
			}
		}

		compaction, err := checkpoints.Service{Store: service.Store}.Compact(ctx, checkpoints.CompactParams{
			TaskID:               task.ID,
			RunID:                &run.ID,
			Trigger:              checkpoints.TriggerRestart,
			CheckpointKey:        fmt.Sprintf("startup-recovery-%d", run.ID),
			Objective:            objective,
			TaskStatus:           taskStatus,
			BlockingReason:       blockingReason,
			NextSteps:            nextSteps,
			Constraints:          constraints,
			SelectedCapabilities: selectedCapabilities,
			Evidence:             evidence,
			ManifestSummary:      manifestSummary,
			PolicySummary:        policySummary,
			OpenTaskSummary:      openTaskSummary,
			ApprovalSummary:      approvalSummary,
			Invocation:           invocation,
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
