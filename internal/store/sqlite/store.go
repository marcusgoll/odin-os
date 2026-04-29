package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	runtimeevents "odin-os/internal/runtime/events"

	_ "modernc.org/sqlite"
)

var ErrWorktreeLeaseConflict = errors.New("worktree lease conflict")
var ErrInvalidActionApprovalBinding = errors.New("invalid action approval binding")
var ErrInvalidActionEvidenceLink = errors.New("invalid action evidence link")
var ErrApprovalPayloadMismatch = errors.New("approval_payload_mismatch")

type Store struct {
	db        *sql.DB
	closeOnce sync.Once
	Now       func() time.Time
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		_ = db.Close()
		return nil, err
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Store{db: db}, nil
}

func (store *Store) Close() error {
	var err error
	store.closeOnce.Do(func() {
		err = store.db.Close()
	})
	return err
}

func (store *Store) DB() *sql.DB {
	return store.db
}

func (store *Store) now() time.Time {
	if store.Now != nil {
		return store.Now().UTC()
	}
	return time.Now().UTC()
}

func (store *Store) CreateProject(ctx context.Context, params CreateProjectParams) (Project, error) {
	now := store.now()
	var project Project

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO projects (key, name, scope, git_root, default_branch, github_repo, manifest_path, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			params.Key,
			params.Name,
			params.Scope,
			params.GitRoot,
			params.DefaultBranch,
			nullIfEmpty(params.GitHubRepo),
			params.ManifestPath,
			formatTime(now),
			formatTime(now),
		)
		if err != nil {
			return err
		}

		projectID, err := result.LastInsertId()
		if err != nil {
			return err
		}

		project = Project{
			ID:            projectID,
			Key:           params.Key,
			Name:          params.Name,
			Scope:         params.Scope,
			GitRoot:       params.GitRoot,
			DefaultBranch: params.DefaultBranch,
			GitHubRepo:    params.GitHubRepo,
			ManifestPath:  params.ManifestPath,
			CreatedAt:     now,
			UpdatedAt:     now,
		}

		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamProject,
			StreamID:   project.ID,
			EventType:  runtimeevents.EventProjectCreated,
			Scope:      project.Scope,
			ProjectID:  &project.ID,
			Payload: runtimeevents.ProjectCreatedPayload{
				Key:           project.Key,
				Name:          project.Name,
				Scope:         project.Scope,
				GitRoot:       project.GitRoot,
				DefaultBranch: project.DefaultBranch,
				GitHubRepo:    project.GitHubRepo,
				ManifestPath:  project.ManifestPath,
			},
			OccurredAt: now,
		})
	})

	return project, err
}

func (store *Store) CreateTask(ctx context.Context, params CreateTaskParams) (Task, error) {
	now := store.now()
	var task Task

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		project, err := store.getProjectTx(ctx, tx, params.ProjectID)
		if err != nil {
			return err
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO tasks (project_id, key, title, status, scope, requested_by, current_run_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, NULL, ?, ?)
		`,
			params.ProjectID,
			params.Key,
			params.Title,
			params.Status,
			params.Scope,
			params.RequestedBy,
			formatTime(now),
			formatTime(now),
		)
		if err != nil {
			return err
		}

		taskID, err := result.LastInsertId()
		if err != nil {
			return err
		}

		task = Task{
			ID:          taskID,
			ProjectID:   params.ProjectID,
			Key:         params.Key,
			Title:       params.Title,
			Status:      params.Status,
			Scope:       params.Scope,
			RequestedBy: params.RequestedBy,
			CreatedAt:   now,
			UpdatedAt:   now,
		}

		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamTask,
			StreamID:   task.ID,
			EventType:  runtimeevents.EventTaskCreated,
			Scope:      task.Scope,
			ProjectID:  &project.ID,
			TaskID:     &task.ID,
			Payload: runtimeevents.TaskCreatedPayload{
				Key:         task.Key,
				Title:       task.Title,
				Status:      task.Status,
				Scope:       task.Scope,
				RequestedBy: task.RequestedBy,
			},
			OccurredAt: now,
		})
	})

	return task, err
}

func (store *Store) UpdateTaskStatus(ctx context.Context, params UpdateTaskStatusParams) (Task, error) {
	now := store.now()
	var task Task

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		current, err := store.getTaskTx(ctx, tx, params.TaskID)
		if err != nil {
			return err
		}
		previousStatus := current.Status

		if _, err := tx.ExecContext(ctx, `
			UPDATE tasks
			SET status = ?, updated_at = ?
			WHERE id = ?
		`, params.Status, formatTime(now), params.TaskID); err != nil {
			return err
		}

		current.Status = params.Status
		current.UpdatedAt = now
		task = current

		projectID := task.ProjectID
		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamTask,
			StreamID:   task.ID,
			EventType:  runtimeevents.EventTaskStatusChanged,
			Scope:      task.Scope,
			ProjectID:  &projectID,
			TaskID:     &task.ID,
			Payload: runtimeevents.TaskStatusChangedPayload{
				PreviousStatus: previousStatus,
				Status:         params.Status,
			},
			OccurredAt: now,
		})
	})

	return task, err
}

func (store *Store) StartRun(ctx context.Context, params StartRunParams) (Run, error) {
	now := store.now()
	var run Run

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		task, err := store.getTaskTx(ctx, tx, params.TaskID)
		if err != nil {
			return err
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO runs (task_id, executor, status, attempt, started_at, finished_at, summary)
			VALUES (?, ?, ?, ?, ?, NULL, '')
		`,
			params.TaskID,
			params.Executor,
			params.Status,
			params.Attempt,
			formatTime(now),
		)
		if err != nil {
			return err
		}

		runID, err := result.LastInsertId()
		if err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE tasks
			SET current_run_id = ?, updated_at = ?
			WHERE id = ?
		`, runID, formatTime(now), params.TaskID); err != nil {
			return err
		}

		run = Run{
			ID:        runID,
			TaskID:    params.TaskID,
			Executor:  params.Executor,
			Status:    params.Status,
			Attempt:   params.Attempt,
			StartedAt: now,
		}

		projectID := task.ProjectID
		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamRun,
			StreamID:   run.ID,
			EventType:  runtimeevents.EventRunStarted,
			Scope:      task.Scope,
			ProjectID:  &projectID,
			TaskID:     &task.ID,
			RunID:      &run.ID,
			Payload: runtimeevents.RunStartedPayload{
				TaskID:   task.ID,
				Executor: run.Executor,
				Attempt:  run.Attempt,
				Status:   run.Status,
			},
			OccurredAt: now,
		})
	})

	return run, err
}

func (store *Store) FinishRun(ctx context.Context, params FinishRunParams) (Run, error) {
	now := store.now()
	var run Run

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		current, task, err := store.getRunWithTaskTx(ctx, tx, params.RunID)
		if err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE runs
			SET status = ?, finished_at = ?, summary = ?
			WHERE id = ?
		`, params.Status, formatTime(now), params.Summary, params.RunID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE tasks
			SET current_run_id = NULL, updated_at = ?
			WHERE id = ?
		`, formatTime(now), task.ID); err != nil {
			return err
		}

		current.Status = params.Status
		current.FinishedAt = &now
		current.Summary = params.Summary
		run = current

		projectID := task.ProjectID
		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamRun,
			StreamID:   run.ID,
			EventType:  runtimeevents.EventRunFinished,
			Scope:      task.Scope,
			ProjectID:  &projectID,
			TaskID:     &task.ID,
			RunID:      &run.ID,
			Payload: runtimeevents.RunFinishedPayload{
				Status:  run.Status,
				Summary: run.Summary,
			},
			OccurredAt: now,
		})
	})

	return run, err
}

func (store *Store) RequestApproval(ctx context.Context, params RequestApprovalParams) (Approval, error) {
	now := store.now()
	var approval Approval

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		task, err := store.getTaskTx(ctx, tx, params.TaskID)
		if err != nil {
			return err
		}
		if err := validateActionApprovalBinding(ctx, tx, params.TaskID, params.RunID, params.ActionID, params.PayloadHash); err != nil {
			return err
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO approvals (task_id, run_id, action_id, payload_hash, status, requested_at, resolved_at, decision_by, reason)
			VALUES (?, ?, ?, ?, ?, ?, NULL, '', '')
		`,
			params.TaskID,
			nullInt64(params.RunID),
			nullInt64(params.ActionID),
			nullIfEmpty(params.PayloadHash),
			params.Status,
			formatTime(now),
		)
		if err != nil {
			return err
		}

		approvalID, err := result.LastInsertId()
		if err != nil {
			return err
		}

		approval = Approval{
			ID:          approvalID,
			TaskID:      params.TaskID,
			RunID:       params.RunID,
			ActionID:    params.ActionID,
			PayloadHash: params.PayloadHash,
			Status:      params.Status,
			RequestedAt: now,
		}

		projectID := task.ProjectID
		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamApproval,
			StreamID:   approval.ID,
			EventType:  runtimeevents.EventApprovalRequested,
			Scope:      task.Scope,
			ProjectID:  &projectID,
			TaskID:     &task.ID,
			RunID:      params.RunID,
			Payload: runtimeevents.ApprovalRequestedPayload{
				TaskID:      task.ID,
				RunID:       params.RunID,
				ActionID:    approval.ActionID,
				PayloadHash: approval.PayloadHash,
				Status:      approval.Status,
				RequestedBy: params.RequestedBy,
			},
			OccurredAt: now,
		})
	})

	return approval, err
}

func (store *Store) ResolveApproval(ctx context.Context, params ResolveApprovalParams) (Approval, error) {
	now := store.now()
	var approval Approval

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		current, task, err := store.getApprovalWithTaskTx(ctx, tx, params.ApprovalID)
		if err != nil {
			return err
		}
		if err := validateApprovalPayloadCurrent(ctx, tx, current); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE approvals
			SET status = ?, resolved_at = ?, decision_by = ?, reason = ?
			WHERE id = ?
		`,
			params.Status,
			formatTime(now),
			params.DecisionBy,
			params.Reason,
			params.ApprovalID,
		); err != nil {
			return err
		}

		current.Status = params.Status
		current.ResolvedAt = &now
		current.DecisionBy = params.DecisionBy
		current.Reason = params.Reason
		approval = current

		projectID := task.ProjectID
		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamApproval,
			StreamID:   approval.ID,
			EventType:  runtimeevents.EventApprovalResolved,
			Scope:      task.Scope,
			ProjectID:  &projectID,
			TaskID:     &task.ID,
			RunID:      approval.RunID,
			Payload: runtimeevents.ApprovalResolvedPayload{
				Status:      approval.Status,
				DecisionBy:  approval.DecisionBy,
				Reason:      approval.Reason,
				ActionID:    approval.ActionID,
				PayloadHash: approval.PayloadHash,
			},
			OccurredAt: now,
		})
	})

	return approval, err
}

func (store *Store) CreateActionWithPayload(ctx context.Context, params CreateActionWithPayloadParams) (Action, ActionPayload, error) {
	now := store.now()
	var action Action
	var payload ActionPayload

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := store.getRunTx(ctx, tx, params.WorkflowRunID); err != nil {
			return err
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO actions (workflow_key, workflow_run_id, action_type, lifecycle_state, current_payload_hash, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`,
			params.WorkflowKey,
			params.WorkflowRunID,
			params.ActionType,
			"prepared",
			params.PayloadHash,
			formatTime(now),
			formatTime(now),
		)
		if err != nil {
			return err
		}

		actionID, err := result.LastInsertId()
		if err != nil {
			return err
		}

		payloadResult, err := tx.ExecContext(ctx, `
			INSERT INTO action_payloads (
				action_id,
				payload_schema,
				payload_schema_version,
				payload_hash,
				payload_json,
				submit_path,
				readback_path,
				proof_requirement,
				created_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			actionID,
			params.PayloadSchema,
			params.PayloadSchemaVersion,
			params.PayloadHash,
			params.PayloadJSON,
			params.SubmitPath,
			params.ReadbackPath,
			params.ProofRequirement,
			formatTime(now),
		)
		if err != nil {
			return err
		}

		payloadID, err := payloadResult.LastInsertId()
		if err != nil {
			return err
		}

		action = Action{
			ID:                 actionID,
			WorkflowKey:        params.WorkflowKey,
			WorkflowRunID:      params.WorkflowRunID,
			ActionType:         params.ActionType,
			LifecycleState:     "prepared",
			CurrentPayloadHash: params.PayloadHash,
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		payload = ActionPayload{
			ID:                   payloadID,
			ActionID:             actionID,
			PayloadSchema:        params.PayloadSchema,
			PayloadSchemaVersion: params.PayloadSchemaVersion,
			PayloadHash:          params.PayloadHash,
			PayloadJSON:          params.PayloadJSON,
			SubmitPath:           params.SubmitPath,
			ReadbackPath:         params.ReadbackPath,
			ProofRequirement:     params.ProofRequirement,
			CreatedAt:            now,
		}
		return nil
	})

	return action, payload, err
}

func (store *Store) AppendActionEvidence(ctx context.Context, params AppendActionEvidenceParams) (ActionEvidenceEvent, error) {
	now := store.now()
	var event ActionEvidenceEvent

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		if err := validateActionEvidenceLinks(ctx, tx, params); err != nil {
			return err
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO action_evidence_events (
				action_id,
				event_type,
				event_version,
				payload_hash,
				approval_id,
				run_id,
				source,
				evidence_json,
				occurred_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			params.ActionID,
			params.EventType,
			params.EventVersion,
			nullIfEmpty(params.PayloadHash),
			nullInt64(params.ApprovalID),
			nullInt64(params.RunID),
			params.Source,
			params.EvidenceJSON,
			formatTime(now),
		)
		if err != nil {
			return err
		}

		eventID, err := result.LastInsertId()
		if err != nil {
			return err
		}

		event = ActionEvidenceEvent{
			ID:           eventID,
			ActionID:     params.ActionID,
			EventType:    params.EventType,
			EventVersion: params.EventVersion,
			PayloadHash:  stringPtrIfNotEmpty(params.PayloadHash),
			ApprovalID:   params.ApprovalID,
			RunID:        params.RunID,
			Source:       params.Source,
			EvidenceJSON: params.EvidenceJSON,
			OccurredAt:   now,
		}
		return nil
	})
	return event, err
}

func (store *Store) GetAction(ctx context.Context, actionID int64) (Action, ActionPayload, error) {
	action, err := store.getAction(ctx, actionID)
	if err != nil {
		return Action{}, ActionPayload{}, err
	}

	row := store.db.QueryRowContext(ctx, `
		SELECT id, action_id, payload_schema, payload_schema_version, payload_hash, payload_json, submit_path, readback_path, proof_requirement, created_at
		FROM action_payloads
		WHERE action_id = ? AND payload_hash = ?
	`, action.ID, action.CurrentPayloadHash)
	payload, err := scanActionPayload(row)
	if err != nil {
		return Action{}, ActionPayload{}, err
	}

	return action, payload, nil
}

func (store *Store) ListActions(ctx context.Context, params ListActionsParams) ([]Action, error) {
	query := `
		SELECT id, workflow_key, workflow_run_id, action_type, lifecycle_state, current_payload_hash, created_at, updated_at
		FROM actions
		WHERE 1 = 1
	`
	var args []any
	if params.WorkflowKey != "" {
		query += ` AND workflow_key = ?`
		args = append(args, params.WorkflowKey)
	}
	if params.WorkflowRunID != nil {
		query += ` AND workflow_run_id = ?`
		args = append(args, *params.WorkflowRunID)
	}
	query += ` ORDER BY created_at DESC, id DESC`

	rows, err := store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var actions []Action
	for rows.Next() {
		action, err := scanAction(rows)
		if err != nil {
			return nil, err
		}
		actions = append(actions, action)
	}
	return actions, rows.Err()
}

func (store *Store) ListActionEvidence(ctx context.Context, actionID int64) ([]ActionEvidenceEvent, error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT id, action_id, event_type, event_version, payload_hash, approval_id, run_id, source, evidence_json, occurred_at
		FROM action_evidence_events
		WHERE action_id = ?
		ORDER BY id ASC
	`, actionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []ActionEvidenceEvent
	for rows.Next() {
		event, err := scanActionEvidenceEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (store *Store) OpenIncident(ctx context.Context, params OpenIncidentParams) (Incident, error) {
	now := store.now()
	var incident Incident

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		contextRow, err := store.runContextTx(ctx, tx, params.RunID)
		if err != nil {
			return err
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO incidents (run_id, severity, status, summary, details_json, opened_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`,
			nullInt64(params.RunID),
			params.Severity,
			params.Status,
			params.Summary,
			params.DetailsJSON,
			formatTime(now),
			formatTime(now),
		)
		if err != nil {
			return err
		}

		incidentID, err := result.LastInsertId()
		if err != nil {
			return err
		}

		incident = Incident{
			ID:          incidentID,
			RunID:       params.RunID,
			Severity:    params.Severity,
			Status:      params.Status,
			Summary:     params.Summary,
			DetailsJSON: params.DetailsJSON,
			OpenedAt:    now,
			UpdatedAt:   now,
		}

		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamIncident,
			StreamID:   incident.ID,
			EventType:  runtimeevents.EventIncidentOpened,
			Scope:      contextRow.Scope,
			ProjectID:  contextRow.ProjectID,
			TaskID:     contextRow.TaskID,
			RunID:      params.RunID,
			Payload: runtimeevents.IncidentOpenedPayload{
				Severity: incident.Severity,
				Status:   incident.Status,
				Summary:  incident.Summary,
			},
			OccurredAt: now,
		})
	})

	return incident, err
}

func (store *Store) UpdateIncidentStatus(ctx context.Context, params UpdateIncidentStatusParams) (Incident, error) {
	now := store.now()
	var incident Incident

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		current, contextRow, err := store.getIncidentTx(ctx, tx, params.IncidentID)
		if err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE incidents
			SET status = ?, details_json = ?, updated_at = ?
			WHERE id = ?
		`, params.Status, params.DetailsJSON, formatTime(now), params.IncidentID); err != nil {
			return err
		}

		previousStatus := current.Status
		current.Status = params.Status
		current.DetailsJSON = params.DetailsJSON
		current.UpdatedAt = now
		incident = current

		eventType, payload, ok := incidentStatusEvent(previousStatus, params.Status, params.Reason)
		if !ok {
			return nil
		}

		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamIncident,
			StreamID:   incident.ID,
			EventType:  eventType,
			Scope:      contextRow.Scope,
			ProjectID:  contextRow.ProjectID,
			TaskID:     contextRow.TaskID,
			RunID:      incident.RunID,
			Payload:    payload,
			OccurredAt: now,
		})
	})

	return incident, err
}

