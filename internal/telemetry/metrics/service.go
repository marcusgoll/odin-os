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
	DB     *sql.DB
	Config Config
	Now    func() time.Time
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
	if err := service.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE status = 'queued'`).Scan(&queuedTasks); err != nil {
		return Snapshot{}, err
	}

	var escalatedIncidents int
	if err := service.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM incidents WHERE status = 'escalated'`).Scan(&escalatedIncidents); err != nil {
		return Snapshot{}, err
	}

	var staleExecutors int
	if err := service.DB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM (
			SELECT checked_at, status
			FROM executor_health
			ORDER BY checked_at DESC, id DESC
			LIMIT 1
		)
		WHERE status != 'healthy' OR checked_at < ?
	`, now.Add(-config.ExecutorFreshnessTTL).Format(time.RFC3339Nano)).Scan(&staleExecutors); err != nil {
		return Snapshot{}, err
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
	`, now.Add(-config.SourceFreshnessTTL).Format(time.RFC3339Nano)).Scan(&staleSources); err != nil {
		return Snapshot{}, err
	}

	var staleProjections int
	if err := service.DB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM projection_freshness
		WHERE refreshed_at < ?
	`, now.Add(-config.ProjectionFreshnessTTL).Format(time.RFC3339Nano)).Scan(&staleProjections); err != nil {
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
