package recovery

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func formatSQLiteTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z")
}

func (monitor Monitor) Observe(ctx context.Context) ([]Observation, error) {
	if monitor.DB == nil {
		return nil, fmt.Errorf("recovery monitor database is not configured")
	}

	now := time.Now().UTC()
	if monitor.Now != nil {
		now = monitor.Now().UTC()
	}
	config := applyDefaults(monitor.Config)

	var observations []Observation

	executorObservation, err := monitor.executorHealthObservation(ctx, now, config)
	if err != nil {
		return nil, err
	}
	if executorObservation != nil {
		observations = append(observations, *executorObservation)
	}

	projectionObservations, err := monitor.projectionObservations(ctx, now, config)
	if err != nil {
		return nil, err
	}
	observations = append(observations, projectionObservations...)

	sourceObservation, err := monitor.sourceFreshnessObservation(ctx, now, config)
	if err != nil {
		return nil, err
	}
	if sourceObservation != nil {
		observations = append(observations, *sourceObservation)
	}

	queueObservation, err := monitor.queuePressureObservation(ctx, now, config)
	if err != nil {
		return nil, err
	}
	if queueObservation != nil {
		observations = append(observations, *queueObservation)
	}

	runFailureObservations, err := monitor.repeatedRunFailureObservations(ctx, config)
	if err != nil {
		return nil, err
	}
	observations = append(observations, runFailureObservations...)

	return observations, nil
}

func (monitor Monitor) executorHealthObservation(ctx context.Context, now time.Time, config Config) (*Observation, error) {
	var executor string
	var status string
	var checkedAt string
	err := monitor.DB.QueryRowContext(ctx, `
		SELECT executor, status, checked_at
		FROM executor_health
		ORDER BY checked_at DESC, id DESC
		LIMIT 1
	`).Scan(&executor, &status, &checkedAt)
	switch err {
	case sql.ErrNoRows:
		return &Observation{
			FaultKey:   FaultExecutorHealthStale,
			SubjectKey: "unknown",
			Scope:      "global",
			Severity:   "warning",
			Summary:    "no executor health sample is recorded",
		}, nil
	case nil:
	default:
		return nil, err
	}

	parsed, err := time.Parse(time.RFC3339Nano, checkedAt)
	if err != nil {
		return nil, err
	}
	if status == "healthy" && !parsed.Before(now.Add(-config.ExecutorFreshnessTTL)) {
		return nil, nil
	}

	return &Observation{
		FaultKey:   FaultExecutorHealthStale,
		SubjectKey: executor,
		Scope:      "global",
		Severity:   "warning",
		Summary:    "executor health is stale or unavailable",
	}, nil
}

func (monitor Monitor) projectionObservations(ctx context.Context, now time.Time, config Config) ([]Observation, error) {
	rows, err := monitor.DB.QueryContext(ctx, `
		SELECT surface
		FROM projection_freshness
		WHERE refreshed_at < ?
		ORDER BY surface ASC
	`, formatSQLiteTime(now.Add(-config.ProjectionFreshnessTTL)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var observations []Observation
	for rows.Next() {
		var surface string
		if err := rows.Scan(&surface); err != nil {
			return nil, err
		}
		observations = append(observations, Observation{
			FaultKey:   FaultProjectionStale,
			SubjectKey: surface,
			Scope:      "global",
			Severity:   "warning",
			Summary:    "projection freshness is stale",
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(observations) > 0 {
		return observations, nil
	}

	var total int
	if err := monitor.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM projection_freshness`).Scan(&total); err != nil {
		return nil, err
	}
	if total == 0 {
		return []Observation{{
			FaultKey:   FaultProjectionStale,
			SubjectKey: "all",
			Scope:      "global",
			Severity:   "warning",
			Summary:    "projection freshness has not been recorded",
		}}, nil
	}
	return nil, nil
}

func (monitor Monitor) sourceFreshnessObservation(ctx context.Context, now time.Time, config Config) (*Observation, error) {
	var source string
	var compiledAt string
	err := monitor.DB.QueryRowContext(ctx, `
		SELECT source, compiled_at
		FROM registry_versions
		ORDER BY compiled_at DESC, id DESC
		LIMIT 1
	`).Scan(&source, &compiledAt)
	switch err {
	case sql.ErrNoRows:
		return &Observation{
			FaultKey:   FaultSourceFreshnessStale,
			SubjectKey: "registry",
			Scope:      "global",
			Severity:   "warning",
			Summary:    "registry compilation is missing",
		}, nil
	case nil:
	default:
		return nil, err
	}

	parsed, err := time.Parse(time.RFC3339Nano, compiledAt)
	if err != nil {
		return nil, err
	}
	if !parsed.Before(now.Add(-config.SourceFreshnessTTL)) {
		return nil, nil
	}

	return &Observation{
		FaultKey:   FaultSourceFreshnessStale,
		SubjectKey: source,
		Scope:      "global",
		Severity:   "warning",
		Summary:    "registry compilation is stale",
	}, nil
}

func (monitor Monitor) queuePressureObservation(ctx context.Context, now time.Time, config Config) (*Observation, error) {
	var queued int
	if err := monitor.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE status = 'queued' AND next_eligible_at <= ?`, formatSQLiteTime(now)).Scan(&queued); err != nil {
		return nil, err
	}
	if queued <= config.QueuePressureThreshold {
		return nil, nil
	}

	return &Observation{
		FaultKey:   FaultQueuePressureHigh,
		SubjectKey: "task_queue",
		Scope:      "global",
		Severity:   "warning",
		Summary:    fmt.Sprintf("queued tasks are above threshold: %d", queued),
	}, nil
}

func (monitor Monitor) repeatedRunFailureObservations(ctx context.Context, config Config) ([]Observation, error) {
	rows, err := monitor.DB.QueryContext(ctx, `
		SELECT
			t.project_id,
			t.id,
			r.id,
			t.key,
			t.scope,
			COUNT(*) AS terminal_runs
		FROM runs r
		JOIN tasks t ON t.id = r.task_id
		WHERE r.status IN ('failed', 'timeout')
		GROUP BY t.project_id, t.id, t.key, t.scope
		HAVING COUNT(*) >= ?
		ORDER BY t.id ASC
	`, config.RepeatedRunFailureThreshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var observations []Observation
	for rows.Next() {
		var projectID int64
		var taskID int64
		var latestRunID int64
		var taskKey string
		var scope string
		var terminalRuns int
		if err := rows.Scan(&projectID, &taskID, &latestRunID, &taskKey, &scope, &terminalRuns); err != nil {
			return nil, err
		}
		projectIDCopy := projectID
		taskIDCopy := taskID
		runIDCopy := latestRunID
		observations = append(observations, Observation{
			FaultKey:   FaultRunFailureRepeated,
			SubjectKey: "task:" + taskKey,
			Scope:      scope,
			Severity:   "warning",
			Summary:    fmt.Sprintf("task has %d failed or timed-out runs", terminalRuns),
			ProjectID:  &projectIDCopy,
			TaskID:     &taskIDCopy,
			RunID:      &runIDCopy,
		})
	}

	return observations, rows.Err()
}
