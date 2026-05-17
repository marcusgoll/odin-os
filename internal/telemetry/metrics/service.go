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
	GeneratedAt             time.Time
	ActiveRuns              int
	BlockedItems            int
	ApprovalsWaiting        int
	OpenIncidents           int
	EscalatedIncidents      int
	ActiveRecoveries        int
	QueuedTasks             int
	ReviewQueueItems        int
	FailedWorkItems         int
	RecoveryRecommendations int
	StaleExecutors          int
	StaleSources            int
	StaleProjections        int
	MediaOpenIncidents      int
	MediaCandidates         int
	OS                      OSSnapshot
}

type OSSnapshot struct {
	HealthScore               int
	Status                    string
	LifecyclePhase            string
	TelemetryStale            bool
	BackupAgeSeconds          int64
	BackupAgeSecondsSet       bool
	RestoreTestAgeSeconds     int64
	RestoreTestAgeSecondsSet  bool
	UpdatesPending            int
	UpdatesPendingSet         bool
	SecurityUpdatesPending    int
	SecurityUpdatesPendingSet bool
	RebootRequired            bool
	RebootRequiredSet         bool
	SystemdFailedUnits        int
	SystemdFailedUnitsSet     bool
	CriticalServices          []CriticalServiceMetric
	CriticalContainers        []CriticalContainerMetric
}

type CriticalServiceMetric struct {
	Name string
	Up   bool
}

type CriticalContainerMetric struct {
	Name string
	Up   bool
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
		fmt.Sprintf("odin_review_queue_items %d", snapshot.ReviewQueueItems),
		fmt.Sprintf("odin_failed_work_items %d", snapshot.FailedWorkItems),
		fmt.Sprintf("odin_recovery_recommendations %d", snapshot.RecoveryRecommendations),
		fmt.Sprintf("odin_stale_executors %d", snapshot.StaleExecutors),
		fmt.Sprintf("odin_stale_sources %d", snapshot.StaleSources),
		fmt.Sprintf("odin_stale_projections %d", snapshot.StaleProjections),
		fmt.Sprintf("odin_media_open_incidents %d", snapshot.MediaOpenIncidents),
		fmt.Sprintf("odin_media_candidates %d", snapshot.MediaCandidates),
	}
	lines = append(lines, renderOSMetrics(snapshot.OS)...)
	return strings.Join(lines, "\n") + "\n"
}

func renderOSMetrics(snapshot OSSnapshot) []string {
	status := snapshot.Status
	if status == "" {
		status = "unknown"
	}
	phase := snapshot.LifecyclePhase
	if phase == "" {
		phase = "run"
	}

	lines := []string{
		fmt.Sprintf("odin_os_health_score %d", snapshot.HealthScore),
		fmt.Sprintf("odin_os_status{status=%q} 1", status),
		fmt.Sprintf("odin_os_lifecycle_phase{phase=%q} 1", phase),
		fmt.Sprintf("odin_os_telemetry_stale %d", boolMetric(snapshot.TelemetryStale)),
	}
	if snapshot.BackupAgeSecondsSet || snapshot.BackupAgeSeconds != 0 {
		lines = append(lines, fmt.Sprintf("odin_os_backup_age_seconds %d", snapshot.BackupAgeSeconds))
	}
	if snapshot.RestoreTestAgeSecondsSet || snapshot.RestoreTestAgeSeconds != 0 {
		lines = append(lines, fmt.Sprintf("odin_os_restore_test_age_seconds %d", snapshot.RestoreTestAgeSeconds))
	}
	if snapshot.UpdatesPendingSet || snapshot.UpdatesPending != 0 {
		lines = append(lines, fmt.Sprintf("odin_os_updates_pending_total %d", snapshot.UpdatesPending))
	}
	if snapshot.SecurityUpdatesPendingSet || snapshot.SecurityUpdatesPending != 0 {
		lines = append(lines, fmt.Sprintf("odin_os_security_updates_pending_total %d", snapshot.SecurityUpdatesPending))
	}
	if snapshot.RebootRequiredSet || snapshot.RebootRequired {
		lines = append(lines, fmt.Sprintf("odin_os_reboot_required %d", boolMetric(snapshot.RebootRequired)))
	}
	if snapshot.SystemdFailedUnitsSet || snapshot.SystemdFailedUnits != 0 {
		lines = append(lines, fmt.Sprintf("odin_os_systemd_failed_units_total %d", snapshot.SystemdFailedUnits))
	}
	for _, service := range snapshot.CriticalServices {
		lines = append(lines, fmt.Sprintf("odin_os_critical_service_up{service=%q} %d", service.Name, boolMetric(service.Up)))
	}
	for _, container := range snapshot.CriticalContainers {
		lines = append(lines, fmt.Sprintf("odin_os_critical_container_up{container=%q} %d", container.Name, boolMetric(container.Up)))
	}
	return lines
}