func (store *Store) StartRecovery(ctx context.Context, params StartRecoveryParams) (Recovery, error) {
	now := store.now()
	var recovery Recovery

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		contextRow, err := store.recoveryContextTx(ctx, tx, params.IncidentID, params.RunID)
		if err != nil {
			return err
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO recoveries (incident_id, run_id, status, strategy, details_json, started_at, finished_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, NULL, ?)
		`,
			nullInt64(params.IncidentID),
			nullInt64(params.RunID),
			params.Status,
			params.Strategy,
			params.DetailsJSON,
			formatTime(now),
			formatTime(now),
		)
		if err != nil {
			return err
		}

		recoveryID, err := result.LastInsertId()
		if err != nil {
			return err
		}

		recovery = Recovery{
			ID:          recoveryID,
			IncidentID:  params.IncidentID,
			RunID:       params.RunID,
			Status:      params.Status,
			Strategy:    params.Strategy,
			DetailsJSON: params.DetailsJSON,
			StartedAt:   now,
			UpdatedAt:   now,
		}

		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamRecovery,
			StreamID:   recovery.ID,
			EventType:  runtimeevents.EventRecoveryStarted,
			Scope:      contextRow.Scope,
			ProjectID:  contextRow.ProjectID,
			TaskID:     contextRow.TaskID,
			RunID:      params.RunID,
			Payload: runtimeevents.RecoveryStartedPayload{
				Status:   recovery.Status,
				Strategy: recovery.Strategy,
			},
			OccurredAt: now,
		})
	})

	return recovery, err
}

func (store *Store) RecordRecoveryAction(ctx context.Context, params RecordRecoveryActionParams) error {
	now := store.now()

	return store.withTx(ctx, func(tx *sql.Tx) error {
		recovery, contextRow, err := store.getRecoveryTx(ctx, tx, params.RecoveryID)
		if err != nil {
			return err
		}

		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamRecovery,
			StreamID:   recovery.ID,
			EventType:  runtimeevents.EventRecoveryActionExecuted,
			Scope:      contextRow.Scope,
			ProjectID:  contextRow.ProjectID,
			TaskID:     contextRow.TaskID,
			RunID:      recovery.RunID,
			Payload: runtimeevents.RecoveryActionExecutedPayload{
				Playbook:    params.Playbook,
				FaultKey:    params.FaultKey,
				ActionName:  params.ActionName,
				Attempt:     params.Attempt,
				Result:      params.Result,
				Description: params.Description,
			},
			OccurredAt: now,
		})
	})
}

func (store *Store) CompleteRecovery(ctx context.Context, params CompleteRecoveryParams) (Recovery, error) {
	now := store.now()
	var recovery Recovery

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		current, contextRow, err := store.getRecoveryTx(ctx, tx, params.RecoveryID)
		if err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE recoveries
			SET status = ?, details_json = ?, finished_at = ?, updated_at = ?
			WHERE id = ?
		`,
			params.Status,
			params.DetailsJSON,
			formatTime(now),
			formatTime(now),
			params.RecoveryID,
		); err != nil {
			return err
		}

		current.Status = params.Status
		current.DetailsJSON = params.DetailsJSON
		current.FinishedAt = &now
		current.UpdatedAt = now
		recovery = current

		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamRecovery,
			StreamID:   recovery.ID,
			EventType:  runtimeevents.EventRecoveryCompleted,
			Scope:      contextRow.Scope,
			ProjectID:  contextRow.ProjectID,
			TaskID:     contextRow.TaskID,
			RunID:      recovery.RunID,
			Payload: runtimeevents.RecoveryCompletedPayload{
				Status: recovery.Status,
			},
			OccurredAt: now,
		})
	})

	return recovery, err
}

func (store *Store) RecordRegistryVersion(ctx context.Context, params RecordRegistryVersionParams) (RegistryVersion, error) {
	now := store.now()
	var version RegistryVersion

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO registry_versions (source, version_hash, compiled_at, notes)
			VALUES (?, ?, ?, ?)
		`,
			params.Source,
			params.VersionHash,
			formatTime(now),
			nullIfEmpty(params.Notes),
		)
		if err != nil {
			return err
		}

		versionID, err := result.LastInsertId()
		if err != nil {
			return err
		}

		version = RegistryVersion{
			ID:          versionID,
			Source:      params.Source,
			VersionHash: params.VersionHash,
			CompiledAt:  now,
			Notes:       params.Notes,
		}

		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamRegistryVersion,
			StreamID:   version.ID,
			EventType:  runtimeevents.EventRegistryVersionRecorded,
			Scope:      "global",
			Payload: runtimeevents.RegistryVersionRecordedPayload{
				Source:      version.Source,
				VersionHash: version.VersionHash,
				Notes:       version.Notes,
			},
			OccurredAt: now,
		})
	})

	return version, err
}

func (store *Store) RecordExecutorHealth(ctx context.Context, params RecordExecutorHealthParams) (ExecutorHealth, error) {
	now := store.now()
	var health ExecutorHealth

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO executor_health (executor, status, checked_at, latency_ms, details_json)
			VALUES (?, ?, ?, ?, ?)
		`,
			params.Executor,
			params.Status,
			formatTime(now),
			params.LatencyMS,
			params.DetailsJSON,
		)
		if err != nil {
			return err
		}

		healthID, err := result.LastInsertId()
		if err != nil {
			return err
		}

		health = ExecutorHealth{
			ID:          healthID,
			Executor:    params.Executor,
			Status:      params.Status,
			CheckedAt:   now,
			LatencyMS:   params.LatencyMS,
			DetailsJSON: params.DetailsJSON,
		}

		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamExecutorHealth,
			StreamID:   health.ID,
			EventType:  runtimeevents.EventExecutorHealthRecorded,
			Scope:      "global",
			Payload: runtimeevents.ExecutorHealthRecordedPayload{
				Executor:  health.Executor,
				Status:    health.Status,
				LatencyMS: health.LatencyMS,
			},
			OccurredAt: now,
		})
	})

	return health, err
}

func (store *Store) CreateContextPacket(ctx context.Context, params CreateContextPacketParams) (ContextPacket, error) {
	now := store.now()
	var packet ContextPacket
	params = normalizeCreateContextPacketParams(params)

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		contextRow, err := store.contextPacketContextTx(ctx, tx, params.TaskID, params.RunID)
		if err != nil {
			return err
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO context_packets (
				task_id,
				run_id,
				packet_kind,
				packet_scope,
				trigger,
				checkpoint_key,
				supersedes_packet_id,
				status,
				summary,
				payload_json,
				created_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			nullInt64(params.TaskID),
			nullInt64(params.RunID),
			params.PacketKind,
			params.PacketScope,
			params.Trigger,
			params.CheckpointKey,
			nullInt64(params.SupersedesPacketID),
			params.Status,
			params.Summary,
			params.PayloadJSON,
			formatTime(now),
		)
		if err != nil {
			return err
		}

		packetID, err := result.LastInsertId()
		if err != nil {
			return err
		}

		packet = ContextPacket{
			ID:                 packetID,
			TaskID:             params.TaskID,
			RunID:              params.RunID,
			PacketKind:         params.PacketKind,
			PacketScope:        params.PacketScope,
			Trigger:            params.Trigger,
			CheckpointKey:      params.CheckpointKey,
			SupersedesPacketID: params.SupersedesPacketID,
			Status:             params.Status,
			Summary:            params.Summary,
			PayloadJSON:        params.PayloadJSON,
			CreatedAt:          now,
		}

		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamContextPacket,
			StreamID:   packet.ID,
			EventType:  runtimeevents.EventContextPacketCreated,
			Scope:      contextRow.Scope,
			ProjectID:  contextRow.ProjectID,
			TaskID:     params.TaskID,
			RunID:      params.RunID,
			Payload: runtimeevents.ContextPacketCreatedPayload{
				PacketKind:  packet.PacketKind,
				PacketScope: packet.PacketScope,
				Trigger:     packet.Trigger,
				Status:      packet.Status,
				Summary:     packet.Summary,
			},
			OccurredAt: now,
		})
	})

	return packet, err
}

func (store *Store) RecordConversationTranscript(ctx context.Context, params RecordConversationTranscriptParams) (ConversationTranscript, error) {
	now := store.now()
	var transcript ConversationTranscript

	params.Scope = strings.TrimSpace(params.Scope)
	params.ScopeKey = strings.TrimSpace(params.ScopeKey)
	params.Mode = strings.TrimSpace(params.Mode)
	if params.Scope == "" {
		return ConversationTranscript{}, fmt.Errorf("conversation transcript scope is required")
	}
	if params.ScopeKey == "" {
		return ConversationTranscript{}, fmt.Errorf("conversation transcript scope key is required")
	}
	if params.Mode == "" {
		params.Mode = "ask"
	}

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		if _, _, _, err := store.validateProjectTaskRunLineageTx(ctx, tx, params.ProjectID, params.TaskID, params.RunID, params.Scope, params.ScopeKey, "conversation transcript"); err != nil {
			return err
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO conversation_transcripts (
				project_id,
				task_id,
				run_id,
				scope,
				scope_key,
				mode,
				prompt,
				response,
				tool_summary,
				executor,
				created_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			nullInt64(params.ProjectID),
			nullInt64(params.TaskID),
			nullInt64(params.RunID),
			params.Scope,
			params.ScopeKey,
			params.Mode,
			params.Prompt,
			params.Response,
			params.ToolSummary,
			params.Executor,
			formatTime(now),
		)
		if err != nil {
			return err
		}

		transcriptID, err := result.LastInsertId()
		if err != nil {
			return err
		}

		transcript = ConversationTranscript{
			ID:          transcriptID,
			ProjectID:   params.ProjectID,
			TaskID:      params.TaskID,
			RunID:       params.RunID,
			Scope:       params.Scope,
			ScopeKey:    params.ScopeKey,
			Mode:        params.Mode,
			Prompt:      params.Prompt,
			Response:    params.Response,
			ToolSummary: params.ToolSummary,
			Executor:    params.Executor,
			CreatedAt:   now,
		}

		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamConversation,
			StreamID:   transcript.ID,
			EventType:  runtimeevents.EventConversationTranscriptRecorded,
			Scope:      transcript.Scope,
			ProjectID:  transcript.ProjectID,
			TaskID:     transcript.TaskID,
			RunID:      transcript.RunID,
			Payload: runtimeevents.ConversationTranscriptRecordedPayload{
				Scope:    transcript.Scope,
				ScopeKey: transcript.ScopeKey,
				Mode:     transcript.Mode,
				Executor: transcript.Executor,
				TaskID:   transcript.TaskID,
				RunID:    transcript.RunID,
			},
			OccurredAt: now,
		})
	})

	return transcript, err
}

func (store *Store) ListConversationTranscripts(ctx context.Context, params ListConversationTranscriptsParams) ([]ConversationTranscript, error) {
	query := `
		SELECT
			id,
			project_id,
			task_id,
			run_id,
			scope,
			scope_key,
			mode,
			prompt,
			response,
			tool_summary,
			executor,
			created_at
		FROM conversation_transcripts
		WHERE 1 = 1
	`
	var args []any
	if params.ProjectID != nil {
		query += ` AND project_id = ?`
		args = append(args, *params.ProjectID)
	}
	if params.TaskID != nil {
		query += ` AND task_id = ?`
		args = append(args, *params.TaskID)
	}
	if params.RunID != nil {
		query += ` AND run_id = ?`
		args = append(args, *params.RunID)
	}
	if params.Scope != "" {
		query += ` AND scope = ?`
		args = append(args, params.Scope)
	}
	if params.ScopeKey != "" {
		query += ` AND scope_key = ?`
		args = append(args, params.ScopeKey)
	}
	if params.Mode != "" {
		query += ` AND mode = ?`
		args = append(args, params.Mode)
	}
	query += ` ORDER BY id ASC`

	rows, err := store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var transcripts []ConversationTranscript
	for rows.Next() {
		transcript, err := scanConversationTranscript(rows)
		if err != nil {
			return nil, err
		}
		transcripts = append(transcripts, transcript)
	}

	return transcripts, rows.Err()
}

func (store *Store) RecordMemorySummary(ctx context.Context, params RecordMemorySummaryParams) (MemorySummary, error) {
	now := store.now()
	var summary MemorySummary

	params.Scope = strings.TrimSpace(params.Scope)
	params.ScopeKey = strings.TrimSpace(params.ScopeKey)
	params.MemoryType = strings.TrimSpace(params.MemoryType)
	if params.Scope == "" {
		return MemorySummary{}, fmt.Errorf("memory summary scope is required")
	}
	if params.ScopeKey == "" {
		return MemorySummary{}, fmt.Errorf("memory summary scope key is required")
	}
	if params.MemoryType == "" {
		return MemorySummary{}, fmt.Errorf("memory summary type is required")
	}

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		if _, _, _, err := store.validateProjectTaskRunLineageTx(ctx, tx, params.ProjectID, params.TaskID, params.RunID, params.Scope, params.ScopeKey, "memory summary"); err != nil {
			return err
		}
		if params.SourceTranscriptID != nil {
			transcript, err := store.getConversationTranscriptTx(ctx, tx, *params.SourceTranscriptID)
			if err != nil {
				return err
			}
			if params.Scope != transcript.Scope || params.ScopeKey != transcript.ScopeKey {
				return fmt.Errorf("memory summary scope %q/%q does not match source transcript scope %q/%q", params.Scope, params.ScopeKey, transcript.Scope, transcript.ScopeKey)
			}
			switch {
			case transcript.ProjectID != nil && params.ProjectID == nil:
				return fmt.Errorf("memory summary sourced from transcript %d requires matching project", transcript.ID)
			case transcript.ProjectID == nil && params.ProjectID != nil:
				return fmt.Errorf("memory summary project %d does not match global source transcript %d", *params.ProjectID, transcript.ID)
			case transcript.ProjectID != nil && params.ProjectID != nil && *transcript.ProjectID != *params.ProjectID:
				return fmt.Errorf("memory summary project %d does not match source transcript project %d", *params.ProjectID, *transcript.ProjectID)
			}
			if params.TaskID != nil {
				if transcript.TaskID == nil || *transcript.TaskID != *params.TaskID {
					return fmt.Errorf("memory summary task %d does not match source transcript task", *params.TaskID)
				}
			}
			if params.RunID != nil {
				if transcript.RunID == nil || *transcript.RunID != *params.RunID {
					return fmt.Errorf("memory summary run %d does not match source transcript run", *params.RunID)
				}
			}
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO memory_summaries (
				project_id,
				source_transcript_id,
				task_id,
				run_id,
				scope,
				scope_key,
				memory_type,
				summary,
				details_json,
				created_at,
				updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			nullInt64(params.ProjectID),
			nullInt64(params.SourceTranscriptID),
			nullInt64(params.TaskID),
			nullInt64(params.RunID),
			params.Scope,
			params.ScopeKey,
			params.MemoryType,
			params.Summary,
			params.DetailsJSON,
			formatTime(now),
			formatTime(now),
		)
		if err != nil {
			return err
		}

		summaryID, err := result.LastInsertId()
		if err != nil {
			return err
		}

		summary = MemorySummary{
			ID:                 summaryID,
			ProjectID:          params.ProjectID,
			SourceTranscriptID: params.SourceTranscriptID,
			TaskID:             params.TaskID,
			RunID:              params.RunID,
			Scope:              params.Scope,
			ScopeKey:           params.ScopeKey,
			MemoryType:         params.MemoryType,
			Summary:            params.Summary,
			DetailsJSON:        params.DetailsJSON,
			CreatedAt:          now,
			UpdatedAt:          now,
		}

		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamMemorySummary,
			StreamID:   summary.ID,
			EventType:  runtimeevents.EventMemorySummaryRecorded,
			Scope:      summary.Scope,
			ProjectID:  summary.ProjectID,
			TaskID:     summary.TaskID,
			RunID:      summary.RunID,
			Payload: runtimeevents.MemorySummaryRecordedPayload{
				Scope:              summary.Scope,
				ScopeKey:           summary.ScopeKey,
				MemoryType:         summary.MemoryType,
				SourceTranscriptID: summary.SourceTranscriptID,
				TaskID:             summary.TaskID,
				RunID:              summary.RunID,
			},
			OccurredAt: now,
		})
	})

	return summary, err
}

func (store *Store) ListMemorySummaries(ctx context.Context, params ListMemorySummariesParams) ([]MemorySummary, error) {
	query := `
		SELECT
			id,
			project_id,
			source_transcript_id,
			task_id,
			run_id,
			scope,
			scope_key,
			memory_type,
			summary,
			details_json,
			created_at,
			updated_at
		FROM memory_summaries
		WHERE 1 = 1
	`
	var args []any
	if params.ProjectID != nil {
		query += ` AND project_id = ?`
		args = append(args, *params.ProjectID)
	}
	if params.SourceTranscriptID != nil {
		query += ` AND source_transcript_id = ?`
		args = append(args, *params.SourceTranscriptID)
	}
	if params.TaskID != nil {
		query += ` AND task_id = ?`
		args = append(args, *params.TaskID)
	}
	if params.RunID != nil {
		query += ` AND run_id = ?`
		args = append(args, *params.RunID)
	}
	if params.Scope != "" {
		query += ` AND scope = ?`
		args = append(args, params.Scope)
	}
	if params.ScopeKey != "" {
		query += ` AND scope_key = ?`
		args = append(args, params.ScopeKey)
	}
	if params.MemoryType != "" {
		query += ` AND memory_type = ?`
		args = append(args, params.MemoryType)
	}
	query += ` ORDER BY id ASC`

	rows, err := store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []MemorySummary
	for rows.Next() {
		summary, err := scanMemorySummary(rows)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}

	return summaries, rows.Err()
}

