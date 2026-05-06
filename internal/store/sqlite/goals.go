package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	runtimeevents "odin-os/internal/runtime/events"
)

var ErrInvalidGoalTransition = errors.New("invalid goal transition")
var ErrActiveGoalRunExists = errors.New("active goal run exists")

type GoalStatus string
type GoalRunStatus string

const (
	GoalStatusCreated              GoalStatus = "created"
	GoalStatusPlanned              GoalStatus = "planned"
	GoalStatusApprovedForExecution GoalStatus = "approved_for_execution"
	GoalStatusRunning              GoalStatus = "running"
	GoalStatusVerifying            GoalStatus = "verifying"
	GoalStatusCompleted            GoalStatus = "completed"
	GoalStatusBlocked              GoalStatus = "blocked"
	GoalStatusWaitingForHuman      GoalStatus = "waiting_for_human"
	GoalStatusWaitingForExternal   GoalStatus = "waiting_for_external"
)

const (
	GoalRunStatusPending            GoalRunStatus = "pending"
	GoalRunStatusRunning            GoalRunStatus = "running"
	GoalRunStatusWaitingForHuman    GoalRunStatus = "waiting_for_human"
	GoalRunStatusWaitingForExternal GoalRunStatus = "waiting_for_external"
	GoalRunStatusCompleted          GoalRunStatus = "completed"
	GoalRunStatusFailed             GoalRunStatus = "failed"
	GoalRunStatusCanceled           GoalRunStatus = "canceled"
)

