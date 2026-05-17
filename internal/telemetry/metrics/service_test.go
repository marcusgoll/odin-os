package metrics

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"odin-os/internal/store/sqlite"
)

func TestRenderExportsMachineParseableMetrics(t *testing.T) {
	t.Parallel()

	exported := Render(Snapshot{
		GeneratedAt:             time.Date(2026, 4, 9, 19, 0, 0, 0, time.UTC),
		ActiveRuns:              3,
		BlockedItems:            2,
		ApprovalsWaiting:        4,
		OpenIncidents:           1,
		EscalatedIncidents:      2,
		ActiveRecoveries:        1,
		QueuedTasks:             5,
		ReviewQueueItems:        6,
		FailedWorkItems:         1,
		RecoveryRecommendations: 1,
		StaleExecutors:          1,
		StaleSources:            2,
		StaleProjections:        1,
		MediaOpenIncidents:      2,
		MediaCandidates:         1,
	})

	for _, want := range []string{
		"odin_active_runs 3",
		"odin_blocked_items 2",
		"odin_approvals_waiting 4",
		"odin_open_incidents 1",
		"odin_escalated_incidents 2",
		"odin_active_recoveries 1",
		"odin_queued_tasks 5",
		"odin_review_queue_items 6",
		"odin_failed_work_items 1",
		"odin_recovery_recommendations 1",
		"odin_stale_executors 1",
		"odin_stale_sources 2",
		"odin_stale_projections 1",
		"odin_media_open_incidents 2",
		"odin_media_candidates 1",
	} {
		assertMetricLine(t, exported, want)
	}
}

func TestRenderExportsOdinOSMetricsWithoutRenamingCompatibilityMetrics(t *testing.T) {
	t.Parallel()

	exported := Render(Snapshot{
		ActiveRuns: 3,
		OS: OSSnapshot{
			HealthScore:       87,
			Status:            "degraded",
			LifecyclePhase:    "run",
			TelemetryStale:    false,
			BackupAgeSeconds:  14400,
			UpdatesPendingSet: true,
			RebootRequired:    false,
			RebootRequiredSet: true,
			CriticalServices: []CriticalServiceMetric{
				{Name: "odin", Up: true},
			},
		},
	})

	for _, want := range []string{
		"odin_active_runs 3",
		"odin_os_health_score 87",
		`odin_os_status{status="degraded"} 1`,
		`odin_os_lifecycle_phase{phase="run"} 1`,
		"odin_os_telemetry_stale 0",
		"odin_os_backup_age_seconds 14400",
		"odin_os_updates_pending_total 0",
		"odin_os_reboot_required 0",
		`odin_os_critical_service_up{service="odin"} 1`,
	} {
		assertMetricLine(t, exported, want)
	}
	if !strings.HasSuffix(exported, "\n") {
		t.Fatalf("Render() missing trailing newline:\n%s", exported)
	}
}

func TestRenderOmitsUnpopulatedOdinOSHostFacts(t *testing.T) {
	t.Parallel()

	exported := Render(Snapshot{
		OS: OSSnapshot{
			HealthScore:    100,
			Status:         "healthy",
			LifecyclePhase: "run",
			TelemetryStale: false,
		},
	})

	for _, want := range []string{
		"odin_os_health_score 100",
		`odin_os_status{status="healthy"} 1`,
		`odin_os_lifecycle_phase{phase="run"} 1`,
		"odin_os_telemetry_stale 0",
	} {
		assertMetricLine(t, exported, want)
	}
	for _, absent := range []string{
		"odin_os_backup_age_seconds 0",
		"odin_os_restore_test_age_seconds 0",
		"odin_os_updates_pending_total 0",
		"odin_os_security_updates_pending_total 0",
		"odin_os_reboot_required 0",
		"odin_os_systemd_failed_units_total 0",
	} {
		assertNoMetricLine(t, exported, absent)
	}
}

