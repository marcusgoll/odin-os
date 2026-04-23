package approvals

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"odin-os/internal/adapters/web"
	"odin-os/internal/runtime/checkpoints"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/tools/invocation"
)

type Service struct {
	Store       *sqlite.Store
	Checkpoints checkpoints.Service
	Invocation  invocation.Service
}

type ResolveParams struct {
	ApprovalID int64
	Action     string
	DecisionBy string
	Reason     string
}

type ResolveResult struct {
	Approval  sqlite.Approval
	SubmitRun *sqlite.Run
}

type Receipt struct {
	Line    string
	Summary string
}

func (service Service) Resolve(ctx context.Context, params ResolveParams) (ResolveResult, error) {
	if service.Store == nil {
		return ResolveResult{}, fmt.Errorf("approval store is required")
	}
	if params.ApprovalID <= 0 {
		return ResolveResult{}, fmt.Errorf("approval id must be positive")
	}
	if strings.TrimSpace(params.DecisionBy) == "" {
		return ResolveResult{}, fmt.Errorf("decision maker is required")
	}
	if strings.TrimSpace(params.Reason) == "" {
		return ResolveResult{}, fmt.Errorf("reason is required")
	}

	action := strings.ToLower(strings.TrimSpace(params.Action))
	status, err := resolutionStatusForAction(action)
	if err != nil {
		return ResolveResult{}, err
	}

	current, err := service.Store.GetApproval(ctx, params.ApprovalID)
	if err != nil {
		return ResolveResult{}, err
	}
	if current.Status != "pending" {
		return ResolveResult{}, fmt.Errorf("approval %d is %s, want pending", params.ApprovalID, current.Status)
	}

	approval, err := service.Store.ResolveApproval(ctx, sqlite.ResolveApprovalParams{
		ApprovalID: params.ApprovalID,
		Status:     status,
		DecisionBy: strings.TrimSpace(params.DecisionBy),
		Reason:     strings.TrimSpace(params.Reason),
	})
	if err != nil {
		return ResolveResult{}, err
	}

	result := ResolveResult{Approval: approval}
	if action != "approve" {
		return result, nil
	}

	task, err := service.Store.GetTask(ctx, approval.TaskID)
	if err != nil {
		return ResolveResult{}, err
	}
	resumeState, err := service.resumeState(ctx, task.ProjectID, task.ID)
	if err != nil {
		return ResolveResult{}, err
	}
	if resumeState != nil && isPreparedTransfer(*resumeState) {
		return service.resumePreparedTransfer(ctx, task, approval, *resumeState)
	}

	executor, err := service.executorForContinuation(ctx, approval)
	if err != nil {
		return ResolveResult{}, err
	}
	attempt, err := service.nextRunAttempt(ctx, approval.TaskID)
	if err != nil {
		return ResolveResult{}, err
	}

	submitRun, err := service.Store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   approval.TaskID,
		Executor: executor,
		Attempt:  attempt,
		Status:   "running",
	})
	if err != nil {
		return ResolveResult{}, err
	}
	if _, err := service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID: approval.TaskID,
		Status: "running",
	}); err != nil {
		return ResolveResult{}, err
	}

	result.SubmitRun = &submitRun
	return result, nil
}

func (service Service) checkpointService() checkpoints.Service {
	if service.Checkpoints.Store == nil {
		return checkpoints.Service{Store: service.Store}
	}
	return service.Checkpoints
}

