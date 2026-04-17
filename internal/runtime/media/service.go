package media

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	coremedia "odin-os/internal/core/media"
	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/store/sqlite"
)

const maintenanceTaskPrefix = "media-maintenance-"

type incidentDetails struct {
	Domain         string `json:"domain"`
	Signal         string `json:"signal"`
	Status         string `json:"status,omitempty"`
	Summary        string `json:"summary,omitempty"`
	LastObservedAt string `json:"last_observed_at,omitempty"`
	ResolvedAt     string `json:"resolved_at,omitempty"`
	ResolvedStatus string `json:"resolved_status,omitempty"`
}

func (service Service) RunCycle(ctx context.Context) (CycleResult, error) {
	if service.Store == nil {
		return CycleResult{}, fmt.Errorf("media supervisor store is required")
	}
	if service.Config == nil || !service.Config.Enabled {
		return CycleResult{}, nil
	}
	if service.Checker == nil {
		return CycleResult{}, fmt.Errorf("media supervisor checker is required")
	}

	now := time.Now().UTC()
	if service.Now != nil {
		now = service.Now().UTC()
	}

	checks, err := service.Checker.Checks(ctx, healthsvc.DefaultConfig(), now)
	if err != nil {
		return CycleResult{}, err
	}

	result := CycleResult{Checks: checks}
	observed := make(map[string]healthsvc.Check, len(checks))
	critical := make(map[string]healthsvc.Check, len(checks))
	for _, check := range checks {
		observed[check.Name] = check
		if check.Status == healthsvc.StatusFailed {
			critical[check.Name] = check
		}
	}

	activeIncidents, err := service.listActiveMediaIncidents(ctx)
	if err != nil {
		return CycleResult{}, err
	}

	for signal, check := range critical {
		if _, exists := activeIncidents[signal]; exists {
			continue
		}

		incident, err := service.Store.OpenIncident(ctx, sqlite.OpenIncidentParams{
			Severity:    "critical",
			Status:      "open",
			Summary:     check.Summary,
			DetailsJSON: marshalIncidentDetails(incidentDetails{Domain: "media", Signal: signal, Status: string(check.Status), Summary: check.Summary, LastObservedAt: now.Format(time.RFC3339Nano)}),
		})
		if err != nil {
			return result, err
		}
		result.OpenedIncidentIDs = append(result.OpenedIncidentIDs, incident.ID)
	}

	for signal, incident := range activeIncidents {
		check, seen := observed[signal]
		if !seen || check.Status == healthsvc.StatusFailed {
			continue
		}

		updated, err := service.Store.UpdateIncidentStatus(ctx, sqlite.UpdateIncidentStatusParams{
			IncidentID:  incident.ID,
			Status:      "resolved",
			Reason:      "media signal recovered",
			DetailsJSON: marshalIncidentDetails(incidentDetails{Domain: "media", Signal: signal, Status: incident.Status, Summary: incident.Summary, LastObservedAt: now.Format(time.RFC3339Nano), ResolvedAt: now.Format(time.RFC3339Nano), ResolvedStatus: string(check.Status)}),
		})
		if err != nil {
			return result, err
		}
		result.ResolvedIncidentIDs = append(result.ResolvedIncidentIDs, updated.ID)
	}

	candidateTaskID, err := service.recordMaintenanceCandidate(ctx, now, len(critical) == 0)
	if err != nil {
		return result, err
	}
	result.CandidateTaskID = candidateTaskID

	return result, nil
}

func (service Service) Maintenance() MaintenanceService {
	return MaintenanceService{
		Store:       service.Store,
		Config:      service.Config,
		RuntimeRoot: service.RuntimeRoot,
		Now:         service.Now,
	}
}

func (service Service) recordMaintenanceCandidate(ctx context.Context, now time.Time, healthy bool) (*int64, error) {
	if !healthy {
		return nil, nil
	}
	if (coremedia.Service{}).ClassifyAction(*service.Config, "media_maintenance_candidate") != coremedia.AutomationClassNotifyOnly {
		return nil, nil
	}
	if !withinMaintenanceWindow(service.Config.MaintenanceWindow, now) {
		return nil, nil
	}
	if strings.TrimSpace(service.SystemProject.Key) == "" {
		return nil, nil
	}

	project, err := service.ensureSystemProject(ctx)
	if err != nil {
		return nil, err
	}

	key := maintenanceCandidateKey(now)
	if existingID, ok, err := findTaskByProjectAndKey(ctx, service.Store.DB(), project.ID, key); err != nil {
		return nil, err
	} else if ok {
		return nil, nil
	} else {
		year, week := isoWeek(now)
		task, err := service.Store.CreateTask(ctx, sqlite.CreateTaskParams{
			ProjectID:   project.ID,
			Key:         key,
			Title:       fmt.Sprintf("Media maintenance candidate for %d-W%02d", year, week),
			Status:      "blocked",
			Scope:       project.Scope,
			RequestedBy: "media-supervisor",
		})
		if err != nil {
			return nil, err
		}
		_ = existingID
		return &task.ID, nil
	}
}