type Goal struct {
	ID           int64
	Title        string
	Description  string
	Status       GoalStatus
	CreatedBy    string
	Source       string
	CurrentRunID *int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type GoalRun struct {
	ID             int64
	GoalID         int64
	Status         GoalRunStatus
	Executor       string
	Attempt        int
	Attempts       int
	MaxAttempts    int
	LastProgressAt *time.Time
	NextWakeAt     *time.Time
	StartedAt      time.Time
	FinishedAt     *time.Time
	EndedAt        *time.Time
	LeaseOwner     string
	Summary        string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type GoalEvent struct {
	ID             int64
	GoalID         int64
	GoalRunID      *int64
	EventType      string
	PreviousStatus string
	Status         string
	Actor          string
	Reason         string
	PayloadJSON    string
	OccurredAt     time.Time
}

type GoalBlocker struct {
	ID          int64
	GoalID      int64
	Status      string
	BlockerType string
	Summary     string
	DetailsJSON string
	CreatedBy   string
	ResolvedBy  string
	ResolvedAt  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type GoalEvidence struct {
	ID           int64
	GoalID       int64
	GoalRunID    *int64
	EvidenceType string
	Summary      string
	URI          string
	PayloadJSON  string
	CreatedBy    string
	CreatedAt    time.Time
}

type CreateGoalParams struct {
	Title       string
	Description string
	CreatedBy   string
	Source      string
}

type TransitionGoalParams struct {
	GoalID int64
	Status GoalStatus
	Actor  string
	Reason string
}

type UpdateGoalParams struct {
	GoalID         int64
	Title          string
	TitleSet       bool
	Description    string
	DescriptionSet bool
	Actor          string
	Reason         string
}

type ListGoalsParams struct {
	Status GoalStatus
	Limit  int
}

type ListGoalEventsParams struct {
	GoalID int64
}

type ListGoalBlockersParams struct {
	GoalID int64
	Status string
}

type RecordReviewApprovedParams struct {
	ReviewID   string
	SourceType string
	SourceID   int64
	GoalID     int64
	Status     GoalStatus
	Actor      string
	Reason     string
}

type RecordReviewRejectedParams struct {
	ReviewID   string
	SourceType string
	SourceID   int64
	GoalID     int64
	BlockerID  int64
	Status     GoalStatus
	Actor      string
	Reason     string
}

type RecordGoalRunnerObservedParams struct {
	GoalID int64
	Action string
	Reason string
	Actor  string
}

type StartGoalRunParams struct {
	GoalID   int64
	Status   string
	Executor string
	Attempt  int
}

type CreateGoalRunParams struct {
	GoalID         int64
	Status         GoalRunStatus
	Executor       string
	Attempts       int
	MaxAttempts    int
	LastProgressAt *time.Time
	NextWakeAt     *time.Time
	StartedAt      *time.Time
	LeaseOwner     string
}

type FinishGoalRunParams struct {
	GoalRunID int64
	Status    string
	Summary   string
}

type UpdateGoalRunStatusParams struct {
	GoalRunID      int64
	Status         GoalRunStatus
	Attempts       int
	MaxAttempts    int
	LastProgressAt *time.Time
	NextWakeAt     *time.Time
	EndedAt        *time.Time
	LeaseOwner     string
	Summary        string
}

type AddGoalBlockerParams struct {
	GoalID      int64
	Status      string
	BlockerType string
	Summary     string
	DetailsJSON string
	CreatedBy   string
}

type AddGoalEvidenceParams struct {
	GoalID       int64
	GoalRunID    *int64
	EvidenceType string
	Summary      string
	URI          string
	PayloadJSON  string
	CreatedBy    string
}

func (store *Store) CreateGoal(ctx context.Context, params CreateGoalParams) (Goal, error) {
	params.Title = strings.TrimSpace(params.Title)
	if params.Title == "" {
		return Goal{}, fmt.Errorf("goal title is required")
	}
	now := store.now()
	goal := Goal{
		Title:       params.Title,
		Description: strings.TrimSpace(params.Description),
		Status:      GoalStatusCreated,
		CreatedBy:   defaultString(params.CreatedBy, "operator"),
		Source:      defaultString(params.Source, "runtime"),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO goals (title, description, status, created_by, source, current_run_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, NULL, ?, ?)
		`, goal.Title, goal.Description, string(goal.Status), goal.CreatedBy, goal.Source, formatTime(now), formatTime(now))
		if err != nil {
			return err
		}
		goal.ID, err = result.LastInsertId()
		if err != nil {
			return err
		}
		return store.appendGoalEventTx(ctx, tx, goalEventInsert{
			Goal:      goal,
			EventType: runtimeevents.EventGoalCreated,
			Status:    string(goal.Status),
			Actor:     goal.CreatedBy,
			Reason:    "created",
			Payload: runtimeevents.GoalCreatedPayload{
				Title:       goal.Title,
				Description: goal.Description,
				Status:      string(goal.Status),
				CreatedBy:   goal.CreatedBy,
				Source:      goal.Source,
			},
			OccurredAt: now,
		})
	})
	return goal, err
}

func (store *Store) TransitionGoal(ctx context.Context, params TransitionGoalParams) (Goal, error) {
	if params.GoalID <= 0 {
		return Goal{}, fmt.Errorf("goal id must be positive")
	}
	status := normalizeGoalStatus(params.Status)
	if status == "" {
		return Goal{}, fmt.Errorf("goal status is required")
	}
	now := store.now()
	var updated Goal
	err := store.withTx(ctx, func(tx *sql.Tx) error {
		current, err := store.getGoalTx(ctx, tx, params.GoalID)
		if err != nil {
			return err
		}
		if !isAllowedGoalTransition(current.Status, status) {
			return fmt.Errorf("%w: %s -> %s", ErrInvalidGoalTransition, current.Status, status)
		}
		if current.Status == status {
			updated = current
			return nil
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE goals
			SET status = ?, updated_at = ?
			WHERE id = ?
		`, string(status), formatTime(now), current.ID); err != nil {
			return err
		}
		updated = current
		updated.Status = status
		updated.UpdatedAt = now
		return store.appendGoalEventTx(ctx, tx, goalEventInsert{
			Goal:           updated,
			EventType:      runtimeevents.EventGoalStatusChanged,
			PreviousStatus: string(current.Status),
			Status:         string(status),
			Actor:          defaultString(params.Actor, "operator"),
			Reason:         strings.TrimSpace(params.Reason),
			Payload: runtimeevents.GoalStatusChangedPayload{
				PreviousStatus: string(current.Status),
				Status:         string(status),
				Actor:          defaultString(params.Actor, "operator"),
				Reason:         strings.TrimSpace(params.Reason),
			},
			OccurredAt: now,
		})
	})
	return updated, err
}

func (store *Store) UpdateGoal(ctx context.Context, params UpdateGoalParams) (Goal, error) {
	if params.GoalID <= 0 {
		return Goal{}, fmt.Errorf("goal id must be positive")
	}
	if !params.TitleSet && !params.DescriptionSet {
		return Goal{}, fmt.Errorf("at least one goal field is required")
	}
	title := strings.TrimSpace(params.Title)
	if params.TitleSet && title == "" {
		return Goal{}, fmt.Errorf("goal title is required")
	}
	description := strings.TrimSpace(params.Description)
	now := store.now()
	var updated Goal
	err := store.withTx(ctx, func(tx *sql.Tx) error {
		current, err := store.getGoalTx(ctx, tx, params.GoalID)
		if err != nil {
			return err
		}
		updated = current
		if params.TitleSet {
			updated.Title = title
		}
		if params.DescriptionSet {
			updated.Description = description
		}
		updated.UpdatedAt = now
		if _, err := tx.ExecContext(ctx, `
			UPDATE goals
			SET title = ?, description = ?, updated_at = ?
			WHERE id = ?
		`, updated.Title, updated.Description, formatTime(now), current.ID); err != nil {
			return err
		}
		return store.appendGoalEventTx(ctx, tx, goalEventInsert{
			Goal:      updated,
			EventType: runtimeevents.EventGoalUpdated,
			Status:    string(updated.Status),
			Actor:     defaultString(params.Actor, "operator"),
			Reason:    strings.TrimSpace(params.Reason),
			Payload: runtimeevents.GoalUpdatedPayload{
				PreviousTitle:       current.Title,
				Title:               updated.Title,
				PreviousDescription: current.Description,
				Description:         updated.Description,
				Status:              string(updated.Status),
				Actor:               defaultString(params.Actor, "operator"),
				Reason:              strings.TrimSpace(params.Reason),
			},
			OccurredAt: now,
		})
	})
	return updated, err
}

func (store *Store) GetGoal(ctx context.Context, id int64) (Goal, error) {
	row := store.db.QueryRowContext(ctx, goalSelectSQL()+` WHERE id = ?`, id)
	return scanGoal(row)
}

func (store *Store) ListGoals(ctx context.Context, params ListGoalsParams) ([]Goal, error) {
	query := goalSelectSQL()
	var args []any
	if params.Status != "" {
		status := normalizeGoalStatus(params.Status)
		if status == "" {
			return nil, fmt.Errorf("unsupported goal status: %s", params.Status)
		}
		query += ` WHERE status = ?`
		args = append(args, string(status))
	}
	query += ` ORDER BY id ASC`
	if params.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, params.Limit)
	}
	rows, err := store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	goals := make([]Goal, 0)
	for rows.Next() {
		goal, err := scanGoal(rows)
		if err != nil {
			return nil, err
		}
		goals = append(goals, goal)
	}
	return goals, rows.Err()
}

