package health

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"odin-os/internal/executors/contract"
	executorrouter "odin-os/internal/executors/router"
	"odin-os/internal/store/sqlite"
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
	RuntimeHeartbeatTTL    time.Duration
}

func formatSQLiteTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z")
}

type Service struct {
	DB                *sql.DB
	Config            Config
	Now               func() time.Time
	ExecutorKeys      []string
	ExpectedExecutors []string
	ImmediateNotReady *atomic.Bool
	Media             *MediaChecks
}

func DefaultConfig() Config {
	return Config{
		QueuePressureThreshold: 10,
		ExecutorFreshnessTTL:   30 * time.Minute,
		SourceFreshnessTTL:     30 * time.Minute,
		ProjectionFreshnessTTL: 30 * time.Minute,
		RuntimeHeartbeatTTL:    2 * time.Minute,
	}
}

func (service Service) Doctor(ctx context.Context, registryHealthy bool) (Report, error) {
	now := time.Now().UTC()
	if service.Now != nil {
		now = service.Now().UTC()
	}

	config := service.resolvedConfig()

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

func (service Service) DispatchReport(ctx context.Context, registryHealthy bool) (Report, bool, error) {
	report, err := service.Doctor(ctx, registryHealthy)
	if err != nil {
		return Report{}, false, err
	}
	return report, dispatchSafe(report), nil
}

func (service Service) Readiness(ctx context.Context, registryHealthy bool) (Report, bool, error) {
	report, safeToDispatch, err := service.DispatchReport(ctx, registryHealthy)
	if err != nil {
		return Report{}, false, err
	}
	if service.ImmediateNotReady != nil && service.ImmediateNotReady.Load() {
		return report, false, nil
	}

	runtimeReady, err := service.runtimeReady(ctx)
	if err != nil {
		return Report{}, false, err
	}
	return report, safeToDispatch && runtimeReady, nil
}

func (service Service) ExecutorStatus(ctx context.Context, executor string) (Check, bool, error) {
	now := time.Now().UTC()
	if service.Now != nil {
		now = service.Now().UTC()
	}

	config := service.resolvedConfig()

	check, found, err := service.executorCheckFor(ctx, executor, now, config)
	if err != nil {
		return Check{}, false, err
	}
	return check, found, nil
}

func (service Service) executorCheck(ctx context.Context, now time.Time, config Config) (Check, error) {
	check, _, err := service.executorCheckFor(ctx, "", now, config)
	return check, err
}

func (service Service) executorCheckFor(ctx context.Context, executor string, now time.Time, config Config) (Check, bool, error) {
	if executor == "" {
		return service.aggregateExecutorCheck(ctx, now, config)
	}

	query := `
		SELECT status, checked_at
		FROM executor_health
	`
	args := []any{}
	if executor != "" {
		query += ` WHERE executor = ?`
		args = append(args, executor)
	}
	query += `
		ORDER BY checked_at DESC, id DESC
		LIMIT 1
	`

	var status string
	var checkedAt string
	err := service.DB.QueryRowContext(ctx, query, args...).Scan(&status, &checkedAt)
	switch err {
	case sql.ErrNoRows:
		check := Check{
			Name:       "executor",
			Status:     StatusDegraded,
			Summary:    "no executor health samples recorded",
			ObservedAt: now,
		}
		if executor != "" {
			check.Details = map[string]string{"executor": executor}
		}
		return check, false, nil
	case nil:
	default:
		return Check{}, false, err
	}

	parsed, err := time.Parse(time.RFC3339Nano, checkedAt)
	if err != nil {
		return Check{}, false, err
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
	if executor != "" {
		check.Details["executor"] = executor
	}
	return check, true, nil
}

func (service Service) aggregateExecutorCheck(ctx context.Context, now time.Time, config Config) (Check, bool, error) {
	executorKeys := service.executorKeys()
	if executorKeys != nil && len(executorKeys) == 0 {
		return Check{
			Name:       "executor",
			Status:     StatusDegraded,
			Summary:    "no enabled executor lanes configured",
			ObservedAt: now,
			Details: map[string]string{
				"tracked_executors":   "0",
				"healthy_executors":   "0",
				"stale_executors":     "0",
				"unhealthy_executors": "0",
			},
		}, false, nil
	}

	query := `
		SELECT eh.executor, eh.status, eh.checked_at
		FROM executor_health eh
		JOIN (
			SELECT executor, MAX(id) AS max_id
			FROM executor_health
			GROUP BY executor
		) latest ON latest.max_id = eh.id
	`
	args := []any{}
	if executorKeys != nil {
		query += fmt.Sprintf(" WHERE eh.executor IN (%s)", placeholders(len(executorKeys)))
		for _, key := range executorKeys {
			args = append(args, key)
		}
	}
	query += ` ORDER BY eh.executor ASC`

	rows, err := service.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return Check{}, false, err
	}
	defer rows.Close()

	total := 0
	healthy := 0
	stale := 0
	unhealthy := 0
	for rows.Next() {
		var (
			executor  string
			status    string
			checkedAt string
		)
		if err := rows.Scan(&executor, &status, &checkedAt); err != nil {
			return Check{}, false, err
		}
		total++
		parsed, err := time.Parse(time.RFC3339Nano, checkedAt)
		if err != nil {
			return Check{}, false, err
		}
		if status != "healthy" {
			unhealthy++
		}
		if parsed.Before(now.Add(-config.ExecutorFreshnessTTL)) {
			stale++
			continue
		}
		if status == "healthy" {
			healthy++
		}
	}
	if err := rows.Err(); err != nil {
		return Check{}, false, err
	}

	check := Check{
		Name:       "executor",
		Status:     StatusHealthy,
		Summary:    "executor health is fresh",
		ObservedAt: now,
		Details: map[string]string{
			"tracked_executors":   fmt.Sprintf("%d", total),
			"healthy_executors":   fmt.Sprintf("%d", healthy),
			"stale_executors":     fmt.Sprintf("%d", stale),
			"unhealthy_executors": fmt.Sprintf("%d", unhealthy),
		},
	}
	if total == 0 {
		check.Status = StatusDegraded
		check.Summary = "no executor health samples recorded"
		return check, false, nil
	}
	if healthy == 0 {
		check.Status = StatusDegraded
		check.Summary = "no healthy executor lanes are available"
		return check, true, nil
	}
	check.Summary = "executor capacity is available"
	return check, true, nil
}

func (service Service) executorKeys() []string {
	if service.ExecutorKeys != nil {
		return service.ExecutorKeys
	}
	return service.ExpectedExecutors
}

func (service Service) queueCheck(ctx context.Context, now time.Time, config Config) (Check, error) {
	var queued int
	var running int
	if err := service.DB.QueryRowContext(ctx, `
		SELECT
			COUNT(CASE WHEN status = 'queued' AND next_eligible_at <= ? THEN 1 END),
			COUNT(CASE WHEN status = 'running' THEN 1 END)
		FROM tasks
	`, formatSQLiteTime(now)).Scan(&queued, &running); err != nil {
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
	`, formatSQLiteTime(now.Add(-config.ProjectionFreshnessTTL))).Scan(&total, &stale); err != nil {
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

func dispatchSafe(report Report) bool {
	for _, check := range report.Checks {
		switch check.Name {
		case "database", "registry", "executor", "projections", "source_freshness":
			if check.Status != StatusHealthy {
				return false
			}
		default:
			if strings.HasPrefix(check.Name, "media.") && check.Status != StatusHealthy {
				return false
			}
		}
	}
	return true
}

func (service Service) runtimeReady(ctx context.Context) (bool, error) {
	if service.DB == nil {
		return false, nil
	}

	config := service.resolvedConfig()
	now := time.Now().UTC()
	if service.Now != nil {
		now = service.Now().UTC()
	}

	var (
		status          string
		lastHeartbeatAt string
	)
	err := service.DB.QueryRowContext(ctx, `
		SELECT status, last_heartbeat_at
		FROM runtime_state
		WHERE singleton_key = ?
	`, "primary").Scan(&status, &lastHeartbeatAt)
	switch err {
	case nil:
		if status != "ready" {
			return false, nil
		}
		parsed, err := time.Parse(time.RFC3339Nano, lastHeartbeatAt)
		if err != nil {
			return false, err
		}
		return !parsed.Before(now.Add(-config.RuntimeHeartbeatTTL)), nil
	case sql.ErrNoRows:
		return false, nil
	default:
		return false, err
	}
}

func (service Service) resolvedConfig() Config {
	config := service.Config
	defaults := DefaultConfig()
	if config.QueuePressureThreshold == 0 {
		config.QueuePressureThreshold = defaults.QueuePressureThreshold
	}
	if config.ExecutorFreshnessTTL <= 0 {
		config.ExecutorFreshnessTTL = defaults.ExecutorFreshnessTTL
	}
	if config.SourceFreshnessTTL <= 0 {
		config.SourceFreshnessTTL = defaults.SourceFreshnessTTL
	}
	if config.ProjectionFreshnessTTL <= 0 {
		config.ProjectionFreshnessTTL = defaults.ProjectionFreshnessTTL
	}
	if config.RuntimeHeartbeatTTL <= 0 {
		config.RuntimeHeartbeatTTL = defaults.RuntimeHeartbeatTTL
	}
	return config
}

func (service Service) SampleConfiguredExecutors(ctx context.Context, store *sqlite.Store, config executorrouter.Config, executors map[string]contract.Executor, source string) error {
	if store == nil {
		return fmt.Errorf("health sampling store is required")
	}

	restore := service.withStoreClock(store)
	defer restore()

	for _, executorConfig := range config.Executors {
		if !executorConfig.Enabled {
			continue
		}

		status := contract.HealthStatusUnavailable
		details := "executor not found in catalog"
		if executor, ok := executors[executorConfig.Key]; ok {
			report, err := executor.Health(ctx)
			if err != nil {
				details = err.Error()
			} else {
				status = report.Status
				details = report.Details
			}
		}

		if _, err := store.RecordExecutorHealth(ctx, sqlite.RecordExecutorHealthParams{
			Executor:    executorConfig.Key,
			Status:      string(status),
			LatencyMS:   0,
			DetailsJSON: healthDetailsJSON(source, details),
		}); err != nil {
			return err
		}
	}

	return nil
}

func (service Service) RefreshProjectionFreshness(ctx context.Context, store *sqlite.Store, surfaces []string, source string) error {
	if store == nil {
		return fmt.Errorf("projection freshness store is required")
	}

	restore := service.withStoreClock(store)
	defer restore()

	for _, surface := range surfaces {
		if _, err := store.RecordProjectionFreshness(ctx, sqlite.RecordProjectionFreshnessParams{
			Surface:     surface,
			Status:      "current",
			DetailsJSON: healthDetailsJSON(source, ""),
		}); err != nil {
			return err
		}
	}

	return nil
}

func (service Service) withStoreClock(store *sqlite.Store) func() {
	if store == nil || service.Now == nil {
		return func() {}
	}

	originalNow := store.Now
	store.Now = service.Now
	return func() {
		store.Now = originalNow
	}
}

func healthDetailsJSON(source string, details string) string {
	payload := map[string]string{
		"source": source,
	}
	if details != "" {
		payload["details"] = details
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return `{"source":"health"}`
	}
	return string(encoded)
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