func (store *Store) RecordKnowledgeArtifact(ctx context.Context, params RecordKnowledgeArtifactParams) (KnowledgeArtifact, error) {
	params.SHA256 = strings.TrimSpace(params.SHA256)
	params.SourceType = strings.TrimSpace(params.SourceType)
	params.MimeType = strings.TrimSpace(params.MimeType)
	params.ArtifactPath = strings.TrimSpace(params.ArtifactPath)
	params.OriginalPath = strings.TrimSpace(params.OriginalPath)

	if params.SHA256 == "" {
		return KnowledgeArtifact{}, fmt.Errorf("knowledge artifact sha256 is required")
	}
	if params.SizeBytes < 0 {
		return KnowledgeArtifact{}, fmt.Errorf("knowledge artifact size must be non-negative")
	}
	if params.SourceType == "" {
		return KnowledgeArtifact{}, fmt.Errorf("knowledge artifact source type is required")
	}
	if params.MimeType == "" {
		return KnowledgeArtifact{}, fmt.Errorf("knowledge artifact mime type is required")
	}
	if params.ArtifactPath == "" {
		return KnowledgeArtifact{}, fmt.Errorf("knowledge artifact path is required")
	}
	if params.OriginalPath == "" {
		return KnowledgeArtifact{}, fmt.Errorf("knowledge artifact original path is required")
	}

	now := store.now()
	var artifact KnowledgeArtifact
	err := store.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO knowledge_artifacts (
				sha256,
				size_bytes,
				source_type,
				mime_type,
				artifact_path,
				original_path,
				ocr_required,
				recorded_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(sha256) DO NOTHING
		`,
			params.SHA256,
			params.SizeBytes,
			params.SourceType,
			params.MimeType,
			params.ArtifactPath,
			params.OriginalPath,
			boolToInt(params.OCRRequired),
			formatTime(now),
		); err != nil {
			return err
		}

		record, err := scanKnowledgeArtifact(tx.QueryRowContext(ctx, `
			SELECT id, sha256, size_bytes, source_type, mime_type, artifact_path, original_path, ocr_required, recorded_at
			FROM knowledge_artifacts
			WHERE sha256 = ?
		`, params.SHA256))
		if err != nil {
			return err
		}
		artifact = record
		return nil
	})

	return artifact, err
}

func (store *Store) UpsertKnowledgeSource(ctx context.Context, params UpsertKnowledgeSourceParams) (KnowledgeSource, error) {
	params.Key = strings.TrimSpace(params.Key)
	params.Title = strings.TrimSpace(params.Title)
	params.Scope = strings.TrimSpace(params.Scope)
	params.ScopeKey = strings.TrimSpace(params.ScopeKey)
	params.SourceKind = strings.TrimSpace(params.SourceKind)
	params.SourceClass = strings.TrimSpace(params.SourceClass)
	params.Lifecycle = strings.TrimSpace(params.Lifecycle)
	params.ManifestPath = strings.TrimSpace(params.ManifestPath)

	if params.Key == "" {
		return KnowledgeSource{}, fmt.Errorf("knowledge source key is required")
	}
	if params.Title == "" {
		return KnowledgeSource{}, fmt.Errorf("knowledge source title is required")
	}
	if params.Scope == "" {
		return KnowledgeSource{}, fmt.Errorf("knowledge source scope is required")
	}
	if params.ScopeKey == "" {
		return KnowledgeSource{}, fmt.Errorf("knowledge source scope key is required")
	}
	if params.SourceKind == "" {
		return KnowledgeSource{}, fmt.Errorf("knowledge source kind is required")
	}
	if params.SourceClass == "" {
		return KnowledgeSource{}, fmt.Errorf("knowledge source class is required")
	}
	if params.Lifecycle == "" {
		return KnowledgeSource{}, fmt.Errorf("knowledge source lifecycle is required")
	}
	if params.ManifestPath == "" {
		return KnowledgeSource{}, fmt.Errorf("knowledge source manifest path is required")
	}
	if err := validateKnowledgeManifestPath(params.ManifestPath); err != nil {
		return KnowledgeSource{}, err
	}
	if err := validateKnowledgeSourceClass(params.SourceClass); err != nil {
		return KnowledgeSource{}, err
	}
	if err := validateKnowledgeLifecycle(params.Lifecycle); err != nil {
		return KnowledgeSource{}, err
	}

	now := store.now()
	var source KnowledgeSource
	err := store.withTx(ctx, func(tx *sql.Tx) error {
		existing, exists, err := store.getKnowledgeSourceByKeyTx(ctx, tx, params.Key)
		if err != nil {
			return err
		}

		currentArtifactID := params.CurrentArtifactID
		currentExtractionID := params.CurrentExtractionID
		if exists {
			if currentArtifactID == nil {
				currentArtifactID = existing.CurrentArtifactID
			}
			if currentExtractionID == nil {
				currentExtractionID = existing.CurrentExtractionID
			}
			if err := store.validateKnowledgeArtifactLifecycleTx(ctx, tx, currentArtifactID, params.Lifecycle); err != nil {
				return err
			}
			if err := store.validateKnowledgeCurrentExtractionTx(ctx, tx, existing.ID, currentArtifactID, currentExtractionID, params.Lifecycle); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE knowledge_sources
				SET title = ?,
					scope = ?,
					scope_key = ?,
					restricted = ?,
					source_kind = ?,
					source_class = ?,
					lifecycle = ?,
					manifest_path = ?,
					current_artifact_id = ?,
					current_extraction_id = ?,
					updated_at = ?
				WHERE id = ?
			`,
				params.Title,
				params.Scope,
				params.ScopeKey,
				boolToInt(params.Restricted),
				params.SourceKind,
				params.SourceClass,
				params.Lifecycle,
				params.ManifestPath,
				nullInt64(currentArtifactID),
				nullInt64(currentExtractionID),
				formatTime(now),
				existing.ID,
			); err != nil {
				return err
			}
			if existing.Title != params.Title {
				if err := store.reindexKnowledgeSourceChunksTx(ctx, tx, existing.ID); err != nil {
					return err
				}
			}
		} else {
			if currentExtractionID != nil {
				return fmt.Errorf("knowledge source current extraction cannot be set before source exists")
			}
			if err := store.validateKnowledgeArtifactLifecycleTx(ctx, tx, currentArtifactID, params.Lifecycle); err != nil {
				return err
			}
			if err := validateKnowledgeCurrentExtractionRequired(params.Lifecycle, currentExtractionID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO knowledge_sources (
					key,
					title,
					scope,
					scope_key,
					restricted,
					source_kind,
					source_class,
					lifecycle,
					manifest_path,
					current_artifact_id,
					current_extraction_id,
					created_at,
					updated_at
				)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			`,
				params.Key,
				params.Title,
				params.Scope,
				params.ScopeKey,
				boolToInt(params.Restricted),
				params.SourceKind,
				params.SourceClass,
				params.Lifecycle,
				params.ManifestPath,
				nullInt64(currentArtifactID),
				nullInt64(currentExtractionID),
				formatTime(now),
				formatTime(now),
			); err != nil {
				return err
			}
		}

		record, err := scanKnowledgeSource(tx.QueryRowContext(ctx, `
			SELECT id, key, title, scope, scope_key, restricted, source_kind, source_class, lifecycle, manifest_path, current_artifact_id, current_extraction_id, created_at, updated_at
			FROM knowledge_sources
			WHERE key = ?
		`, params.Key))
		if err != nil {
			return err
		}
		source = record

		if !exists {
			return appendEventTx(ctx, tx, eventInsert{
				StreamType: runtimeevents.StreamKnowledgeSource,
				StreamID:   source.ID,
				EventType:  runtimeevents.EventKnowledgeSourceIngested,
				Scope:      source.Scope,
				Payload: runtimeevents.KnowledgeSourceIngestedPayload{
					SourceID:     source.ID,
					SourceKey:    source.Key,
					Scope:        source.Scope,
					ScopeKey:     source.ScopeKey,
					ArtifactID:   source.CurrentArtifactID,
					ManifestPath: source.ManifestPath,
					Lifecycle:    source.Lifecycle,
				},
				OccurredAt: now,
			})
		}
		if existing.Lifecycle != source.Lifecycle {
			return appendKnowledgeLifecycleChangedTx(ctx, tx, existing, source, now)
		}
		return nil
	})

	return source, err
}

func (store *Store) GetKnowledgeSourceByKey(ctx context.Context, key string) (KnowledgeSource, error) {
	source, exists, err := store.getKnowledgeSourceByKeyTx(ctx, nil, strings.TrimSpace(key))
	if err != nil {
		return KnowledgeSource{}, err
	}
	if !exists {
		return KnowledgeSource{}, sql.ErrNoRows
	}
	return source, nil
}

func (store *Store) ListKnowledgeSources(ctx context.Context, params ListKnowledgeSourcesParams) ([]KnowledgeSource, error) {
	query := `
		SELECT id, key, title, scope, scope_key, restricted, source_kind, source_class, lifecycle, manifest_path, current_artifact_id, current_extraction_id, created_at, updated_at
		FROM knowledge_sources
		WHERE 1 = 1
	`
	var args []any
	if params.Scope != "" {
		query += ` AND scope = ?`
		args = append(args, params.Scope)
	}
	if params.ScopeKey != "" {
		query += ` AND scope_key = ?`
		args = append(args, params.ScopeKey)
	}
	if params.Lifecycle != "" {
		query += ` AND lifecycle = ?`
		args = append(args, params.Lifecycle)
	}
	if params.Restricted != nil {
		query += ` AND restricted = ?`
		args = append(args, boolToInt(*params.Restricted))
	}
	query += ` ORDER BY key ASC`

	rows, err := store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sources []KnowledgeSource
	for rows.Next() {
		source, err := scanKnowledgeSource(rows)
		if err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	return sources, rows.Err()
}

func (store *Store) RecordKnowledgeExtraction(ctx context.Context, params RecordKnowledgeExtractionParams) (KnowledgeExtraction, error) {
	params.ExtractorName = strings.TrimSpace(params.ExtractorName)
	params.ExtractorVersion = strings.TrimSpace(params.ExtractorVersion)
	params.Status = strings.TrimSpace(params.Status)
	params.Lifecycle = strings.TrimSpace(params.Lifecycle)
	params.FailureCode = strings.TrimSpace(params.FailureCode)
	params.FailureSummary = strings.TrimSpace(params.FailureSummary)
	params.ExtractedTextHash = strings.TrimSpace(params.ExtractedTextHash)
	params.NormalizedMarkdownPath = strings.TrimSpace(params.NormalizedMarkdownPath)

	if params.SourceID == 0 {
		return KnowledgeExtraction{}, fmt.Errorf("knowledge extraction source id is required")
	}
	if params.ArtifactID == 0 {
		return KnowledgeExtraction{}, fmt.Errorf("knowledge extraction artifact id is required")
	}
	if params.ExtractorName == "" {
		return KnowledgeExtraction{}, fmt.Errorf("knowledge extraction extractor name is required")
	}
	if params.ExtractorVersion == "" {
		return KnowledgeExtraction{}, fmt.Errorf("knowledge extraction extractor version is required")
	}
	if params.Status == "" {
		return KnowledgeExtraction{}, fmt.Errorf("knowledge extraction status is required")
	}

	now := store.now()
	startedAt := now
	if params.StartedAt != nil {
		startedAt = params.StartedAt.UTC()
	}
	finishedAt := params.FinishedAt
	if finishedAt == nil && (params.Status == "succeeded" || params.Status == "failed") {
		finishedAt = &now
	}
	lifecycle := params.Lifecycle
	if lifecycle == "" {
		lifecycle = lifecycleForKnowledgeExtractionStatus(params.Status)
	}
	if err := validateKnowledgeExtractionStatusLifecycle(params.Status, lifecycle); err != nil {
		return KnowledgeExtraction{}, err
	}

	var extraction KnowledgeExtraction
	err := store.withTx(ctx, func(tx *sql.Tx) error {
		source, err := store.getKnowledgeSourceTx(ctx, tx, params.SourceID)
		if err != nil {
			return err
		}
		if err := validateKnowledgeLifecycle(lifecycle); err != nil {
			return err
		}
		if err := store.validateKnowledgeArtifactLifecycleTx(ctx, tx, &params.ArtifactID, lifecycle); err != nil {
			return err
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO knowledge_extractions (
				source_id,
				artifact_id,
				extractor_name,
				extractor_version,
				status,
				failure_code,
				failure_summary,
				extracted_text_hash,
				normalized_markdown_path,
				started_at,
				finished_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			params.SourceID,
			params.ArtifactID,
			params.ExtractorName,
			params.ExtractorVersion,
			params.Status,
			params.FailureCode,
			params.FailureSummary,
			params.ExtractedTextHash,
			params.NormalizedMarkdownPath,
			formatTime(startedAt),
			nullTime(finishedAt),
		)
		if err != nil {
			return err
		}

		extractionID, err := result.LastInsertId()
		if err != nil {
			return err
		}

		record, err := scanKnowledgeExtraction(tx.QueryRowContext(ctx, `
			SELECT id, source_id, artifact_id, extractor_name, extractor_version, status, failure_code, failure_summary, extracted_text_hash, normalized_markdown_path, started_at, finished_at
			FROM knowledge_extractions
			WHERE id = ?
		`, extractionID))
		if err != nil {
			return err
		}
		extraction = record

		if _, err := tx.ExecContext(ctx, `
			UPDATE knowledge_sources
			SET current_artifact_id = ?, current_extraction_id = ?, lifecycle = ?, updated_at = ?
			WHERE id = ?
		`, params.ArtifactID, extraction.ID, lifecycle, formatTime(now), params.SourceID); err != nil {
			return err
		}

		updatedSource := source
		updatedSource.CurrentArtifactID = &params.ArtifactID
		updatedSource.CurrentExtractionID = &extraction.ID
		updatedSource.Lifecycle = lifecycle
		updatedSource.UpdatedAt = now

		if err := appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamKnowledgeSource,
			StreamID:   source.ID,
			EventType:  runtimeevents.EventKnowledgeExtractionRecorded,
			Scope:      source.Scope,
			Payload: runtimeevents.KnowledgeExtractionRecordedPayload{
				SourceID:     source.ID,
				SourceKey:    source.Key,
				ArtifactID:   params.ArtifactID,
				ExtractionID: extraction.ID,
				Status:       extraction.Status,
				Extractor:    extraction.ExtractorName + ":" + extraction.ExtractorVersion,
			},
			OccurredAt: now,
		}); err != nil {
			return err
		}
		if source.Lifecycle != updatedSource.Lifecycle {
			return appendKnowledgeLifecycleChangedTx(ctx, tx, source, updatedSource, now)
		}
		return nil
	})

	return extraction, err
}

func (store *Store) RecordKnowledgeChunk(ctx context.Context, params RecordKnowledgeChunkParams) (KnowledgeChunk, error) {
	params.Text = strings.TrimSpace(params.Text)
	params.Anchor = strings.TrimSpace(params.Anchor)

	if params.SourceID == 0 {
		return KnowledgeChunk{}, fmt.Errorf("knowledge chunk source id is required")
	}
	if params.ExtractionID == 0 {
		return KnowledgeChunk{}, fmt.Errorf("knowledge chunk extraction id is required")
	}
	if params.Ordinal < 0 {
		return KnowledgeChunk{}, fmt.Errorf("knowledge chunk ordinal must be non-negative")
	}
	if params.Text == "" {
		return KnowledgeChunk{}, fmt.Errorf("knowledge chunk text is required")
	}

	now := store.now()
	var chunk KnowledgeChunk
	err := store.withTx(ctx, func(tx *sql.Tx) error {
		if err := validateKnowledgeExtractionLineageTx(ctx, tx, params.SourceID, params.ExtractionID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO knowledge_chunks (
				source_id,
				extraction_id,
				ordinal,
				text,
				anchor,
				page_number,
				restricted,
				created_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(extraction_id, ordinal) DO UPDATE SET
				source_id = excluded.source_id,
				text = excluded.text,
				anchor = excluded.anchor,
				page_number = excluded.page_number,
				restricted = excluded.restricted
		`,
			params.SourceID,
			params.ExtractionID,
			params.Ordinal,
			params.Text,
			params.Anchor,
			nullInt64(params.PageNumber),
			boolToInt(params.Restricted),
			formatTime(now),
		); err != nil {
			return err
		}

		record, err := scanKnowledgeChunk(tx.QueryRowContext(ctx, `
			SELECT id, source_id, extraction_id, ordinal, text, anchor, page_number, restricted, created_at
			FROM knowledge_chunks
			WHERE extraction_id = ? AND ordinal = ?
		`, params.ExtractionID, params.Ordinal))
		if err != nil {
			return err
		}
		chunk = record

		return store.indexKnowledgeChunkTx(ctx, tx, IndexKnowledgeChunkParams{ChunkID: chunk.ID})
	})

	return chunk, err
}

func (store *Store) IndexKnowledgeChunk(ctx context.Context, params IndexKnowledgeChunkParams) error {
	if params.ChunkID == 0 {
		return fmt.Errorf("knowledge chunk id is required")
	}
	return store.withTx(ctx, func(tx *sql.Tx) error {
		return store.indexKnowledgeChunkTx(ctx, tx, params)
	})
}