func TestMediaMetricsCollectAndRenderExposeMediaCounters(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	store.Now = func() time.Time { return now }
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "odin-core",
		GitRoot:       "/tmp/odin-os",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	if _, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "media-maintenance-2026-week16",
		Title:       "Media maintenance candidate",
		Status:      "blocked",
		Scope:       "odin-core",
		RequestedBy: "media-supervisor",
	}); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	if _, err := store.OpenIncident(ctx, sqlite.OpenIncidentParams{
		Severity:    "critical",
		Status:      "open",
		Summary:     "mount mismatch detected",
		DetailsJSON: `{"domain":"media","signal":"media.mounts"}`,
	}); err != nil {
		t.Fatalf("OpenIncident() error = %v", err)
	}

	snapshot, err := Service{
		DB:  store.DB(),
		Now: func() time.Time { return now },
	}.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if snapshot.MediaOpenIncidents != 1 {
		t.Fatalf("MediaOpenIncidents = %d, want 1", snapshot.MediaOpenIncidents)
	}
	if snapshot.MediaCandidates != 1 {
		t.Fatalf("MediaCandidates = %d, want 1", snapshot.MediaCandidates)
	}

	exported := Render(snapshot)
	if !strings.Contains(exported, "odin_media_open_incidents 1") {
		t.Fatalf("Render() = %q, want media incident count", exported)
	}
	if !strings.Contains(exported, "odin_media_candidates 1") {
		t.Fatalf("Render() = %q, want media candidate count", exported)
	}
}

