package health

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Status string

const (
	StatusHealthy  Status = "healthy"
	StatusDegraded Status = "degraded"
	StatusFailed   Status = "failed"
)

type Check struct {
	Name       string            `json:"name"`
	Status     Status            `json:"status"`
	Summary    string            `json:"summary"`
	Details    map[string]string `json:"details,omitempty"`
	ObservedAt time.Time         `json:"observed_at"`
}

type Report struct {
	Status      Status    `json:"status"`
	GeneratedAt time.Time `json:"generated_at"`
	Checks      []Check   `json:"checks"`
}

type Summary struct {
	Status          Status
	DatabaseHealthy bool
	RegistryHealthy bool
	ExecutorStatus  string
}

type Config struct {
	QueuePressureThreshold int
	ExecutorFreshnessTTL   time.Duration
	SourceFreshnessTTL     time.Duration
	ProjectionFreshnessTTL time.Duration
}

type Service struct {
	DB     *sql.DB
	Config Config
	Now    func() time.Time
	Media  *MediaChecks
}

func DefaultConfig() Config {
	return Config{
		QueuePressureThreshold: 10,
		ExecutorFreshnessTTL:   30 * time.Minute,
		SourceFreshnessTTL:     30 * time.Minute,
		ProjectionFreshnessTTL: 30 * time.Minute,
	}
}

func (service Service) Doctor(ctx context.Context, registryHealthy bool) (Report, error) {
	now := time.Now().UTC()
	if service.Now != nil {
		now = service.Now().UTC()
	}

	config := service.Config
	if config == (Config{}) {
		config = DefaultConfig()
	}

	report := Report{
		Status:      StatusHealthy,
		GeneratedAt: now,
	}

	if service.DB == nil {
		report.Status = StatusFailed
		report.Checks = append(report.Checks, Check{
			Name:       "database",
			Status:     StatusFailed,
			Summary:    "database handle is not configured",
			ObservedAt: now,
		})
		return report, nil
	}

	if err := service.DB.PingContext(ctx); err != nil {
		report.Status = StatusFailed
		report.Checks = append(report.Checks, Check{
			Name:       "database",
			Status:     StatusFailed,
			Summary:    "database connectivity failed",
			Details:    map[string]string{"error": err.Error()},
			ObservedAt: now,
		})
		return report, nil
	}

	report.Checks = append(report.Checks, Check{
		Name:       "database",
		Status:     StatusHealthy,
		Summary:    "database reachable",
		ObservedAt: now,
	})

	registryCheck := Check{
		Name:       "registry",
		Status:     StatusHealthy,
		Summary:    "registry loaded cleanly",
		ObservedAt: now,
	}
	if !registryHealthy {
		registryCheck.Status = StatusDegraded
		registryCheck.Summary = "registry diagnostics present"
	}
	report.Checks = append(report.Checks, registryCheck)
	report.Status = combineStatus(report.Status, registryCheck.Status)

	executorCheck, err := service.executorCheck(ctx, now, config)
	if err != nil {
		return Report{}, err
	}
	report.Checks = append(report.Checks, executorCheck)
	report.Status = combineStatus(report.Status, executorCheck.Status)

	queueCheck, err := service.queueCheck(ctx, now, config)
	if err != nil {
		return Report{}, err
	}
	report.Checks = append(report.Checks, queueCheck)
	report.Status = combineStatus(report.Status, queueCheck.Status)

	projectionCheck, err := service.projectionCheck(ctx, now, config)
	if err != nil {
		return Report{}, err
	}
	report.Checks = append(report.Checks, projectionCheck)
	report.Status = combineStatus(report.Status, projectionCheck.Status)

	sourceCheck, err := service.sourceCheck(ctx, now, config)
	if err != nil {
		return Report{}, err
	}
	report.Checks = append(report.Checks, sourceCheck)
	report.Status = combineStatus(report.Status, sourceCheck.Status)

	if service.Media != nil {
		mediaChecks, err := service.Media.Checks(ctx, config, now)
		if err != nil {
			return Report{}, err
		}
		for _, mediaCheck := range mediaChecks {
			report.Checks = append(report.Checks, mediaCheck)
			report.Status = combineStatus(report.Status, mediaCheck.Status)
		}
	}

	return report, nil
}

func (service Service) Summary(ctx context.Context, registryHealthy bool) (Summary, error) {
	report, err := service.Doctor(ctx, registryHealthy)
	if err != nil {
		return Summary{}, err
	}

	summary := Summary{Status: report.Status}
	for _, check := range report.Checks {
		switch check.Name {
		case "database":
			summary.DatabaseHealthy = check.Status == StatusHealthy
		case "registry":
			summary.RegistryHealthy = check.Status == StatusHealthy
		case "executor":
			summary.ExecutorStatus = string(check.Status)
		}
	}

	return summary, nil
}

