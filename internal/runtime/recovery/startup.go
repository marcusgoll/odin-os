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
		pendingApprovals, err := service.pendingApprovalCount(ctx, task.ID)
		if err != nil {
			return StartupResult{}, err
		}
		latestApprovalStatus, hasApproval, err := service.latestApprovalStatus(ctx, task.ID)
		if err != nil {
			return StartupResult{}, err
		}
		terminalTask := task.Status == "completed" || task.Status == "failed"
		rejectedApprovalBlock := task.Status == "blocked" && pendingApprovals == 0 && hasApproval && latestApprovalStatus == "rejected"
		requeueable := !terminalTask && pendingApprovals == 0 && !rejectedApprovalBlock
		needsApprovalRepair := pendingApprovals > 0 && task.Status != "blocked"

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

		if requeueable {
			if _, err := service.workItemService().Requeue(ctx, task.ID); err != nil {
				return StartupResult{}, err
			}
		} else if needsApprovalRepair {
			// Repair legacy or partially written approval-gated state so blocked-task
			// projections and resume state agree with the pending approval.
			if _, err := service.workItemService().Block(ctx, task.ID); err != nil {
				return StartupResult{}, err
			}
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
		recoveryDescription := "interrupted running run, requeued task, and created restart wake packet"

		if terminalTask {
			taskStatus = task.Status
			blockingReason = "task was already terminal before startup recovery"
			openTaskSummary = fmt.Sprintf("task remains %s", task.Status)
			nextSteps = []string{
				"Review why a terminal task still had a running run",
				"Confirm no further task action is required",
			}
			recoveryDescription = "interrupted stale running run for terminal task and created restart wake packet"
		} else if rejectedApprovalBlock {
			taskStatus = "blocked"
			blockingReason = "approval was rejected before restart"
			openTaskSummary = "task remains blocked after rejected approval"
			approvalSummary = "rejected"
			nextSteps = []string{
				"Review the rejected approval decision",
				"Create a new work item if the task should be attempted again",
			}
			recoveryDescription = "interrupted running run and preserved blocked task after rejected approval"
		} else if !requeueable {
			taskStatus = "blocked"
			blockingReason = "pending approval before restart"
			openTaskSummary = "task remains blocked pending approval"
			approvalSummary = pendingApprovalSummary(pendingApprovals)
			nextSteps = []string{
				"Review the pending approval",
				"Resume the task after approval is resolved and the runtime is healthy",
			}
			recoveryDescription = "interrupted running run, preserved blocked task, and created restart wake packet"
		}

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
			Description: recoveryDescription,
		}); err != nil {
			return StartupResult{}, err
		}

		if _, err := service.Store.FinishRun(ctx, sqlite.FinishRunParams{
			RunID:   run.ID,
			Status:  "interrupted",
			Summary: "interrupted by startup recovery",
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

func (service Service) latestApprovalStatus(ctx context.Context, taskID int64) (string, bool, error) {
	row := service.Store.DB().QueryRowContext(ctx, `
		SELECT status
		FROM approvals
		WHERE task_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, taskID)

	var status string
	if err := row.Scan(&status); err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, err
	}
	return status, true, nil
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