func (store *Store) ListGoalEvents(ctx context.Context, params ListGoalEventsParams) ([]GoalEvent, error) {
	query := `
		SELECT id, goal_id, goal_run_id, event_type, previous_status, status, actor, reason, payload_json, occurred_at
		FROM goal_events
		WHERE 1 = 1
	`
	var args []any
	if params.GoalID > 0 {
		query += ` AND goal_id = ?`
		args = append(args, params.GoalID)
	}
	query += ` ORDER BY id ASC`
	rows, err := store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]GoalEvent, 0)
	for rows.Next() {
		event, err := scanGoalEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (store *Store) ListGoalBlockers(ctx context.Context, params ListGoalBlockersParams) ([]GoalBlocker, error) {
	query := `
		SELECT id, goal_id, status, blocker_type, summary, details_json, created_by, resolved_by, resolved_at, created_at, updated_at
		FROM goal_blockers
		WHERE 1 = 1
	`
	var args []any
	if params.GoalID > 0 {
		query += ` AND goal_id = ?`
		args = append(args, params.GoalID)
	}
	if strings.TrimSpace(params.Status) != "" {
		query += ` AND status = ?`
		args = append(args, strings.TrimSpace(params.Status))
	}
	query += ` ORDER BY id ASC`
	rows, err := store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	blockers := make([]GoalBlocker, 0)
	for rows.Next() {
		blocker, err := scanGoalBlocker(rows)
		if err != nil {
			return nil, err
		}
		blockers = append(blockers, blocker)
	}
	return blockers, rows.Err()
}

func (store *Store) RecordReviewApproved(ctx context.Context, params RecordReviewApprovedParams) error {
	if params.GoalID <= 0 {
		return fmt.Errorf("goal id must be positive")
	}
	reviewID := strings.TrimSpace(params.ReviewID)
	if reviewID == "" {
		return fmt.Errorf("review id is required")
	}
	sourceType := strings.TrimSpace(params.SourceType)
	if sourceType == "" {
		return fmt.Errorf("review source type is required")
	}
	status := normalizeGoalStatus(params.Status)
	if status == "" {
		return fmt.Errorf("goal status is required")
	}
	now := store.now()
	return store.withTx(ctx, func(tx *sql.Tx) error {
		goal, err := store.getGoalTx(ctx, tx, params.GoalID)
		if err != nil {
			return err
		}
		return store.appendGoalEventTx(ctx, tx, goalEventInsert{
			Goal:      goal,
			EventType: runtimeevents.EventReviewApproved,
			Status:    string(status),
			Actor:     defaultString(params.Actor, "review"),
			Reason:    defaultString(params.Reason, "review approved"),
			Payload: runtimeevents.ReviewApprovedPayload{
				ReviewID:   reviewID,
				SourceType: sourceType,
				SourceID:   params.SourceID,
				GoalID:     params.GoalID,
				Status:     string(status),
				Actor:      defaultString(params.Actor, "review"),
				Reason:     defaultString(params.Reason, "review approved"),
			},
			OccurredAt: now,
		})
	})
}

func (store *Store) RecordReviewRejected(ctx context.Context, params RecordReviewRejectedParams) error {
	if params.GoalID <= 0 {
		return fmt.Errorf("goal id must be positive")
	}
	reviewID := strings.TrimSpace(params.ReviewID)
	if reviewID == "" {
		return fmt.Errorf("review id is required")
	}
	sourceType := strings.TrimSpace(params.SourceType)
	if sourceType == "" {
		return fmt.Errorf("review source type is required")
	}
	reason := strings.TrimSpace(params.Reason)
	if reason == "" {
		return fmt.Errorf("review rejection reason is required")
	}
	status := normalizeGoalStatus(params.Status)
	if status == "" {
		return fmt.Errorf("goal status is required")
	}
	now := store.now()
	return store.withTx(ctx, func(tx *sql.Tx) error {
		goal, err := store.getGoalTx(ctx, tx, params.GoalID)
		if err != nil {
			return err
		}
		return store.appendGoalEventTx(ctx, tx, goalEventInsert{
			Goal:      goal,
			EventType: runtimeevents.EventReviewRejected,
			Status:    string(status),
			Actor:     defaultString(params.Actor, "review"),
			Reason:    reason,
			Payload: runtimeevents.ReviewRejectedPayload{
				ReviewID:   reviewID,
				SourceType: sourceType,
				SourceID:   params.SourceID,
				GoalID:     params.GoalID,
				BlockerID:  params.BlockerID,
				Status:     string(status),
				Actor:      defaultString(params.Actor, "review"),
				Reason:     reason,
			},
			OccurredAt: now,
		})
	})
}

func (store *Store) RecordGoalRunnerObserved(ctx context.Context, params RecordGoalRunnerObservedParams) error {
	if params.GoalID <= 0 {
		return fmt.Errorf("goal id must be positive")
	}
	action := strings.TrimSpace(params.Action)
	if action == "" {
		return fmt.Errorf("goal runner action is required")
	}
	now := store.now()
	return store.withTx(ctx, func(tx *sql.Tx) error {
		goal, err := store.getGoalTx(ctx, tx, params.GoalID)
		if err != nil {
			return err
		}
		return store.appendGoalEventTx(ctx, tx, goalEventInsert{
			Goal:      goal,
			EventType: runtimeevents.EventGoalRunnerObserved,
			Status:    string(goal.Status),
			Actor:     defaultString(params.Actor, "goal_runner"),
			Reason:    strings.TrimSpace(params.Reason),
			Payload: runtimeevents.GoalRunnerObservedPayload{
				GoalID: goal.ID,
				Status: string(goal.Status),
				Action: action,
				Reason: strings.TrimSpace(params.Reason),
				Actor:  defaultString(params.Actor, "goal_runner"),
			},
			OccurredAt: now,
		})
	})
}

func (store *Store) StartGoalRun(ctx context.Context, params StartGoalRunParams) (GoalRun, error) {
	if params.GoalID <= 0 {
		return GoalRun{}, fmt.Errorf("goal id must be positive")
	}
	status := normalizeGoalRunStatus(GoalRunStatus(params.Status))
	if status == "" {
		status = GoalRunStatusRunning
	}
	attempts := params.Attempt
	if attempts <= 0 {
		attempts = 1
	}
	return store.CreateGoalRun(ctx, CreateGoalRunParams{
		GoalID:      params.GoalID,
		Status:      status,
		Executor:    params.Executor,
		Attempts:    attempts,
		MaxAttempts: attempts,
	})
}

func (store *Store) CreateGoalRun(ctx context.Context, params CreateGoalRunParams) (GoalRun, error) {
	if params.GoalID <= 0 {
		return GoalRun{}, fmt.Errorf("goal id must be positive")
	}
	status := normalizeGoalRunStatus(params.Status)
	if status == "" {
		status = GoalRunStatusRunning
	}
	now := store.now()
	startedAt := now
	if params.StartedAt != nil {
		startedAt = params.StartedAt.UTC()
	}
	attempts := params.Attempts
	if attempts <= 0 {
		attempts = 1
	}
	maxAttempts := params.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	lastProgressAt := cloneTimePtr(params.LastProgressAt)
	if lastProgressAt == nil {
		lastProgressAt = cloneTimePtr(&startedAt)
	}
	run := GoalRun{
		GoalID:         params.GoalID,
		Status:         status,
		Executor:       strings.TrimSpace(params.Executor),
		Attempt:        attempts,
		Attempts:       attempts,
		MaxAttempts:    maxAttempts,
		LastProgressAt: lastProgressAt,
		NextWakeAt:     cloneTimePtr(params.NextWakeAt),
		StartedAt:      startedAt,
		LeaseOwner:     strings.TrimSpace(params.LeaseOwner),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	err := store.withTx(ctx, func(tx *sql.Tx) error {
		goal, err := store.getGoalTx(ctx, tx, params.GoalID)
		if err != nil {
			return err
		}
		if isActiveGoalRunStatus(run.Status) {
			if _, err := store.getActiveGoalRunByGoalIDTx(ctx, tx, params.GoalID); err == nil {
				return ErrActiveGoalRunExists
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO goal_runs (
				goal_id, status, executor, attempt, attempts, max_attempts,
				last_progress_at, next_wake_at, started_at, finished_at, ended_at,
				lease_owner, summary, created_at, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, ?, '', ?, ?)
		`, run.GoalID, string(run.Status), run.Executor, run.Attempt, run.Attempts, run.MaxAttempts, nullTime(run.LastProgressAt), nullTime(run.NextWakeAt), formatTime(run.StartedAt), run.LeaseOwner, formatTime(now), formatTime(now))
		if err != nil {
			if isActiveGoalRunConstraintError(err) {
				return ErrActiveGoalRunExists
			}
			return err
		}
		run.ID, err = result.LastInsertId()
		if err != nil {
			return err
		}
		if isActiveGoalRunStatus(run.Status) {
			if _, err := tx.ExecContext(ctx, `UPDATE goals SET current_run_id = ?, updated_at = ? WHERE id = ?`, run.ID, formatTime(now), goal.ID); err != nil {
				return err
			}
		}
		runID := run.ID
		return store.appendGoalEventTx(ctx, tx, goalEventInsert{
			Goal:      goal,
			GoalRunID: &runID,
			EventType: runtimeevents.EventGoalRunStarted,
			Status:    string(run.Status),
			Actor:     "runtime",
			Reason:    "goal run started",
			Payload: runtimeevents.GoalRunStartedPayload{
				GoalRunID: run.ID,
				Status:    string(run.Status),
				Executor:  run.Executor,
				Attempt:   run.Attempt,
			},
			OccurredAt: now,
		})
	})
	return run, err
}

func (store *Store) FinishGoalRun(ctx context.Context, params FinishGoalRunParams) (GoalRun, error) {
	if params.GoalRunID <= 0 {
		return GoalRun{}, fmt.Errorf("goal run id must be positive")
	}
	now := store.now()
	var run GoalRun
	err := store.withTx(ctx, func(tx *sql.Tx) error {
		current, err := store.getGoalRunTx(ctx, tx, params.GoalRunID)
		if err != nil {
			return err
		}
		status := normalizeGoalRunStatus(GoalRunStatus(params.Status))
		if status == "" {
			status = GoalRunStatusCompleted
		}
		summary := strings.TrimSpace(params.Summary)
		if _, err := tx.ExecContext(ctx, `
			UPDATE goal_runs
			SET status = ?, finished_at = ?, ended_at = ?, summary = ?, updated_at = ?
			WHERE id = ?
		`, string(status), formatTime(now), formatTime(now), summary, formatTime(now), current.ID); err != nil {
			return err
		}
		run = current
		run.Status = status
		run.FinishedAt = &now
		run.EndedAt = &now
		run.Summary = summary
		run.UpdatedAt = now
		if _, err := tx.ExecContext(ctx, `UPDATE goals SET current_run_id = NULL, updated_at = ? WHERE id = ? AND current_run_id = ?`, formatTime(now), run.GoalID, run.ID); err != nil {
			return err
		}
		goal, err := store.getGoalTx(ctx, tx, run.GoalID)
		if err != nil {
			return err
		}
		runID := run.ID
		return store.appendGoalEventTx(ctx, tx, goalEventInsert{
			Goal:      goal,
			GoalRunID: &runID,
			EventType: runtimeevents.EventGoalRunFinished,
			Status:    string(run.Status),
			Actor:     "runtime",
			Reason:    "goal run finished",
			Payload: runtimeevents.GoalRunFinishedPayload{
				GoalRunID: run.ID,
				Status:    string(run.Status),
				Summary:   run.Summary,
			},
			OccurredAt: now,
		})
	})
	return run, err
}

func (store *Store) GetActiveGoalRunByGoalID(ctx context.Context, goalID int64) (GoalRun, error) {
	if goalID <= 0 {
		return GoalRun{}, fmt.Errorf("goal id must be positive")
	}
	row := store.db.QueryRowContext(ctx, activeGoalRunSelectSQL(), goalID)
	return scanGoalRun(row)
}

func (store *Store) ListGoalRunsByGoalID(ctx context.Context, goalID int64) ([]GoalRun, error) {
	if goalID <= 0 {
		return nil, fmt.Errorf("goal id must be positive")
	}
	rows, err := store.db.QueryContext(ctx, goalRunSelectSQL()+` WHERE goal_id = ? ORDER BY id ASC`, goalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := make([]GoalRun, 0)
	for rows.Next() {
		run, err := scanGoalRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (store *Store) UpdateGoalRunStatus(ctx context.Context, params UpdateGoalRunStatusParams) (GoalRun, error) {
	if params.GoalRunID <= 0 {
		return GoalRun{}, fmt.Errorf("goal run id must be positive")
	}
	status := normalizeGoalRunStatus(params.Status)
	if status == "" {
		return GoalRun{}, fmt.Errorf("goal run status is required")
	}
	now := store.now()
	var updated GoalRun
	err := store.withTx(ctx, func(tx *sql.Tx) error {
		current, err := store.getGoalRunTx(ctx, tx, params.GoalRunID)
		if err != nil {
			return err
		}
		updated = current
		updated.Status = status
		if params.Attempts > 0 {
			updated.Attempt = params.Attempts
			updated.Attempts = params.Attempts
		}
		if params.MaxAttempts > 0 {
			updated.MaxAttempts = params.MaxAttempts
		}
		if params.LastProgressAt != nil {
			updated.LastProgressAt = cloneTimePtr(params.LastProgressAt)
		}
		if params.NextWakeAt != nil {
			updated.NextWakeAt = cloneTimePtr(params.NextWakeAt)
		}
		if strings.TrimSpace(params.LeaseOwner) != "" {
			updated.LeaseOwner = strings.TrimSpace(params.LeaseOwner)
		}
		updated.Summary = strings.TrimSpace(params.Summary)
		if params.EndedAt != nil {
			updated.EndedAt = cloneTimePtr(params.EndedAt)
			updated.FinishedAt = cloneTimePtr(params.EndedAt)
		} else if isTerminalGoalRunStatus(status) && updated.EndedAt == nil {
			updated.EndedAt = cloneTimePtr(&now)
			updated.FinishedAt = cloneTimePtr(&now)
		}
		updated.UpdatedAt = now
		if _, err := tx.ExecContext(ctx, `
			UPDATE goal_runs
			SET status = ?, attempt = ?, attempts = ?, max_attempts = ?, last_progress_at = ?, next_wake_at = ?,
				finished_at = ?, ended_at = ?, lease_owner = ?, summary = ?, updated_at = ?
			WHERE id = ?
		`, string(updated.Status), updated.Attempt, updated.Attempts, updated.MaxAttempts, nullTime(updated.LastProgressAt), nullTime(updated.NextWakeAt), nullTime(updated.FinishedAt), nullTime(updated.EndedAt), updated.LeaseOwner, updated.Summary, formatTime(now), updated.ID); err != nil {
			return err
		}
		if isTerminalGoalRunStatus(updated.Status) {
			if _, err := tx.ExecContext(ctx, `UPDATE goals SET current_run_id = NULL, updated_at = ? WHERE id = ? AND current_run_id = ?`, formatTime(now), updated.GoalID, updated.ID); err != nil {
				return err
			}
		} else if isActiveGoalRunStatus(updated.Status) {
			if _, err := tx.ExecContext(ctx, `UPDATE goals SET current_run_id = ?, updated_at = ? WHERE id = ?`, updated.ID, formatTime(now), updated.GoalID); err != nil {
				return err
			}
		}
		goal, err := store.getGoalTx(ctx, tx, updated.GoalID)
		if err != nil {
			return err
		}
		runID := updated.ID
		return store.appendGoalEventTx(ctx, tx, goalEventInsert{
			Goal:           goal,
			GoalRunID:      &runID,
			EventType:      runtimeevents.EventGoalRunStatusChanged,
			PreviousStatus: string(current.Status),
			Status:         string(updated.Status),
			Actor:          "runtime",
			Reason:         strings.TrimSpace(params.Summary),
			Payload: runtimeevents.GoalRunStatusChangedPayload{
				GoalRunID:      updated.ID,
				PreviousStatus: string(current.Status),
				Status:         string(updated.Status),
				Attempts:       updated.Attempts,
				MaxAttempts:    updated.MaxAttempts,
				LeaseOwner:     updated.LeaseOwner,
				NextWakeAt:     timePtrString(updated.NextWakeAt),
				LastProgressAt: timePtrString(updated.LastProgressAt),
				EndedAt:        timePtrString(updated.EndedAt),
				Summary:        updated.Summary,
			},
			OccurredAt: now,
		})
	})
	return updated, err
}

func (store *Store) AddGoalBlocker(ctx context.Context, params AddGoalBlockerParams) (GoalBlocker, error) {
	if params.GoalID <= 0 {
		return GoalBlocker{}, fmt.Errorf("goal id must be positive")
	}
	if strings.TrimSpace(params.Summary) == "" {
		return GoalBlocker{}, fmt.Errorf("goal blocker summary is required")
	}
	now := store.now()
	blocker := GoalBlocker{
		GoalID:      params.GoalID,
		Status:      defaultString(params.Status, "open"),
		BlockerType: strings.TrimSpace(params.BlockerType),
		Summary:     strings.TrimSpace(params.Summary),
		DetailsJSON: defaultString(params.DetailsJSON, "{}"),
		CreatedBy:   defaultString(params.CreatedBy, "operator"),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	err := store.withTx(ctx, func(tx *sql.Tx) error {
		goal, err := store.getGoalTx(ctx, tx, params.GoalID)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO goal_blockers (goal_id, status, blocker_type, summary, details_json, created_by, resolved_by, resolved_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, '', NULL, ?, ?)
		`, blocker.GoalID, blocker.Status, blocker.BlockerType, blocker.Summary, blocker.DetailsJSON, blocker.CreatedBy, formatTime(now), formatTime(now))
		if err != nil {
			return err
		}
		blocker.ID, err = result.LastInsertId()
		if err != nil {
			return err
		}
		return store.appendGoalEventTx(ctx, tx, goalEventInsert{
			Goal:      goal,
			EventType: runtimeevents.EventGoalBlockerRecorded,
			Status:    string(goal.Status),
			Actor:     blocker.CreatedBy,
			Reason:    blocker.Summary,
			Payload: runtimeevents.GoalBlockerRecordedPayload{
				BlockerID:   blocker.ID,
				Status:      blocker.Status,
				BlockerType: blocker.BlockerType,
				Summary:     blocker.Summary,
				CreatedBy:   blocker.CreatedBy,
			},
			OccurredAt: now,
		})
	})
	return blocker, err
}

func (store *Store) AddGoalEvidence(ctx context.Context, params AddGoalEvidenceParams) (GoalEvidence, error) {
	if params.GoalID <= 0 {
		return GoalEvidence{}, fmt.Errorf("goal id must be positive")
	}
	if strings.TrimSpace(params.EvidenceType) == "" {
		return GoalEvidence{}, fmt.Errorf("goal evidence type is required")
	}
	if strings.TrimSpace(params.Summary) == "" {
		return GoalEvidence{}, fmt.Errorf("goal evidence summary is required")
	}
	now := store.now()
	evidence := GoalEvidence{
		GoalID:       params.GoalID,
		GoalRunID:    cloneInt64Ptr(params.GoalRunID),
		EvidenceType: strings.TrimSpace(params.EvidenceType),
		Summary:      strings.TrimSpace(params.Summary),
		URI:          strings.TrimSpace(params.URI),
		PayloadJSON:  defaultString(params.PayloadJSON, "{}"),
		CreatedBy:    defaultString(params.CreatedBy, "operator"),
		CreatedAt:    now,
	}
	err := store.withTx(ctx, func(tx *sql.Tx) error {
		goal, err := store.getGoalTx(ctx, tx, params.GoalID)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO goal_evidence (goal_id, goal_run_id, evidence_type, summary, uri, payload_json, created_by, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, evidence.GoalID, nullInt64(evidence.GoalRunID), evidence.EvidenceType, evidence.Summary, evidence.URI, evidence.PayloadJSON, evidence.CreatedBy, formatTime(now))
		if err != nil {
			return err
		}
		evidence.ID, err = result.LastInsertId()
		if err != nil {
			return err
		}
		return store.appendGoalEventTx(ctx, tx, goalEventInsert{
			Goal:      goal,
			GoalRunID: evidence.GoalRunID,
			EventType: runtimeevents.EventGoalEvidenceRecorded,
			Status:    string(goal.Status),
			Actor:     evidence.CreatedBy,
			Reason:    evidence.Summary,
			Payload: runtimeevents.GoalEvidenceRecordedPayload{
				EvidenceID:   evidence.ID,
				GoalRunID:    evidence.GoalRunID,
				EvidenceType: evidence.EvidenceType,
				Summary:      evidence.Summary,
				URI:          evidence.URI,
				CreatedBy:    evidence.CreatedBy,
			},
			OccurredAt: now,
		})
	})
	return evidence, err
}

type goalEventInsert struct {
	Goal           Goal
	GoalRunID      *int64
	EventType      runtimeevents.Type
	PreviousStatus string
	Status         string
	Actor          string
	Reason         string
	Payload        any
	OccurredAt     time.Time
}

func (store *Store) appendGoalEventTx(ctx context.Context, tx *sql.Tx, params goalEventInsert) error {
	payloadJSON, err := runtimeevents.EncodePayload(params.Payload)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO goal_events (goal_id, goal_run_id, event_type, previous_status, status, actor, reason, payload_json, occurred_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, params.Goal.ID, nullInt64(params.GoalRunID), string(params.EventType), params.PreviousStatus, params.Status, params.Actor, params.Reason, string(payloadJSON), formatTime(params.OccurredAt)); err != nil {
		return err
	}
	return appendEventTx(ctx, tx, eventInsert{
		StreamType: runtimeevents.StreamGoal,
		StreamID:   params.Goal.ID,
		EventType:  params.EventType,
		Scope:      "goal",
		Payload:    params.Payload,
		OccurredAt: params.OccurredAt,
	})
}

func (store *Store) getGoalTx(ctx context.Context, tx *sql.Tx, id int64) (Goal, error) {
	row := tx.QueryRowContext(ctx, goalSelectSQL()+` WHERE id = ?`, id)
	return scanGoal(row)
}

func (store *Store) getGoalRunTx(ctx context.Context, tx *sql.Tx, id int64) (GoalRun, error) {
	row := tx.QueryRowContext(ctx, goalRunSelectSQL()+` WHERE id = ?`, id)
	return scanGoalRun(row)
}

func (store *Store) getActiveGoalRunByGoalIDTx(ctx context.Context, tx *sql.Tx, goalID int64) (GoalRun, error) {
	row := tx.QueryRowContext(ctx, activeGoalRunSelectSQL(), goalID)
	return scanGoalRun(row)
}

func goalSelectSQL() string {
	return `
		SELECT id, title, description, status, created_by, source, current_run_id, created_at, updated_at
		FROM goals
	`
}

func goalRunSelectSQL() string {
	return `
		SELECT id, goal_id, status, executor, attempt, attempts, max_attempts,
			last_progress_at, next_wake_at, started_at, finished_at, ended_at,
			lease_owner, summary, created_at, updated_at
		FROM goal_runs
	`
}

func activeGoalRunSelectSQL() string {
	return goalRunSelectSQL() + `
		WHERE goal_id = ?
			AND ended_at IS NULL
			AND status NOT IN ('completed', 'failed', 'canceled')
		ORDER BY id ASC
		LIMIT 1
	`
}

type goalScanner interface {
	Scan(dest ...any) error
}

func scanGoal(scanner goalScanner) (Goal, error) {
	var goal Goal
	var currentRunID sql.NullInt64
	var createdAt, updatedAt string
	if err := scanner.Scan(&goal.ID, &goal.Title, &goal.Description, &goal.Status, &goal.CreatedBy, &goal.Source, &currentRunID, &createdAt, &updatedAt); err != nil {
		return Goal{}, err
	}
	goal.CurrentRunID = nullableInt64Ptr(currentRunID)
	parsedCreatedAt, err := parseTime(createdAt)
	if err != nil {
		return Goal{}, err
	}
	goal.CreatedAt = parsedCreatedAt
	parsedUpdatedAt, err := parseTime(updatedAt)
	if err != nil {
		return Goal{}, err
	}
	goal.UpdatedAt = parsedUpdatedAt
	return goal, nil
}

func scanGoalEvent(scanner goalScanner) (GoalEvent, error) {
	var event GoalEvent
	var goalRunID sql.NullInt64
	var occurredAt string
	if err := scanner.Scan(&event.ID, &event.GoalID, &goalRunID, &event.EventType, &event.PreviousStatus, &event.Status, &event.Actor, &event.Reason, &event.PayloadJSON, &occurredAt); err != nil {
		return GoalEvent{}, err
	}
	event.GoalRunID = nullableInt64Ptr(goalRunID)
	parsedOccurredAt, err := parseTime(occurredAt)
	if err != nil {
		return GoalEvent{}, err
	}
	event.OccurredAt = parsedOccurredAt
	return event, nil
}

func scanGoalBlocker(scanner goalScanner) (GoalBlocker, error) {
	var blocker GoalBlocker
	var resolvedAt sql.NullString
	var createdAt, updatedAt string
	if err := scanner.Scan(
		&blocker.ID,
		&blocker.GoalID,
		&blocker.Status,
		&blocker.BlockerType,
		&blocker.Summary,
		&blocker.DetailsJSON,
		&blocker.CreatedBy,
		&blocker.ResolvedBy,
		&resolvedAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return GoalBlocker{}, err
	}
	var err error
	blocker.ResolvedAt, err = parseNullableTime(resolvedAt)
	if err != nil {
		return GoalBlocker{}, err
	}
	blocker.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return GoalBlocker{}, err
	}
	blocker.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return GoalBlocker{}, err
	}
	return blocker, nil
}

func scanGoalRun(scanner goalScanner) (GoalRun, error) {
	var run GoalRun
	var lastProgressAt, nextWakeAt, finishedAt, endedAt sql.NullString
	var startedAt, createdAt, updatedAt string
	if err := scanner.Scan(
		&run.ID,
		&run.GoalID,
		&run.Status,
		&run.Executor,
		&run.Attempt,
		&run.Attempts,
		&run.MaxAttempts,
		&lastProgressAt,
		&nextWakeAt,
		&startedAt,
		&finishedAt,
		&endedAt,
		&run.LeaseOwner,
		&run.Summary,
		&createdAt,
		&updatedAt,
	); err != nil {
		return GoalRun{}, err
	}
	if run.Attempts <= 0 {
		run.Attempts = run.Attempt
	}
	if run.MaxAttempts <= 0 {
		run.MaxAttempts = 1
	}
	var err error
	parsedStartedAt, err := parseTime(startedAt)
	if err != nil {
		return GoalRun{}, err
	}
	run.StartedAt = parsedStartedAt
	run.LastProgressAt, err = parseNullableTime(lastProgressAt)
	if err != nil {
		return GoalRun{}, err
	}
	run.NextWakeAt, err = parseNullableTime(nextWakeAt)
	if err != nil {
		return GoalRun{}, err
	}
	run.FinishedAt, err = parseNullableTime(finishedAt)
	if err != nil {
		return GoalRun{}, err
	}
	run.EndedAt, err = parseNullableTime(endedAt)
	if err != nil {
		return GoalRun{}, err
	}
	run.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return GoalRun{}, err
	}
	run.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return GoalRun{}, err
	}
	return run, nil
}

