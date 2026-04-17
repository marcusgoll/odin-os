package projections

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	runtimeevents "odin-os/internal/runtime/events"
)

type Queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type TaskStatusView struct {
	TaskID           int64
	ProjectID        int64
	ProjectKey       string
	TaskKey          string
	Title            string
	Status           string
	Scope            string
	CurrentRunID     *int64
	CurrentRunStatus string
}

type RunSummaryView struct {
	RunID      int64
	TaskID     int64
	TaskKey    string
	Executor   string
	Status     string
	Attempt    int
	StartedAt  string
	FinishedAt *string
}

type PendingApprovalView struct {
	ApprovalID  int64
	TaskID      int64
	TaskKey     string
	Status      string
	RequestedAt string
}

type ProjectTransitionView struct {
	ProjectID       int64
	ProjectKey      string
	Name            string
	Scope           string
	TaskCount       int
	OpenTaskCount   int
	LastEventAt     *string
	TransitionState string
	Controller      string
	LastReportType  string
	LastReportAt    *string
}

type ActiveRunView struct {
	RunID      int64
	TaskID     int64
	TaskKey    string
	ProjectKey string
	Executor   string
	Status     string
	Attempt    int
	StartedAt  string
}

type BlockedItemView struct {
	TaskID        int64
	TaskKey       string
	ProjectKey    string
	WorkspaceKey  string
	InitiativeKey string
	CompanionKey  string
	Objective     string
	NextStep      string
	Source        string
	Reason        string
}

type IncidentView struct {
	IncidentID int64
	RunID      int64
	TaskID     int64
	TaskKey    string
	ProjectKey string
	Severity   string
	Status     string
	Summary    string
	OpenedAt   string
}

type RecoveryView struct {
	RecoveryID int64
	RunID      int64
	Status     string
	Strategy   string
	StartedAt  string
}

type FreshnessView struct {
	Surface     string
	Status      string
	RefreshedAt string
	DetailsJSON string
}

type ProjectPortfolioView struct {
	ProjectID            int64
	ProjectKey           string
	Name                 string
	Scope                string
	OpenTaskCount        int
	ActiveRunCount       int
	PendingApprovalCount int
	OpenIncidentCount    int
}

type LearningProposalView struct {
	ProposalID      int64
	ProposalType    string
	Scope           string
	TargetKey       string
	Summary         string
	Status          string
	LatestScore     *float64
	LatestOutcome   string
	LastEvaluatedAt *string
}

type ActiveLearningPromotionView struct {
	PromotionID  int64
	ProposalID   int64
	ProposalType string
	Scope        string
	TargetKey    string
	Status       string
	PromotedBy   string
	PromotedAt   string
}

func ListTaskStatusViews(ctx context.Context, queryer Queryer) ([]TaskStatusView, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT
			t.id,
			p.id,
			p.key,
			t.key,
			t.title,
			t.status,
			t.scope,
			t.current_run_id,
			COALESCE(r.status, '')
		FROM tasks t
		JOIN projects p ON p.id = t.project_id
		LEFT JOIN runs r ON r.id = t.current_run_id
		ORDER BY t.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var views []TaskStatusView
	for rows.Next() {
		var view TaskStatusView
		var currentRunID sql.NullInt64
		if err := rows.Scan(
			&view.TaskID,
			&view.ProjectID,
			&view.ProjectKey,
			&view.TaskKey,
			&view.Title,
			&view.Status,
			&view.Scope,
			&currentRunID,
			&view.CurrentRunStatus,
		); err != nil {
			return nil, err
		}
		view.CurrentRunID = nullableInt64Ptr(currentRunID)
		views = append(views, view)
	}

	return views, rows.Err()
}

func ListRunSummaryViews(ctx context.Context, queryer Queryer) ([]RunSummaryView, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT
			r.id,
			r.task_id,
			t.key,
			r.executor,
			r.status,
			r.attempt,
			r.started_at,
			r.finished_at
		FROM runs r
		JOIN tasks t ON t.id = r.task_id
		ORDER BY r.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var views []RunSummaryView
	for rows.Next() {
		var view RunSummaryView
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
		); err != nil {
			return nil, err
		}
		if finishedAt.Valid {
			view.FinishedAt = &finishedAt.String
		}
		views = append(views, view)
	}

	return views, rows.Err()
}

