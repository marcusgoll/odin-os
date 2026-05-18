package runs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/capabilities"
	"odin-os/internal/executors/contract"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
)

type Service struct {
	DB    *sql.DB
	Store *sqlite.Store
}

type RunRecord struct {
	RunID      int64
	Status     string
	Summary    string
	StartedAt  string
	FinishedAt *string
}

type Detail struct {
	RunID           int64
	TaskID          int64
	TaskKey         string
	ProjectKey      string
	Executor        string
	Status          string
	Attempt         int
	Summary         string
	ArtifactsJSON   string
	FailureAnalysis *FailureAnalysisDetail
	Artifacts       []sqlite.RunArtifact
}

type ModelRoutingReadback struct {
	RunID               int64  `json:"run_id"`
	TaskID              int64  `json:"task_id"`
	TaskKey             string `json:"task_key"`
	ProjectKey          string `json:"project_key"`
	RunStatus           string `json:"run_status"`
	RunExecutor         string `json:"run_executor"`
	ArtifactID          int64  `json:"artifact_id"`
	RouteName           string `json:"route_name"`
	ExecutorLane        string `json:"executor_lane"`
	ModelKey            string `json:"model_key,omitempty"`
	ProviderKey         string `json:"provider_key,omitempty"`
	ProviderModelID     string `json:"provider_model_id,omitempty"`
	FallbackUsed        string `json:"fallback_used"`
	PolicyReason        string `json:"policy_reason,omitempty"`
	EstimatedCostUSD    string `json:"estimated_cost_usd,omitempty"`
	ContextWindowTokens string `json:"context_window_tokens,omitempty"`
	LatencyTier         string `json:"latency_tier,omitempty"`
	TaskClass           string `json:"task_class,omitempty"`
	RiskClass           string `json:"risk_class,omitempty"`
	Source              string `json:"source"`
}

type FailureAnalysisDetail struct {
	Category            string   `json:"category"`
	SuggestedFix        string   `json:"suggested_fix"`
	NextStepTarget      string   `json:"next_step_target"`
	RetryRecommended    bool     `json:"retry_recommended"`
	MaxAttemptsReached  bool     `json:"max_attempts_reached"`
	FollowUpRecommended bool     `json:"follow_up_recommended"`
	FollowUpTitle       string   `json:"follow_up_title,omitempty"`
	FollowUpReason      string   `json:"follow_up_reason,omitempty"`
	FollowUpLabels      []string `json:"follow_up_labels,omitempty"`
}

func (service Service) ModelRoutingForRun(ctx context.Context, resolved scope.Resolution, runID int64) (ModelRoutingReadback, error) {
	if runID <= 0 {
		return ModelRoutingReadback{}, fmt.Errorf("run id must be a positive integer")
	}
	detail, err := service.Show(ctx, resolved, runID)
	if err != nil {
		return ModelRoutingReadback{}, err
	}
	return service.modelRoutingForDetail(detail)
}

func (service Service) ModelRoutingForTask(ctx context.Context, resolved scope.Resolution, taskRef string) (ModelRoutingReadback, error) {
	taskRef = strings.TrimSpace(taskRef)
	if taskRef == "" {
		return ModelRoutingReadback{}, fmt.Errorf("task reference is required")
	}
	db := service.DB
	if db == nil && service.Store != nil {
		db = service.Store.DB()
	}
	if db == nil {
		return ModelRoutingReadback{}, fmt.Errorf("run store is required")
	}

	var rows *sql.Rows
	var err error
	if taskID, parseErr := strconv.ParseInt(taskRef, 10, 64); parseErr == nil && taskID > 0 {
		rows, err = db.QueryContext(ctx, modelRoutingTaskRunsQuery()+` WHERE t.id = ? ORDER BY r.id DESC`, taskID)
	} else {
		rows, err = db.QueryContext(ctx, modelRoutingTaskRunsQuery()+` WHERE t.key = ? ORDER BY r.id DESC`, taskRef)
	}
	if err != nil {
		return ModelRoutingReadback{}, err
	}
	var matched Detail
	var found bool
	for rows.Next() {
		detail, taskScope, err := scanModelRoutingDetail(rows)
		if err != nil {
			rows.Close()
			return ModelRoutingReadback{}, err
		}
		if !matchesRunScope(detail.ProjectKey, taskScope, resolved) {
			continue
		}
		matched = detail
		found = true
		break
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ModelRoutingReadback{}, err
	}
	if err := rows.Close(); err != nil {
		return ModelRoutingReadback{}, err
	}
	if !found {
		return ModelRoutingReadback{}, sql.ErrNoRows
	}
	if service.Store != nil {
		artifacts, err := service.Store.ListRunArtifacts(ctx, sqlite.ListRunArtifactsParams{RunID: matched.RunID})
		if err != nil {
			return ModelRoutingReadback{}, err
		}
		matched.Artifacts = artifacts
	}
	return service.modelRoutingForDetail(matched)
}