func (store *Store) SearchKnowledgeChunks(ctx context.Context, params SearchKnowledgeChunksParams) ([]KnowledgeSearchResult, error) {
	query := strings.TrimSpace(params.Query)
	if query == "" {
		return nil, fmt.Errorf("knowledge search query is required")
	}
	limit := params.Limit
	if limit <= 0 {
		limit = 20
	}

	matchQuery := knowledgeFTSMatchQuery(query)
	rows, err := store.db.QueryContext(ctx, `
		SELECT
			ks.id,
			ks.key,
			ks.title,
			ks.manifest_path,
			kc.id,
			kc.extraction_id,
			ke.artifact_id,
			ka.sha256,
			ka.ocr_required,
			ke.extractor_name,
			ke.extractor_version,
			ke.extracted_text_hash,
			ke.normalized_markdown_path,
			ke.finished_at,
			kc.text,
			kc.anchor,
			kc.page_number,
			kc.restricted,
			bm25(knowledge_fts) AS rank
		FROM knowledge_fts
		JOIN knowledge_chunks kc ON kc.id = knowledge_fts.rowid
		JOIN knowledge_sources ks ON ks.id = kc.source_id
		JOIN knowledge_extractions ke ON ke.id = kc.extraction_id
		JOIN knowledge_artifacts ka ON ka.id = ke.artifact_id
		WHERE knowledge_fts MATCH ?
			AND ks.lifecycle = 'ready'
			AND (? = '' OR ks.scope = ?)
			AND (? = '' OR ks.scope_key = ?)
		ORDER BY rank ASC, kc.id ASC
		LIMIT ?
	`, matchQuery, params.Scope, params.Scope, params.ScopeKey, params.ScopeKey, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []KnowledgeSearchResult
	for rows.Next() {
		var result KnowledgeSearchResult
		var pageNumber sql.NullInt64
		var extractionFinishedAt sql.NullString
		var restricted int
		var ocrRequired int
		if err := rows.Scan(
			&result.SourceID,
			&result.SourceKey,
			&result.Title,
			&result.ManifestPath,
			&result.ChunkID,
			&result.ExtractionID,
			&result.ArtifactID,
			&result.ArtifactSHA256,
			&ocrRequired,
			&result.ExtractorName,
			&result.ExtractorVersion,
			&result.ExtractedTextHash,
			&result.NormalizedMarkdownPath,
			&extractionFinishedAt,
			&result.Text,
			&result.Anchor,
			&pageNumber,
			&restricted,
			&result.Rank,
		); err != nil {
			return nil, err
		}
		result.PageNumber = nullableInt64Ptr(pageNumber)
		result.ExtractionFinishedAt, err = parseNullableTime(extractionFinishedAt)
		if err != nil {
			return nil, err
		}
		result.Restricted = restricted != 0
		results = append(results, result)
	}

	return results, rows.Err()
}

func (store *Store) RecordRestrictedKnowledgeUseApproval(ctx context.Context, params RecordRestrictedKnowledgeUseApprovalParams) (RestrictedKnowledgeUseApproval, error) {
	params.UseType = strings.TrimSpace(params.UseType)
	params.Reason = strings.TrimSpace(params.Reason)
	params.Decision = strings.TrimSpace(params.Decision)
	params.EvidenceJSON = strings.TrimSpace(params.EvidenceJSON)
	params.DecidedBy = strings.TrimSpace(params.DecidedBy)

	if params.SourceID == 0 {
		return RestrictedKnowledgeUseApproval{}, fmt.Errorf("restricted knowledge use approval source id is required")
	}
	if params.UseType == "" {
		return RestrictedKnowledgeUseApproval{}, fmt.Errorf("restricted knowledge use approval use type is required")
	}
	if err := validateRestrictedKnowledgeUseType(params.UseType); err != nil {
		return RestrictedKnowledgeUseApproval{}, err
	}
	if params.Reason == "" {
		return RestrictedKnowledgeUseApproval{}, fmt.Errorf("restricted knowledge use approval reason is required")
	}
	if params.Decision == "" {
		return RestrictedKnowledgeUseApproval{}, fmt.Errorf("restricted knowledge use approval decision is required")
	}
	if params.EvidenceJSON == "" {
		return RestrictedKnowledgeUseApproval{}, fmt.Errorf("restricted knowledge use approval evidence json is required")
	}
	if params.DecidedBy == "" {
		return RestrictedKnowledgeUseApproval{}, fmt.Errorf("restricted knowledge use approval decided by is required")
	}

	decidedAt := store.now()
	if params.DecidedAt != nil {
		decidedAt = params.DecidedAt.UTC()
	}

	var approval RestrictedKnowledgeUseApproval
	err := store.withTx(ctx, func(tx *sql.Tx) error {
		source, err := store.getKnowledgeSourceTx(ctx, tx, params.SourceID)
		if err != nil {
			return err
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO restricted_knowledge_use_approvals (
				source_id,
				use_type,
				reason,
				decision,
				evidence_json,
				decided_by,
				decided_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`,
			params.SourceID,
			params.UseType,
			params.Reason,
			params.Decision,
			params.EvidenceJSON,
			params.DecidedBy,
			formatTime(decidedAt),
		)
		if err != nil {
			return err
		}

		approvalID, err := result.LastInsertId()
		if err != nil {
			return err
		}

		record, err := scanRestrictedKnowledgeUseApproval(tx.QueryRowContext(ctx, `
			SELECT id, source_id, use_type, reason, decision, evidence_json, decided_by, decided_at
			FROM restricted_knowledge_use_approvals
			WHERE id = ?
		`, approvalID))
		if err != nil {
			return err
		}
		approval = record

		if approval.Decision != "approved" {
			return nil
		}
		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamKnowledgeSource,
			StreamID:   source.ID,
			EventType:  runtimeevents.EventRestrictedKnowledgeUseApproved,
			Scope:      source.Scope,
			Payload: runtimeevents.RestrictedKnowledgeUseApprovedPayload{
				SourceID:  source.ID,
				SourceKey: source.Key,
				UseType:   approval.UseType,
				Reason:    approval.Reason,
				Decision:  approval.Decision,
			},
			OccurredAt: decidedAt,
		})
	})

	return approval, err
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (store *Store) getKnowledgeSourceTx(ctx context.Context, tx *sql.Tx, sourceID int64) (KnowledgeSource, error) {
	var q queryRower = store.db
	if tx != nil {
		q = tx
	}
	return scanKnowledgeSource(q.QueryRowContext(ctx, `
		SELECT id, key, title, scope, scope_key, restricted, source_kind, source_class, lifecycle, manifest_path, current_artifact_id, current_extraction_id, created_at, updated_at
		FROM knowledge_sources
		WHERE id = ?
	`, sourceID))
}

func (store *Store) getKnowledgeSourceByKeyTx(ctx context.Context, tx *sql.Tx, key string) (KnowledgeSource, bool, error) {
	if key == "" {
		return KnowledgeSource{}, false, nil
	}
	var q queryRower = store.db
	if tx != nil {
		q = tx
	}
	source, err := scanKnowledgeSource(q.QueryRowContext(ctx, `
		SELECT id, key, title, scope, scope_key, restricted, source_kind, source_class, lifecycle, manifest_path, current_artifact_id, current_extraction_id, created_at, updated_at
		FROM knowledge_sources
		WHERE key = ?
	`, key))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return KnowledgeSource{}, false, nil
		}
		return KnowledgeSource{}, false, err
	}
	return source, true, nil
}

func validateKnowledgeExtractionLineageTx(ctx context.Context, tx *sql.Tx, sourceID int64, extractionID int64) error {
	var exists int
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM knowledge_extractions
			WHERE id = ? AND source_id = ?
		)
	`, extractionID, sourceID).Scan(&exists); err != nil {
		return err
	}
	if exists != 1 {
		return fmt.Errorf("knowledge chunk extraction %d does not belong to source %d", extractionID, sourceID)
	}
	return nil
}

func validateKnowledgeManifestPath(manifestPath string) error {
	if !strings.HasPrefix(manifestPath, "memory/knowledge/") || !strings.HasSuffix(manifestPath, ".md") {
		return fmt.Errorf("knowledge source manifest path %q must be under memory/knowledge/ and end in .md", manifestPath)
	}
	return nil
}

func validateKnowledgeSourceClass(sourceClass string) error {
	switch sourceClass {
	case "markdown", "text", "machine_readable_pdf":
		return nil
	default:
		return fmt.Errorf("knowledge source class %q is not supported", sourceClass)
	}
}

func validateKnowledgeLifecycle(lifecycle string) error {
	switch lifecycle {
	case "declared", "artifact_available", "extracted", "indexed", "ready", "stale", "failed":
		return nil
	default:
		return fmt.Errorf("knowledge source lifecycle %q is not supported", lifecycle)
	}
}

func validateKnowledgeExtractionStatusLifecycle(status string, lifecycle string) error {
	switch status {
	case "pending", "running":
		if lifecycle != "artifact_available" {
			return fmt.Errorf("knowledge extraction status %q requires lifecycle %q", status, "artifact_available")
		}
	case "succeeded":
		switch lifecycle {
		case "extracted", "indexed", "ready":
		default:
			return fmt.Errorf("knowledge extraction status %q cannot enter lifecycle %q", status, lifecycle)
		}
	case "failed":
		if lifecycle != "failed" {
			return fmt.Errorf("knowledge extraction status %q requires lifecycle %q", status, "failed")
		}
	default:
		return fmt.Errorf("knowledge extraction status %q is not supported", status)
	}
	return nil
}

func validateRestrictedKnowledgeUseType(useType string) error {
	switch useType {
	case "bulk_export", "broad_extraction", "sharing", "executor_context_injection":
		return nil
	default:
		return fmt.Errorf("restricted knowledge use type %q is not supported", useType)
	}
}

func (store *Store) reindexKnowledgeSourceChunksTx(ctx context.Context, tx *sql.Tx, sourceID int64) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT id
		FROM knowledge_chunks
		WHERE source_id = ?
	`, sourceID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var chunkIDs []int64
	for rows.Next() {
		var chunkID int64
		if err := rows.Scan(&chunkID); err != nil {
			return err
		}
		chunkIDs = append(chunkIDs, chunkID)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, chunkID := range chunkIDs {
		if err := store.indexKnowledgeChunkTx(ctx, tx, IndexKnowledgeChunkParams{ChunkID: chunkID}); err != nil {
			return err
		}
	}
	return nil
}

func validateKnowledgeCurrentExtractionRequired(lifecycle string, extractionID *int64) error {
	switch lifecycle {
	case "extracted", "indexed", "ready":
		if extractionID == nil {
			return fmt.Errorf("knowledge source lifecycle %q requires current extraction", lifecycle)
		}
	}
	return nil
}

func (store *Store) validateKnowledgeCurrentExtractionTx(ctx context.Context, tx *sql.Tx, sourceID int64, artifactID *int64, extractionID *int64, lifecycle string) error {
	if err := validateKnowledgeCurrentExtractionRequired(lifecycle, extractionID); err != nil {
		return err
	}
	if extractionID == nil {
		return nil
	}

	var extractionSourceID int64
	var extractionArtifactID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT source_id, artifact_id
		FROM knowledge_extractions
		WHERE id = ?
	`, *extractionID).Scan(&extractionSourceID, &extractionArtifactID); err != nil {
		return err
	}
	if extractionSourceID != sourceID {
		return fmt.Errorf("knowledge source current extraction %d does not belong to source %d", *extractionID, sourceID)
	}
	if artifactID == nil {
		return fmt.Errorf("knowledge source current extraction %d requires current artifact", *extractionID)
	}
	if extractionArtifactID != *artifactID {
		return fmt.Errorf("knowledge source current extraction %d artifact %d does not match current artifact %d", *extractionID, extractionArtifactID, *artifactID)
	}
	return nil
}

func (store *Store) validateKnowledgeArtifactLifecycleTx(ctx context.Context, tx *sql.Tx, artifactID *int64, lifecycle string) error {
	if artifactID == nil {
		return nil
	}
	switch lifecycle {
	case "extracted", "indexed", "ready":
	default:
		return nil
	}

	var ocrRequired int
	if err := tx.QueryRowContext(ctx, `
		SELECT ocr_required
		FROM knowledge_artifacts
		WHERE id = ?
	`, *artifactID).Scan(&ocrRequired); err != nil {
		return err
	}
	if ocrRequired != 0 {
		return fmt.Errorf("ocr-required knowledge artifact cannot enter lifecycle %q", lifecycle)
	}
	return nil
}

func (store *Store) indexKnowledgeChunkTx(ctx context.Context, tx *sql.Tx, params IndexKnowledgeChunkParams) error {
	if params.ChunkID == 0 {
		return fmt.Errorf("knowledge chunk id is required")
	}

	var sourceKey string
	var title string
	var text string
	topicsText := strings.Join(params.Topics, " ")
	entitiesText := strings.Join(params.Entities, " ")
	if params.Topics == nil || params.Entities == nil {
		var existingTopics sql.NullString
		var existingEntities sql.NullString
		if err := tx.QueryRowContext(ctx, `
			SELECT topics, entities
			FROM knowledge_fts
			WHERE rowid = ?
		`, params.ChunkID).Scan(&existingTopics, &existingEntities); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		} else if err == nil {
			if params.Topics == nil {
				topicsText = existingTopics.String
			}
			if params.Entities == nil {
				entitiesText = existingEntities.String
			}
		}
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT ks.key, ks.title, kc.text
		FROM knowledge_chunks kc
		JOIN knowledge_sources ks ON ks.id = kc.source_id
		WHERE kc.id = ?
	`, params.ChunkID).Scan(&sourceKey, &title, &text); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_fts WHERE rowid = ?`, params.ChunkID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO knowledge_fts(rowid, source_key, title, topics, entities, chunk_text)
		VALUES (?, ?, ?, ?, ?, ?)
	`,
		params.ChunkID,
		sourceKey,
		title,
		topicsText,
		entitiesText,
		text,
	)
	return err
}

func appendKnowledgeLifecycleChangedTx(ctx context.Context, tx *sql.Tx, previous KnowledgeSource, next KnowledgeSource, occurredAt time.Time) error {
	return appendEventTx(ctx, tx, eventInsert{
		StreamType: runtimeevents.StreamKnowledgeSource,
		StreamID:   next.ID,
		EventType:  runtimeevents.EventKnowledgeSourceLifecycleChanged,
		Scope:      next.Scope,
		Payload: runtimeevents.KnowledgeSourceLifecycleChangedPayload{
			SourceID:          next.ID,
			SourceKey:         next.Key,
			PreviousLifecycle: previous.Lifecycle,
			Lifecycle:         next.Lifecycle,
			ArtifactID:        next.CurrentArtifactID,
			ExtractionID:      next.CurrentExtractionID,
		},
		OccurredAt: occurredAt,
	})
}

func lifecycleForKnowledgeExtractionStatus(status string) string {
	switch status {
	case "succeeded":
		return "extracted"
	case "failed":
		return "failed"
	default:
		return "artifact_available"
	}
}

func knowledgeFTSMatchQuery(query string) string {
	terms := strings.Fields(query)
	if len(terms) == 0 {
		return `""`
	}
	quoted := make([]string, 0, len(terms))
	for _, term := range terms {
		term = strings.ReplaceAll(term, `"`, `""`)
		quoted = append(quoted, `"`+term+`"`)
	}
	return strings.Join(quoted, " AND ")
}

func (store *Store) GetWorkspaceProfile(ctx context.Context, workspaceID string) (WorkspaceProfile, error) {
	return scanWorkspaceProfile(store.db.QueryRowContext(ctx, `
		SELECT
			id,
			workspace_id,
			preferences_json,
			boundaries_json,
			cadence_defaults_json,
			created_at,
			updated_at
		FROM workspace_profile
		WHERE workspace_id = ?
	`, workspaceID))
}

func (store *Store) UpsertWorkspaceProfile(ctx context.Context, params UpsertWorkspaceProfileParams) (WorkspaceProfile, error) {
	now := store.now()
	var profile WorkspaceProfile

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO workspace_profile (
				workspace_id,
				preferences_json,
				boundaries_json,
				cadence_defaults_json,
				created_at,
				updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(workspace_id) DO UPDATE SET
				preferences_json = excluded.preferences_json,
				boundaries_json = excluded.boundaries_json,
				cadence_defaults_json = excluded.cadence_defaults_json,
				updated_at = excluded.updated_at
		`,
			params.WorkspaceID,
			params.PreferencesJSON,
			params.BoundariesJSON,
			params.CadenceDefaultsJSON,
			formatTime(now),
			formatTime(now),
		); err != nil {
			return err
		}

		record, err := scanWorkspaceProfile(tx.QueryRowContext(ctx, `
			SELECT
				id,
				workspace_id,
				preferences_json,
				boundaries_json,
				cadence_defaults_json,
				created_at,
				updated_at
			FROM workspace_profile
			WHERE workspace_id = ?
		`, params.WorkspaceID))
		if err != nil {
			return err
		}
		profile = record
		return nil
	})

	return profile, err
}

func (store *Store) SetProjectTransition(ctx context.Context, params SetProjectTransitionParams) (ProjectTransition, error) {
	now := store.now()
	var transition ProjectTransition

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		project, err := store.getProjectTx(ctx, tx, params.ProjectID)
		if err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO project_transitions (
				project_id,
				state,
				controller,
				limited_actions_json,
				notes,
				changed_by,
				changed_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(project_id) DO UPDATE SET
				state = excluded.state,
				controller = excluded.controller,
				limited_actions_json = excluded.limited_actions_json,
				notes = excluded.notes,
				changed_by = excluded.changed_by,
				changed_at = excluded.changed_at
		`,
			params.ProjectID,
			params.State,
			params.Controller,
			params.LimitedActionsJSON,
			params.Notes,
			params.ChangedBy,
			formatTime(now),
		); err != nil {
			return err
		}

		row := tx.QueryRowContext(ctx, `
			SELECT project_id, state, controller, limited_actions_json, notes, changed_by, changed_at
			FROM project_transitions
			WHERE project_id = ?
		`, params.ProjectID)
		transition, err = scanProjectTransition(row)
		if err != nil {
			return err
		}

		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamProject,
			StreamID:   project.ID,
			EventType:  runtimeevents.EventProjectTransitionChanged,
			Scope:      project.Scope,
			ProjectID:  &project.ID,
			Payload: runtimeevents.ProjectTransitionChangedPayload{
				State:          transition.State,
				Controller:     transition.Controller,
				LimitedActions: transition.LimitedActionsJSON,
				Notes:          transition.Notes,
				ChangedBy:      transition.ChangedBy,
			},
			OccurredAt: now,
		})
	})

	return transition, err
}

