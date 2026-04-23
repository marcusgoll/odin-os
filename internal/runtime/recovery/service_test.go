package recovery_test

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"odin-os/internal/executors/router"
	"odin-os/internal/runtime/health"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/runtime/recovery"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/telemetry/logs"
	"odin-os/internal/telemetry/metrics"
)

func TestServiceRefreshesProjectionFreshnessAndLogsAction(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 4, 9, 17, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	store.Now = func() time.Time { return now }

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	if _, err := store.RecordProjectionFreshness(ctx, sqlite.RecordProjectionFreshnessParams{
		Surface:     "doctor",
		Status:      "stale",
		DetailsJSON: `{"source":"test"}`,
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

	var logBuffer bytes.Buffer
	service := recovery.Service{
		Store:           store,
		RegistryRoot:    filepath.Join("/home/orchestrator/.config/superpowers/worktrees/odin-os/phase-11-self-heal", "registry"),
		ExecutorCatalog: router.DefaultCatalog(),
		HealthConfig: health.Config{
			ExecutorFreshnessTTL:   30 * time.Minute,
			ProjectionFreshnessTTL: 30 * time.Minute,
			SourceFreshnessTTL:     30 * time.Minute,
		},
		Logger: &logs.Logger{
			Writer: &logBuffer,
			Now:    func() time.Time { return now },
		},
		Now: func() time.Time { return now },
	}

	result, err := service.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle() error = %v", err)
	}
	if len(result.Outcomes) == 0 {
		t.Fatalf("RunCycle() outcomes = 0, want at least one recovery outcome")
	}

	record, err := store.GetProjectionFreshness(ctx, "doctor")
	if err != nil {
		t.Fatalf("GetProjectionFreshness() error = %v", err)
	}
	if !record.RefreshedAt.Equal(now) {
		t.Fatalf("doctor refreshed_at = %v, want %v", record.RefreshedAt, now)
	}

	logOutput := logBuffer.String()
	if !strings.Contains(logOutput, "\"component\":\"self_heal\"") || !strings.Contains(logOutput, "\"fault_key\":\"projection_stale\"") {
		t.Fatalf("self-heal logs = %q, want component and fault key", logOutput)
	}
}

func TestServiceEscalatesRepeatedRunFailuresIntoProjectionsAndMetrics(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 4, 9, 18, 0, 0, 0, time.UTC)

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
		Key:         "alpha-task",
		Title:       "Alpha task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	runOne, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(first) error = %v", err)
	}
	if _, err := store.FinishRun(ctx, sqlite.FinishRunParams{
		RunID:   runOne.ID,
		Status:  "failed",
		Summary: "first failed run",
	}); err != nil {
		t.Fatalf("FinishRun(first) error = %v", err)
	}

	runTwo, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  2,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(second) error = %v", err)
	}
	if _, err := store.FinishRun(ctx, sqlite.FinishRunParams{
		RunID:   runTwo.ID,
		Status:  "failed",
		Summary: "second failed run",
	}); err != nil {
		t.Fatalf("FinishRun(second) error = %v", err)
	}

	if _, err := store.RecordExecutorHealth(ctx, sqlite.RecordExecutorHealthParams{
		Executor:    "codex_headless",
		Status:      "healthy",
		LatencyMS:   10,
		DetailsJSON: `{"source":"test"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth() error = %v", err)
	}
	if _, err := store.RecordProjectionFreshness(ctx, sqlite.RecordProjectionFreshnessParams{
		Surface:     "doctor",
		Status:      "healthy",
		DetailsJSON: `{"source":"test"}`,
	}); err != nil {
		t.Fatalf("RecordProjectionFreshness() error = %v", err)
	}
	if _, err := store.RecordRegistryVersion(ctx, sqlite.RecordRegistryVersionParams{
		Source:      "registry",
		VersionHash: "abc123",
		Notes:       "fresh",
	}); err != nil {
		t.Fatalf("RecordRegistryVersion() error = %v", err)
	}

	service := recovery.Service{
		Store:           store,
		RegistryRoot:    filepath.Join("/home/orchestrator/.config/superpowers/worktrees/odin-os/phase-11-self-heal", "registry"),
		ExecutorCatalog: router.DefaultCatalog(),
		HealthConfig: health.Config{
			QueuePressureThreshold: 10,
			ExecutorFreshnessTTL:   30 * time.Minute,
			ProjectionFreshnessTTL: 30 * time.Minute,
			SourceFreshnessTTL:     30 * time.Minute,
		},
		Now: func() time.Time { return now },
	}

	result, err := service.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle() error = %v", err)
	}
	if len(result.Outcomes) == 0 {
		t.Fatalf("RunCycle() outcomes = 0, want repeated-failure escalation")
	}

	incidents, err := projections.ListIncidentViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListIncidentViews() error = %v", err)
	}
	if len(incidents) != 1 || incidents[0].Status != "escalated" {
		t.Fatalf("incidents = %+v, want one escalated incident", incidents)
	}

	recoveries, err := projections.ListRecoveryViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListRecoveryViews() error = %v", err)
	}
	if len(recoveries) != 1 || recoveries[0].Status != "escalated" {
		t.Fatalf("recoveries = %+v, want one escalated recovery", recoveries)
	}

	blocked, err := projections.ListBlockedItemViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListBlockedItemViews() error = %v", err)
	}
	if len(blocked) == 0 {
		t.Fatalf("blocked items = 0, want wake-packet handoff block")
	}

	snapshot, err := metrics.Service{
		DB: store.DB(),
		Config: metrics.Config{
			ExecutorFreshnessTTL:   30 * time.Minute,
			ProjectionFreshnessTTL: 30 * time.Minute,
			SourceFreshnessTTL:     30 * time.Minute,
		},
		Now: func() time.Time { return now },
	}.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if snapshot.EscalatedIncidents != 1 {
		t.Fatalf("EscalatedIncidents = %d, want 1", snapshot.EscalatedIncidents)
	}
}

func TestShutdownRequestedSkipsRecoveryCycle(t *testing.T) {
	var shutdownRequested atomic.Bool
	shutdownRequested.Store(true)

	result, err := (recovery.Service{
		ShutdownRequested: &shutdownRequested,
	}).RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle() error = %v", err)
	}
	if len(result.Observations) != 0 || len(result.Decisions) != 0 || len(result.Outcomes) != 0 {
		t.Fatalf("RunCycle() = %+v, want empty result while shutdown is requested", result)
	}
}