func TestServiceCollectReflectsRuntimeConditions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Now().UTC()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
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
	queuedTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "queued-task",
		Title:       "Queued task",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(queued) error = %v", err)
	}
	_ = queuedTask

	for index := 0; index < 3; index++ {
		delayedTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
			ProjectID:   project.ID,
			Key:         "delayed-task-" + string(rune('a'+index)),
			Title:       "Delayed task",
			Status:      "queued",
			Scope:       "project",
			RequestedBy: "operator",
		})
		if err != nil {
			t.Fatalf("CreateTask(delayed %d) error = %v", index, err)
		}
		if _, err := store.RequeueTaskAt(ctx, sqlite.RequeueTaskAtParams{
			TaskID:         delayedTask.ID,
			NextEligibleAt: now.Add(500 * time.Millisecond),
		}); err != nil {
			t.Fatalf("RequeueTaskAt(delayed %d) error = %v", index, err)
		}
	}
	runningTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "running-task",
		Title:       "Running task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(running) error = %v", err)
	}
	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   runningTask.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	if _, err := store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      runningTask.ID,
		RunID:       &run.ID,
		Status:      "pending",
		RequestedBy: "system",
	}); err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}
	incident, err := store.OpenIncident(ctx, sqlite.OpenIncidentParams{
		RunID:       &run.ID,
		Severity:    "warning",
		Status:      "open",
		Summary:     "executor degraded",
		DetailsJSON: `{"stage":"build"}`,
	})
	if err != nil {
		t.Fatalf("OpenIncident() error = %v", err)
	}
	if _, err := store.StartRecovery(ctx, sqlite.StartRecoveryParams{
		IncidentID:  &incident.ID,
		RunID:       &run.ID,
		Status:      "running",
		Strategy:    "retry-once",
		DetailsJSON: `{"attempt":1}`,
	}); err != nil {
		t.Fatalf("StartRecovery() error = %v", err)
	}
	if _, err := store.RecordExecutorHealth(ctx, sqlite.RecordExecutorHealthParams{
		Executor:    "codex",
		Status:      "healthy",
		LatencyMS:   42,
		DetailsJSON: `{"mode":"local"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth() error = %v", err)
	}
	if _, err := store.RecordRegistryVersion(ctx, sqlite.RecordRegistryVersionParams{
		Source:      "registry",
		VersionHash: "fresh",
		Notes:       "fresh metrics sample",
	}); err != nil {
		t.Fatalf("RecordRegistryVersion() error = %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE executor_health
		SET checked_at = ?
	`, now.Add(-2*time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("force stale executor health error = %v", err)
	}
	if _, err := store.RecordProjectionFreshness(ctx, sqlite.RecordProjectionFreshnessParams{
		Surface:     "doctor",
		Status:      "healthy",
		DetailsJSON: `{"source":"runtime"}`,
	}); err != nil {
		t.Fatalf("RecordProjectionFreshness() error = %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE projection_freshness
		SET refreshed_at = ?, updated_at = ?
		WHERE surface = 'doctor'
	`, now.Add(-2*time.Hour).Format(time.RFC3339Nano), now.Add(-2*time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("force stale projection freshness error = %v", err)
	}

	service := Service{
		DB: store.DB(),
		Config: Config{
			ExecutorFreshnessTTL:   30 * time.Minute,
			ProjectionFreshnessTTL: 30 * time.Minute,
			SourceFreshnessTTL:     30 * time.Minute,
		},
		Now: func() time.Time { return now },
	}

	snapshot, err := service.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if snapshot.ActiveRuns != 1 || snapshot.QueuedTasks != 1 || snapshot.ApprovalsWaiting != 1 {
		t.Fatalf("snapshot counts = %+v", snapshot)
	}
	if snapshot.OpenIncidents != 1 || snapshot.ActiveRecoveries != 1 {
		t.Fatalf("incident or recovery counts = %+v", snapshot)
	}
	if snapshot.StaleExecutors != 1 || snapshot.StaleProjections != 1 {
		t.Fatalf("staleness counts = %+v", snapshot)
	}
	if snapshot.OS.HealthScore != 80 || snapshot.OS.Status != "unknown" || snapshot.OS.LifecyclePhase != "run" {
		t.Fatalf("OS snapshot = %+v, want unknown run health score 80", snapshot.OS)
	}
	if !snapshot.OS.TelemetryStale {
		t.Fatalf("OS telemetry stale = false, want true")
	}
	if snapshot.BlockedItems == 0 {
		t.Fatalf("blocked items = %d, want > 0", snapshot.BlockedItems)
	}
}

func TestServiceCollectKeepsFreshIncidentRecoveryVisibleWithoutDegradingOdinOS(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Now().UTC()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	seedFreshTelemetry(t, ctx, store, now)

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
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
	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "running-task",
		Title:       "Running task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	incident, err := store.OpenIncident(ctx, sqlite.OpenIncidentParams{
		RunID:       &run.ID,
		Severity:    "warning",
		Status:      "open",
		Summary:     "operator intervention required",
		DetailsJSON: `{"stage":"review"}`,
	})
	if err != nil {
		t.Fatalf("OpenIncident() error = %v", err)
	}
	if _, err := store.StartRecovery(ctx, sqlite.StartRecoveryParams{
		IncidentID:  &incident.ID,
		RunID:       &run.ID,
		Status:      "running",
		Strategy:    "manual-review",
		DetailsJSON: `{"attempt":1}`,
	}); err != nil {
		t.Fatalf("StartRecovery() error = %v", err)
	}

	snapshot, err := Service{
		DB: store.DB(),
		Config: Config{
			ExecutorFreshnessTTL:   30 * time.Minute,
			ProjectionFreshnessTTL: 30 * time.Minute,
			SourceFreshnessTTL:     30 * time.Minute,
		},
		Now: func() time.Time { return now },
	}.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if snapshot.OpenIncidents == 0 || snapshot.ActiveRecoveries == 0 {
		t.Fatalf("snapshot incidents=%d recoveries=%d, want incident and recovery counts preserved", snapshot.OpenIncidents, snapshot.ActiveRecoveries)
	}
	if snapshot.OS.HealthScore != 100 || snapshot.OS.Status != "healthy" || snapshot.OS.LifecyclePhase != "run" {
		t.Fatalf("OS snapshot = %+v, want healthy run health score 100", snapshot.OS)
	}
	if snapshot.OS.TelemetryStale {
		t.Fatalf("OS telemetry stale = true, want false")
	}
}

func TestServiceCollectIgnoresResolvedIncidentsForOdinOSHealth(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Now().UTC()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	seedFreshTelemetry(t, ctx, store, now)

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
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
	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "resolved-task",
		Title:       "Resolved task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	incident, err := store.OpenIncident(ctx, sqlite.OpenIncidentParams{
		RunID:       &run.ID,
		Severity:    "warning",
		Status:      "open",
		Summary:     "resolved before metrics collection",
		DetailsJSON: `{"stage":"review"}`,
	})
	if err != nil {
		t.Fatalf("OpenIncident() error = %v", err)
	}
	if _, err := store.UpdateIncidentStatus(ctx, sqlite.UpdateIncidentStatusParams{
		IncidentID:  incident.ID,
		Status:      "resolved",
		Reason:      "operator resolved",
		DetailsJSON: `{"status":"resolved"}`,
	}); err != nil {
		t.Fatalf("UpdateIncidentStatus() error = %v", err)
	}

	snapshot, err := Service{
		DB: store.DB(),
		Config: Config{
			ExecutorFreshnessTTL:   30 * time.Minute,
			ProjectionFreshnessTTL: 30 * time.Minute,
			SourceFreshnessTTL:     30 * time.Minute,
		},
		Now: func() time.Time { return now },
	}.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if snapshot.OpenIncidents != 1 {
		t.Fatalf("compatibility incident count = %d, want 1", snapshot.OpenIncidents)
	}
	if snapshot.OS.HealthScore != 100 || snapshot.OS.Status != "healthy" {
		t.Fatalf("OS snapshot = %+v, want healthy from fresh telemetry with resolved incident", snapshot.OS)
	}
	if snapshot.OS.TelemetryStale {
		t.Fatalf("OS telemetry stale = true, want false")
	}
}

func TestServiceCollectSetsHealthyOdinOSDefaults(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Now().UTC()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	seedFreshTelemetry(t, ctx, store, now)

	snapshot, err := Service{
		DB: store.DB(),
		Config: Config{
			ExecutorFreshnessTTL:   30 * time.Minute,
			ProjectionFreshnessTTL: 30 * time.Minute,
			SourceFreshnessTTL:     30 * time.Minute,
		},
		Now: func() time.Time { return now },
	}.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if snapshot.OS.HealthScore != 100 || snapshot.OS.Status != "healthy" || snapshot.OS.LifecyclePhase != "run" {
		t.Fatalf("OS snapshot = %+v, want healthy run health score 100", snapshot.OS)
	}
	if snapshot.OS.TelemetryStale {
		t.Fatalf("OS telemetry stale = true, want false")
	}
}

func TestServiceCollectDoesNotTreatFreshOptionalUnavailableExecutorsAsStaleOdinOSTelemetry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Now().UTC()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	seedFreshTelemetry(t, ctx, store, now)
	if _, err := store.RecordExecutorHealth(ctx, sqlite.RecordExecutorHealthParams{
		Executor:    "optional_api",
		Status:      "unknown",
		LatencyMS:   0,
		DetailsJSON: `{"required":false}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth(optional) error = %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE executor_health
		SET checked_at = ?
		WHERE executor = 'optional_api'
	`, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("force fresh optional executor health error = %v", err)
	}

	snapshot, err := Service{
		DB: store.DB(),
		Config: Config{
			ExecutorFreshnessTTL:   30 * time.Minute,
			ProjectionFreshnessTTL: 30 * time.Minute,
			SourceFreshnessTTL:     30 * time.Minute,
		},
		Now: func() time.Time { return now },
	}.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if snapshot.StaleExecutors != 1 {
		t.Fatalf("compatibility stale executor count = %d, want 1 for non-healthy optional executor", snapshot.StaleExecutors)
	}
	if snapshot.OS.TelemetryStale || snapshot.OS.Status != "healthy" || snapshot.OS.HealthScore != 100 {
		t.Fatalf("OS snapshot = %+v, want healthy telemetry despite fresh optional unavailable executor", snapshot.OS)
	}
}

func assertMetricLine(t *testing.T, exported string, want string) {
	t.Helper()

	for _, line := range strings.Split(strings.TrimSuffix(exported, "\n"), "\n") {
		if line == want {
			return
		}
	}
	t.Fatalf("Render() missing line %q in:\n%s", want, exported)
}

func assertNoMetricLine(t *testing.T, exported string, absent string) {
	t.Helper()

	for _, line := range strings.Split(strings.TrimSuffix(exported, "\n"), "\n") {
		if line == absent {
			t.Fatalf("Render() unexpectedly included line %q in:\n%s", absent, exported)
		}
	}
}

func TestServiceCollectIgnoresHistoricalExecutorsWhenScopeIsExplicitlyEmpty(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Now().UTC()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	if _, err := store.RecordExecutorHealth(ctx, sqlite.RecordExecutorHealthParams{
		Executor:    "codex_headless",
		Status:      "unavailable",
		LatencyMS:   10,
		DetailsJSON: `{"source":"test"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth() error = %v", err)
	}

	snapshot, err := Service{
		DB:           store.DB(),
		Config:       Config{ExecutorFreshnessTTL: time.Hour, SourceFreshnessTTL: time.Hour, ProjectionFreshnessTTL: time.Hour},
		Now:          func() time.Time { return now },
		ExecutorKeys: []string{},
	}.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if snapshot.StaleExecutors != 0 {
		t.Fatalf("StaleExecutors = %d, want 0 when executor scope is explicitly empty", snapshot.StaleExecutors)
	}
}

func TestServiceCollectTreatsMissingTelemetryAsUnknown(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Now().UTC()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	snapshot, err := Service{
		DB:  store.DB(),
		Now: func() time.Time { return now },
	}.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if snapshot.OS.Status != "unknown" || snapshot.OS.HealthScore != 80 {
		t.Fatalf("OS snapshot = %+v, want unknown health score 80", snapshot.OS)
	}
	if !snapshot.OS.TelemetryStale {
		t.Fatalf("OS telemetry stale = false, want true for missing telemetry")
	}
}

func seedFreshTelemetry(t *testing.T, ctx context.Context, store *sqlite.Store, now time.Time) {
	t.Helper()

	if _, err := store.RecordRegistryVersion(ctx, sqlite.RecordRegistryVersionParams{
		Source:      "registry",
		VersionHash: "fresh",
		Notes:       "fresh metrics sample",
	}); err != nil {
		t.Fatalf("RecordRegistryVersion() error = %v", err)
	}
	if _, err := store.RecordExecutorHealth(ctx, sqlite.RecordExecutorHealthParams{
		Executor:    "codex",
		Status:      "healthy",
		LatencyMS:   10,
		DetailsJSON: `{"status":"healthy"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth() error = %v", err)
	}
	if _, err := store.RecordProjectionFreshness(ctx, sqlite.RecordProjectionFreshnessParams{
		Surface:     "metrics",
		Status:      "healthy",
		DetailsJSON: `{"source":"test"}`,
	}); err != nil {
		t.Fatalf("RecordProjectionFreshness() error = %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE registry_versions
		SET compiled_at = ?
	`, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("force fresh registry version error = %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE executor_health
		SET checked_at = ?
	`, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("force fresh executor health error = %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE projection_freshness
		SET refreshed_at = ?, updated_at = ?
		WHERE surface = 'metrics'
	`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("force fresh projection freshness error = %v", err)
	}
}