func (service Service) resumeState(ctx context.Context, projectID int64, taskID int64) (*checkpoints.ResumeState, error) {
	state, err := service.checkpointService().LoadResumeState(ctx, projectID, taskID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &state, nil
}

func isPreparedTransfer(state checkpoints.ResumeState) bool {
	if state.Trigger != checkpoints.TriggerApprovalWait || state.RunContext == nil {
		return false
	}
	for _, tool := range state.RunContext.ToolResults {
		if tool.Key == "robinhood_transfer_prepare" {
			return true
		}
	}
	return false
}

func (service Service) resumePreparedTransfer(ctx context.Context, task sqlite.Task, approval sqlite.Approval, state checkpoints.ResumeState) (ResolveResult, error) {
	attempt, err := service.nextRunAttempt(ctx, approval.TaskID)
	if err != nil {
		return ResolveResult{}, err
	}

	submitRun, err := service.Store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   approval.TaskID,
		Executor: "robinhood_transfer_submit",
		Attempt:  attempt,
		Status:   "running",
	})
	if err != nil {
		return ResolveResult{}, err
	}
	if _, err := service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID: approval.TaskID,
		Status: "running",
	}); err != nil {
		return ResolveResult{}, err
	}

	driverResult, err := service.Invocation.RobinhoodTransfer(ctx, web.RobinhoodTransferRequest{
		Input: web.RobinhoodTransferInput{
			Mode:               "submit",
			Direction:          prepareFact(state, "direction"),
			AmountUSD:          prepareFact(state, "amount_usd"),
			SourceAccount:      prepareFact(state, "source_account"),
			DestinationAccount: prepareFact(state, "destination_account"),
			Memo:               prepareFact(state, "memo"),
			ResumeFacts: map[string]string{
				"expected_review_state": prepareFact(state, "session_state"),
			},
		},
	})
	if err != nil {
		if finishErr := service.finishSubmitFailure(ctx, task.ID, submitRun.ID, "submit continuation failed", "submit invocation failed"); finishErr != nil {
			return ResolveResult{}, errors.Join(err, finishErr)
		}
		return ResolveResult{}, err
	}

	if _, err := service.Store.RecordRunArtifact(ctx, sqlite.RecordRunArtifactParams{
		RunID:        submitRun.ID,
		ArtifactType: "driver_result",
		Summary:      driverResult.Summary,
		DetailsJSON:  driverResult.RawOutput,
	}); err != nil {
		return ResolveResult{}, err
	}

	sessionState := artifactString(driverResult.Artifacts, "session_state")
	submitToolResult := checkpoints.ToolResult{
		Key:     "robinhood_transfer_submit",
		Summary: driverResult.Summary,
		Facts: map[string]string{
			"approval_id":   fmt.Sprintf("%d", approval.ID),
			"run_id":        fmt.Sprintf("%d", submitRun.ID),
			"session_state": sessionState,
		},
	}
	if prior := artifactString(driverResult.Artifacts, "prior_session_state"); prior != "" {
		submitToolResult.Facts["prior_session_state"] = prior
	}

	switch sessionState {
	case "submitted":
		if _, err := service.Store.FinishRun(ctx, sqlite.FinishRunParams{
			RunID:   submitRun.ID,
			Status:  "completed",
			Summary: driverResult.Summary,
		}); err != nil {
			return ResolveResult{}, err
		}
		if _, err := service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
			TaskID:  task.ID,
			Status:  "completed",
			Summary: driverResult.Summary,
		}); err != nil {
			return ResolveResult{}, err
		}
		if _, err := service.checkpointService().Compact(ctx, checkpoints.CompactParams{
			TaskID:                 task.ID,
			RunID:                  &submitRun.ID,
			Trigger:                checkpoints.TriggerCompletion,
			CheckpointKey:          fmt.Sprintf("approval-%d-submit-%d", approval.ID, submitRun.ID),
			Objective:              state.Objective,
			TaskStatus:             "completed",
			LastCompletedStep:      driverResult.Summary,
			Constraints:            append([]string(nil), state.Constraints...),
			SelectedCapabilities:   append([]string(nil), state.Capabilities...),
			ManifestSummary:        projectManifestSummary(state),
			PolicySummary:          projectPolicySummary(state),
			OpenTaskSummary:        projectOpenTaskSummary(state),
			ApprovalSummary:        fmt.Sprintf("approval %d approved", approval.ID),
			ToolResults:            append(copyToolResults(state), submitToolResult),
			SupersedesWakePacketID: &state.WakePacketID,
		}); err != nil {
			return ResolveResult{}, err
		}
	case "session_expired", "resume_verification_failed":
		if _, err := service.checkpointService().SealWakePacket(ctx, checkpoints.SealWakePacketParams{
			PacketID:          state.WakePacketID,
			BlockingReason:    "stale_context",
			LastCompletedStep: driverResult.Summary,
		}); err != nil {
			return ResolveResult{}, err
		}
		if _, err := service.Store.FinishRun(ctx, sqlite.FinishRunParams{
			RunID:          submitRun.ID,
			Status:         "failed",
			Summary:        driverResult.Summary,
			TerminalReason: sessionState,
		}); err != nil {
			return ResolveResult{}, err
		}
		if _, err := service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
			TaskID:         task.ID,
			Status:         "blocked",
			Summary:        driverResult.Summary,
			TerminalReason: "stale_context",
		}); err != nil {
			return ResolveResult{}, err
		}
	default:
		if finishErr := service.finishSubmitFailure(ctx, task.ID, submitRun.ID, "submit continuation failed", fmt.Sprintf("unsupported robinhood transfer submit session_state %q", sessionState)); finishErr != nil {
			return ResolveResult{}, finishErr
		}
		return ResolveResult{}, fmt.Errorf("unsupported robinhood transfer submit session_state %q", sessionState)
	}

	finalRun, err := service.Store.GetRun(ctx, submitRun.ID)
	if err != nil {
		return ResolveResult{}, err
	}
	submitRun = finalRun

	return ResolveResult{
		Approval:  approval,
		SubmitRun: &submitRun,
	}, nil
}