func ListPendingApprovalViews(ctx context.Context, queryer Queryer) ([]PendingApprovalView, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT
			a.id,
			a.task_id,
			t.key,
			a.status,
			a.requested_at
		FROM approvals a
		JOIN tasks t ON t.id = a.task_id
		WHERE a.status = 'pending'
		ORDER BY a.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var views []PendingApprovalView
	for rows.Next() {
		var view PendingApprovalView
		if err := rows.Scan(
			&view.ApprovalID,
			&view.TaskID,
			&view.TaskKey,
			&view.Status,
			&view.RequestedAt,
		); err != nil {
			return nil, err
		}
		views = append(views, view)
	}

	return views, rows.Err()
}

func ListProjectTransitionViews(ctx context.Context, queryer Queryer) ([]ProjectTransitionView, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT
			p.id,
			p.key,
			p.name,
			p.scope,
			COUNT(DISTINCT t.id),
			COUNT(DISTINCT CASE WHEN t.status NOT IN ('completed', 'cancelled') THEN t.id END),
			MAX(e.occurred_at),
			COALESCE(pt.state, ''),
			COALESCE(pt.controller, ''),
			COALESCE((
				SELECT ptr.report_type
				FROM project_transition_reports ptr
				WHERE ptr.project_id = p.id
				ORDER BY ptr.id DESC
				LIMIT 1
			), ''),
			(
				SELECT ptr.recorded_at
				FROM project_transition_reports ptr
				WHERE ptr.project_id = p.id
				ORDER BY ptr.id DESC
				LIMIT 1
			)
		FROM projects p
		LEFT JOIN tasks t ON t.project_id = p.id
		LEFT JOIN events e ON e.project_id = p.id
		LEFT JOIN project_transitions pt ON pt.project_id = p.id
		GROUP BY p.id, p.key, p.name, p.scope
		ORDER BY p.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var views []ProjectTransitionView
	for rows.Next() {
		var view ProjectTransitionView
		var lastEventAt sql.NullString
		var lastReportAt sql.NullString
		if err := rows.Scan(
			&view.ProjectID,
			&view.ProjectKey,
			&view.Name,
			&view.Scope,
			&view.TaskCount,
			&view.OpenTaskCount,
			&lastEventAt,
			&view.TransitionState,
			&view.Controller,
			&view.LastReportType,
			&lastReportAt,
		); err != nil {
			return nil, err
		}
		if lastEventAt.Valid {
			view.LastEventAt = &lastEventAt.String
		}
		if lastReportAt.Valid {
			view.LastReportAt = &lastReportAt.String
		}
		views = append(views, view)
	}

	return views, rows.Err()
}