func (service Service) modelRoutingForDetail(detail Detail) (ModelRoutingReadback, error) {
	for index := len(detail.Artifacts) - 1; index >= 0; index-- {
		artifact := detail.Artifacts[index]
		if artifact.ArtifactType != "model_routing" {
			continue
		}
		var fields map[string]string
		if err := json.Unmarshal([]byte(artifact.DetailsJSON), &fields); err != nil {
			return ModelRoutingReadback{}, fmt.Errorf("model_routing artifact %d: %w", artifact.ID, err)
		}
		return ModelRoutingReadback{
			RunID:               detail.RunID,
			TaskID:              detail.TaskID,
			TaskKey:             detail.TaskKey,
			ProjectKey:          detail.ProjectKey,
			RunStatus:           detail.Status,
			RunExecutor:         detail.Executor,
			ArtifactID:          artifact.ID,
			RouteName:           strings.TrimSpace(fields["route_name"]),
			ExecutorLane:        strings.TrimSpace(fields["executor_lane"]),
			ModelKey:            strings.TrimSpace(fields["model_key"]),
			ProviderKey:         strings.TrimSpace(fields["provider_key"]),
			ProviderModelID:     strings.TrimSpace(fields["provider_model_id"]),
			FallbackUsed:        strings.TrimSpace(fields["fallback_used"]),
			PolicyReason:        strings.TrimSpace(fields["policy_reason"]),
			EstimatedCostUSD:    strings.TrimSpace(fields["estimated_cost_usd"]),
			ContextWindowTokens: strings.TrimSpace(fields["context_window_tokens"]),
			LatencyTier:         strings.TrimSpace(fields["latency_tier"]),
			TaskClass:           strings.TrimSpace(fields["task_class"]),
			RiskClass:           strings.TrimSpace(fields["risk_class"]),
			Source:              "run_artifact:model_routing",
		}, nil
	}
	return ModelRoutingReadback{}, fmt.Errorf("run %d has no model_routing artifact", detail.RunID)
}

