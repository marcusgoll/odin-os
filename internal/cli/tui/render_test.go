package tui

import (
	"strings"
	"testing"
)

func TestRenderOverviewShowsUnknownWhenTelemetryIsStale(t *testing.T) {
	t.Parallel()

	output := RenderOverview(Model{
		TelemetryAvailable: true,
		Status:             "healthy",
		HealthScore:        99,
		TelemetryStale:     true,
	})
	if !strings.Contains(output, "HEALTH: UNKNOWN") {
		t.Fatalf("output = %q, want UNKNOWN", output)
	}
}

func TestRenderOverviewStableTextOutput(t *testing.T) {
	t.Parallel()

	output := RenderOverview(Model{
		TelemetryAvailable: true,
		Status:             "degraded",
		HealthScore:        87,
		TelemetryStale:     false,
		LifecyclePhase:     "run",
		ActiveRuns:         3,
		Logs: []LogEntry{
			{Timestamp: "1714521600000000000", Line: `{"level":"info","message":"ready"}`},
		},
	})

	for _, want := range []string{
		"ODIN OBSERVABILITY",
		"HEALTH: DEGRADED",
		"HEALTH_SCORE: 87",
		"TELEMETRY_STALE: false",
		"LIFECYCLE_PHASE: run",
		"ACTIVE_RUNS: 3",
		`{"level":"info","message":"ready"}`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, want %q", output, want)
		}
	}
}

func TestRenderOverviewShowsUnavailableLogs(t *testing.T) {
	t.Parallel()

	output := RenderOverview(Model{
		TelemetryAvailable: true,
		Status:             "healthy",
		HealthScore:        100,
		LifecyclePhase:     "run",
		LogsUnavailable:    "loki query failed",
	})
	if !strings.Contains(output, "RECENT_LOGS:\n  unavailable: loki query failed") {
		t.Fatalf("output = %q, want unavailable logs", output)
	}
}