func normalizeGoalRunStatus(status GoalRunStatus) GoalRunStatus {
	switch GoalRunStatus(strings.ToLower(strings.TrimSpace(string(status)))) {
	case GoalRunStatusPending:
		return GoalRunStatusPending
	case GoalRunStatusRunning:
		return GoalRunStatusRunning
	case GoalRunStatusWaitingForHuman:
		return GoalRunStatusWaitingForHuman
	case GoalRunStatusWaitingForExternal:
		return GoalRunStatusWaitingForExternal
	case GoalRunStatusCompleted:
		return GoalRunStatusCompleted
	case GoalRunStatusFailed:
		return GoalRunStatusFailed
	case GoalRunStatusCanceled:
		return GoalRunStatusCanceled
	default:
		return ""
	}
}

func isActiveGoalRunStatus(status GoalRunStatus) bool {
	status = normalizeGoalRunStatus(status)
	return status != "" && !isTerminalGoalRunStatus(status)
}

func isTerminalGoalRunStatus(status GoalRunStatus) bool {
	switch normalizeGoalRunStatus(status) {
	case GoalRunStatusCompleted, GoalRunStatusFailed, GoalRunStatusCanceled:
		return true
	default:
		return false
	}
}

func isActiveGoalRunConstraintError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "idx_goal_runs_one_active") ||
		strings.Contains(err.Error(), "UNIQUE constraint failed: goal_runs.goal_id")
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func timePtrString(value *time.Time) string {
	if value == nil {
		return ""
	}
	return formatTime(*value)
}

