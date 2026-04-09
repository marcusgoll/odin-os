package health

import (
	"context"
	"database/sql"
)

type Status string

const (
	StatusOK       Status = "ok"
	StatusDegraded Status = "degraded"
	StatusUnknown  Status = "unknown"
)

type Summary struct {
	Status          Status
	DatabaseHealthy bool
	RegistryHealthy bool
	ExecutorStatus  string
}

type Service struct {
	DB *sql.DB
}

func (service Service) Summary(ctx context.Context, registryHealthy bool) (Summary, error) {
	summary := Summary{
		RegistryHealthy: registryHealthy,
	}

	if service.DB == nil {
		summary.Status = StatusDegraded
		return summary, nil
	}

	if err := service.DB.PingContext(ctx); err != nil {
		summary.Status = StatusDegraded
		return summary, nil
	}
	summary.DatabaseHealthy = true

	var executorStatus string
	err := service.DB.QueryRowContext(ctx, `
		SELECT status
		FROM executor_health
		ORDER BY checked_at DESC, id DESC
		LIMIT 1
	`).Scan(&executorStatus)
	switch {
	case err == sql.ErrNoRows:
		summary.Status = StatusUnknown
	case err != nil:
		return Summary{}, err
	default:
		summary.ExecutorStatus = executorStatus
		if executorStatus == "healthy" {
			summary.Status = StatusOK
		} else {
			summary.Status = StatusDegraded
		}
	}

	if !registryHealthy {
		summary.Status = StatusDegraded
	}
	if summary.Status == "" {
		summary.Status = StatusUnknown
	}

	return summary, nil
}