func ListActiveRunViews(ctx context.Context, queryer Queryer) ([]ActiveRunView, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT
			r.id,
			r.task_id,
			t.key,
			p.key,
			r.executor,
			r.status,
			r.attempt,
			r.started_at
		FROM runs r
		JOIN tasks t ON t.id = r.task_id
		JOIN projects p ON p.id = t.project_id
		WHERE r.status NOT IN ('completed', 'cancelled', 'failed')
		ORDER BY r.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var views []ActiveRunView
	for rows.Next() {
		var view ActiveRunView
		if err := rows.Scan(
			&view.RunID,
			&view.TaskID,
			&view.TaskKey,
			&view.ProjectKey,
			&view.Executor,
			&view.Status,
			&view.Attempt,
			&view.StartedAt,
		); err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, rows.Err()
}

func ListBlockedItemViews(ctx context.Context, queryer Queryer) ([]BlockedItemView, error) {
	var views []BlockedItemView

	approvalRows, err := queryer.QueryContext(ctx, `
		SELECT t.id, t.key, p.key, w.key, COALESCE(i.key, ''), COALESCE(c.key, ''), a.status
		FROM approvals a
		JOIN tasks t ON t.id = a.task_id
		JOIN projects p ON p.id = t.project_id
		JOIN workspaces w ON w.id = t.workspace_id
		LEFT JOIN initiatives i ON i.id = t.initiative_id
		LEFT JOIN companions c ON c.id = t.companion_id
		WHERE a.status = 'pending'
		ORDER BY a.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer approvalRows.Close()

	for approvalRows.Next() {
		var view BlockedItemView
		var approvalStatus string
		if err := approvalRows.Scan(&view.TaskID, &view.TaskKey, &view.ProjectKey, &view.WorkspaceKey, &view.InitiativeKey, &view.CompanionKey, &approvalStatus); err != nil {
			return nil, err
		}
		view.Source = "approval"
		view.Reason = approvalStatus
		views = append(views, view)
	}
	if err := approvalRows.Err(); err != nil {
		return nil, err
	}

	incidentRows, err := queryer.QueryContext(ctx, `
		SELECT t.id, t.key, p.key, w.key, COALESCE(wi.key, ''), COALESCE(c.key, ''), i.summary
		FROM incidents i
		JOIN runs r ON r.id = i.run_id
		JOIN tasks t ON t.id = r.task_id
		JOIN projects p ON p.id = t.project_id
		JOIN workspaces w ON w.id = t.workspace_id
		LEFT JOIN initiatives wi ON wi.id = t.initiative_id
		LEFT JOIN companions c ON c.id = t.companion_id
		WHERE i.status = 'open'
		ORDER BY i.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer incidentRows.Close()

	for incidentRows.Next() {
		var view BlockedItemView
		if err := incidentRows.Scan(&view.TaskID, &view.TaskKey, &view.ProjectKey, &view.WorkspaceKey, &view.InitiativeKey, &view.CompanionKey, &view.Reason); err != nil {
			return nil, err
		}
		view.Source = "incident"
		views = append(views, view)
	}
	if err := incidentRows.Err(); err != nil {
		return nil, err
	}

	wakeRows, err := queryer.QueryContext(ctx, `
		SELECT cp.task_id, t.key, p.key, w.key, i.key, c.key, cp.payload_json
		FROM context_packets cp
		JOIN tasks t ON t.id = cp.task_id
		JOIN projects p ON p.id = t.project_id
		JOIN workspaces w ON w.id = t.workspace_id
		LEFT JOIN initiatives i ON i.id = t.initiative_id
		LEFT JOIN companions c ON c.id = t.companion_id
		WHERE cp.packet_scope = 'task_wake_packet'
		  AND cp.status = 'active'
		ORDER BY cp.id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer wakeRows.Close()

	seenWakeTasks := make(map[int64]bool)
	for wakeRows.Next() {
		var taskID int64
		var taskKey string
		var projectKey string
		var workspaceKey string
		var initiativeKey sql.NullString
		var companionKey sql.NullString
		var payloadJSON string
		if err := wakeRows.Scan(&taskID, &taskKey, &projectKey, &workspaceKey, &initiativeKey, &companionKey, &payloadJSON); err != nil {
			return nil, err
		}
		if seenWakeTasks[taskID] {
			continue
		}
		seenWakeTasks[taskID] = true

		var payload struct {
			WorkspaceKey   string   `json:"workspace_key"`
			InitiativeKey  string   `json:"initiative_key"`
			CompanionKey   string   `json:"companion_key"`
			ProjectKey     string   `json:"project_key"`
			Objective      string   `json:"objective"`
			BlockingReason string   `json:"blocking_reason"`
			NextSteps      []string `json:"next_steps"`
		}
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
			return nil, err
		}
		if payload.BlockingReason == "" {
			continue
		}
		itemWorkspaceKey := workspaceKey
		if payload.WorkspaceKey != "" {
			itemWorkspaceKey = payload.WorkspaceKey
		}
		itemInitiativeKey := initiativeKey.String
		if payload.InitiativeKey != "" {
			itemInitiativeKey = payload.InitiativeKey
		}
		itemCompanionKey := companionKey.String
		if payload.CompanionKey != "" {
			itemCompanionKey = payload.CompanionKey
		}
		itemProjectKey := projectKey
		if payload.ProjectKey != "" {
			itemProjectKey = payload.ProjectKey
		}
		nextStep := ""
		if len(payload.NextSteps) > 0 {
			nextStep = payload.NextSteps[0]
		}
		views = append(views, BlockedItemView{
			TaskID:        taskID,
			TaskKey:       taskKey,
			ProjectKey:    itemProjectKey,
			WorkspaceKey:  itemWorkspaceKey,
			InitiativeKey: itemInitiativeKey,
			CompanionKey:  itemCompanionKey,
			Objective:     payload.Objective,
			NextStep:      nextStep,
			Source:        "wake_packet",
			Reason:        payload.BlockingReason,
		})
	}

	return views, wakeRows.Err()
}

func ListIncidentViews(ctx context.Context, queryer Queryer) ([]IncidentView, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT
			i.id,
			r.id,
			t.id,
			t.key,
			p.key,
			i.severity,
			i.status,
			i.summary,
			i.opened_at
		FROM incidents i
		JOIN runs r ON r.id = i.run_id
		JOIN tasks t ON t.id = r.task_id
		JOIN projects p ON p.id = t.project_id
		ORDER BY i.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var views []IncidentView
	for rows.Next() {
		var view IncidentView
		if err := rows.Scan(
			&view.IncidentID,
			&view.RunID,
			&view.TaskID,
			&view.TaskKey,
			&view.ProjectKey,
			&view.Severity,
			&view.Status,
			&view.Summary,
			&view.OpenedAt,
		); err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, rows.Err()
}

func ListRecoveryViews(ctx context.Context, queryer Queryer) ([]RecoveryView, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT id, COALESCE(run_id, 0), status, strategy, started_at
		FROM recoveries
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var views []RecoveryView
	for rows.Next() {
		var view RecoveryView
		if err := rows.Scan(&view.RecoveryID, &view.RunID, &view.Status, &view.Strategy, &view.StartedAt); err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, rows.Err()
}

func ListFreshnessViews(ctx context.Context, queryer Queryer) ([]FreshnessView, error) {
	var views []FreshnessView

	rows, err := queryer.QueryContext(ctx, `
		SELECT surface, status, refreshed_at, details_json
		FROM projection_freshness
		ORDER BY surface ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var view FreshnessView
		if err := rows.Scan(&view.Surface, &view.Status, &view.RefreshedAt, &view.DetailsJSON); err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var compiledAt string
	if err := scanOptionalSingleString(queryer, `
		SELECT compiled_at
		FROM registry_versions
		ORDER BY compiled_at DESC, id DESC
		LIMIT 1
	`, &compiledAt); err != nil {
		return nil, err
	}
	if compiledAt != "" {
		views = append(views, FreshnessView{
			Surface:     "registry_source",
			Status:      "recorded",
			RefreshedAt: compiledAt,
			DetailsJSON: "{}",
		})
	}

	var executorCheckedAt string
	if err := scanOptionalSingleString(queryer, `
		SELECT checked_at
		FROM executor_health
		ORDER BY checked_at DESC, id DESC
		LIMIT 1
	`, &executorCheckedAt); err != nil {
		return nil, err
	}
	if executorCheckedAt != "" {
		views = append(views, FreshnessView{
			Surface:     "executor_health",
			Status:      "recorded",
			RefreshedAt: executorCheckedAt,
			DetailsJSON: "{}",
		})
	}

	return views, nil
}

func ListProjectPortfolioViews(ctx context.Context, queryer Queryer) ([]ProjectPortfolioView, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT
			p.id,
			p.key,
			p.name,
			p.scope,
			(SELECT COUNT(*) FROM tasks t WHERE t.project_id = p.id AND t.status NOT IN ('completed', 'cancelled')) AS open_task_count,
			(SELECT COUNT(*)
			 FROM runs r
			 JOIN tasks t ON t.id = r.task_id
			 WHERE t.project_id = p.id
			   AND r.status NOT IN ('completed', 'cancelled', 'failed')) AS active_run_count,
			(SELECT COUNT(*)
			 FROM approvals a
			 JOIN tasks t ON t.id = a.task_id
			 WHERE t.project_id = p.id
			   AND a.status = 'pending') AS pending_approval_count,
			(SELECT COUNT(*)
			 FROM incidents i
			 JOIN runs r ON r.id = i.run_id
			 JOIN tasks t ON t.id = r.task_id
			 WHERE t.project_id = p.id
			   AND i.status = 'open') AS open_incident_count
		FROM projects p
		ORDER BY p.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var views []ProjectPortfolioView
	for rows.Next() {
		var view ProjectPortfolioView
		if err := rows.Scan(
			&view.ProjectID,
			&view.ProjectKey,
			&view.Name,
			&view.Scope,
			&view.OpenTaskCount,
			&view.ActiveRunCount,
			&view.PendingApprovalCount,
			&view.OpenIncidentCount,
		); err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, rows.Err()
}

func ListLearningProposalViews(ctx context.Context, queryer Queryer) ([]LearningProposalView, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT
			lp.id,
			lp.proposal_type,
			lp.scope,
			lp.target_key,
			lp.summary,
			lp.status,
			le.score,
			COALESCE(le.outcome, ''),
			le.recorded_at
		FROM learning_proposals lp
		LEFT JOIN learning_evaluations le ON le.id = (
			SELECT le2.id
			FROM learning_evaluations le2
			WHERE le2.proposal_id = lp.id
			ORDER BY le2.id DESC
			LIMIT 1
		)
		ORDER BY lp.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var views []LearningProposalView
	for rows.Next() {
		var view LearningProposalView
		var latestScore sql.NullFloat64
		var lastEvaluatedAt sql.NullString
		if err := rows.Scan(
			&view.ProposalID,
			&view.ProposalType,
			&view.Scope,
			&view.TargetKey,
			&view.Summary,
			&view.Status,
			&latestScore,
			&view.LatestOutcome,
			&lastEvaluatedAt,
		); err != nil {
			return nil, err
		}
		if latestScore.Valid {
			score := latestScore.Float64
			view.LatestScore = &score
		}
		if lastEvaluatedAt.Valid {
			view.LastEvaluatedAt = &lastEvaluatedAt.String
		}
		views = append(views, view)
	}

	return views, rows.Err()
}

func ListActiveLearningPromotionViews(ctx context.Context, queryer Queryer) ([]ActiveLearningPromotionView, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT id, proposal_id, proposal_type, scope, target_key, status, promoted_by, promoted_at
		FROM learning_promotions
		WHERE status = 'active'
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var views []ActiveLearningPromotionView
	for rows.Next() {
		var view ActiveLearningPromotionView
		if err := rows.Scan(
			&view.PromotionID,
			&view.ProposalID,
			&view.ProposalType,
			&view.Scope,
			&view.TargetKey,
			&view.Status,
			&view.PromotedBy,
			&view.PromotedAt,
		); err != nil {
			return nil, err
		}
		views = append(views, view)
	}

	return views, rows.Err()
}

type LifecycleReplay struct {
	Tasks     map[int64]TaskReplay
	Runs      map[int64]RunReplay
	Approvals map[int64]ApprovalReplay
}

type TaskReplay struct {
	ID           int64
	Key          string
	Title        string
	Status       string
	Scope        string
	CurrentRunID *int64
}

type RunReplay struct {
	ID       int64
	TaskID   int64
	Executor string
	Attempt  int
	Status   string
	Summary  string
}

type ApprovalReplay struct {
	ID          int64
	TaskID      int64
	RunID       *int64
	Status      string
	RequestedBy string
	DecisionBy  string
	Reason      string
}

func ReplayLifecycle(records []runtimeevents.Record) (LifecycleReplay, error) {
	replay := LifecycleReplay{
		Tasks:     make(map[int64]TaskReplay),
		Runs:      make(map[int64]RunReplay),
		Approvals: make(map[int64]ApprovalReplay),
	}

	for _, record := range records {
		switch record.Type {
		case runtimeevents.EventTaskCreated:
			payload, err := runtimeevents.DecodePayload[runtimeevents.TaskCreatedPayload](record.Payload)
			if err != nil {
				return LifecycleReplay{}, fmt.Errorf("decode %s payload: %w", record.Type, err)
			}
			replay.Tasks[record.StreamID] = TaskReplay{
				ID:     record.StreamID,
				Key:    payload.Key,
				Title:  payload.Title,
				Status: payload.Status,
				Scope:  payload.Scope,
			}
		case runtimeevents.EventTaskStatusChanged:
			payload, err := runtimeevents.DecodePayload[runtimeevents.TaskStatusChangedPayload](record.Payload)
			if err != nil {
				return LifecycleReplay{}, fmt.Errorf("decode %s payload: %w", record.Type, err)
			}
			task := replay.Tasks[record.StreamID]
			task.ID = record.StreamID
			task.Status = payload.Status
			if record.RunID != nil {
				task.CurrentRunID = record.RunID
			}
			replay.Tasks[record.StreamID] = task
		case runtimeevents.EventRunStarted:
			payload, err := runtimeevents.DecodePayload[runtimeevents.RunStartedPayload](record.Payload)
			if err != nil {
				return LifecycleReplay{}, fmt.Errorf("decode %s payload: %w", record.Type, err)
			}
			replay.Runs[record.StreamID] = RunReplay{
				ID:       record.StreamID,
				TaskID:   payload.TaskID,
				Executor: payload.Executor,
				Attempt:  payload.Attempt,
				Status:   payload.Status,
			}
			task := replay.Tasks[payload.TaskID]
			task.ID = payload.TaskID
			runID := record.StreamID
			task.CurrentRunID = &runID
			replay.Tasks[payload.TaskID] = task
		case runtimeevents.EventRunFinished:
			payload, err := runtimeevents.DecodePayload[runtimeevents.RunFinishedPayload](record.Payload)
			if err != nil {
				return LifecycleReplay{}, fmt.Errorf("decode %s payload: %w", record.Type, err)
			}
			run := replay.Runs[record.StreamID]
			run.ID = record.StreamID
			run.Status = payload.Status
			run.Summary = payload.Summary
			replay.Runs[record.StreamID] = run
		case runtimeevents.EventApprovalRequested:
			payload, err := runtimeevents.DecodePayload[runtimeevents.ApprovalRequestedPayload](record.Payload)
			if err != nil {
				return LifecycleReplay{}, fmt.Errorf("decode %s payload: %w", record.Type, err)
			}
			replay.Approvals[record.StreamID] = ApprovalReplay{
				ID:          record.StreamID,
				TaskID:      payload.TaskID,
				RunID:       payload.RunID,
				Status:      payload.Status,
				RequestedBy: payload.RequestedBy,
			}
		case runtimeevents.EventApprovalResolved:
			payload, err := runtimeevents.DecodePayload[runtimeevents.ApprovalResolvedPayload](record.Payload)
			if err != nil {
				return LifecycleReplay{}, fmt.Errorf("decode %s payload: %w", record.Type, err)
			}
			approval := replay.Approvals[record.StreamID]
			approval.ID = record.StreamID
			approval.Status = payload.Status
			approval.DecisionBy = payload.DecisionBy
			approval.Reason = payload.Reason
			replay.Approvals[record.StreamID] = approval
		}
	}

	return replay, nil
}

func scanOptionalSingleString(queryer Queryer, query string, value *string) error {
	rows, err := queryer.QueryContext(context.Background(), query)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		if err := rows.Scan(value); err != nil {
			return err
		}
	}
	return rows.Err()
}

func nullableInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	ptr := new(int64)
	*ptr = value.Int64
	return ptr
}