func (service Service) List(ctx context.Context, resolved scope.Resolution) ([]projections.RunSummaryView, error) {
	db := service.DB
	if db == nil && service.Store != nil {
		db = service.Store.DB()
	}
	if db == nil {
		return nil, fmt.Errorf("run store is required")
	}

	rows, err := db.QueryContext(ctx, `
		SELECT
			r.id,
			r.task_id,
			t.key,
			r.executor,
			r.status,
			r.attempt,
			r.started_at,
			r.finished_at,
			p.key,
			p.git_root,
			COALESCE(wl.worktree_path, p.git_root),
			COALESCE(wl.branch_name, p.default_branch),
			COALESCE(t.execution_intent, ''),
			COALESCE(t.execution_intent_source, ''),
			t.scope
		FROM runs r
		JOIN tasks t ON t.id = r.task_id
		JOIN projects p ON p.id = t.project_id
		LEFT JOIN worktree_leases wl ON wl.run_id = r.id
		ORDER BY r.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var views []projections.RunSummaryView
	for rows.Next() {
		var view projections.RunSummaryView
		var projectKey string
		var taskScope string
		var finishedAt sql.NullString
		if err := rows.Scan(
			&view.RunID,
			&view.TaskID,
			&view.TaskKey,
			&view.Executor,
			&view.Status,
			&view.Attempt,
			&view.StartedAt,
			&finishedAt,
			&projectKey,
			&view.RepoRoot,
			&view.WorktreePath,
			&view.BranchName,
			&view.ExecutionIntent,
			&view.ExecutionIntentSource,
			&taskScope,
		); err != nil {
			return nil, err
		}
		view.ProjectKey = projectKey
		if finishedAt.Valid {
			view.FinishedAt = &finishedAt.String
		}
		if matchesRunScope(projectKey, taskScope, resolved) {
			views = append(views, view)
		}
	}

	return views, rows.Err()
}

func (service Service) GetRun(ctx context.Context, runID int64) (RunRecord, error) {
	db := service.DB
	if db == nil && service.Store != nil {
		db = service.Store.DB()
	}
	if db == nil {
		return RunRecord{}, fmt.Errorf("run store is required")
	}

	row := db.QueryRowContext(ctx, `
		SELECT id, status, summary, started_at, finished_at
		FROM runs
		WHERE id = ?
	`, runID)

	var record RunRecord
	var finishedAt sql.NullString
	if err := row.Scan(
		&record.RunID,
		&record.Status,
		&record.Summary,
		&record.StartedAt,
		&finishedAt,
	); err != nil {
		return RunRecord{}, err
	}
	if finishedAt.Valid {
		record.FinishedAt = &finishedAt.String
	}
	return record, nil
}

func (service Service) Show(ctx context.Context, resolved scope.Resolution, runID int64) (Detail, error) {
	db := service.DB
	if db == nil && service.Store != nil {
		db = service.Store.DB()
	}
	if db == nil {
		return Detail{}, fmt.Errorf("run store is required")
	}

	row := db.QueryRowContext(ctx, `
		SELECT
			r.id,
			r.task_id,
			t.key,
			p.key,
			r.executor,
			r.status,
			r.attempt,
			r.summary,
			r.artifacts_json,
			t.scope
		FROM runs r
		JOIN tasks t ON t.id = r.task_id
		JOIN projects p ON p.id = t.project_id
		WHERE r.id = ?
	`, runID)

	var detail Detail
	var taskScope string
	if err := row.Scan(
		&detail.RunID,
		&detail.TaskID,
		&detail.TaskKey,
		&detail.ProjectKey,
		&detail.Executor,
		&detail.Status,
		&detail.Attempt,
		&detail.Summary,
		&detail.ArtifactsJSON,
		&taskScope,
	); err != nil {
		return Detail{}, err
	}
	detail.FailureAnalysis = failureAnalysisFromArtifactJSON(detail.ArtifactsJSON)
	if !matchesRunScope(detail.ProjectKey, taskScope, resolved) {
		return Detail{}, fmt.Errorf("run %d is outside the current scope", runID)
	}
	if service.Store != nil {
		artifacts, err := service.Store.ListRunArtifacts(ctx, sqlite.ListRunArtifactsParams{RunID: runID})
		if err != nil {
			return Detail{}, err
		}
		detail.Artifacts = artifacts
	}

	return detail, nil
}

func modelRoutingTaskRunsQuery() string {
	return `
		SELECT
			r.id,
			r.task_id,
			t.key,
			p.key,
			r.executor,
			r.status,
			r.attempt,
			r.summary,
			r.artifacts_json,
			t.scope
		FROM runs r
		JOIN tasks t ON t.id = r.task_id
		JOIN projects p ON p.id = t.project_id
	`
}

func scanModelRoutingDetail(scanner interface {
	Scan(dest ...any) error
}) (Detail, string, error) {
	var detail Detail
	var taskScope string
	err := scanner.Scan(
		&detail.RunID,
		&detail.TaskID,
		&detail.TaskKey,
		&detail.ProjectKey,
		&detail.Executor,
		&detail.Status,
		&detail.Attempt,
		&detail.Summary,
		&detail.ArtifactsJSON,
		&taskScope,
	)
	if err != nil {
		return Detail{}, "", err
	}
	return detail, taskScope, nil
}

func failureAnalysisFromArtifactJSON(raw string) *FailureAnalysisDetail {
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "[]" {
		return nil
	}

	if detail := failureAnalysisFromObject(raw); detail != nil {
		return detail
	}

	var artifacts []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &artifacts); err != nil {
		return nil
	}
	for _, artifact := range artifacts {
		if detail := failureAnalysisFromObject(string(artifact)); detail != nil {
			return detail
		}
	}
	return nil
}

func failureAnalysisFromObject(raw string) *FailureAnalysisDetail {
	var payload struct {
		FailureAnalysis struct {
			Category           string `json:"category"`
			SuggestedFix       string `json:"suggested_fix"`
			NextStepTarget     string `json:"next_step_target"`
			RetryRecommended   bool   `json:"retry_recommended"`
			MaxAttemptsReached bool   `json:"max_attempts_reached"`
			FollowUp           struct {
				Recommended bool     `json:"recommended"`
				Title       string   `json:"title"`
				Labels      []string `json:"labels"`
				Reason      string   `json:"reason"`
			} `json:"follow_up"`
		} `json:"failure_analysis"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	analysis := payload.FailureAnalysis
	if strings.TrimSpace(analysis.Category) == "" &&
		strings.TrimSpace(analysis.SuggestedFix) == "" &&
		strings.TrimSpace(analysis.NextStepTarget) == "" {
		return nil
	}
	return &FailureAnalysisDetail{
		Category:            analysis.Category,
		SuggestedFix:        analysis.SuggestedFix,
		NextStepTarget:      analysis.NextStepTarget,
		RetryRecommended:    analysis.RetryRecommended,
		MaxAttemptsReached:  analysis.MaxAttemptsReached,
		FollowUpRecommended: analysis.FollowUp.Recommended,
		FollowUpTitle:       analysis.FollowUp.Title,
		FollowUpReason:      analysis.FollowUp.Reason,
		FollowUpLabels:      analysis.FollowUp.Labels,
	}
}

func (service Service) GetRunEnvelope(ctx context.Context, runID int64) (capabilities.RunEnvelope, error) {
	record, err := service.GetRun(ctx, runID)
	if err != nil {
		return capabilities.RunEnvelope{}, err
	}

	return capabilities.RunEnvelope{
		RunID:     strconv.FormatInt(record.RunID, 10),
		Status:    record.Status,
		Artifacts: []capabilities.Artifact{},
	}, nil
}

func (service Service) Start(ctx context.Context, task sqlite.Task, executorKey string) (sqlite.Run, error) {
	if service.Store == nil {
		return sqlite.Run{}, fmt.Errorf("run store is required")
	}

	attempt, err := service.nextRunAttempt(ctx, task.ID)
	if err != nil {
		return sqlite.Run{}, err
	}

	run, err := service.Store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: executorKey,
		Attempt:  attempt,
		Status:   "running",
	})
	if err != nil {
		return sqlite.Run{}, err
	}
	if _, err := service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID:                 task.ID,
		Status:                 "running",
		AllowedCurrentStatuses: []string{"queued"},
	}); err != nil {
		currentTask, loadErr := service.Store.GetTask(ctx, task.ID)
		if loadErr != nil {
			return sqlite.Run{}, loadErr
		}
		if currentTask.Status != "queued" {
			if _, finishErr := service.Store.FinishRun(ctx, sqlite.FinishRunParams{
				RunID:          run.ID,
				Status:         "interrupted",
				Summary:        err.Error(),
				TerminalReason: err.Error(),
				ArtifactsJSON:  "[]",
			}); finishErr != nil {
				return sqlite.Run{}, finishErr
			}
			return sqlite.Run{}, err
		}
		if _, finishErr := service.Store.FinishRun(ctx, sqlite.FinishRunParams{
			RunID:          run.ID,
			Status:         "failed",
			Summary:        err.Error(),
			TerminalReason: err.Error(),
			ArtifactsJSON:  "[]",
		}); finishErr != nil {
			return sqlite.Run{}, finishErr
		}
		if _, taskErr := service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
			TaskID:                 task.ID,
			Status:                 "failed",
			Summary:                err.Error(),
			TerminalReason:         err.Error(),
			ArtifactsJSON:          "[]",
			AllowedCurrentStatuses: []string{"queued", "running"},
		}); taskErr != nil {
			return sqlite.Run{}, taskErr
		}
		return sqlite.Run{}, err
	}
	return run, nil
}