func (store *Store) GetProjectTransition(ctx context.Context, projectID int64) (ProjectTransition, error) {
	row := store.db.QueryRowContext(ctx, `
		SELECT project_id, state, controller, limited_actions_json, notes, changed_by, changed_at
		FROM project_transitions
		WHERE project_id = ?
	`, projectID)
	return scanProjectTransition(row)
}

func (store *Store) RecordProjectTransitionReport(ctx context.Context, params RecordProjectTransitionReportParams) (ProjectTransitionReport, error) {
	now := store.now()
	var report ProjectTransitionReport

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		project, err := store.getProjectTx(ctx, tx, params.ProjectID)
		if err != nil {
			return err
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO project_transition_reports (project_id, report_type, summary, details_json, recorded_at)
			VALUES (?, ?, ?, ?, ?)
		`,
			params.ProjectID,
			params.ReportType,
			params.Summary,
			params.DetailsJSON,
			formatTime(now),
		)
		if err != nil {
			return err
		}

		reportID, err := result.LastInsertId()
		if err != nil {
			return err
		}

		report = ProjectTransitionReport{
			ID:          reportID,
			ProjectID:   params.ProjectID,
			ReportType:  params.ReportType,
			Summary:     params.Summary,
			DetailsJSON: params.DetailsJSON,
			RecordedAt:  now,
		}

		eventType := runtimeevents.EventProjectShadowObservationRecorded
		switch params.ReportType {
		case "shadow_observation":
			eventType = runtimeevents.EventProjectShadowObservationRecorded
		case "compare_report":
			eventType = runtimeevents.EventProjectCompareReportRecorded
		default:
			return fmt.Errorf("unsupported transition report type %q", params.ReportType)
		}

		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamProject,
			StreamID:   project.ID,
			EventType:  eventType,
			Scope:      project.Scope,
			ProjectID:  &project.ID,
			Payload: runtimeevents.ProjectTransitionReportRecordedPayload{
				ReportType: report.ReportType,
				Summary:    report.Summary,
			},
			OccurredAt: now,
		})
	})

	return report, err
}

func (store *Store) RecordProjectTransitionDenied(ctx context.Context, projectID int64, actionClass string, reason string) error {
	now := store.now()

	return store.withTx(ctx, func(tx *sql.Tx) error {
		project, err := store.getProjectTx(ctx, tx, projectID)
		if err != nil {
			return err
		}

		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamProject,
			StreamID:   project.ID,
			EventType:  runtimeevents.EventProjectTransitionDenied,
			Scope:      project.Scope,
			ProjectID:  &project.ID,
			Payload: runtimeevents.ProjectTransitionDeniedPayload{
				ActionClass: actionClass,
				Reason:      reason,
			},
			OccurredAt: now,
		})
	})
}

func (store *Store) ListProjectTransitionReports(ctx context.Context, projectID int64) ([]ProjectTransitionReport, error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT id, project_id, report_type, summary, details_json, recorded_at
		FROM project_transition_reports
		WHERE project_id = ?
		ORDER BY id ASC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reports []ProjectTransitionReport
	for rows.Next() {
		report, err := scanProjectTransitionReport(rows)
		if err != nil {
			return nil, err
		}
		reports = append(reports, report)
	}

	return reports, rows.Err()
}

func (store *Store) CreateLearningProposal(ctx context.Context, params CreateLearningProposalParams) (LearningProposal, error) {
	now := store.now()
	var proposal LearningProposal

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO learning_proposals (
				project_id,
				proposal_type,
				scope,
				target_key,
				summary,
				hypothesis,
				change_payload_json,
				status,
				created_by,
				created_at,
				updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			nullInt64(params.ProjectID),
			params.ProposalType,
			params.Scope,
			params.TargetKey,
			params.Summary,
			params.Hypothesis,
			params.ChangePayloadJSON,
			params.Status,
			params.CreatedBy,
			formatTime(now),
			formatTime(now),
		)
		if err != nil {
			return err
		}

		proposalID, err := result.LastInsertId()
		if err != nil {
			return err
		}

		proposal = LearningProposal{
			ID:                proposalID,
			ProjectID:         params.ProjectID,
			ProposalType:      params.ProposalType,
			Scope:             params.Scope,
			TargetKey:         params.TargetKey,
			Summary:           params.Summary,
			Hypothesis:        params.Hypothesis,
			ChangePayloadJSON: params.ChangePayloadJSON,
			Status:            params.Status,
			CreatedBy:         params.CreatedBy,
			CreatedAt:         now,
			UpdatedAt:         now,
		}

		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamLearningProposal,
			StreamID:   proposal.ID,
			EventType:  runtimeevents.EventLearningProposalCreated,
			Scope:      proposal.Scope,
			ProjectID:  proposal.ProjectID,
			Payload: runtimeevents.LearningProposalCreatedPayload{
				ProposalType: proposal.ProposalType,
				Scope:        proposal.Scope,
				TargetKey:    proposal.TargetKey,
				Status:       proposal.Status,
				Summary:      proposal.Summary,
			},
			OccurredAt: now,
		})
	})

	return proposal, err
}

func (store *Store) UpdateLearningProposalStatus(ctx context.Context, params UpdateLearningProposalStatusParams) (LearningProposal, error) {
	now := store.now()
	var proposal LearningProposal

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		current, err := store.getLearningProposalTx(ctx, tx, params.ProposalID)
		if err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE learning_proposals
			SET status = ?, updated_at = ?
			WHERE id = ?
		`, params.Status, formatTime(now), params.ProposalID); err != nil {
			return err
		}

		current.Status = params.Status
		current.UpdatedAt = now
		proposal = current

		var eventType runtimeevents.Type
		switch params.Status {
		case "submitted":
			eventType = runtimeevents.EventLearningProposalSubmitted
		case "promotion_ready":
			eventType = runtimeevents.EventLearningProposalPromotionReady
		case "rejected":
			eventType = runtimeevents.EventLearningProposalRejected
		default:
			return nil
		}

		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamLearningProposal,
			StreamID:   proposal.ID,
			EventType:  eventType,
			Scope:      proposal.Scope,
			ProjectID:  proposal.ProjectID,
			Payload: runtimeevents.LearningProposalStatusPayload{
				Status: proposal.Status,
			},
			OccurredAt: now,
		})
	})

	return proposal, err
}

func (store *Store) GetLearningProposal(ctx context.Context, proposalID int64) (LearningProposal, error) {
	row := store.db.QueryRowContext(ctx, `
		SELECT id, project_id, proposal_type, scope, target_key, summary, hypothesis, change_payload_json, status, created_by, created_at, updated_at
		FROM learning_proposals
		WHERE id = ?
	`, proposalID)
	return scanLearningProposal(row)
}

func (store *Store) RecordLearningEvaluation(ctx context.Context, params RecordLearningEvaluationParams) (LearningEvaluation, error) {
	now := store.now()
	var evaluation LearningEvaluation

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		proposal, err := store.getLearningProposalTx(ctx, tx, params.ProposalID)
		if err != nil {
			return err
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO learning_evaluations (
				proposal_id,
				fixture_key,
				mode,
				score,
				baseline_summary_json,
				candidate_summary_json,
				result_summary,
				outcome,
				recorded_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			params.ProposalID,
			params.FixtureKey,
			params.Mode,
			params.Score,
			params.BaselineSummaryJSON,
			params.CandidateSummaryJSON,
			params.ResultSummary,
			params.Outcome,
			formatTime(now),
		)
		if err != nil {
			return err
		}

		evaluationID, err := result.LastInsertId()
		if err != nil {
			return err
		}

		evaluation = LearningEvaluation{
			ID:                   evaluationID,
			ProposalID:           params.ProposalID,
			FixtureKey:           params.FixtureKey,
			Mode:                 params.Mode,
			Score:                params.Score,
			BaselineSummaryJSON:  params.BaselineSummaryJSON,
			CandidateSummaryJSON: params.CandidateSummaryJSON,
			ResultSummary:        params.ResultSummary,
			Outcome:              params.Outcome,
			RecordedAt:           now,
		}

		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamLearningEvaluation,
			StreamID:   evaluation.ID,
			EventType:  runtimeevents.EventLearningEvaluationRecorded,
			Scope:      proposal.Scope,
			ProjectID:  proposal.ProjectID,
			Payload: runtimeevents.LearningEvaluationRecordedPayload{
				ProposalID: evaluation.ProposalID,
				FixtureKey: evaluation.FixtureKey,
				Mode:       evaluation.Mode,
				Score:      evaluation.Score,
				Outcome:    evaluation.Outcome,
			},
			OccurredAt: now,
		})
	})

	return evaluation, err
}

func (store *Store) ListLearningEvaluations(ctx context.Context, proposalID int64) ([]LearningEvaluation, error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT id, proposal_id, fixture_key, mode, score, baseline_summary_json, candidate_summary_json, result_summary, outcome, recorded_at
		FROM learning_evaluations
		WHERE proposal_id = ?
		ORDER BY id ASC
	`, proposalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var evaluations []LearningEvaluation
	for rows.Next() {
		evaluation, err := scanLearningEvaluation(rows)
		if err != nil {
			return nil, err
		}
		evaluations = append(evaluations, evaluation)
	}

	return evaluations, rows.Err()
}

func (store *Store) PromoteLearningProposal(ctx context.Context, params PromoteLearningProposalParams) (LearningPromotion, error) {
	now := store.now()
	var promotion LearningPromotion

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		proposal, err := store.getLearningProposalTx(ctx, tx, params.ProposalID)
		if err != nil {
			return err
		}

		var supersedesPromotionID *int64
		active, err := store.getActiveLearningPromotionForTargetTx(ctx, tx, proposal.ProposalType, proposal.Scope, proposal.TargetKey)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil {
			if _, err := tx.ExecContext(ctx, `
				UPDATE learning_promotions
				SET status = ?
				WHERE id = ?
			`, "superseded", active.ID); err != nil {
				return err
			}
			supersedesPromotionID = &active.ID
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO learning_promotions (
				proposal_id,
				proposal_type,
				scope,
				target_key,
				status,
				supersedes_promotion_id,
				promoted_by,
				promoted_at,
				rollback_reason
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, '')
		`,
			proposal.ID,
			proposal.ProposalType,
			proposal.Scope,
			proposal.TargetKey,
			"active",
			nullInt64(supersedesPromotionID),
			params.PromotedBy,
			formatTime(now),
		)
		if err != nil {
			return err
		}

		promotionID, err := result.LastInsertId()
		if err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE learning_proposals
			SET status = ?, updated_at = ?
			WHERE id = ?
		`, "promoted", formatTime(now), proposal.ID); err != nil {
			return err
		}

		promotion = LearningPromotion{
			ID:                    promotionID,
			ProposalID:            proposal.ID,
			ProposalType:          proposal.ProposalType,
			Scope:                 proposal.Scope,
			TargetKey:             proposal.TargetKey,
			Status:                "active",
			SupersedesPromotionID: supersedesPromotionID,
			PromotedBy:            params.PromotedBy,
			PromotedAt:            now,
		}

		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamLearningPromotion,
			StreamID:   promotion.ID,
			EventType:  runtimeevents.EventLearningPromotionApplied,
			Scope:      proposal.Scope,
			ProjectID:  proposal.ProjectID,
			Payload: runtimeevents.LearningPromotionAppliedPayload{
				ProposalID:            promotion.ProposalID,
				ProposalType:          promotion.ProposalType,
				Scope:                 promotion.Scope,
				TargetKey:             promotion.TargetKey,
				Status:                promotion.Status,
				SupersedesPromotionID: promotion.SupersedesPromotionID,
			},
			OccurredAt: now,
		})
	})

	return promotion, err
}

func (store *Store) GetLearningPromotion(ctx context.Context, promotionID int64) (LearningPromotion, error) {
	row := store.db.QueryRowContext(ctx, `
		SELECT id, proposal_id, proposal_type, scope, target_key, status, supersedes_promotion_id, promoted_by, promoted_at, rolled_back_by, rolled_back_at, rollback_reason
		FROM learning_promotions
		WHERE id = ?
	`, promotionID)
	return scanLearningPromotion(row)
}

func (store *Store) ListActiveLearningPromotions(ctx context.Context) ([]LearningPromotion, error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT id, proposal_id, proposal_type, scope, target_key, status, supersedes_promotion_id, promoted_by, promoted_at, rolled_back_by, rolled_back_at, rollback_reason
		FROM learning_promotions
		WHERE status = 'active'
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var promotions []LearningPromotion
	for rows.Next() {
		promotion, err := scanLearningPromotion(rows)
		if err != nil {
			return nil, err
		}
		promotions = append(promotions, promotion)
	}

	return promotions, rows.Err()
}

func (store *Store) RollbackLearningPromotion(ctx context.Context, params RollbackLearningPromotionParams) (LearningPromotion, error) {
	now := store.now()
	var rolledBack LearningPromotion

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		current, err := store.getLearningPromotionTx(ctx, tx, params.PromotionID)
		if err != nil {
			return err
		}
		if current.Status != "active" {
			return fmt.Errorf("learning promotion %d is not active", current.ID)
		}

		proposal, err := store.getLearningProposalTx(ctx, tx, current.ProposalID)
		if err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE learning_promotions
			SET status = ?, rolled_back_by = ?, rolled_back_at = ?, rollback_reason = ?
			WHERE id = ?
		`, "rolled_back", params.RolledBackBy, formatTime(now), params.RollbackReason, current.ID); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE learning_proposals
			SET status = ?, updated_at = ?
			WHERE id = ?
		`, "rolled_back", formatTime(now), current.ProposalID); err != nil {
			return err
		}

		var restoredPromotionID *int64
		if current.SupersedesPromotionID != nil {
			if _, err := tx.ExecContext(ctx, `
				UPDATE learning_promotions
				SET status = ?
				WHERE id = ?
			`, "active", *current.SupersedesPromotionID); err != nil {
				return err
			}
			restoredPromotionID = current.SupersedesPromotionID

			restoredPromotion, err := store.getLearningPromotionTx(ctx, tx, *current.SupersedesPromotionID)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE learning_proposals
				SET status = ?, updated_at = ?
				WHERE id = ?
			`, "promoted", formatTime(now), restoredPromotion.ProposalID); err != nil {
				return err
			}
		}

		rolledBack = current
		rolledBack.Status = "rolled_back"
		rolledBack.RolledBackBy = params.RolledBackBy
		rolledBack.RolledBackAt = &now
		rolledBack.RollbackReason = params.RollbackReason

		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamLearningPromotion,
			StreamID:   current.ID,
			EventType:  runtimeevents.EventLearningPromotionRolledBack,
			Scope:      proposal.Scope,
			ProjectID:  proposal.ProjectID,
			Payload: runtimeevents.LearningPromotionRolledBackPayload{
				ProposalID:          current.ProposalID,
				RolledBackBy:        params.RolledBackBy,
				RollbackReason:      params.RollbackReason,
				RestoredPromotionID: restoredPromotionID,
			},
			OccurredAt: now,
		})
	})

	return rolledBack, err
}

func (store *Store) GetTask(ctx context.Context, taskID int64) (Task, error) {
	return store.getTaskQuery(ctx, store.db, taskID)
}

func (store *Store) GetProject(ctx context.Context, projectID int64) (Project, error) {
	row := store.db.QueryRowContext(ctx, `
		SELECT id, key, name, scope, git_root, default_branch, github_repo, manifest_path, created_at, updated_at
		FROM projects
		WHERE id = ?
	`, projectID)
	return scanProject(row)
}

func (store *Store) GetProjectByKey(ctx context.Context, key string) (Project, error) {
	row := store.db.QueryRowContext(ctx, `
		SELECT id, key, name, scope, git_root, default_branch, github_repo, manifest_path, created_at, updated_at
		FROM projects
		WHERE key = ?
	`, key)
	return scanProject(row)
}

func (store *Store) GetRun(ctx context.Context, runID int64) (Run, error) {
	row := store.db.QueryRowContext(ctx, `
		SELECT id, task_id, executor, status, attempt, started_at, finished_at, summary
		FROM runs
		WHERE id = ?
	`, runID)
	return scanRun(row)
}