func copyToolResults(state checkpoints.ResumeState) []checkpoints.ToolResult {
	if state.RunContext == nil {
		return nil
	}
	results := make([]checkpoints.ToolResult, len(state.RunContext.ToolResults))
	copy(results, state.RunContext.ToolResults)
	return results
}

func projectManifestSummary(state checkpoints.ResumeState) string {
	if state.ProjectContext == nil {
		return ""
	}
	return state.ProjectContext.ManifestSummary
}

func projectPolicySummary(state checkpoints.ResumeState) string {
	if state.ProjectContext == nil {
		return ""
	}
	return state.ProjectContext.PolicySummary
}

func projectOpenTaskSummary(state checkpoints.ResumeState) string {
	if state.ProjectContext == nil {
		return ""
	}
	return state.ProjectContext.OpenTaskSummary
}

func prepareFact(state checkpoints.ResumeState, key string) string {
	if state.RunContext == nil {
		return ""
	}
	for _, tool := range state.RunContext.ToolResults {
		if tool.Key != "robinhood_transfer_prepare" {
			continue
		}
		return tool.Facts[key]
	}
	return ""
}

func artifactString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func (service Service) finishSubmitFailure(ctx context.Context, taskID int64, runID int64, summary string, terminalReason string) error {
	if _, err := service.Store.FinishRun(ctx, sqlite.FinishRunParams{
		RunID:          runID,
		Status:         "failed",
		Summary:        summary,
		TerminalReason: terminalReason,
	}); err != nil {
		return err
	}
	_, err := service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID:         taskID,
		Status:         "failed",
		Summary:        summary,
		TerminalReason: terminalReason,
	})
	return err
}

func resolutionStatusForAction(action string) (string, error) {
	switch action {
	case "approve":
		return "approved", nil
	case "deny":
		return "denied", nil
	default:
		return "", fmt.Errorf("unsupported approval action %q", action)
	}
}

func (service Service) executorForContinuation(ctx context.Context, approval sqlite.Approval) (string, error) {
	if approval.RunID == nil {
		return "approval-submit", nil
	}

	var executor string
	err := service.Store.DB().QueryRowContext(ctx, `SELECT executor FROM runs WHERE id = ?`, *approval.RunID).Scan(&executor)
	if err != nil {
		if err == sql.ErrNoRows {
			return "approval-submit", nil
		}
		return "", err
	}
	if strings.TrimSpace(executor) == "" {
		return "approval-submit", nil
	}
	return executor, nil
}

func (service Service) nextRunAttempt(ctx context.Context, taskID int64) (int, error) {
	var attempt int
	err := service.Store.DB().QueryRowContext(ctx, `SELECT COALESCE(MAX(attempt), 0) + 1 FROM runs WHERE task_id = ?`, taskID).Scan(&attempt)
	if err != nil {
		return 0, err
	}
	return attempt, nil
}

func FormatReceipt(result ResolveResult) (Receipt, error) {
	switch result.Approval.Status {
	case "approved":
		line := fmt.Sprintf("approval=%d status=resolved result=approved", result.Approval.ID)
		if result.SubmitRun != nil {
			line = fmt.Sprintf("%s run=%d", line, result.SubmitRun.ID)
		}
		return Receipt{
			Line:    line,
			Summary: "summary=approval granted; submit continuation started",
		}, nil
	case "denied":
		return Receipt{
			Line:    fmt.Sprintf("approval=%d status=resolved result=denied", result.Approval.ID),
			Summary: "summary=approval denied; later retry requires fresh prepare",
		}, nil
	default:
		return Receipt{}, fmt.Errorf("unsupported approval status %q", result.Approval.Status)
	}
}