func boolMetric(value bool) int {
	if value {
		return 1
	}
	return 0
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
	actualUse, err := projections.GetActualUseSummaryView(ctx, service.DB, "")
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

	staleExecutorTelemetry, err := service.executorTelemetryStaleCount(ctx, config, now)
	if err != nil {
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

	var mediaOpenIncidents int
	if err := service.DB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM incidents
		WHERE status IN ('open', 'escalated')
		  AND details_json LIKE '%"domain":"media"%'
	`).Scan(&mediaOpenIncidents); err != nil {
		return Snapshot{}, err
	}

	var mediaCandidates int
	if err := service.DB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM tasks
		WHERE status = 'blocked'
		  AND requested_by = 'media-supervisor'
		  AND key LIKE 'media-maintenance-%'
	`).Scan(&mediaCandidates); err != nil {
		return Snapshot{}, err
	}

	executorSamples, sourceSamples, projectionSamples, err := service.telemetrySampleCounts(ctx)
	if err != nil {
		return Snapshot{}, err
	}

	snapshot := Snapshot{
		GeneratedAt:             now,
		ActiveRuns:              len(activeRuns),
		BlockedItems:            len(blocked),
		ApprovalsWaiting:        len(approvals),
		OpenIncidents:           len(incidents),
		EscalatedIncidents:      escalatedIncidents,
		ActiveRecoveries:        countActiveRecoveries(recoveries),
		QueuedTasks:             queuedTasks,
		ReviewQueueItems:        actualUse.ReviewQueueItems,
		FailedWorkItems:         actualUse.FailedWorkItems,
		RecoveryRecommendations: actualUse.RecoveryRecommendations,
		StaleExecutors:          staleExecutors,
		StaleSources:            staleSources,
		StaleProjections:        staleProjections,
		MediaOpenIncidents:      mediaOpenIncidents,
		MediaCandidates:         mediaCandidates,
	}
	snapshot.OS = deriveOSSnapshot(snapshot, executorSamples == 0 || sourceSamples == 0 || projectionSamples == 0 || staleExecutorTelemetry > 0)
	return snapshot, nil
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

func (service Service) telemetrySampleCounts(ctx context.Context) (int, int, int, error) {
	var executorSamples int
	var sourceSamples int
	var projectionSamples int
	if err := service.DB.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM executor_health),
			(SELECT COUNT(*) FROM registry_versions),
			(SELECT COUNT(*) FROM projection_freshness)
	`).Scan(&executorSamples, &sourceSamples, &projectionSamples); err != nil {
		return 0, 0, 0, err
	}
	return executorSamples, sourceSamples, projectionSamples, nil
}

func (service Service) executorTelemetryStaleCount(ctx context.Context, config Config, now time.Time) (int, error) {
	if service.ExecutorKeys != nil && len(service.ExecutorKeys) == 0 {
		return 0, nil
	}

	query := `
		SELECT COUNT(*)
		FROM (
			SELECT eh.checked_at
			FROM executor_health eh
			JOIN (
				SELECT executor, MAX(id) AS max_id
				FROM executor_health
				GROUP BY executor
			) latest ON latest.max_id = eh.id
		)
	`
	args := []any{}
	if service.ExecutorKeys != nil {
		query = `
			SELECT COUNT(*)
			FROM (
				SELECT eh.checked_at
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
	query += ` WHERE checked_at < ?`
	args = append(args, formatSQLiteTime(now.Add(-config.ExecutorFreshnessTTL)))

	var count int
	if err := service.DB.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func deriveOSSnapshot(snapshot Snapshot, telemetryMissing bool) OSSnapshot {
	telemetryStale := telemetryMissing || snapshot.StaleSources > 0 || snapshot.StaleProjections > 0

	osSnapshot := OSSnapshot{
		HealthScore:    100,
		Status:         "healthy",
		LifecyclePhase: "run",
		TelemetryStale: telemetryStale,
	}
	if telemetryStale {
		osSnapshot.HealthScore = 80
		osSnapshot.Status = "unknown"
		return osSnapshot
	}
	return osSnapshot
}
