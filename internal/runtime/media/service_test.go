package media

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	appmedia "odin-os/internal/core/media"
	"odin-os/internal/core/projects"
	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/store/sqlite"
)

func TestMediaServiceOpensIncidentForCriticalSignal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	store := openMediaStore(t, now)
	defer store.Close()

	service := Service{
		Store:  store,
		Config: enabledMediaRuntimeConfig(),
		Checker: stubChecker{checks: []healthsvc.Check{
			{Name: "media.mounts", Status: healthsvc.StatusFailed, Summary: "mount mismatch", ObservedAt: now},
		}},
		Now: func() time.Time { return now },
	}

	result, err := service.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle() error = %v", err)
	}
	if len(result.OpenedIncidentIDs) != 1 {
		t.Fatalf("OpenedIncidentIDs = %+v, want one incident", result.OpenedIncidentIDs)
	}

	incident := getOnlyMediaIncident(t, ctx, store)
	if incident.Status != "open" {
		t.Fatalf("incident status = %q, want open", incident.Status)
	}
	if incident.Severity != "critical" {
		t.Fatalf("incident severity = %q, want critical", incident.Severity)
	}
	details := decodeMediaIncidentDetails(t, incident.DetailsJSON)
	if details.Signal != "media.mounts" {
		t.Fatalf("incident details signal = %q, want media.mounts", details.Signal)
	}
}

func TestMediaServiceResolvesIncidentWhenSignalRecovers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	store := openMediaStore(t, now)
	defer store.Close()

	service := Service{
		Store:  store,
		Config: enabledMediaRuntimeConfig(),
		Checker: stubChecker{checks: []healthsvc.Check{
			{Name: "media.mounts", Status: healthsvc.StatusFailed, Summary: "mount mismatch", ObservedAt: now},
		}},
		Now: func() time.Time { return now },
	}

	firstResult, err := service.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle(first) error = %v", err)
	}
	if len(firstResult.OpenedIncidentIDs) != 1 {
		t.Fatalf("OpenedIncidentIDs = %+v, want one incident", firstResult.OpenedIncidentIDs)
	}

	service.Checker = stubChecker{checks: []healthsvc.Check{
		{Name: "media.mounts", Status: healthsvc.StatusHealthy, Summary: "mount audit passed", ObservedAt: now.Add(5 * time.Minute)},
	}}
	service.Now = func() time.Time { return now.Add(5 * time.Minute) }

	secondResult, err := service.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle(second) error = %v", err)
	}
	if len(secondResult.ResolvedIncidentIDs) != 1 {
		t.Fatalf("ResolvedIncidentIDs = %+v, want one resolved incident", secondResult.ResolvedIncidentIDs)
	}

	incident, err := store.GetIncident(ctx, firstResult.OpenedIncidentIDs[0])
	if err != nil {
		t.Fatalf("GetIncident() error = %v", err)
	}
	if incident.Status != "resolved" {
		t.Fatalf("incident status = %q, want resolved", incident.Status)
	}
}

func TestMediaServiceCreatesWeeklyMaintenanceCandidateWithoutQueueing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	store := openMediaStore(t, now)
	defer store.Close()

	service := Service{
		Store:         store,
		Config:        enabledMediaRuntimeConfig(),
		SystemProject: systemProjectManifest(),
		Checker: stubChecker{checks: []healthsvc.Check{
			{Name: "media.queue", Status: healthsvc.StatusHealthy, Summary: "queue backlog within threshold", ObservedAt: now},
		}},
		Now: func() time.Time { return now },
	}

	result, err := service.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle() error = %v", err)
	}
	if result.CandidateTaskID == nil {
		t.Fatalf("CandidateTaskID = nil, want maintenance candidate task")
	}

	task, err := store.GetTask(ctx, *result.CandidateTaskID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.Status != "blocked" {
		t.Fatalf("task status = %q, want blocked", task.Status)
	}
	if task.RequestedBy != "media-supervisor" {
		t.Fatalf("task requested_by = %q, want media-supervisor", task.RequestedBy)
	}

	if queued := countTasksByStatus(t, ctx, store.DB(), "queued"); queued != 0 {
		t.Fatalf("queued tasks = %d, want 0", queued)
	}

	secondResult, err := service.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle(second) error = %v", err)
	}
	if secondResult.CandidateTaskID != nil {
		t.Fatalf("second CandidateTaskID = %v, want nil due to weekly dedupe", *secondResult.CandidateTaskID)
	}
	if blocked := countTasksByStatus(t, ctx, store.DB(), "blocked"); blocked != 1 {
		t.Fatalf("blocked tasks = %d, want 1", blocked)
	}
}

type stubChecker struct {
	checks []healthsvc.Check
	err    error
}

func (checker stubChecker) Checks(context.Context, healthsvc.Config, time.Time) ([]healthsvc.Check, error) {
	return checker.checks, checker.err
}

type mediaIncidentDetails struct {
	Domain string `json:"domain"`
	Signal string `json:"signal"`
}

func openMediaStore(t *testing.T, now time.Time) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	store.Now = func() time.Time { return now }
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func enabledMediaRuntimeConfig() *appmedia.Config {
	return &appmedia.Config{
		Enabled:           true,
		MaintenanceWindow: "Fri 11:00-13:00",
		Policies: appmedia.Policies{
			NotifyOnly: []string{"media_maintenance_candidate"},
		},
	}
}

func systemProjectManifest() projects.Manifest {
	return projects.Manifest{
		Key:           "odin-core",
		Name:          "Odin Core",
		ProjectClass:  projects.ProjectClassSystem,
		SystemProject: true,
		GitRoot:       "/tmp/odin-os",
		DefaultBranch: "main",
		SourcePath:    "config/projects.yaml",
	}
}

func getOnlyMediaIncident(t *testing.T, ctx context.Context, store *sqlite.Store) sqlite.Incident {
	t.Helper()

	row := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM incidents
		WHERE details_json LIKE '%"domain":"media"%'
	`)

	var count int
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count media incidents: %v", err)
	}
	if count != 1 {
		t.Fatalf("media incident count = %d, want 1", count)
	}

	row = store.DB().QueryRowContext(ctx, `
		SELECT id
		FROM incidents
		WHERE details_json LIKE '%"domain":"media"%'
		ORDER BY id ASC
		LIMIT 1
	`)

	var incidentID int64
	if err := row.Scan(&incidentID); err != nil {
		t.Fatalf("select media incident id: %v", err)
	}

	incident, err := store.GetIncident(ctx, incidentID)
	if err != nil {
		t.Fatalf("GetIncident() error = %v", err)
	}
	return incident
}

func decodeMediaIncidentDetails(t *testing.T, payload string) mediaIncidentDetails {
	t.Helper()

	var details mediaIncidentDetails
	if err := json.Unmarshal([]byte(payload), &details); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return details
}

func countTasksByStatus(t *testing.T, ctx context.Context, db *sql.DB, status string) int {
	t.Helper()

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE status = ?`, status).Scan(&count); err != nil {
		t.Fatalf("count tasks by status %q: %v", status, err)
	}
	return count
}