func (store *Store) ListRunsByStatus(ctx context.Context, status string) ([]Run, error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT id, task_id, executor, status, attempt, started_at, finished_at, summary
		FROM runs
		WHERE status = ?
		ORDER BY started_at ASC, id ASC
	`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []Run
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}

	return runs, rows.Err()
}

func (store *Store) GetApproval(ctx context.Context, approvalID int64) (Approval, error) {
	row := store.db.QueryRowContext(ctx, `
		SELECT id, task_id, run_id, action_id, payload_hash, status, requested_at, resolved_at, decision_by, reason
		FROM approvals
		WHERE id = ?
	`, approvalID)
	return scanApproval(row)
}

func (store *Store) GetIncident(ctx context.Context, incidentID int64) (Incident, error) {
	row := store.db.QueryRowContext(ctx, `
		SELECT id, run_id, severity, status, summary, details_json, opened_at, updated_at
		FROM incidents
		WHERE id = ?
	`, incidentID)
	return scanIncident(row)
}

func (store *Store) GetRecovery(ctx context.Context, recoveryID int64) (Recovery, error) {
	row := store.db.QueryRowContext(ctx, `
		SELECT id, incident_id, run_id, status, strategy, details_json, started_at, finished_at, updated_at
		FROM recoveries
		WHERE id = ?
	`, recoveryID)
	return scanRecovery(row)
}

func (store *Store) GetContextPacket(ctx context.Context, packetID int64) (ContextPacket, error) {
	row := store.db.QueryRowContext(ctx, `
		SELECT
			id,
			task_id,
			run_id,
			packet_kind,
			packet_scope,
			trigger,
			checkpoint_key,
			supersedes_packet_id,
			status,
			summary,
			payload_json,
			created_at
		FROM context_packets
		WHERE id = ?
	`, packetID)
	return scanContextPacket(row)
}

func (store *Store) ListContextPackets(ctx context.Context, params ListContextPacketsParams) ([]ContextPacket, error) {
	query := `
		SELECT
			id,
			task_id,
			run_id,
			packet_kind,
			packet_scope,
			trigger,
			checkpoint_key,
			supersedes_packet_id,
			status,
			summary,
			payload_json,
			created_at
		FROM context_packets
		WHERE 1 = 1
	`
	var args []any
	if params.TaskID != nil {
		query += ` AND task_id = ?`
		args = append(args, *params.TaskID)
	}
	if params.RunID != nil {
		query += ` AND run_id = ?`
		args = append(args, *params.RunID)
	}
	if params.PacketKind != "" {
		query += ` AND packet_kind = ?`
		args = append(args, params.PacketKind)
	}
	if params.PacketScope != "" {
		query += ` AND packet_scope = ?`
		args = append(args, params.PacketScope)
	}
	if params.Status != "" {
		query += ` AND status = ?`
		args = append(args, params.Status)
	}
	query += ` ORDER BY id ASC`

	rows, err := store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var packets []ContextPacket
	for rows.Next() {
		packet, err := scanContextPacket(rows)
		if err != nil {
			return nil, err
		}
		packets = append(packets, packet)
	}

	return packets, rows.Err()
}

func (store *Store) GetLatestTaskWakePacket(ctx context.Context, projectID int64, taskID int64) (ContextPacket, error) {
	row := store.db.QueryRowContext(ctx, `
		SELECT
			cp.id,
			cp.task_id,
			cp.run_id,
			cp.packet_kind,
			cp.packet_scope,
			cp.trigger,
			cp.checkpoint_key,
			cp.supersedes_packet_id,
			cp.status,
			cp.summary,
			cp.payload_json,
			cp.created_at
		FROM context_packets cp
		JOIN tasks t ON t.id = cp.task_id
		WHERE t.project_id = ?
		  AND cp.task_id = ?
		  AND cp.packet_scope = 'task_wake_packet'
		  AND cp.status IN ('active', 'sealed')
		ORDER BY cp.id DESC
		LIMIT 1
	`, projectID, taskID)
	return scanContextPacket(row)
}

func (store *Store) CreateWorktreeLease(ctx context.Context, params CreateWorktreeLeaseParams) (WorktreeLease, error) {
	now := store.now()
	var lease WorktreeLease
	state := params.State
	if state == "" {
		state = "active"
	}

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO worktree_leases (
				project_id,
				task_id,
				run_id,
				mode,
				branch_name,
				worktree_path,
				repo_root,
				state,
				heartbeat_at,
				released_at,
				cleaned_up_at,
				created_at,
				updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, ?, ?)
		`,
			params.ProjectID,
			params.TaskID,
			params.RunID,
			params.Mode,
			params.BranchName,
			params.WorktreePath,
			params.RepoRoot,
			state,
			formatTime(now),
			formatTime(now),
			formatTime(now),
		)
		if err != nil {
			return mapWorktreeLeaseError(err)
		}

		leaseID, err := result.LastInsertId()
		if err != nil {
			return err
		}

		lease = WorktreeLease{
			ID:           leaseID,
			ProjectID:    params.ProjectID,
			TaskID:       params.TaskID,
			RunID:        params.RunID,
			Mode:         params.Mode,
			BranchName:   params.BranchName,
			WorktreePath: params.WorktreePath,
			RepoRoot:     params.RepoRoot,
			State:        state,
			HeartbeatAt:  now,
			CreatedAt:    now,
			UpdatedAt:    now,
		}

		return nil
	})

	return lease, err
}

func (store *Store) HeartbeatWorktreeLease(ctx context.Context, leaseID int64) (WorktreeLease, error) {
	now := store.now()
	if _, err := store.db.ExecContext(ctx, `
		UPDATE worktree_leases
		SET heartbeat_at = ?, updated_at = ?
		WHERE id = ?
	`, formatTime(now), formatTime(now), leaseID); err != nil {
		return WorktreeLease{}, err
	}

	return store.GetWorktreeLease(ctx, leaseID)
}

func (store *Store) ReleaseWorktreeLease(ctx context.Context, params ReleaseWorktreeLeaseParams) (WorktreeLease, error) {
	now := store.now()
	state := params.State
	if state == "" {
		state = "released"
	}

	if _, err := store.db.ExecContext(ctx, `
		UPDATE worktree_leases
		SET state = ?, released_at = ?, updated_at = ?
		WHERE id = ?
	`, state, formatTime(now), formatTime(now), params.LeaseID); err != nil {
		return WorktreeLease{}, err
	}

	return store.GetWorktreeLease(ctx, params.LeaseID)
}

func (store *Store) GetWorktreeLease(ctx context.Context, leaseID int64) (WorktreeLease, error) {
	row := store.db.QueryRowContext(ctx, `
		SELECT
			id,
			project_id,
			task_id,
			run_id,
			mode,
			branch_name,
			worktree_path,
			repo_root,
			state,
			heartbeat_at,
			released_at,
			cleaned_up_at,
			created_at,
			updated_at
		FROM worktree_leases
		WHERE id = ?
	`, leaseID)
	return scanWorktreeLease(row)
}

func (store *Store) GetActiveWorktreeLeaseByTaskRun(ctx context.Context, taskID int64, runID int64) (WorktreeLease, error) {
	row := store.db.QueryRowContext(ctx, `
		SELECT
			id,
			project_id,
			task_id,
			run_id,
			mode,
			branch_name,
			worktree_path,
			repo_root,
			state,
			heartbeat_at,
			released_at,
			cleaned_up_at,
			created_at,
			updated_at
		FROM worktree_leases
		WHERE task_id = ?
		  AND run_id = ?
		  AND state = 'active'
		ORDER BY id DESC
		LIMIT 1
	`, taskID, runID)
	return scanWorktreeLease(row)
}

func (store *Store) ListCleanupEligibleWorktreeLeases(ctx context.Context, staleBefore time.Time) ([]WorktreeLease, error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT
			id,
			project_id,
			task_id,
			run_id,
			mode,
			branch_name,
			worktree_path,
			repo_root,
			state,
			heartbeat_at,
			released_at,
			cleaned_up_at,
			created_at,
			updated_at
		FROM worktree_leases
		WHERE cleaned_up_at IS NULL
		  AND (
			state = 'released'
			OR (state = 'active' AND heartbeat_at < ?)
		  )
		ORDER BY id ASC
	`, formatTime(staleBefore))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var leases []WorktreeLease
	for rows.Next() {
		lease, err := scanWorktreeLease(rows)
		if err != nil {
			return nil, err
		}
		leases = append(leases, lease)
	}

	return leases, rows.Err()
}

func (store *Store) MarkWorktreeLeaseCleanedUp(ctx context.Context, leaseID int64) (WorktreeLease, error) {
	now := store.now()
	if _, err := store.db.ExecContext(ctx, `
		UPDATE worktree_leases
		SET state = 'cleaned', cleaned_up_at = ?, updated_at = ?
		WHERE id = ?
	`, formatTime(now), formatTime(now), leaseID); err != nil {
		return WorktreeLease{}, err
	}

	return store.GetWorktreeLease(ctx, leaseID)
}

func (store *Store) RecordProjectionFreshness(ctx context.Context, params RecordProjectionFreshnessParams) (ProjectionFreshness, error) {
	now := store.now()
	if _, err := store.db.ExecContext(ctx, `
		INSERT INTO projection_freshness (surface, status, refreshed_at, details_json, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(surface) DO UPDATE SET
			status = excluded.status,
			refreshed_at = excluded.refreshed_at,
			details_json = excluded.details_json,
			updated_at = excluded.updated_at
	`, params.Surface, params.Status, formatTime(now), params.DetailsJSON, formatTime(now)); err != nil {
		return ProjectionFreshness{}, err
	}

	return store.GetProjectionFreshness(ctx, params.Surface)
}

func (store *Store) GetProjectionFreshness(ctx context.Context, surface string) (ProjectionFreshness, error) {
	row := store.db.QueryRowContext(ctx, `
		SELECT surface, status, refreshed_at, details_json, updated_at
		FROM projection_freshness
		WHERE surface = ?
	`, surface)
	return scanProjectionFreshness(row)
}

func (store *Store) ListProjectionFreshness(ctx context.Context) ([]ProjectionFreshness, error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT surface, status, refreshed_at, details_json, updated_at
		FROM projection_freshness
		ORDER BY surface ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []ProjectionFreshness
	for rows.Next() {
		record, err := scanProjectionFreshness(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	return records, rows.Err()
}

func (store *Store) ListStaleProjectionFreshness(ctx context.Context, staleBefore time.Time) ([]ProjectionFreshness, error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT surface, status, refreshed_at, details_json, updated_at
		FROM projection_freshness
		WHERE refreshed_at < ?
		ORDER BY refreshed_at ASC, surface ASC
	`, formatTime(staleBefore))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []ProjectionFreshness
	for rows.Next() {
		record, err := scanProjectionFreshness(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	return records, rows.Err()
}

func (store *Store) ListEvents(ctx context.Context, params ListEventsParams) ([]runtimeevents.Record, error) {
	query := `
		SELECT id, stream_type, stream_id, event_type, event_version, scope, project_id, task_id, run_id, payload_json, occurred_at
		FROM events
		WHERE 1 = 1
	`
	var args []any
	if params.ProjectID != nil {
		query += ` AND project_id = ?`
		args = append(args, *params.ProjectID)
	}
	if params.TaskID != nil {
		query += ` AND task_id = ?`
		args = append(args, *params.TaskID)
	}
	if params.RunID != nil {
		query += ` AND run_id = ?`
		args = append(args, *params.RunID)
	}
	query += ` ORDER BY id ASC`

	rows, err := store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []runtimeevents.Record
	for rows.Next() {
		record, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	return records, rows.Err()
}

type sqlQueryRow interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (store *Store) getTaskTx(ctx context.Context, tx *sql.Tx, taskID int64) (Task, error) {
	return store.getTaskQuery(ctx, tx, taskID)
}

func (store *Store) getTaskQuery(ctx context.Context, queryer sqlQueryRow, taskID int64) (Task, error) {
	row := queryer.QueryRowContext(ctx, `
		SELECT id, project_id, key, title, status, scope, requested_by, current_run_id, created_at, updated_at
		FROM tasks
		WHERE id = ?
	`, taskID)
	return scanTask(row)
}

func (store *Store) getProjectTx(ctx context.Context, tx *sql.Tx, projectID int64) (Project, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, key, name, scope, git_root, default_branch, github_repo, manifest_path, created_at, updated_at
		FROM projects
		WHERE id = ?
	`, projectID)
	return scanProject(row)
}

func (store *Store) getRunTx(ctx context.Context, tx *sql.Tx, runID int64) (Run, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, task_id, executor, status, attempt, started_at, finished_at, summary
		FROM runs
		WHERE id = ?
	`, runID)
	return scanRun(row)
}

func (store *Store) getAction(ctx context.Context, actionID int64) (Action, error) {
	row := store.db.QueryRowContext(ctx, `
		SELECT id, workflow_key, workflow_run_id, action_type, lifecycle_state, current_payload_hash, created_at, updated_at
		FROM actions
		WHERE id = ?
	`, actionID)
	return scanAction(row)
}

func (store *Store) getConversationTranscriptTx(ctx context.Context, tx *sql.Tx, transcriptID int64) (ConversationTranscript, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT
			id,
			project_id,
			task_id,
			run_id,
			scope,
			scope_key,
			mode,
			prompt,
			response,
			tool_summary,
			executor,
			created_at
		FROM conversation_transcripts
		WHERE id = ?
	`, transcriptID)
	return scanConversationTranscript(row)
}

func (store *Store) validateProjectTaskRunLineageTx(ctx context.Context, tx *sql.Tx, projectID *int64, taskID *int64, runID *int64, scope string, scopeKey string, recordLabel string) (Project, Task, Run, error) {
	var (
		project Project
		task    Task
		run     Run
		err     error
	)

	if projectID != nil {
		project, err = store.getProjectTx(ctx, tx, *projectID)
		if err != nil {
			return Project{}, Task{}, Run{}, err
		}
		if scope != "global" && strings.TrimSpace(scopeKey) != "" && project.Key != scopeKey {
			return Project{}, Task{}, Run{}, fmt.Errorf("%s scope key %q does not match project %q", recordLabel, scopeKey, project.Key)
		}
	}

	if taskID != nil {
		task, err = store.getTaskTx(ctx, tx, *taskID)
		if err != nil {
			return Project{}, Task{}, Run{}, err
		}
		if projectID != nil && task.ProjectID != *projectID {
			return Project{}, Task{}, Run{}, fmt.Errorf("%s task %d does not belong to project %d", recordLabel, task.ID, *projectID)
		}
		if strings.TrimSpace(scope) != "" && task.Scope != scope {
			return Project{}, Task{}, Run{}, fmt.Errorf("%s task %d has scope %q, want %q", recordLabel, task.ID, task.Scope, scope)
		}
	}

	if runID != nil {
		if taskID == nil {
			return Project{}, Task{}, Run{}, fmt.Errorf("%s run lineage requires task identity", recordLabel)
		}
		run, err = store.getRunTx(ctx, tx, *runID)
		if err != nil {
			return Project{}, Task{}, Run{}, err
		}
		if run.TaskID != *taskID {
			return Project{}, Task{}, Run{}, fmt.Errorf("%s run %d does not belong to task %d", recordLabel, run.ID, *taskID)
		}
	}

	return project, task, run, nil
}

func (store *Store) getLearningProposalTx(ctx context.Context, tx *sql.Tx, proposalID int64) (LearningProposal, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, project_id, proposal_type, scope, target_key, summary, hypothesis, change_payload_json, status, created_by, created_at, updated_at
		FROM learning_proposals
		WHERE id = ?
	`, proposalID)
	return scanLearningProposal(row)
}

func (store *Store) getLearningPromotionTx(ctx context.Context, tx *sql.Tx, promotionID int64) (LearningPromotion, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, proposal_id, proposal_type, scope, target_key, status, supersedes_promotion_id, promoted_by, promoted_at, rolled_back_by, rolled_back_at, rollback_reason
		FROM learning_promotions
		WHERE id = ?
	`, promotionID)
	return scanLearningPromotion(row)
}

func (store *Store) getActiveLearningPromotionForTargetTx(ctx context.Context, tx *sql.Tx, proposalType string, scope string, targetKey string) (LearningPromotion, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, proposal_id, proposal_type, scope, target_key, status, supersedes_promotion_id, promoted_by, promoted_at, rolled_back_by, rolled_back_at, rollback_reason
		FROM learning_promotions
		WHERE proposal_type = ? AND scope = ? AND target_key = ? AND status = 'active'
	`, proposalType, scope, targetKey)
	return scanLearningPromotion(row)
}

func (store *Store) getRunWithTaskTx(ctx context.Context, tx *sql.Tx, runID int64) (Run, Task, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT
			r.id, r.task_id, r.executor, r.status, r.attempt, r.started_at, r.finished_at, r.summary,
			t.id, t.project_id, t.key, t.title, t.status, t.scope, t.requested_by, t.current_run_id, t.created_at, t.updated_at
		FROM runs r
		JOIN tasks t ON t.id = r.task_id
		WHERE r.id = ?
	`, runID)

	var run Run
	var task Task
	var finishedAt sql.NullString
	var summary sql.NullString
	var currentRunID sql.NullInt64
	var startedAt string
	var taskCreatedAt string
	var taskUpdatedAt string

	if err := row.Scan(
		&run.ID,
		&run.TaskID,
		&run.Executor,
		&run.Status,
		&run.Attempt,
		&startedAt,
		&finishedAt,
		&summary,
		&task.ID,
		&task.ProjectID,
		&task.Key,
		&task.Title,
		&task.Status,
		&task.Scope,
		&task.RequestedBy,
		&currentRunID,
		&taskCreatedAt,
		&taskUpdatedAt,
	); err != nil {
		return Run{}, Task{}, err
	}

	var err error
	run.StartedAt, err = parseTime(startedAt)
	if err != nil {
		return Run{}, Task{}, err
	}
	run.FinishedAt, err = parseNullableTime(finishedAt)
	if err != nil {
		return Run{}, Task{}, err
	}
	run.Summary = summary.String

	task.CurrentRunID = nullableInt64Ptr(currentRunID)
	task.CreatedAt, err = parseTime(taskCreatedAt)
	if err != nil {
		return Run{}, Task{}, err
	}
	task.UpdatedAt, err = parseTime(taskUpdatedAt)
	if err != nil {
		return Run{}, Task{}, err
	}

	return run, task, nil
}

func (store *Store) getApprovalWithTaskTx(ctx context.Context, tx *sql.Tx, approvalID int64) (Approval, Task, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT
			a.id, a.task_id, a.run_id, a.action_id, a.payload_hash, a.status, a.requested_at, a.resolved_at, a.decision_by, a.reason,
			t.id, t.project_id, t.key, t.title, t.status, t.scope, t.requested_by, t.current_run_id, t.created_at, t.updated_at
		FROM approvals a
		JOIN tasks t ON t.id = a.task_id
		WHERE a.id = ?
	`, approvalID)

	var approval Approval
	var task Task
	var runID sql.NullInt64
	var actionID sql.NullInt64
	var payloadHash sql.NullString
	var resolvedAt sql.NullString
	var decisionBy sql.NullString
	var reason sql.NullString
	var requestedAt string
	var currentRunID sql.NullInt64
	var taskCreatedAt string
	var taskUpdatedAt string

	if err := row.Scan(
		&approval.ID,
		&approval.TaskID,
		&runID,
		&actionID,
		&payloadHash,
		&approval.Status,
		&requestedAt,
		&resolvedAt,
		&decisionBy,
		&reason,
		&task.ID,
		&task.ProjectID,
		&task.Key,
		&task.Title,
		&task.Status,
		&task.Scope,
		&task.RequestedBy,
		&currentRunID,
		&taskCreatedAt,
		&taskUpdatedAt,
	); err != nil {
		return Approval{}, Task{}, err
	}

	var err error
	approval.RunID = nullableInt64Ptr(runID)
	approval.ActionID = nullableInt64Ptr(actionID)
	approval.PayloadHash = payloadHash.String
	approval.RequestedAt, err = parseTime(requestedAt)
	if err != nil {
		return Approval{}, Task{}, err
	}
	approval.ResolvedAt, err = parseNullableTime(resolvedAt)
	if err != nil {
		return Approval{}, Task{}, err
	}
	approval.DecisionBy = decisionBy.String
	approval.Reason = reason.String

	task.CurrentRunID = nullableInt64Ptr(currentRunID)
	task.CreatedAt, err = parseTime(taskCreatedAt)
	if err != nil {
		return Approval{}, Task{}, err
	}
	task.UpdatedAt, err = parseTime(taskUpdatedAt)
	if err != nil {
		return Approval{}, Task{}, err
	}

	return approval, task, nil
}

