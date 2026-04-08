package sqlite

import (
	"context"
	"database/sql"
	"sync"
	"time"

	runtimeevents "odin-os/internal/runtime/events"

	_ "modernc.org/sqlite"
)

type Store struct {
	db        *sql.DB
	closeOnce sync.Once
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

	err := store.withTx(ctx, func(tx *sql.Tx) error {
		contextRow, err := store.contextPacketContextTx(ctx, tx, params.TaskID, params.RunID)
		if err != nil {
			return err
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO context_packets (task_id, run_id, packet_kind, summary, payload_json, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`,
			nullInt64(params.TaskID),
			nullInt64(params.RunID),
			params.PacketKind,
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
			ID:          packetID,
			TaskID:      params.TaskID,
			RunID:       params.RunID,
			PacketKind:  params.PacketKind,
			Summary:     params.Summary,
			PayloadJSON: params.PayloadJSON,
			CreatedAt:   now,
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
				PacketKind: packet.PacketKind,
				Summary:    packet.Summary,
			},
			OccurredAt: now,
		})
	})

	return packet, err
}

func (store *Store) GetTask(ctx context.Context, taskID int64) (Task, error) {
	return store.getTaskQuery(ctx, store.db, taskID)
}

func (store *Store) GetRun(ctx context.Context, runID int64) (Run, error) {
	row := store.db.QueryRowContext(ctx, `
		SELECT id, task_id, executor, status, attempt, started_at, finished_at, summary
		FROM runs
		WHERE id = ?
	`, runID)
	return scanRun(row)
}

func (store *Store) GetApproval(ctx context.Context, approvalID int64) (Approval, error) {
	row := store.db.QueryRowContext(ctx, `
		SELECT id, task_id, run_id, status, requested_at, resolved_at, decision_by, reason
		FROM approvals
		WHERE id = ?
	`, approvalID)
	return scanApproval(row)
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
