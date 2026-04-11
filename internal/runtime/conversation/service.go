package conversation

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"odin-os/internal/runtime/projections"
)

type Service struct {
	DB             *sql.DB
	Now            func() time.Time
	StalledTimeout time.Duration
}

type Snapshot struct {
	GeneratedAt                time.Time                  `json:"generated_at"`
	ApprovalsWaiting           []ApprovalWaitingView      `json:"approvals_waiting"`
	StalledRuns                []StalledRunView           `json:"stalled_runs"`
	ActiveRuns                 []ActiveRunView            `json:"active_runs"`
	ProjectTransitions         []ProjectTransitionView    `json:"project_transitions"`
	ProjectTransitionOwnership ProjectTransitionOwnership `json:"project_transition_ownership"`
}

type ApprovalWaitingView struct {
	ApprovalID  int64  `json:"approval_id"`
	TaskID      int64  `json:"task_id"`
	TaskKey     string `json:"task_key"`
	Status      string `json:"status"`
	RequestedAt string `json:"requested_at"`
}

type ActiveRunView struct {
	RunID      int64  `json:"run_id"`
	TaskID     int64  `json:"task_id"`
	TaskKey    string `json:"task_key"`
	ProjectKey string `json:"project_key"`
	Executor   string `json:"executor"`
	Status     string `json:"status"`
	Attempt    int    `json:"attempt"`
	StartedAt  string `json:"started_at"`
}

type StalledRunView struct {
	RunID      int64  `json:"run_id"`
	TaskID     int64  `json:"task_id"`
	TaskKey    string `json:"task_key"`
	ProjectKey string `json:"project_key"`
	Executor   string `json:"executor"`
	Status     string `json:"status"`
	Attempt    int    `json:"attempt"`
	StartedAt  string `json:"started_at"`
}

type ProjectTransitionView struct {
	ProjectID       int64   `json:"project_id"`
	ProjectKey      string  `json:"project_key"`
	Name            string  `json:"name"`
	Scope           string  `json:"scope"`
	TaskCount       int     `json:"task_count"`
	OpenTaskCount   int     `json:"open_task_count"`
	LastEventAt     *string `json:"last_event_at,omitempty"`
	TransitionState string  `json:"transition_state"`
	Controller      string  `json:"controller"`
	LastReportType  string  `json:"last_report_type"`
	LastReportAt    *string `json:"last_report_at,omitempty"`
}

type ProjectTransitionOwnership struct {
	LegacyOdin int `json:"legacy_odin"`
	OdinOS     int `json:"odin_os"`
	Unknown    int `json:"unknown"`
}

func (service Service) Snapshot(ctx context.Context) (Snapshot, error) {
	db := service.DB
	if db == nil {
		return Snapshot{}, fmt.Errorf("status store is required")
	}

	now := time.Now().UTC()
	if service.Now != nil {
		now = service.Now().UTC()
	}
	stalledTimeout := service.StalledTimeout
	if stalledTimeout <= 0 {
		stalledTimeout = 30 * time.Minute
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Snapshot{}, err
	}
	defer tx.Rollback()

	approvals, err := projections.ListPendingApprovalViews(ctx, tx)
	if err != nil {
		return Snapshot{}, err
	}
	activeRuns, err := projections.ListActiveRunViews(ctx, tx)
	if err != nil {
		return Snapshot{}, err
	}
	stalledRuns, err := projections.ListStalledRunViews(ctx, tx, now.Add(-stalledTimeout))
	if err != nil {
		return Snapshot{}, err
	}
	transitions, err := projections.ListProjectTransitionViews(ctx, tx)
	if err != nil {
		return Snapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return Snapshot{}, err
	}

	return Snapshot{
		GeneratedAt:                now,
		ApprovalsWaiting:           toApprovalWaitingViews(approvals),
		StalledRuns:                toStalledRunViews(stalledRuns),
		ActiveRuns:                 toActiveRunViews(activeRuns),
		ProjectTransitions:         toProjectTransitionViews(transitions),
		ProjectTransitionOwnership: summarizeProjectTransitions(transitions),
	}, nil
}

func toApprovalWaitingViews(views []projections.PendingApprovalView) []ApprovalWaitingView {
	result := make([]ApprovalWaitingView, 0, len(views))
	for _, view := range views {
		result = append(result, ApprovalWaitingView{
			ApprovalID:  view.ApprovalID,
			TaskID:      view.TaskID,
			TaskKey:     view.TaskKey,
			Status:      view.Status,
			RequestedAt: view.RequestedAt,
		})
	}
	return result
}

func toActiveRunViews(views []projections.ActiveRunView) []ActiveRunView {
	result := make([]ActiveRunView, 0, len(views))
	for _, view := range views {
		result = append(result, ActiveRunView{
			RunID:      view.RunID,
			TaskID:     view.TaskID,
			TaskKey:    view.TaskKey,
			ProjectKey: view.ProjectKey,
			Executor:   view.Executor,
			Status:     view.Status,
			Attempt:    view.Attempt,
			StartedAt:  view.StartedAt,
		})
	}
	return result
}

func toStalledRunViews(views []projections.StalledRunView) []StalledRunView {
	result := make([]StalledRunView, 0, len(views))
	for _, view := range views {
		result = append(result, StalledRunView{
			RunID:      view.RunID,
			TaskID:     view.TaskID,
			TaskKey:    view.TaskKey,
			ProjectKey: view.ProjectKey,
			Executor:   view.Executor,
			Status:     view.Status,
			Attempt:    view.Attempt,
			StartedAt:  view.StartedAt,
		})
	}
	return result
}

func toProjectTransitionViews(views []projections.ProjectTransitionView) []ProjectTransitionView {
	result := make([]ProjectTransitionView, 0, len(views))
	for _, view := range views {
		result = append(result, ProjectTransitionView{
			ProjectID:       view.ProjectID,
			ProjectKey:      view.ProjectKey,
			Name:            view.Name,
			Scope:           view.Scope,
			TaskCount:       view.TaskCount,
			OpenTaskCount:   view.OpenTaskCount,
			LastEventAt:     view.LastEventAt,
			TransitionState: view.TransitionState,
			Controller:      view.Controller,
			LastReportType:  view.LastReportType,
			LastReportAt:    view.LastReportAt,
		})
	}
	return result
}

func summarizeProjectTransitions(views []projections.ProjectTransitionView) ProjectTransitionOwnership {
	var summary ProjectTransitionOwnership
	for _, view := range views {
		switch view.Controller {
		case "legacy_odin":
			summary.LegacyOdin++
		case "odin_os":
			summary.OdinOS++
		default:
			summary.Unknown++
		}
	}
	return summary
}