func (store *Store) getIncidentTx(ctx context.Context, tx *sql.Tx, incidentID int64) (Incident, contextualIDs, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT
			i.id, i.run_id, i.severity, i.status, i.summary, i.details_json, i.opened_at, i.updated_at,
			t.project_id, t.id, t.scope
		FROM incidents i
		LEFT JOIN runs r ON r.id = i.run_id
		LEFT JOIN tasks t ON t.id = r.task_id
		WHERE i.id = ?
	`, incidentID)

	var incident Incident
	var runID sql.NullInt64
	var openedAt string
	var updatedAt string
	var projectID sql.NullInt64
	var taskID sql.NullInt64
	var scope sql.NullString
	if err := row.Scan(
		&incident.ID,
		&runID,
		&incident.Severity,
		&incident.Status,
		&incident.Summary,
		&incident.DetailsJSON,
		&openedAt,
		&updatedAt,
		&projectID,
		&taskID,
		&scope,
	); err != nil {
		return Incident{}, contextualIDs{}, err
	}

	var err error
	incident.RunID = nullableInt64Ptr(runID)
	incident.OpenedAt, err = parseTime(openedAt)
	if err != nil {
		return Incident{}, contextualIDs{}, err
	}
	incident.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return Incident{}, contextualIDs{}, err
	}

	return incident, contextualIDs{
		ProjectID: nullableInt64Ptr(projectID),
		TaskID:    nullableInt64Ptr(taskID),
		Scope:     stringOrDefault(scope, "global"),
	}, nil
}

type contextualIDs struct {
	ProjectID *int64
	TaskID    *int64
	Scope     string
}

func (store *Store) runContextTx(ctx context.Context, tx *sql.Tx, runID *int64) (contextualIDs, error) {
	if runID == nil {
		return contextualIDs{Scope: "global"}, nil
	}

	row := tx.QueryRowContext(ctx, `
		SELECT t.project_id, t.id, t.scope
		FROM runs r
		JOIN tasks t ON t.id = r.task_id
		WHERE r.id = ?
	`, *runID)

	var projectID int64
	var taskID int64
	var scope string
	if err := row.Scan(&projectID, &taskID, &scope); err != nil {
		return contextualIDs{}, err
	}

	return contextualIDs{
		ProjectID: &projectID,
		TaskID:    &taskID,
		Scope:     scope,
	}, nil
}

func (store *Store) recoveryContextTx(ctx context.Context, tx *sql.Tx, incidentID *int64, runID *int64) (contextualIDs, error) {
	if runID != nil {
		return store.runContextTx(ctx, tx, runID)
	}
	if incidentID == nil {
		return contextualIDs{Scope: "global"}, nil
	}

	row := tx.QueryRowContext(ctx, `
		SELECT t.project_id, t.id, t.scope
		FROM incidents i
		LEFT JOIN runs r ON r.id = i.run_id
		LEFT JOIN tasks t ON t.id = r.task_id
		WHERE i.id = ?
	`, *incidentID)

	var projectID sql.NullInt64
	var taskID sql.NullInt64
	var scope sql.NullString
	if err := row.Scan(&projectID, &taskID, &scope); err != nil {
		return contextualIDs{}, err
	}

	return contextualIDs{
		ProjectID: nullableInt64Ptr(projectID),
		TaskID:    nullableInt64Ptr(taskID),
		Scope:     stringOrDefault(scope, "global"),
	}, nil
}

func (store *Store) contextPacketContextTx(ctx context.Context, tx *sql.Tx, taskID *int64, runID *int64) (contextualIDs, error) {
	if taskID != nil {
		row := tx.QueryRowContext(ctx, `SELECT project_id, scope FROM tasks WHERE id = ?`, *taskID)
		var projectID int64
		var scope string
		if err := row.Scan(&projectID, &scope); err != nil {
			return contextualIDs{}, err
		}
		return contextualIDs{ProjectID: &projectID, TaskID: taskID, Scope: scope}, nil
	}

	return store.runContextTx(ctx, tx, runID)
}

func (store *Store) getRecoveryTx(ctx context.Context, tx *sql.Tx, recoveryID int64) (Recovery, contextualIDs, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT
			rec.id, rec.incident_id, rec.run_id, rec.status, rec.strategy, rec.details_json, rec.started_at, rec.finished_at, rec.updated_at,
			t.project_id, t.id, t.scope
		FROM recoveries rec
		LEFT JOIN runs r ON r.id = rec.run_id
		LEFT JOIN tasks t ON t.id = r.task_id
		WHERE rec.id = ?
	`, recoveryID)

	var recovery Recovery
	var incidentID sql.NullInt64
	var runID sql.NullInt64
	var finishedAt sql.NullString
	var startedAt string
	var updatedAt string
	var projectID sql.NullInt64
	var taskID sql.NullInt64
	var scope sql.NullString

	if err := row.Scan(
		&recovery.ID,
		&incidentID,
		&runID,
		&recovery.Status,
		&recovery.Strategy,
		&recovery.DetailsJSON,
		&startedAt,
		&finishedAt,
		&updatedAt,
		&projectID,
		&taskID,
		&scope,
	); err != nil {
		return Recovery{}, contextualIDs{}, err
	}

	var err error
	recovery.IncidentID = nullableInt64Ptr(incidentID)
	recovery.RunID = nullableInt64Ptr(runID)
	recovery.StartedAt, err = parseTime(startedAt)
	if err != nil {
		return Recovery{}, contextualIDs{}, err
	}
	recovery.FinishedAt, err = parseNullableTime(finishedAt)
	if err != nil {
		return Recovery{}, contextualIDs{}, err
	}
	recovery.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return Recovery{}, contextualIDs{}, err
	}

	return recovery, contextualIDs{
		ProjectID: nullableInt64Ptr(projectID),
		TaskID:    nullableInt64Ptr(taskID),
		Scope:     stringOrDefault(scope, "global"),
	}, nil
}

func (store *Store) withTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackOnError(tx)

	if err := fn(tx); err != nil {
		return err
	}

	return tx.Commit()
}

type eventInsert struct {
	StreamType runtimeevents.StreamType
	StreamID   int64
	EventType  runtimeevents.Type
	Scope      string
	ProjectID  *int64
	TaskID     *int64
	RunID      *int64
	Payload    any
	OccurredAt time.Time
	Version    int
}