func (service Service) ensureSystemProject(ctx context.Context) (sqlite.Project, error) {
	project, err := service.Store.GetProjectByKey(ctx, service.SystemProject.Key)
	if err == nil {
		return project, nil
	}
	if err != sql.ErrNoRows {
		return sqlite.Project{}, err
	}

	scope := "project"
	if service.SystemProject.SystemProject {
		scope = "odin-core"
	}
	return service.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           service.SystemProject.Key,
		Name:          service.SystemProject.Name,
		Scope:         scope,
		GitRoot:       service.SystemProject.GitRoot,
		DefaultBranch: service.SystemProject.DefaultBranch,
		GitHubRepo:    service.SystemProject.GitHub.Repo,
		ManifestPath:  service.SystemProject.SourcePath,
	})
}

func (service Service) listActiveMediaIncidents(ctx context.Context) (map[string]sqlite.Incident, error) {
	rows, err := service.Store.DB().QueryContext(ctx, `
		SELECT id, run_id, severity, status, summary, details_json, opened_at, updated_at
		FROM incidents
		WHERE status IN ('open', 'escalated')
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	incidents := make(map[string]sqlite.Incident)
	for rows.Next() {
		incident, err := scanIncidentRow(rows)
		if err != nil {
			return nil, err
		}
		var details incidentDetails
		if err := json.Unmarshal([]byte(incident.DetailsJSON), &details); err != nil {
			continue
		}
		if details.Domain != "media" || strings.TrimSpace(details.Signal) == "" {
			continue
		}
		incidents[details.Signal] = incident
	}
	return incidents, rows.Err()
}

func maintenanceCandidateKey(now time.Time) string {
	year, week := isoWeek(now)
	return fmt.Sprintf("%s%d-week%02d", maintenanceTaskPrefix, year, week)
}

func isoWeek(now time.Time) (int, int) {
	return now.ISOWeek()
}

func withinMaintenanceWindow(window string, now time.Time) bool {
	parts := strings.Fields(strings.TrimSpace(window))
	if len(parts) != 2 {
		return false
	}

	if weekdayFromToken(parts[0]) != now.Weekday() {
		return false
	}

	rangeParts := strings.SplitN(parts[1], "-", 2)
	if len(rangeParts) != 2 {
		return false
	}

	start, err := time.Parse("15:04", rangeParts[0])
	if err != nil {
		return false
	}
	end, err := time.Parse("15:04", rangeParts[1])
	if err != nil {
		return false
	}

	currentMinutes := now.Hour()*60 + now.Minute()
	startMinutes := start.Hour()*60 + start.Minute()
	endMinutes := end.Hour()*60 + end.Minute()
	return currentMinutes >= startMinutes && currentMinutes < endMinutes
}

func weekdayFromToken(token string) time.Weekday {
	switch strings.ToLower(strings.TrimSpace(token)) {
	case "sun":
		return time.Sunday
	case "mon":
		return time.Monday
	case "tue":
		return time.Tuesday
	case "wed":
		return time.Wednesday
	case "thu":
		return time.Thursday
	case "fri":
		return time.Friday
	case "sat":
		return time.Saturday
	default:
		return time.Weekday(-1)
	}
}

func findTaskByProjectAndKey(ctx context.Context, db *sql.DB, projectID int64, key string) (int64, bool, error) {
	row := db.QueryRowContext(ctx, `SELECT id FROM tasks WHERE project_id = ? AND key = ?`, projectID, key)
	var taskID int64
	if err := row.Scan(&taskID); err != nil {
		if err == sql.ErrNoRows {
			return 0, false, nil
		}
		return 0, false, err
	}
	return taskID, true, nil
}

func marshalIncidentDetails(details incidentDetails) string {
	payload, err := json.Marshal(details)
	if err != nil {
		return `{"domain":"media","signal":"unknown"}`
	}
	return string(payload)
}

func scanIncidentRow(scanner interface{ Scan(...any) error }) (sqlite.Incident, error) {
	var incident sqlite.Incident
	var runID sql.NullInt64
	var openedAt string
	var updatedAt string
	if err := scanner.Scan(
		&incident.ID,
		&runID,
		&incident.Severity,
		&incident.Status,
		&incident.Summary,
		&incident.DetailsJSON,
		&openedAt,
		&updatedAt,
	); err != nil {
		return sqlite.Incident{}, err
	}
	if runID.Valid {
		incident.RunID = &runID.Int64
	}
	parsedOpenedAt, err := time.Parse(time.RFC3339Nano, openedAt)
	if err != nil {
		return sqlite.Incident{}, err
	}
	parsedUpdatedAt, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return sqlite.Incident{}, err
	}
	incident.OpenedAt = parsedOpenedAt
	incident.UpdatedAt = parsedUpdatedAt
	return incident, nil
}
