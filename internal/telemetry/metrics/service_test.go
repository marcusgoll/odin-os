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
		GeneratedAt:        time.Date(2026, 4, 9, 19, 0, 0, 0, time.UTC),
		ActiveRuns:         3,
		BlockedItems:       2,
		ApprovalsWaiting:   4,
		OpenIncidents:      1,
		EscalatedIncidents: 2,
		ActiveRecoveries:   1,
		QueuedTasks:        5,
		StaleExecutors:     1,
		StaleSources:       2,
		StaleProjections:   1,
		MediaOpenIncidents: 2,
		MediaCandidates:    1,
	})

	for _, want := range []string{
		"odin_active_runs 3",
		"odin_blocked_items 2",
		"odin_approvals_waiting 4",
		"odin_open_incidents 1",
		"odin_escalated_incidents 2",
		"odin_active_recoveries 1",
		"odin_queued_tasks 5",
		"odin_stale_executors 1",
		"odin_stale_sources 2",
		"odin_stale_projections 1",
		"odin_media_open_incidents 2",
		"odin_media_candidates 1",
	} {
		if !strings.Contains(exported, want) {
			t.Fatalf("Render() = %q, want substring %q", exported, want)
		}
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
	if snapshot.BlockedItems == 0 {
		t.Fatalf("blocked items = %d, want > 0", snapshot.BlockedItems)
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
