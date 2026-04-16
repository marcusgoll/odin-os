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

const (
	defaultWorkspaceKey    = "marcus"
	defaultWorkspaceName   = "Marcus Workspace"
	defaultWorkspaceOwner  = "marcus"
	defaultWorkspaceStatus = "active"

	managedProjectInitiativeKind   = "managed_project"
	managedProjectInitiativeStatus = "active"
)

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

		workspace, err := store.ensureDefaultWorkspaceTx(ctx, tx, now)
		if err != nil {
			return err
		}

		if _, err := store.reconcileManagedProjectInitiativeTx(ctx, tx, ReconcileManagedProjectInitiativeParams{
			WorkspaceID: workspace.ID,
			ProjectID:   project.ID,
			Key:         project.Key,
			Title:       project.Name,
			Status:      managedProjectInitiativeStatus,
			Summary:     fmt.Sprintf("Managed project for %s", project.Name),
		}, now); err != nil {
			return err
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

func (store *Store) CreateWorkspace(ctx context.Context, params CreateWorkspaceParams) (Workspace, error) {
	now := store.now()
	var workspace Workspace

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO workspaces (key, name, owner_ref, status, default_companion_key, policy_json, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`,
			params.Key,
			params.Name,
			params.OwnerRef,
			params.Status,
			nullIfEmpty(params.DefaultCompanionKey),
			params.PolicyJSON,
			formatTime(now),
			formatTime(now),
		)
		if err != nil {
			return err
		}

		workspaceID, err := result.LastInsertId()
		if err != nil {
			return err
		}

		workspace = Workspace{
			ID:                  workspaceID,
			Key:                 params.Key,
			Name:                params.Name,
			OwnerRef:            params.OwnerRef,
			Status:              params.Status,
			DefaultCompanionKey: params.DefaultCompanionKey,
			PolicyJSON:          params.PolicyJSON,
			CreatedAt:           now,
			UpdatedAt:           now,
		}

		return nil
	})

	return workspace, err
}

func (store *Store) EnsureDefaultWorkspace(ctx context.Context) (Workspace, error) {
	now := store.now()
	var workspace Workspace

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		record, err := store.ensureDefaultWorkspaceTx(ctx, tx, now)
		if err != nil {
			return err
		}
		workspace = record
		return nil
	})

	return workspace, err
}

func (store *Store) CreateInitiative(ctx context.Context, params CreateInitiativeParams) (Initiative, error) {
	now := store.now()
	var initiative Initiative

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := store.getWorkspaceTx(ctx, tx, params.WorkspaceID); err != nil {
			return err
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO initiatives (
				workspace_id, key, title, kind, status, summary, linked_project_id, owner_companion_id, created_at, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			params.WorkspaceID,
			params.Key,
			params.Title,
			params.Kind,
			params.Status,
			params.Summary,
			nullInt64(params.LinkedProjectID),
			nullInt64(params.OwnerCompanionID),
			formatTime(now),
			formatTime(now),
		)
		if err != nil {
			return err
		}

		initiativeID, err := result.LastInsertId()
		if err != nil {
			return err
		}

		initiative = Initiative{
			ID:               initiativeID,
			WorkspaceID:      params.WorkspaceID,
			Key:              params.Key,
			Title:            params.Title,
			Kind:             params.Kind,
			Status:           params.Status,
			Summary:          params.Summary,
			LinkedProjectID:  params.LinkedProjectID,
			OwnerCompanionID: params.OwnerCompanionID,
			CreatedAt:        now,
			UpdatedAt:        now,
		}

		return nil
	})

	return initiative, err
}

func (store *Store) ReconcileManagedProjectInitiative(ctx context.Context, params ReconcileManagedProjectInitiativeParams) (Initiative, error) {
	now := store.now()
	var initiative Initiative

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		record, err := store.reconcileManagedProjectInitiativeTx(ctx, tx, params, now)
		if err != nil {
			return err
		}
		initiative = record
		return nil
	})

	return initiative, err
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

		result, err := tx.ExecContext(ctx, `
			INSERT INTO approvals (task_id, run_id, status, requested_at, resolved_at, decision_by, reason)
			VALUES (?, ?, ?, ?, NULL, '', '')
		`,
			params.TaskID,
			nullInt64(params.RunID),
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
				Status:     approval.Status,
				DecisionBy: approval.DecisionBy,
				Reason:     approval.Reason,
			},
			OccurredAt: now,
		})
	})

	return approval, err
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

func (store *Store) GetWorkspace(ctx context.Context, workspaceID int64) (Workspace, error) {
	row := store.db.QueryRowContext(ctx, `
		SELECT id, key, name, owner_ref, status, default_companion_key, policy_json, created_at, updated_at
		FROM workspaces
		WHERE id = ?
	`, workspaceID)
	return scanWorkspace(row)
}

func (store *Store) GetWorkspaceByKey(ctx context.Context, key string) (Workspace, error) {
	row := store.db.QueryRowContext(ctx, `
		SELECT id, key, name, owner_ref, status, default_companion_key, policy_json, created_at, updated_at
		FROM workspaces
		WHERE key = ?
	`, key)
	return scanWorkspace(row)
}

func (store *Store) GetInitiative(ctx context.Context, initiativeID int64) (Initiative, error) {
	row := store.db.QueryRowContext(ctx, `
		SELECT id, workspace_id, key, title, kind, status, summary, linked_project_id, owner_companion_id, created_at, updated_at
		FROM initiatives
		WHERE id = ?
	`, initiativeID)
	return scanInitiative(row)
}

func (store *Store) GetInitiativeByProjectID(ctx context.Context, projectID int64) (Initiative, error) {
	row := store.db.QueryRowContext(ctx, `
		SELECT id, workspace_id, key, title, kind, status, summary, linked_project_id, owner_companion_id, created_at, updated_at
		FROM initiatives
		WHERE linked_project_id = ?
	`, projectID)
	return scanInitiative(row)
}

func (store *Store) ListWorkspaces(ctx context.Context, params ListWorkspacesParams) ([]Workspace, error) {
	query := `
		SELECT id, key, name, owner_ref, status, default_companion_key, policy_json, created_at, updated_at
		FROM workspaces
		WHERE 1 = 1
	`
	var args []any
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

	var workspaces []Workspace
	for rows.Next() {
		workspace, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		workspaces = append(workspaces, workspace)
	}

	return workspaces, rows.Err()
}

func (store *Store) ListInitiatives(ctx context.Context, params ListInitiativesParams) ([]Initiative, error) {
	query := `
		SELECT id, workspace_id, key, title, kind, status, summary, linked_project_id, owner_companion_id, created_at, updated_at
		FROM initiatives
		WHERE 1 = 1
	`
	var args []any
	if params.WorkspaceID != nil {
		query += ` AND workspace_id = ?`
		args = append(args, *params.WorkspaceID)
	}
	if params.Kind != "" {
		query += ` AND kind = ?`
		args = append(args, params.Kind)
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

	var initiatives []Initiative
	for rows.Next() {
		initiative, err := scanInitiative(rows)
		if err != nil {
			return nil, err
		}
		initiatives = append(initiatives, initiative)
	}

	return initiatives, rows.Err()
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
		SELECT id, task_id, run_id, status, requested_at, resolved_at, decision_by, reason
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

func (store *Store) getWorkspaceTx(ctx context.Context, tx *sql.Tx, workspaceID int64) (Workspace, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, key, name, owner_ref, status, default_companion_key, policy_json, created_at, updated_at
		FROM workspaces
		WHERE id = ?
	`, workspaceID)
	return scanWorkspace(row)
}

func (store *Store) getWorkspaceByKeyTx(ctx context.Context, tx *sql.Tx, key string) (Workspace, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, key, name, owner_ref, status, default_companion_key, policy_json, created_at, updated_at
		FROM workspaces
		WHERE key = ?
	`, key)
	return scanWorkspace(row)
}

func (store *Store) getInitiativeByProjectIDTx(ctx context.Context, tx *sql.Tx, projectID int64) (Initiative, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, workspace_id, key, title, kind, status, summary, linked_project_id, owner_companion_id, created_at, updated_at
		FROM initiatives
		WHERE linked_project_id = ?
	`, projectID)
	return scanInitiative(row)
}

