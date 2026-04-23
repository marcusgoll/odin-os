package transfers

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"odin-os/internal/adapters/web"
	"odin-os/internal/core/projects"
	"odin-os/internal/runtime/checkpoints"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/tools/invocation"
)

const prepareSummary = "review prepared and awaiting approval"

type Service struct {
	Store       *sqlite.Store
	Registry    projects.Registry
	Checkpoints checkpoints.Service
	Invocation  invocation.Service
	Now         func() time.Time
}

type PrepareParams struct {
	ProjectKey         string
	Direction          string
	AmountUSD          string
	SourceAccount      string
	DestinationAccount string
	Memo               string
}

type PrepareResult struct {
	Task       sqlite.Task
	Run        sqlite.Run
	Approval   sqlite.Approval
	WakePacket sqlite.ContextPacket
	Summary    string
}

func (service Service) Prepare(ctx context.Context, params PrepareParams) (PrepareResult, error) {
	if service.Store == nil {
		return PrepareResult{}, fmt.Errorf("transfer store is required")
	}
	if service.Checkpoints.Store == nil {
		service.Checkpoints = checkpoints.Service{Store: service.Store}
	}

	normalized, err := normalizePrepareParams(params)
	if err != nil {
		return PrepareResult{}, err
	}

	manifest, ok := service.Registry.Lookup(normalized.ProjectKey)
	if !ok {
		return PrepareResult{}, fmt.Errorf("unknown project %q", normalized.ProjectKey)
	}
	project, err := service.ensureRuntimeProject(ctx, manifest)
	if err != nil {
		return PrepareResult{}, err
	}

	task, err := service.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         service.taskKey(),
		Title:       taskTitle(normalized),
		Status:      "running",
		Scope:       project.Scope,
		RequestedBy: "operator",
	})
	if err != nil {
		return PrepareResult{}, err
	}

	run, err := service.Store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "robinhood_transfer_prepare",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		return PrepareResult{}, err
	}

	driverResult, err := service.Invocation.RobinhoodTransfer(ctx, web.RobinhoodTransferRequest{
		Input: web.RobinhoodTransferInput{
			Mode:               "prepare",
			Direction:          normalized.Direction,
			AmountUSD:          normalized.AmountUSD,
			SourceAccount:      normalized.SourceAccount,
			DestinationAccount: normalized.DestinationAccount,
			Memo:               normalized.Memo,
		},
	})
	if err != nil {
		return PrepareResult{}, service.failPrepare(ctx, task.ID, run.ID, err)
	}

	if state := artifactString(driverResult.Artifacts, "session_state"); state != "review_ready" {
		return PrepareResult{}, service.failPrepare(ctx, task.ID, run.ID, fmt.Errorf("prepare driver session_state = %q, want review_ready", state))
	}

	if _, err := service.Store.RecordRunArtifact(ctx, sqlite.RecordRunArtifactParams{
		RunID:        run.ID,
		ArtifactType: "driver_result",
		Summary:      driverResult.Summary,
		DetailsJSON:  driverResult.RawOutput,
	}); err != nil {
		return PrepareResult{}, service.failPrepare(ctx, task.ID, run.ID, err)
	}

	run, err = service.Store.FinishRun(ctx, sqlite.FinishRunParams{
		RunID:   run.ID,
		Status:  "completed",
		Summary: prepareSummary,
	})
	if err != nil {
		return PrepareResult{}, err
	}

	task, err = service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID: task.ID,
		Status: "blocked",
	})
	if err != nil {
		return PrepareResult{}, err
	}

	approval, err := service.Store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      task.ID,
		RunID:       &run.ID,
		Status:      "pending",
		RequestedBy: "system",
	})
	if err != nil {
		return PrepareResult{}, err
	}

	compact, err := service.Checkpoints.Compact(ctx, checkpoints.CompactParams{
		TaskID:            task.ID,
		RunID:             &run.ID,
		Trigger:           checkpoints.TriggerApprovalWait,
		CheckpointKey:     fmt.Sprintf("robinhood-transfer-%d", task.ID),
		Objective:         task.Title,
		TaskStatus:        task.Status,
		BlockingReason:    "approval_required",
		LastCompletedStep: "prepared Robinhood transfer review",
		NextSteps: []string{
			fmt.Sprintf("review /runs show %d", run.ID),
			fmt.Sprintf("resolve approval %d when ready", approval.ID),
		},
		ApprovalSummary: fmt.Sprintf("approval %d pending", approval.ID),
		ToolResults: []checkpoints.ToolResult{{
			Key:     "robinhood_transfer_prepare",
			Summary: prepareSummary,
			Facts: map[string]string{
				"direction":           normalized.Direction,
				"amount_usd":          normalized.AmountUSD,
				"source_account":      normalized.SourceAccount,
				"destination_account": normalized.DestinationAccount,
				"memo":                normalized.Memo,
				"session_state":       artifactString(driverResult.Artifacts, "session_state"),
			},
		}},
		Evidence: []checkpoints.Evidence{{
			Kind:    "transfer_intent",
			Summary: task.Title,
		}},
	})
	if err != nil {
		return PrepareResult{}, err
	}

	return PrepareResult{
		Task:       task,
		Run:        run,
		Approval:   approval,
		WakePacket: compact.WakePacket,
		Summary:    prepareSummary,
	}, nil
}

