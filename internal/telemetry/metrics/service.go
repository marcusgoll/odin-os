package metrics

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"odin-os/internal/runtime/projections"
)

type Snapshot struct {
	GeneratedAt        time.Time
	ActiveRuns         int
	BlockedItems       int
	ApprovalsWaiting   int
	OpenIncidents      int
	EscalatedIncidents int
	ActiveRecoveries   int
	QueuedTasks        int
	StaleExecutors     int
	StaleSources       int
	StaleProjections   int
}

type Config struct {
	ExecutorFreshnessTTL   time.Duration
	SourceFreshnessTTL     time.Duration
	ProjectionFreshnessTTL time.Duration
}

type Service struct {
	DB           *sql.DB
	Config       Config
	Now          func() time.Time
	ExecutorKeys []string
}

func formatSQLiteTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z")
}

func Render(snapshot Snapshot) string {
	lines := []string{
		fmt.Sprintf("odin_active_runs %d", snapshot.ActiveRuns),
		fmt.Sprintf("odin_blocked_items %d", snapshot.BlockedItems),
		fmt.Sprintf("odin_approvals_waiting %d", snapshot.ApprovalsWaiting),
		fmt.Sprintf("odin_open_incidents %d", snapshot.OpenIncidents),
		fmt.Sprintf("odin_escalated_incidents %d", snapshot.EscalatedIncidents),
		fmt.Sprintf("odin_active_recoveries %d", snapshot.ActiveRecoveries),
		fmt.Sprintf("odin_queued_tasks %d", snapshot.QueuedTasks),
		fmt.Sprintf("odin_stale_executors %d", snapshot.StaleExecutors),
		fmt.Sprintf("odin_stale_sources %d", snapshot.StaleSources),
		fmt.Sprintf("odin_stale_projections %d", snapshot.StaleProjections),
	}
	return strings.Join(lines, "\n")
}

func (service Service) Collect(ctx context.Context) (Snapshot, error) {
	now := time.Now().UTC()
	if service.Now != nil {
		now = service.Now().UTC()
	}

	config := service.Config
	if config.ExecutorFreshnessTTL == 0 {
		config.ExecutorFreshnessTTL = 30 * time.Minute
	}
	if config.SourceFreshnessTTL == 0 {
		config.SourceFreshnessTTL = 30 * time.Minute
	}
	if config.ProjectionFreshnessTTL == 0 {
		config.ProjectionFreshnessTTL = 30 * time.Minute
	}

	activeRuns, err := projections.ListActiveRunViews(ctx, service.DB)
	if err != nil {
		return Snapshot{}, err
	}
	blocked, err := projections.ListBlockedItemViews(ctx, service.DB)
	if err != nil {
		return Snapshot{}, err
	}
	approvals, err := projections.ListPendingApprovalViews(ctx, service.DB)
	if err != nil {
		return Snapshot{}, err
	}
	incidents, err := projections.ListIncidentViews(ctx, service.DB)
	if err != nil {
		return Snapshot{}, err
	}
	recoveries, err := projections.ListRecoveryViews(ctx, service.DB)
	if err != nil {
		return Snapshot{}, err
	}

	var queuedTasks int
	if err := service.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE status = 'queued' AND next_eligible_at <= ?`, formatSQLiteTime(now)).Scan(&queuedTasks); err != nil {
		return Snapshot{}, err
	}

	var escalatedIncidents int
	if err := service.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM incidents WHERE status = 'escalated'`).Scan(&escalatedIncidents); err != nil {
		return Snapshot{}, err
	}

	query := `
		SELECT COUNT(*)
		FROM (
			SELECT eh.checked_at, eh.status
			FROM executor_health eh
			JOIN (
				SELECT executor, MAX(id) AS max_id
				FROM executor_health
				GROUP BY executor
			) latest ON latest.max_id = eh.id
		)
	`
	args := []any{}
	var staleExecutors int
	if service.ExecutorKeys != nil && len(service.ExecutorKeys) == 0 {
		staleExecutors = 0
	} else {
		if service.ExecutorKeys != nil {
			query = `
			SELECT COUNT(*)
			FROM (
				SELECT eh.checked_at, eh.status
				FROM executor_health eh
				JOIN (
					SELECT executor, MAX(id) AS max_id
					FROM executor_health
					GROUP BY executor
				) latest ON latest.max_id = eh.id
				WHERE eh.executor IN (` + placeholders(len(service.ExecutorKeys)) + `)
			)
		`
			for _, key := range service.ExecutorKeys {
				args = append(args, key)
			}
		}
		query += ` WHERE status != 'healthy' OR checked_at < ?`
		args = append(args, formatSQLiteTime(now.Add(-config.ExecutorFreshnessTTL)))

		if err := service.DB.QueryRowContext(ctx, query, args...).Scan(&staleExecutors); err != nil {
			return Snapshot{}, err
		}
	}

	var staleSources int
	if err := service.DB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM (
			SELECT compiled_at
			FROM registry_versions
			ORDER BY compiled_at DESC, id DESC
			LIMIT 1
		)
		WHERE compiled_at < ?
	`, formatSQLiteTime(now.Add(-config.SourceFreshnessTTL))).Scan(&staleSources); err != nil {
		return Snapshot{}, err
	}

	var staleProjections int
	if err := service.DB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM projection_freshness
		WHERE refreshed_at < ?
	`, formatSQLiteTime(now.Add(-config.ProjectionFreshnessTTL))).Scan(&staleProjections); err != nil {
		return Snapshot{}, err
	}

	return Snapshot{
		GeneratedAt:        now,
		ActiveRuns:         len(activeRuns),
		BlockedItems:       len(blocked),
		ApprovalsWaiting:   len(approvals),
		OpenIncidents:      len(incidents),
		EscalatedIncidents: escalatedIncidents,
		ActiveRecoveries:   countActiveRecoveries(recoveries),
		QueuedTasks:        queuedTasks,
		StaleExecutors:     staleExecutors,
		StaleSources:       staleSources,
		StaleProjections:   staleProjections,
	}, nil
}

func countActiveRecoveries(views []projections.RecoveryView) int {
	count := 0
	for _, view := range views {
		if view.Status == "running" {
			count++
		}
	}
	return count
}

func placeholders(count int) string {
	if count <= 0 {
		return ""
	}
	values := make([]string, 0, count)
	for i := 0; i < count; i++ {
		values = append(values, "?")
	}
	return strings.Join(values, ", ")
}