func (service Service) executorCheck(ctx context.Context, now time.Time, config Config) (Check, error) {
	var status string
	var checkedAt string
	err := service.DB.QueryRowContext(ctx, `
		SELECT status, checked_at
		FROM executor_health
		ORDER BY checked_at DESC, id DESC
		LIMIT 1
	`).Scan(&status, &checkedAt)
	switch err {
	case sql.ErrNoRows:
		return Check{
			Name:       "executor",
			Status:     StatusDegraded,
			Summary:    "no executor health samples recorded",
			ObservedAt: now,
		}, nil
	case nil:
	default:
		return Check{}, err
	}

	parsed, err := time.Parse(time.RFC3339Nano, checkedAt)
	if err != nil {
		return Check{}, err
	}

	check := Check{
		Name:       "executor",
		Status:     StatusHealthy,
		Summary:    "executor health is fresh",
		ObservedAt: now,
		Details: map[string]string{
			"executor_status": status,
			"checked_at":      checkedAt,
		},
	}
	if status != "healthy" || parsed.Before(now.Add(-config.ExecutorFreshnessTTL)) {
		check.Status = StatusDegraded
		check.Summary = "executor health is unavailable or stale"
	}
	return check, nil
}

func (service Service) queueCheck(ctx context.Context, now time.Time, config Config) (Check, error) {
	var queued int
	var running int
	if err := service.DB.QueryRowContext(ctx, `
		SELECT
			COUNT(CASE WHEN status = 'queued' THEN 1 END),
			COUNT(CASE WHEN status = 'running' THEN 1 END)
		FROM tasks
	`).Scan(&queued, &running); err != nil {
		return Check{}, err
	}

	check := Check{
		Name:       "queue",
		Status:     StatusHealthy,
		Summary:    "queue pressure is within threshold",
		ObservedAt: now,
		Details: map[string]string{
			"queued_tasks":  fmt.Sprintf("%d", queued),
			"running_tasks": fmt.Sprintf("%d", running),
		},
	}
	if queued > config.QueuePressureThreshold {
		check.Status = StatusDegraded
		check.Summary = "queue pressure is above threshold"
	}
	return check, nil
}

func (service Service) projectionCheck(ctx context.Context, now time.Time, config Config) (Check, error) {
	var total int
	var stale int
	if err := service.DB.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COUNT(CASE WHEN refreshed_at < ? THEN 1 END)
		FROM projection_freshness
	`, now.Add(-config.ProjectionFreshnessTTL).Format(time.RFC3339Nano)).Scan(&total, &stale); err != nil {
		return Check{}, err
	}

	check := Check{
		Name:       "projections",
		Status:     StatusHealthy,
		Summary:    "projection freshness is current",
		ObservedAt: now,
		Details: map[string]string{
			"tracked_surfaces": fmt.Sprintf("%d", total),
			"stale_surfaces":   fmt.Sprintf("%d", stale),
		},
	}
	if total == 0 || stale > 0 {
		check.Status = StatusDegraded
		check.Summary = "projection freshness is missing or stale"
	}
	return check, nil
}

func (service Service) sourceCheck(ctx context.Context, now time.Time, config Config) (Check, error) {
	var compiledAt string
	err := service.DB.QueryRowContext(ctx, `
		SELECT compiled_at
		FROM registry_versions
		ORDER BY compiled_at DESC, id DESC
		LIMIT 1
	`).Scan(&compiledAt)
	switch err {
	case sql.ErrNoRows:
		return Check{
			Name:       "source_freshness",
			Status:     StatusDegraded,
			Summary:    "no registry compilation recorded",
			ObservedAt: now,
		}, nil
	case nil:
	default:
		return Check{}, err
	}

	parsed, err := time.Parse(time.RFC3339Nano, compiledAt)
	if err != nil {
		return Check{}, err
	}

	check := Check{
		Name:       "source_freshness",
		Status:     StatusHealthy,
		Summary:    "source freshness is current",
		ObservedAt: now,
		Details: map[string]string{
			"compiled_at": compiledAt,
		},
	}
	if parsed.Before(now.Add(-config.SourceFreshnessTTL)) {
		check.Status = StatusDegraded
		check.Summary = "source freshness is stale"
	}
	return check, nil
}

func combineStatus(current Status, next Status) Status {
	if current == StatusFailed || next == StatusFailed {
		return StatusFailed
	}
	if current == StatusDegraded || next == StatusDegraded {
		return StatusDegraded
	}
	return StatusHealthy
}