func normalizePrepareParams(params PrepareParams) (PrepareParams, error) {
	params.ProjectKey = strings.TrimSpace(params.ProjectKey)
	params.Direction = strings.ToLower(strings.TrimSpace(params.Direction))
	params.AmountUSD = strings.TrimSpace(params.AmountUSD)
	params.SourceAccount = strings.TrimSpace(params.SourceAccount)
	params.DestinationAccount = strings.TrimSpace(params.DestinationAccount)
	params.Memo = strings.TrimSpace(params.Memo)

	if params.ProjectKey == "" {
		return PrepareParams{}, fmt.Errorf("project key is required")
	}
	if params.Direction != "deposit" && params.Direction != "withdraw" {
		return PrepareParams{}, fmt.Errorf("direction must be deposit or withdraw")
	}
	amount, err := strconv.ParseFloat(params.AmountUSD, 64)
	if err != nil || amount <= 0 {
		return PrepareParams{}, fmt.Errorf("amount_usd must be a positive decimal amount")
	}
	if params.SourceAccount == "" {
		return PrepareParams{}, fmt.Errorf("source_account is required")
	}
	if params.DestinationAccount == "" {
		return PrepareParams{}, fmt.Errorf("destination_account is required")
	}

	return params, nil
}

func (service Service) ensureRuntimeProject(ctx context.Context, manifest projects.Manifest) (sqlite.Project, error) {
	project, err := service.Store.GetProjectByKey(ctx, manifest.Key)
	if err == nil {
		return project, nil
	}
	if err != sql.ErrNoRows {
		return sqlite.Project{}, err
	}

	scopeValue := "project"
	if manifest.SystemProject {
		scopeValue = "odin-core"
	}

	return service.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           manifest.Key,
		Name:          manifest.Name,
		Scope:         scopeValue,
		GitRoot:       manifest.GitRoot,
		DefaultBranch: manifest.DefaultBranch,
		GitHubRepo:    manifest.GitHub.Repo,
		ManifestPath:  manifest.SourcePath,
	})
}

func (service Service) taskKey() string {
	return fmt.Sprintf("robinhood-transfer-%s", service.now().Format("20060102-150405"))
}

func (service Service) now() time.Time {
	if service.Now != nil {
		return service.Now().UTC()
	}
	return time.Now().UTC()
}

func taskTitle(params PrepareParams) string {
	return fmt.Sprintf(
		"Prepare Robinhood %s of $%s from %s to %s",
		params.Direction,
		params.AmountUSD,
		params.SourceAccount,
		params.DestinationAccount,
	)
}

func artifactString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func (service Service) failPrepare(ctx context.Context, taskID int64, runID int64, cause error) error {
	if _, err := service.Store.FinishRun(ctx, sqlite.FinishRunParams{
		RunID:          runID,
		Status:         "failed",
		Summary:        cause.Error(),
		TerminalReason: cause.Error(),
		ArtifactsJSON:  "[]",
	}); err != nil {
		return err
	}
	if _, err := service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID:                 taskID,
		Status:                 "failed",
		Summary:                cause.Error(),
		TerminalReason:         cause.Error(),
		ArtifactsJSON:          "[]",
		AllowedCurrentStatuses: []string{"running"},
	}); err != nil {
		return err
	}
	return cause
}