func appendEventTx(ctx context.Context, tx *sql.Tx, params eventInsert) error {
	payload, err := runtimeevents.EncodePayload(params.Payload)
	if err != nil {
		return err
	}

	version := params.Version
	if version == 0 {
		version = 1
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO events (stream_type, stream_id, event_type, event_version, scope, project_id, task_id, run_id, payload_json, occurred_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		string(params.StreamType),
		params.StreamID,
		string(params.EventType),
		version,
		params.Scope,
		nullInt64(params.ProjectID),
		nullInt64(params.TaskID),
		nullInt64(params.RunID),
		string(payload),
		formatTime(params.OccurredAt),
	)
	return err
}

func validateActionApprovalBinding(ctx context.Context, tx *sql.Tx, taskID int64, runID *int64, actionID *int64, payloadHash string) error {
	if actionID == nil {
		if strings.TrimSpace(payloadHash) != "" {
			return fmt.Errorf("%w: payload_hash requires action_id", ErrInvalidActionApprovalBinding)
		}
		return nil
	}
	if strings.TrimSpace(payloadHash) == "" {
		return fmt.Errorf("%w: action_id requires payload_hash", ErrInvalidActionApprovalBinding)
	}
	if runID == nil {
		return fmt.Errorf("%w: action_id requires run_id", ErrInvalidActionApprovalBinding)
	}

	var workflowRunID int64
	var workflowTaskID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT a.workflow_run_id, r.task_id
		FROM action_payloads ap
		JOIN actions a ON a.id = ap.action_id
		JOIN runs r ON r.id = a.workflow_run_id
		WHERE ap.action_id = ? AND ap.payload_hash = ?
	`, *actionID, payloadHash).Scan(&workflowRunID, &workflowTaskID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: payload_hash does not belong to action_id", ErrInvalidActionApprovalBinding)
		}
		return err
	}
	if *runID != workflowRunID {
		return fmt.Errorf("%w: run_id does not match action workflow_run_id", ErrInvalidActionApprovalBinding)
	}
	if taskID != workflowTaskID {
		return fmt.Errorf("%w: task_id does not match action workflow run task_id", ErrInvalidActionApprovalBinding)
	}
	return nil
}

func validateApprovalPayloadCurrent(ctx context.Context, tx *sql.Tx, approval Approval) error {
	if approval.ActionID == nil {
		return nil
	}

	var currentPayloadHash string
	if err := tx.QueryRowContext(ctx, `
		SELECT current_payload_hash
		FROM actions
		WHERE id = ?
	`, *approval.ActionID).Scan(&currentPayloadHash); err != nil {
		return err
	}
	if approval.PayloadHash != currentPayloadHash {
		return fmt.Errorf("%w: action_id=%d approval_payload_hash=%s current_payload_hash=%s", ErrApprovalPayloadMismatch, *approval.ActionID, approval.PayloadHash, currentPayloadHash)
	}
	return nil
}

func validateActionEvidenceLinks(ctx context.Context, tx *sql.Tx, params AppendActionEvidenceParams) error {
	var workflowRunID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT workflow_run_id
		FROM actions
		WHERE id = ?
	`, params.ActionID).Scan(&workflowRunID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: action_id not found", ErrInvalidActionEvidenceLink)
		}
		return err
	}

	payloadHash := strings.TrimSpace(params.PayloadHash)
	if params.PayloadHash != payloadHash {
		return fmt.Errorf("%w: payload_hash must not include surrounding whitespace", ErrInvalidActionEvidenceLink)
	}
	if payloadHash != "" {
		var exists int
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1
				FROM action_payloads
				WHERE action_id = ? AND payload_hash = ?
			)
		`, params.ActionID, payloadHash).Scan(&exists); err != nil {
			return err
		}
		if exists != 1 {
			return fmt.Errorf("%w: payload_hash does not belong to action_id", ErrInvalidActionEvidenceLink)
		}
	}

	if params.RunID != nil && *params.RunID != workflowRunID {
		return fmt.Errorf("%w: run_id does not match action workflow_run_id", ErrInvalidActionEvidenceLink)
	}

	if params.ApprovalID != nil {
		var approvalActionID sql.NullInt64
		var approvalPayloadHash sql.NullString
		if err := tx.QueryRowContext(ctx, `
			SELECT action_id, payload_hash
			FROM approvals
			WHERE id = ?
		`, *params.ApprovalID).Scan(&approvalActionID, &approvalPayloadHash); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: approval_id not found", ErrInvalidActionEvidenceLink)
			}
			return err
		}
		if !approvalActionID.Valid || approvalActionID.Int64 != params.ActionID {
			return fmt.Errorf("%w: approval_id does not belong to action_id", ErrInvalidActionEvidenceLink)
		}
		if payloadHash != "" && (!approvalPayloadHash.Valid || approvalPayloadHash.String != payloadHash) {
			return fmt.Errorf("%w: approval payload_hash does not match evidence payload_hash", ErrInvalidActionEvidenceLink)
		}
	}

	return nil
}

func normalizeCreateContextPacketParams(params CreateContextPacketParams) CreateContextPacketParams {
	if params.PacketScope == "" {
		switch params.PacketKind {
		case "project":
			params.PacketScope = "project_context"
		case "run":
			params.PacketScope = "run_context"
		default:
			params.PacketScope = "task_wake_packet"
		}
	}
	if params.Trigger == "" {
		params.Trigger = "handoff"
	}
	if params.Status == "" {
		params.Status = "active"
	}
	return params
}

func scanProject(row interface{ Scan(...any) error }) (Project, error) {
	var project Project
	var githubRepo sql.NullString
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&project.ID,
		&project.Key,
		&project.Name,
		&project.Scope,
		&project.GitRoot,
		&project.DefaultBranch,
		&githubRepo,
		&project.ManifestPath,
		&createdAt,
		&updatedAt,
	); err != nil {
		return Project{}, err
	}

	var err error
	project.GitHubRepo = githubRepo.String
	project.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return Project{}, err
	}
	project.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return Project{}, err
	}
	return project, nil
}

func scanTask(row interface{ Scan(...any) error }) (Task, error) {
	var task Task
	var currentRunID sql.NullInt64
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&task.ID,
		&task.ProjectID,
		&task.Key,
		&task.Title,
		&task.Status,
		&task.Scope,
		&task.RequestedBy,
		&currentRunID,
		&createdAt,
		&updatedAt,
	); err != nil {
		return Task{}, err
	}

	var err error
	task.CurrentRunID = nullableInt64Ptr(currentRunID)
	task.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return Task{}, err
	}
	task.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return Task{}, err
	}
	return task, nil
}

func scanRun(row interface{ Scan(...any) error }) (Run, error) {
	var run Run
	var finishedAt sql.NullString
	var summary sql.NullString
	var startedAt string
	if err := row.Scan(
		&run.ID,
		&run.TaskID,
		&run.Executor,
		&run.Status,
		&run.Attempt,
		&startedAt,
		&finishedAt,
		&summary,
	); err != nil {
		return Run{}, err
	}

	var err error
	run.StartedAt, err = parseTime(startedAt)
	if err != nil {
		return Run{}, err
	}
	run.FinishedAt, err = parseNullableTime(finishedAt)
	if err != nil {
		return Run{}, err
	}
	run.Summary = summary.String
	return run, nil
}

func scanApproval(row interface{ Scan(...any) error }) (Approval, error) {
	var approval Approval
	var runID sql.NullInt64
	var actionID sql.NullInt64
	var payloadHash sql.NullString
	var resolvedAt sql.NullString
	var decisionBy sql.NullString
	var reason sql.NullString
	var requestedAt string
	if err := row.Scan(
		&approval.ID,
		&approval.TaskID,
		&runID,
		&actionID,
		&payloadHash,
		&approval.Status,
		&requestedAt,
		&resolvedAt,
		&decisionBy,
		&reason,
	); err != nil {
		return Approval{}, err
	}

	var err error
	approval.RunID = nullableInt64Ptr(runID)
	approval.ActionID = nullableInt64Ptr(actionID)
	approval.PayloadHash = payloadHash.String
	approval.RequestedAt, err = parseTime(requestedAt)
	if err != nil {
		return Approval{}, err
	}
	approval.ResolvedAt, err = parseNullableTime(resolvedAt)
	if err != nil {
		return Approval{}, err
	}
	approval.DecisionBy = decisionBy.String
	approval.Reason = reason.String
	return approval, nil
}

func scanAction(row interface{ Scan(...any) error }) (Action, error) {
	var action Action
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&action.ID,
		&action.WorkflowKey,
		&action.WorkflowRunID,
		&action.ActionType,
		&action.LifecycleState,
		&action.CurrentPayloadHash,
		&createdAt,
		&updatedAt,
	); err != nil {
		return Action{}, err
	}

	var err error
	action.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return Action{}, err
	}
	action.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return Action{}, err
	}
	return action, nil
}

func scanActionPayload(row interface{ Scan(...any) error }) (ActionPayload, error) {
	var payload ActionPayload
	var createdAt string
	if err := row.Scan(
		&payload.ID,
		&payload.ActionID,
		&payload.PayloadSchema,
		&payload.PayloadSchemaVersion,
		&payload.PayloadHash,
		&payload.PayloadJSON,
		&payload.SubmitPath,
		&payload.ReadbackPath,
		&payload.ProofRequirement,
		&createdAt,
	); err != nil {
		return ActionPayload{}, err
	}

	var err error
	payload.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return ActionPayload{}, err
	}
	return payload, nil
}

func scanActionEvidenceEvent(row interface{ Scan(...any) error }) (ActionEvidenceEvent, error) {
	var event ActionEvidenceEvent
	var payloadHash sql.NullString
	var approvalID sql.NullInt64
	var runID sql.NullInt64
	var occurredAt string
	if err := row.Scan(
		&event.ID,
		&event.ActionID,
		&event.EventType,
		&event.EventVersion,
		&payloadHash,
		&approvalID,
		&runID,
		&event.Source,
		&event.EvidenceJSON,
		&occurredAt,
	); err != nil {
		return ActionEvidenceEvent{}, err
	}

	var err error
	event.PayloadHash = nullableStringPtr(payloadHash)
	event.ApprovalID = nullableInt64Ptr(approvalID)
	event.RunID = nullableInt64Ptr(runID)
	event.OccurredAt, err = parseTime(occurredAt)
	if err != nil {
		return ActionEvidenceEvent{}, err
	}
	return event, nil
}

func scanIncident(row interface{ Scan(...any) error }) (Incident, error) {
	var incident Incident
	var runID sql.NullInt64
	var openedAt string
	var updatedAt string
	if err := row.Scan(
		&incident.ID,
		&runID,
		&incident.Severity,
		&incident.Status,
		&incident.Summary,
		&incident.DetailsJSON,
		&openedAt,
		&updatedAt,
	); err != nil {
		return Incident{}, err
	}

	var err error
	incident.RunID = nullableInt64Ptr(runID)
	incident.OpenedAt, err = parseTime(openedAt)
	if err != nil {
		return Incident{}, err
	}
	incident.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return Incident{}, err
	}
	return incident, nil
}

func scanRecovery(row interface{ Scan(...any) error }) (Recovery, error) {
	var recovery Recovery
	var incidentID sql.NullInt64
	var runID sql.NullInt64
	var startedAt string
	var finishedAt sql.NullString
	var updatedAt string
	if err := row.Scan(
		&recovery.ID,
		&incidentID,
		&runID,
		&recovery.Status,
		&recovery.Strategy,
		&recovery.DetailsJSON,
		&startedAt,
		&finishedAt,
		&updatedAt,
	); err != nil {
		return Recovery{}, err
	}

	var err error
	recovery.IncidentID = nullableInt64Ptr(incidentID)
	recovery.RunID = nullableInt64Ptr(runID)
	recovery.StartedAt, err = parseTime(startedAt)
	if err != nil {
		return Recovery{}, err
	}
	recovery.FinishedAt, err = parseNullableTime(finishedAt)
	if err != nil {
		return Recovery{}, err
	}
	recovery.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return Recovery{}, err
	}
	return recovery, nil
}

func scanContextPacket(row interface{ Scan(...any) error }) (ContextPacket, error) {
	var packet ContextPacket
	var taskID sql.NullInt64
	var runID sql.NullInt64
	var supersedesPacketID sql.NullInt64
	var createdAt string
	if err := row.Scan(
		&packet.ID,
		&taskID,
		&runID,
		&packet.PacketKind,
		&packet.PacketScope,
		&packet.Trigger,
		&packet.CheckpointKey,
		&supersedesPacketID,
		&packet.Status,
		&packet.Summary,
		&packet.PayloadJSON,
		&createdAt,
	); err != nil {
		return ContextPacket{}, err
	}

	var err error
	packet.TaskID = nullableInt64Ptr(taskID)
	packet.RunID = nullableInt64Ptr(runID)
	packet.SupersedesPacketID = nullableInt64Ptr(supersedesPacketID)
	packet.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return ContextPacket{}, err
	}
	return packet, nil
}

func scanConversationTranscript(row interface{ Scan(...any) error }) (ConversationTranscript, error) {
	var transcript ConversationTranscript
	var projectID sql.NullInt64
	var taskID sql.NullInt64
	var runID sql.NullInt64
	var createdAt string
	if err := row.Scan(
		&transcript.ID,
		&projectID,
		&taskID,
		&runID,
		&transcript.Scope,
		&transcript.ScopeKey,
		&transcript.Mode,
		&transcript.Prompt,
		&transcript.Response,
		&transcript.ToolSummary,
		&transcript.Executor,
		&createdAt,
	); err != nil {
		return ConversationTranscript{}, err
	}

	var err error
	transcript.ProjectID = nullableInt64Ptr(projectID)
	transcript.TaskID = nullableInt64Ptr(taskID)
	transcript.RunID = nullableInt64Ptr(runID)
	transcript.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return ConversationTranscript{}, err
	}
	return transcript, nil
}

func scanMemorySummary(row interface{ Scan(...any) error }) (MemorySummary, error) {
	var summary MemorySummary
	var projectID sql.NullInt64
	var sourceTranscriptID sql.NullInt64
	var taskID sql.NullInt64
	var runID sql.NullInt64
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&summary.ID,
		&projectID,
		&sourceTranscriptID,
		&taskID,
		&runID,
		&summary.Scope,
		&summary.ScopeKey,
		&summary.MemoryType,
		&summary.Summary,
		&summary.DetailsJSON,
		&createdAt,
		&updatedAt,
	); err != nil {
		return MemorySummary{}, err
	}

	var err error
	summary.ProjectID = nullableInt64Ptr(projectID)
	summary.SourceTranscriptID = nullableInt64Ptr(sourceTranscriptID)
	summary.TaskID = nullableInt64Ptr(taskID)
	summary.RunID = nullableInt64Ptr(runID)
	summary.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return MemorySummary{}, err
	}
	summary.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return MemorySummary{}, err
	}
	return summary, nil
}

func scanKnowledgeArtifact(row interface{ Scan(...any) error }) (KnowledgeArtifact, error) {
	var artifact KnowledgeArtifact
	var ocrRequired int
	var recordedAt string
	if err := row.Scan(
		&artifact.ID,
		&artifact.SHA256,
		&artifact.SizeBytes,
		&artifact.SourceType,
		&artifact.MimeType,
		&artifact.ArtifactPath,
		&artifact.OriginalPath,
		&ocrRequired,
		&recordedAt,
	); err != nil {
		return KnowledgeArtifact{}, err
	}

	var err error
	artifact.RecordedAt, err = parseTime(recordedAt)
	if err != nil {
		return KnowledgeArtifact{}, err
	}
	artifact.OCRRequired = ocrRequired != 0
	return artifact, nil
}

func scanKnowledgeSource(row interface{ Scan(...any) error }) (KnowledgeSource, error) {
	var source KnowledgeSource
	var restricted int
	var currentArtifactID sql.NullInt64
	var currentExtractionID sql.NullInt64
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&source.ID,
		&source.Key,
		&source.Title,
		&source.Scope,
		&source.ScopeKey,
		&restricted,
		&source.SourceKind,
		&source.SourceClass,
		&source.Lifecycle,
		&source.ManifestPath,
		&currentArtifactID,
		&currentExtractionID,
		&createdAt,
		&updatedAt,
	); err != nil {
		return KnowledgeSource{}, err
	}

	var err error
	source.Restricted = restricted != 0
	source.CurrentArtifactID = nullableInt64Ptr(currentArtifactID)
	source.CurrentExtractionID = nullableInt64Ptr(currentExtractionID)
	source.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return KnowledgeSource{}, err
	}
	source.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return KnowledgeSource{}, err
	}
	return source, nil
}

func scanKnowledgeExtraction(row interface{ Scan(...any) error }) (KnowledgeExtraction, error) {
	var extraction KnowledgeExtraction
	var startedAt string
	var finishedAt sql.NullString
	if err := row.Scan(
		&extraction.ID,
		&extraction.SourceID,
		&extraction.ArtifactID,
		&extraction.ExtractorName,
		&extraction.ExtractorVersion,
		&extraction.Status,
		&extraction.FailureCode,
		&extraction.FailureSummary,
		&extraction.ExtractedTextHash,
		&extraction.NormalizedMarkdownPath,
		&startedAt,
		&finishedAt,
	); err != nil {
		return KnowledgeExtraction{}, err
	}

	var err error
	extraction.StartedAt, err = parseTime(startedAt)
	if err != nil {
		return KnowledgeExtraction{}, err
	}
	extraction.FinishedAt, err = parseNullableTime(finishedAt)
	if err != nil {
		return KnowledgeExtraction{}, err
	}
	return extraction, nil
}

func scanKnowledgeChunk(row interface{ Scan(...any) error }) (KnowledgeChunk, error) {
	var chunk KnowledgeChunk
	var pageNumber sql.NullInt64
	var restricted int
	var createdAt string
	if err := row.Scan(
		&chunk.ID,
		&chunk.SourceID,
		&chunk.ExtractionID,
		&chunk.Ordinal,
		&chunk.Text,
		&chunk.Anchor,
		&pageNumber,
		&restricted,
		&createdAt,
	); err != nil {
		return KnowledgeChunk{}, err
	}

	var err error
	chunk.PageNumber = nullableInt64Ptr(pageNumber)
	chunk.Restricted = restricted != 0
	chunk.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return KnowledgeChunk{}, err
	}
	return chunk, nil
}

func scanRestrictedKnowledgeUseApproval(row interface{ Scan(...any) error }) (RestrictedKnowledgeUseApproval, error) {
	var approval RestrictedKnowledgeUseApproval
	var decidedAt string
	if err := row.Scan(
		&approval.ID,
		&approval.SourceID,
		&approval.UseType,
		&approval.Reason,
		&approval.Decision,
		&approval.EvidenceJSON,
		&approval.DecidedBy,
		&decidedAt,
	); err != nil {
		return RestrictedKnowledgeUseApproval{}, err
	}

	var err error
	approval.DecidedAt, err = parseTime(decidedAt)
	if err != nil {
		return RestrictedKnowledgeUseApproval{}, err
	}
	return approval, nil
}

func scanWorkspaceProfile(row interface{ Scan(...any) error }) (WorkspaceProfile, error) {
	var profile WorkspaceProfile
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&profile.ID,
		&profile.WorkspaceID,
		&profile.PreferencesJSON,
		&profile.BoundariesJSON,
		&profile.CadenceDefaultsJSON,
		&createdAt,
		&updatedAt,
	); err != nil {
		return WorkspaceProfile{}, err
	}

	var err error
	profile.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return WorkspaceProfile{}, err
	}
	profile.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return WorkspaceProfile{}, err
	}
	return profile, nil
}

func scanProjectTransition(row interface{ Scan(...any) error }) (ProjectTransition, error) {
	var transition ProjectTransition
	var changedAt string
	if err := row.Scan(
		&transition.ProjectID,
		&transition.State,
		&transition.Controller,
		&transition.LimitedActionsJSON,
		&transition.Notes,
		&transition.ChangedBy,
		&changedAt,
	); err != nil {
		return ProjectTransition{}, err
	}

	var err error
	transition.ID = transition.ProjectID
	transition.ChangedAt, err = parseTime(changedAt)
	if err != nil {
		return ProjectTransition{}, err
	}
	return transition, nil
}

func scanProjectTransitionReport(row interface{ Scan(...any) error }) (ProjectTransitionReport, error) {
	var report ProjectTransitionReport
	var recordedAt string
	if err := row.Scan(
		&report.ID,
		&report.ProjectID,
		&report.ReportType,
		&report.Summary,
		&report.DetailsJSON,
		&recordedAt,
	); err != nil {
		return ProjectTransitionReport{}, err
	}

	var err error
	report.RecordedAt, err = parseTime(recordedAt)
	if err != nil {
		return ProjectTransitionReport{}, err
	}
	return report, nil
}

func scanLearningProposal(row interface{ Scan(...any) error }) (LearningProposal, error) {
	var proposal LearningProposal
	var projectID sql.NullInt64
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&proposal.ID,
		&projectID,
		&proposal.ProposalType,
		&proposal.Scope,
		&proposal.TargetKey,
		&proposal.Summary,
		&proposal.Hypothesis,
		&proposal.ChangePayloadJSON,
		&proposal.Status,
		&proposal.CreatedBy,
		&createdAt,
		&updatedAt,
	); err != nil {
		return LearningProposal{}, err
	}

	var err error
	proposal.ProjectID = nullableInt64Ptr(projectID)
	proposal.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return LearningProposal{}, err
	}
	proposal.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return LearningProposal{}, err
	}
	return proposal, nil
}

func scanLearningEvaluation(row interface{ Scan(...any) error }) (LearningEvaluation, error) {
	var evaluation LearningEvaluation
	var recordedAt string
	if err := row.Scan(
		&evaluation.ID,
		&evaluation.ProposalID,
		&evaluation.FixtureKey,
		&evaluation.Mode,
		&evaluation.Score,
		&evaluation.BaselineSummaryJSON,
		&evaluation.CandidateSummaryJSON,
		&evaluation.ResultSummary,
		&evaluation.Outcome,
		&recordedAt,
	); err != nil {
		return LearningEvaluation{}, err
	}

	var err error
	evaluation.RecordedAt, err = parseTime(recordedAt)
	if err != nil {
		return LearningEvaluation{}, err
	}
	return evaluation, nil
}

func scanLearningPromotion(row interface{ Scan(...any) error }) (LearningPromotion, error) {
	var promotion LearningPromotion
	var supersedesPromotionID sql.NullInt64
	var promotedAt string
	var rolledBackBy sql.NullString
	var rolledBackAt sql.NullString
	if err := row.Scan(
		&promotion.ID,
		&promotion.ProposalID,
		&promotion.ProposalType,
		&promotion.Scope,
		&promotion.TargetKey,
		&promotion.Status,
		&supersedesPromotionID,
		&promotion.PromotedBy,
		&promotedAt,
		&rolledBackBy,
		&rolledBackAt,
		&promotion.RollbackReason,
	); err != nil {
		return LearningPromotion{}, err
	}

	var err error
	promotion.SupersedesPromotionID = nullableInt64Ptr(supersedesPromotionID)
	promotion.PromotedAt, err = parseTime(promotedAt)
	if err != nil {
		return LearningPromotion{}, err
	}
	promotion.RolledBackBy = rolledBackBy.String
	promotion.RolledBackAt, err = parseNullableTime(rolledBackAt)
	if err != nil {
		return LearningPromotion{}, err
	}
	return promotion, nil
}

func scanWorktreeLease(row interface{ Scan(...any) error }) (WorktreeLease, error) {
	var lease WorktreeLease
	var heartbeatAt string
	var releasedAt sql.NullString
	var cleanedUpAt sql.NullString
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&lease.ID,
		&lease.ProjectID,
		&lease.TaskID,
		&lease.RunID,
		&lease.Mode,
		&lease.BranchName,
		&lease.WorktreePath,
		&lease.RepoRoot,
		&lease.State,
		&heartbeatAt,
		&releasedAt,
		&cleanedUpAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return WorktreeLease{}, err
	}

	var err error
	lease.HeartbeatAt, err = parseTime(heartbeatAt)
	if err != nil {
		return WorktreeLease{}, err
	}
	lease.ReleasedAt, err = parseNullableTime(releasedAt)
	if err != nil {
		return WorktreeLease{}, err
	}
	lease.CleanedUpAt, err = parseNullableTime(cleanedUpAt)
	if err != nil {
		return WorktreeLease{}, err
	}
	lease.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return WorktreeLease{}, err
	}
	lease.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return WorktreeLease{}, err
	}
	return lease, nil
}

func scanProjectionFreshness(row interface{ Scan(...any) error }) (ProjectionFreshness, error) {
	var record ProjectionFreshness
	var refreshedAt string
	var updatedAt string
	if err := row.Scan(
		&record.Surface,
		&record.Status,
		&refreshedAt,
		&record.DetailsJSON,
		&updatedAt,
	); err != nil {
		return ProjectionFreshness{}, err
	}

	var err error
	record.RefreshedAt, err = parseTime(refreshedAt)
	if err != nil {
		return ProjectionFreshness{}, err
	}
	record.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return ProjectionFreshness{}, err
	}
	return record, nil
}

func scanEvent(rows *sql.Rows) (runtimeevents.Record, error) {
	var record runtimeevents.Record
	var streamType string
	var eventType string
	var projectID sql.NullInt64
	var taskID sql.NullInt64
	var runID sql.NullInt64
	var payload string
	var occurredAt string
	if err := rows.Scan(
		&record.ID,
		&streamType,
		&record.StreamID,
		&eventType,
		&record.Version,
		&record.Scope,
		&projectID,
		&taskID,
		&runID,
		&payload,
		&occurredAt,
	); err != nil {
		return runtimeevents.Record{}, err
	}

	parsed, err := parseTime(occurredAt)
	if err != nil {
		return runtimeevents.Record{}, err
	}

	record.StreamType = runtimeevents.StreamType(streamType)
	record.Type = runtimeevents.Type(eventType)
	record.ProjectID = nullableInt64Ptr(projectID)
	record.TaskID = nullableInt64Ptr(taskID)
	record.RunID = nullableInt64Ptr(runID)
	record.Payload = []byte(payload)
	record.OccurredAt = parsed
	return record, nil
}

func incidentStatusEvent(previousStatus string, status string, reason string) (runtimeevents.Type, any, bool) {
	switch status {
	case "resolved":
		return runtimeevents.EventIncidentResolved, runtimeevents.IncidentResolvedPayload{
			PreviousStatus: previousStatus,
			Status:         status,
			Reason:         reason,
		}, true
	case "escalated":
		return runtimeevents.EventIncidentEscalated, runtimeevents.IncidentEscalatedPayload{
			PreviousStatus: previousStatus,
			Status:         status,
			Reason:         reason,
		}, true
	default:
		return "", nil, false
	}
}

func mapWorktreeLeaseError(err error) error {
	if strings.Contains(err.Error(), "idx_worktree_leases_active_task") ||
		strings.Contains(err.Error(), "idx_worktree_leases_active_branch") ||
		strings.Contains(err.Error(), "idx_worktree_leases_active_path") ||
		strings.Contains(err.Error(), "UNIQUE constraint failed: worktree_leases.project_id, worktree_leases.task_id") ||
		strings.Contains(err.Error(), "UNIQUE constraint failed: worktree_leases.branch_name") ||
		strings.Contains(err.Error(), "UNIQUE constraint failed: worktree_leases.worktree_path") {
		return fmt.Errorf("%w: %v", ErrWorktreeLeaseConflict, err)
	}
	return err
}

func parseTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}

func parseNullableTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid || value.String == "" {
		return nil, nil
	}
	parsed, err := parseTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func nullableInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	ptr := new(int64)
	*ptr = value.Int64
	return ptr
}

func nullableStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	ptr := new(string)
	*ptr = value.String
	return ptr
}

func stringPtrIfNotEmpty(value string) *string {
	if value == "" {
		return nil
	}
	ptr := new(string)
	*ptr = value
	return ptr
}

func nullInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTime(*value)
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func stringOrDefault(value sql.NullString, fallback string) string {
	if value.Valid && value.String != "" {
		return value.String
	}
	return fallback
}