func (service Service) Complete(ctx context.Context, runID int64, result contract.ExecutionResult) error {
	if service.Store == nil {
		return fmt.Errorf("run store is required")
	}

	runStatus := strings.TrimSpace(result.Status)
	if runStatus == "" {
		runStatus = "completed"
	}
	summary := strings.TrimSpace(result.Output)
	if summary == "" {
		summary = runStatus
	}
	artifactsJSON := artifactsJSONFromMetadata(result.Metadata)

	run, err := service.Store.GetRun(ctx, runID)
	if err != nil {
		return err
	}

	taskStatus := "completed"
	if runStatus != "completed" {
		taskStatus = "failed"
	}
	if _, err := service.Store.FinishRun(ctx, sqlite.FinishRunParams{
		RunID:          runID,
		Status:         runStatus,
		Summary:        summary,
		TerminalReason: runStatus,
		ArtifactsJSON:  artifactsJSON,
	}); err != nil {
		return err
	}
	_, err = service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID:                 run.TaskID,
		Status:                 taskStatus,
		Summary:                summary,
		TerminalReason:         runStatus,
		ArtifactsJSON:          artifactsJSON,
		AllowedCurrentStatuses: []string{"running"},
	})
	return err
}

func (service Service) Fail(ctx context.Context, runID int64, cause error) error {
	if service.Store == nil {
		return fmt.Errorf("run store is required")
	}

	terminalReason := "failed"
	if cause != nil && strings.TrimSpace(cause.Error()) != "" {
		terminalReason = strings.TrimSpace(cause.Error())
	}

	run, err := service.Store.GetRun(ctx, runID)
	if err != nil {
		return err
	}

	if _, err := service.Store.FinishRun(ctx, sqlite.FinishRunParams{
		RunID:          runID,
		Status:         "failed",
		Summary:        terminalReason,
		TerminalReason: terminalReason,
		ArtifactsJSON:  "[]",
	}); err != nil {
		return err
	}
	_, err = service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID:                 run.TaskID,
		Status:                 "failed",
		Summary:                terminalReason,
		TerminalReason:         terminalReason,
		ArtifactsJSON:          "[]",
		AllowedCurrentStatuses: []string{"running"},
	})
	return err
}

func matchesRunScope(projectKey, taskScope string, resolved scope.Resolution) bool {
	switch resolved.Kind {
	case scope.ScopeGlobal:
		return true
	case scope.ScopeNewProject:
		return taskScope == string(scope.ScopeNewProject)
	case scope.ScopeProject, scope.ScopeOdinCore:
		return projectKey == resolved.ProjectKey
	default:
		return false
	}
}

func (service Service) nextRunAttempt(ctx context.Context, taskID int64) (int, error) {
	row := service.Store.DB().QueryRowContext(ctx, `
		SELECT COALESCE(MAX(attempt), 0) + 1
		FROM runs
		WHERE task_id = ?
	`, taskID)

	var attempt int
	if err := row.Scan(&attempt); err != nil {
		return 0, err
	}
	return attempt, nil
}

func artifactsJSONFromMetadata(metadata map[string]string) string {
	if len(metadata) == 0 {
		return "[]"
	}
	if value := strings.TrimSpace(metadata["artifacts_json"]); value != "" {
		return value
	}
	if value := strings.TrimSpace(metadata["artifact_path"]); value != "" {
		payload, err := json.Marshal([]string{value})
		if err == nil {
			return string(payload)
		}
	}
	return "[]"
}