func normalizeGoalStatus(status GoalStatus) GoalStatus {
	switch GoalStatus(strings.ToLower(strings.TrimSpace(string(status)))) {
	case GoalStatusCreated:
		return GoalStatusCreated
	case GoalStatusPlanned:
		return GoalStatusPlanned
	case GoalStatusApprovedForExecution:
		return GoalStatusApprovedForExecution
	case GoalStatusRunning:
		return GoalStatusRunning
	case GoalStatusVerifying:
		return GoalStatusVerifying
	case GoalStatusCompleted:
		return GoalStatusCompleted
	case GoalStatusBlocked:
		return GoalStatusBlocked
	case GoalStatusWaitingForHuman:
		return GoalStatusWaitingForHuman
	case GoalStatusWaitingForExternal:
		return GoalStatusWaitingForExternal
	default:
		return ""
	}
}

func isAllowedGoalTransition(from GoalStatus, to GoalStatus) bool {
	if from == to {
		return true
	}
	if from == GoalStatusCompleted {
		return false
	}
	if to == GoalStatusBlocked || to == GoalStatusWaitingForHuman || to == GoalStatusWaitingForExternal {
		return true
	}
	switch from {
	case GoalStatusCreated:
		return to == GoalStatusPlanned
	case GoalStatusPlanned:
		return to == GoalStatusApprovedForExecution
	case GoalStatusApprovedForExecution:
		return to == GoalStatusRunning
	case GoalStatusRunning:
		return to == GoalStatusVerifying
	case GoalStatusVerifying:
		return to == GoalStatusCompleted || to == GoalStatusRunning
	case GoalStatusBlocked, GoalStatusWaitingForHuman, GoalStatusWaitingForExternal:
		return to == GoalStatusPlanned || to == GoalStatusRunning
	default:
		return false
	}
}
