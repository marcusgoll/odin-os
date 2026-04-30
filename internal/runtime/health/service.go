package health

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	Detail     string            `json:"detail,omitempty"`
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
	DB         *sql.DB
	RepoRoot   string
	Config     Config
	Env        map[string]string
	LookPath   func(string) (string, error)
	RunCommand func(context.Context, string, ...string) error
	Now        func() time.Time
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

	for _, check := range service.readinessChecks(ctx, now) {
		report.Checks = append(report.Checks, check)
		report.Status = combineStatus(report.Status, check.Status)
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

func (service Service) readinessChecks(ctx context.Context, now time.Time) []Check {
	if strings.TrimSpace(service.RepoRoot) == "" {
		return nil
	}

	var checks []Check
	codexPath, codexErr := service.lookPath("codex")
	if codexErr != nil {
		checks = append(checks,
			service.simpleCheck("codex_cli", StatusDegraded, "codex executable not found", now),
			service.simpleCheck("codex_exec", StatusDegraded, "codex executable missing", now),
		)
	} else {
		checks = append(checks, Check{
			Name:       "codex_cli",
			Status:     StatusHealthy,
			Summary:    "codex executable found",
			Detail:     "codex executable found",
			Details:    map[string]string{"path": codexPath},
			ObservedAt: now,
		})
		if err := service.runCommand(ctx, "codex", "exec", "--help"); err != nil {
			checks = append(checks, Check{
				Name:       "codex_exec",
				Status:     StatusDegraded,
				Summary:    "codex exec is not available",
				Detail:     "codex exec is not available",
				Details:    map[string]string{"error": redactTokenLike(err.Error())},
				ObservedAt: now,
			})
		} else {
			checks = append(checks, service.simpleCheck("codex_exec", StatusHealthy, "codex exec available", now))
		}
	}

	checks = append(checks,
		service.pathCheck("odin_e2e", "odin e2e fixtures available", []string{
			"fixtures/e2e/github-readonly-intake.yaml",
			"internal/e2e/run.go",
		}, now),
		service.makeE2ECheck(now),
		service.fileContainsCheck("agents_e2e_rule", "AGENTS.md e2e rule present", "AGENTS.md", []string{
			"Required verification for Odin-OS changes",
			"make odin-e2e-local",
		}, now),
		service.fileContainsCheck("workflow_e2e_rule", "WORKFLOW.md e2e rule present", "WORKFLOW.md", []string{
			"make odin-e2e-local",
		}, now),
		service.githubTokenCheck(now),
		service.modeCheck("dry_run_mode", "dry-run mode", "ODIN_DRY_RUN", now),
		service.killSwitchCheck(now),
	)

	return checks
}

func (service Service) pathCheck(name string, summary string, relativePaths []string, now time.Time) Check {
	missing := make([]string, 0)
	for _, relativePath := range relativePaths {
		if _, err := os.Stat(filepath.Join(service.RepoRoot, filepath.FromSlash(relativePath))); err != nil {
			missing = append(missing, relativePath)
		}
	}
	if len(missing) > 0 {
		return Check{
			Name:       name,
			Status:     StatusDegraded,
			Summary:    "required files are missing",
			Detail:     "required files are missing",
			Details:    map[string]string{"missing": strings.Join(missing, ",")},
			ObservedAt: now,
		}
	}
	return service.simpleCheck(name, StatusHealthy, summary, now)
}

func (service Service) makeE2ECheck(now time.Time) Check {
	makefile, err := os.ReadFile(filepath.Join(service.RepoRoot, "Makefile"))
	if err != nil {
		return Check{
			Name:       "odin_e2e_command",
			Status:     StatusDegraded,
			Summary:    "Makefile not readable",
			Detail:     "Makefile not readable",
			Details:    map[string]string{"error": err.Error()},
			ObservedAt: now,
		}
	}
	hasTarget := strings.Contains(string(makefile), "odin-e2e-local:")
	hasScript := fileExists(filepath.Join(service.RepoRoot, "scripts", "odin-e2e-local.sh"))
	if !hasTarget || !hasScript {
		return Check{
			Name:    "odin_e2e_command",
			Status:  StatusDegraded,
			Summary: "make odin-e2e-local is not available",
			Detail:  "make odin-e2e-local is not available",
			Details: map[string]string{
				"make_target": fmt.Sprintf("%t", hasTarget),
				"script":      fmt.Sprintf("%t", hasScript),
			},
			ObservedAt: now,
		}
	}
	return service.simpleCheck("odin_e2e_command", StatusHealthy, "make odin-e2e-local available", now)
}

func (service Service) fileContainsCheck(name string, summary string, relativePath string, required []string, now time.Time) Check {
	content, err := os.ReadFile(filepath.Join(service.RepoRoot, filepath.FromSlash(relativePath)))
	if err != nil {
		return Check{
			Name:       name,
			Status:     StatusDegraded,
			Summary:    relativePath + " not readable",
			Detail:     relativePath + " not readable",
			Details:    map[string]string{"error": err.Error()},
			ObservedAt: now,
		}
	}
	text := string(content)
	missing := make([]string, 0)
	for _, value := range required {
		if !strings.Contains(text, value) {
			missing = append(missing, value)
		}
	}
	if len(missing) > 0 {
		return Check{
			Name:       name,
			Status:     StatusDegraded,
			Summary:    "required e2e rule text is missing",
			Detail:     "required e2e rule text is missing",
			Details:    map[string]string{"missing": strings.Join(missing, ",")},
			ObservedAt: now,
		}
	}
	return service.simpleCheck(name, StatusHealthy, summary, now)
}

func (service Service) githubTokenCheck(now time.Time) Check {
	if parseBool(service.env("ODIN_DRY_RUN")) {
		return Check{
			Name:       "github_token",
			Status:     StatusHealthy,
			Summary:    "github token not required for dry-run",
			Detail:     "github token not required for dry-run",
			Details:    map[string]string{"required": "false"},
			ObservedAt: now,
		}
	}
	if service.env("GITHUB_TOKEN") != "" || service.env("GH_TOKEN") != "" {
		return Check{
			Name:       "github_token",
			Status:     StatusHealthy,
			Summary:    "github token present",
			Detail:     "github token present",
			Details:    map[string]string{"required": "true", "present": "true"},
			ObservedAt: now,
		}
	}
	if !strings.Contains(strings.ToLower(service.env("ODIN_PROFILE")), "github") {
		return Check{
			Name:       "github_token",
			Status:     StatusHealthy,
			Summary:    "github token not required for current mode",
			Detail:     "github token not required for current mode",
			Details:    map[string]string{"required": "false", "present": "false"},
			ObservedAt: now,
		}
	}
	return Check{
		Name:       "github_token",
		Status:     StatusDegraded,
		Summary:    "github token missing outside dry-run",
		Detail:     "github token missing outside dry-run",
		Details:    map[string]string{"required": "true", "present": "false"},
		ObservedAt: now,
	}
}

func (service Service) modeCheck(name string, label string, envName string, now time.Time) Check {
	enabled := parseBool(service.env(envName))
	status := "disabled"
	if enabled {
		status = "enabled"
	}
	return Check{
		Name:       name,
		Status:     StatusHealthy,
		Summary:    label + " " + status,
		Detail:     label + " " + status,
		Details:    map[string]string{"status": status},
		ObservedAt: now,
	}
}

func (service Service) killSwitchCheck(now time.Time) Check {
	enabled := parseBool(service.env("ODIN_KILL_SWITCH"))
	status := StatusHealthy
	statusText := "disabled"
	if enabled {
		status = StatusDegraded
		statusText = "enabled"
	}
	return Check{
		Name:       "kill_switch",
		Status:     status,
		Summary:    "kill switch " + statusText,
		Detail:     "kill switch " + statusText,
		Details:    map[string]string{"status": statusText},
		ObservedAt: now,
	}
}

func (service Service) simpleCheck(name string, status Status, summary string, now time.Time) Check {
	return Check{
		Name:       name,
		Status:     status,
		Summary:    summary,
		Detail:     summary,
		ObservedAt: now,
	}
}

func (service Service) env(name string) string {
	if service.Env != nil {
		return service.Env[name]
	}
	return os.Getenv(name)
}

func (service Service) lookPath(name string) (string, error) {
	if service.LookPath != nil {
		return service.LookPath(name)
	}
	return exec.LookPath(name)
}

func (service Service) runCommand(ctx context.Context, name string, args ...string) error {
	if service.RunCommand != nil {
		return service.RunCommand(ctx, name, args...)
	}
	commandCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(commandCtx, name, args...)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	return command.Run()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on", "enabled":
		return true
	default:
		return false
	}
}

func redactTokenLike(value string) string {
	redacted := value
	for _, envName := range []string{"GITHUB_TOKEN", "GH_TOKEN", "API_TOKEN", "ODIN_TRADEBOARD_API_TOKEN"} {
		if token := os.Getenv(envName); token != "" {
			redacted = strings.ReplaceAll(redacted, token, "[REDACTED]")
		}
	}
	return redacted
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
