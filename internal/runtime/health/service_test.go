package health

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"odin-os/internal/store/sqlite"
)

func TestDoctorReportIsHealthyWhenChecksAreFresh(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	store := openStore(t)
	defer store.Close()

	project, err := store.CreateProject(context.Background(), sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(context.Background(), sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "task-1",
		Title:       "Task 1",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	if _, err := store.StartRun(context.Background(), sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	}); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	if _, err := store.RecordExecutorHealth(context.Background(), sqlite.RecordExecutorHealthParams{
		Executor:    "codex_headless",
		Status:      "healthy",
		LatencyMS:   42,
		DetailsJSON: `{"mode":"local"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth() error = %v", err)
	}

	if _, err := store.RecordRegistryVersion(context.Background(), sqlite.RecordRegistryVersionParams{
		Source:      "registry",
		VersionHash: "abc123",
		Notes:       "fresh compile",
	}); err != nil {
		t.Fatalf("RecordRegistryVersion() error = %v", err)
	}

	if _, err := store.RecordProjectionFreshness(context.Background(), sqlite.RecordProjectionFreshnessParams{
		Surface:     "doctor",
		Status:      "healthy",
		DetailsJSON: `{"source":"runtime"}`,
	}); err != nil {
		t.Fatalf("RecordProjectionFreshness() error = %v", err)
	}

	service := Service{
		DB: store.DB(),
		Config: Config{
			QueuePressureThreshold: 5,
			ExecutorFreshnessTTL:   time.Hour,
			SourceFreshnessTTL:     time.Hour,
			ProjectionFreshnessTTL: time.Hour,
		},
		Now: func() time.Time { return now },
	}

	report, err := service.Doctor(context.Background(), true)
	if err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}
	if report.Status != StatusHealthy {
		t.Fatalf("Status = %q, want %q", report.Status, StatusHealthy)
	}
	if len(report.Checks) != 6 {
		t.Fatalf("Checks len = %d, want 6", len(report.Checks))
	}
}

func TestDoctorReportUsesExpectedExecutorsInsteadOfLatestSample(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	store := openStore(t)
	defer store.Close()

	if _, err := store.RecordExecutorHealth(context.Background(), sqlite.RecordExecutorHealthParams{
		Executor:    "codex_headless",
		Status:      "unavailable",
		LatencyMS:   0,
		DetailsJSON: `{"source":"bootstrap"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth(codex_headless) error = %v", err)
	}
	if _, err := store.DB().ExecContext(context.Background(), `
		UPDATE executor_health
		SET checked_at = ?
		WHERE executor = 'codex_headless'
	`, now.Add(-10*time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("force executor freshness error = %v", err)
	}

	if _, err := store.RecordExecutorHealth(context.Background(), sqlite.RecordExecutorHealthParams{
		Executor:    "openai_api",
		Status:      "healthy",
		LatencyMS:   5,
		DetailsJSON: `{"source":"noise"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth(openai_api) error = %v", err)
	}

	if _, err := store.RecordRegistryVersion(context.Background(), sqlite.RecordRegistryVersionParams{
		Source:      "registry",
		VersionHash: "abc123",
		Notes:       "fresh compile",
	}); err != nil {
		t.Fatalf("RecordRegistryVersion() error = %v", err)
	}

	if _, err := store.RecordProjectionFreshness(context.Background(), sqlite.RecordProjectionFreshnessParams{
		Surface:     "doctor",
		Status:      "healthy",
		DetailsJSON: `{"source":"runtime"}`,
	}); err != nil {
		t.Fatalf("RecordProjectionFreshness() error = %v", err)
	}

	service := Service{
		DB: store.DB(),
		Config: Config{
			QueuePressureThreshold: 5,
			ExecutorFreshnessTTL:   time.Hour,
			SourceFreshnessTTL:     time.Hour,
			ProjectionFreshnessTTL: time.Hour,
		},
		Now:               func() time.Time { return now },
		ExpectedExecutors: []string{"codex_headless"},
	}

	report, err := service.Doctor(context.Background(), true)
	if err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}
	if report.Status != StatusDegraded {
		t.Fatalf("Status = %q, want %q", report.Status, StatusDegraded)
	}
}

func TestDoctorReportIsDegradedWhenQueueAndFreshnessAreStale(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	store := openStore(t)
	defer store.Close()

	project, err := store.CreateProject(context.Background(), sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	for index := 0; index < 4; index++ {
		if _, err := store.CreateTask(context.Background(), sqlite.CreateTaskParams{
			ProjectID:   project.ID,
			Key:         "queued-task-" + string(rune('a'+index)),
			Title:       "Queued task",
			Status:      "queued",
			Scope:       "project",
			RequestedBy: "operator",
		}); err != nil {
			t.Fatalf("CreateTask(%d) error = %v", index, err)
		}
	}

	if _, err := store.RecordExecutorHealth(context.Background(), sqlite.RecordExecutorHealthParams{
		Executor:    "codex_headless",
		Status:      "healthy",
		LatencyMS:   42,
		DetailsJSON: `{"mode":"local"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth() error = %v", err)
	}
	if _, err := store.DB().ExecContext(context.Background(), `
		UPDATE executor_health
		SET checked_at = ?
	`, now.Add(-2*time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatalf("force stale executor health error = %v", err)
	}

	if _, err := store.RecordRegistryVersion(context.Background(), sqlite.RecordRegistryVersionParams{
		Source:      "registry",
		VersionHash: "abc123",
		Notes:       "stale compile",
	}); err != nil {
		t.Fatalf("RecordRegistryVersion() error = %v", err)
	}
	if _, err := store.DB().ExecContext(context.Background(), `
		UPDATE registry_versions
		SET compiled_at = ?
	`, now.Add(-2*time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatalf("force stale registry version error = %v", err)
	}

	if _, err := store.RecordProjectionFreshness(context.Background(), sqlite.RecordProjectionFreshnessParams{
		Surface:     "doctor",
		Status:      "healthy",
		DetailsJSON: `{"source":"runtime"}`,
	}); err != nil {
		t.Fatalf("RecordProjectionFreshness() error = %v", err)
	}
	if _, err := store.DB().ExecContext(context.Background(), `
		UPDATE projection_freshness
		SET refreshed_at = ?, updated_at = ?
		WHERE surface = 'doctor'
	`, now.Add(-2*time.Hour).Format(time.RFC3339), now.Add(-2*time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatalf("force stale projection freshness error = %v", err)
	}

	service := Service{
		DB: store.DB(),
		Config: Config{
			QueuePressureThreshold: 3,
			ExecutorFreshnessTTL:   30 * time.Minute,
			SourceFreshnessTTL:     30 * time.Minute,
			ProjectionFreshnessTTL: 30 * time.Minute,
		},
		Now: func() time.Time { return now },
	}

	report, err := service.Doctor(context.Background(), false)
	if err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}
	if report.Status != StatusDegraded {
		t.Fatalf("Status = %q, want %q", report.Status, StatusDegraded)
	}
}

func TestDoctorReportIsFailedWhenDatabaseIsUnavailable(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	service := Service{
		DB:     store.DB(),
		Config: DefaultConfig(),
	}

	report, err := service.Doctor(context.Background(), true)
	if err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}
	if report.Status != StatusFailed {
		t.Fatalf("Status = %q, want %q", report.Status, StatusFailed)
	}
}

func openStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}