func (store *Store) ensureDefaultWorkspaceTx(ctx context.Context, tx *sql.Tx, now time.Time) (Workspace, error) {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO workspaces (key, name, owner_ref, status, default_companion_key, policy_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, NULL, ?, ?, ?)
		ON CONFLICT(key) DO NOTHING
	`,
		defaultWorkspaceKey,
		defaultWorkspaceName,
		defaultWorkspaceOwner,
		defaultWorkspaceStatus,
		`{}`,
		formatTime(now),
		formatTime(now),
	); err != nil {
		return Workspace{}, err
	}

	return store.getWorkspaceByKeyTx(ctx, tx, defaultWorkspaceKey)
}

func (store *Store) reconcileManagedProjectInitiativeTx(ctx context.Context, tx *sql.Tx, params ReconcileManagedProjectInitiativeParams, now time.Time) (Initiative, error) {
	if _, err := store.getWorkspaceTx(ctx, tx, params.WorkspaceID); err != nil {
		return Initiative{}, err
	}
	project, err := store.getProjectTx(ctx, tx, params.ProjectID)
	if err != nil {
		return Initiative{}, err
	}

	key := params.Key
	if key == "" {
		key = project.Key
	}
	title := params.Title
	if title == "" {
		title = project.Name
	}
	status := params.Status
	if status == "" {
		status = managedProjectInitiativeStatus
	}
	summary := params.Summary
	if summary == "" {
		summary = fmt.Sprintf("Managed project for %s", title)
	}

	initiative, err := store.getInitiativeByProjectIDTx(ctx, tx, params.ProjectID)
	if err == nil {
		if _, err := tx.ExecContext(ctx, `
			UPDATE initiatives
			SET workspace_id = ?, key = ?, title = ?, kind = ?, status = ?, summary = ?, updated_at = ?
			WHERE id = ?
		`,
			params.WorkspaceID,
			key,
			title,
			managedProjectInitiativeKind,
			status,
			summary,
			formatTime(now),
			initiative.ID,
		); err != nil {
			return Initiative{}, err
		}

		initiative.WorkspaceID = params.WorkspaceID
		initiative.Key = key
		initiative.Title = title
		initiative.Kind = managedProjectInitiativeKind
		initiative.Status = status
		initiative.Summary = summary
		initiative.UpdatedAt = now
		if initiative.LinkedProjectID == nil {
			initiative.LinkedProjectID = &project.ID
		}
		return initiative, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Initiative{}, err
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO initiatives (
			workspace_id, key, title, kind, status, summary, linked_project_id, owner_companion_id, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?, ?)
	`,
		params.WorkspaceID,
		key,
		title,
		managedProjectInitiativeKind,
		status,
		summary,
		project.ID,
		formatTime(now),
		formatTime(now),
	)
	if err != nil {
		return Initiative{}, err
	}

	initiativeID, err := result.LastInsertId()
	if err != nil {
		return Initiative{}, err
	}

	projectID := project.ID
	return Initiative{
		ID:              initiativeID,
		WorkspaceID:     params.WorkspaceID,
		Key:             key,
		Title:           title,
		Kind:            managedProjectInitiativeKind,
		Status:          status,
		Summary:         summary,
		LinkedProjectID: &projectID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}, nil
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
			a.id, a.task_id, a.run_id, a.status, a.requested_at, a.resolved_at, a.decision_by, a.reason,
			t.id, t.project_id, t.key, t.title, t.status, t.scope, t.requested_by, t.current_run_id, t.created_at, t.updated_at
		FROM approvals a
		JOIN tasks t ON t.id = a.task_id
		WHERE a.id = ?
	`, approvalID)

	var approval Approval
	var task Task
	var runID sql.NullInt64
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

func scanWorkspace(row interface{ Scan(...any) error }) (Workspace, error) {
	var workspace Workspace
	var defaultCompanionKey sql.NullString
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&workspace.ID,
		&workspace.Key,
		&workspace.Name,
		&workspace.OwnerRef,
		&workspace.Status,
		&defaultCompanionKey,
		&workspace.PolicyJSON,
		&createdAt,
		&updatedAt,
	); err != nil {
		return Workspace{}, err
	}

	var err error
	workspace.DefaultCompanionKey = defaultCompanionKey.String
	workspace.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return Workspace{}, err
	}
	workspace.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return Workspace{}, err
	}
	return workspace, nil
}

func scanInitiative(row interface{ Scan(...any) error }) (Initiative, error) {
	var initiative Initiative
	var linkedProjectID sql.NullInt64
	var ownerCompanionID sql.NullInt64
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&initiative.ID,
		&initiative.WorkspaceID,
		&initiative.Key,
		&initiative.Title,
		&initiative.Kind,
		&initiative.Status,
		&initiative.Summary,
		&linkedProjectID,
		&ownerCompanionID,
		&createdAt,
		&updatedAt,
	); err != nil {
		return Initiative{}, err
	}

	var err error
	initiative.LinkedProjectID = nullableInt64Ptr(linkedProjectID)
	initiative.OwnerCompanionID = nullableInt64Ptr(ownerCompanionID)
	initiative.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return Initiative{}, err
	}
	initiative.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return Initiative{}, err
	}
	return initiative, nil
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
	var resolvedAt sql.NullString
	var decisionBy sql.NullString
	var reason sql.NullString
	var requestedAt string
	if err := row.Scan(
		&approval.ID,
		&approval.TaskID,
		&runID,
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

func nullInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func stringOrDefault(value sql.NullString, fallback string) string {
	if value.Valid && value.String != "" {
		return value.String
	}
	return fallback
}
