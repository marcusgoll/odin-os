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
	if !strings.Contains(output, "│ HEALTH        UNKNOWN") {
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
		"┌─ ODIN OBSERVABILITY ",
		"│ HEALTH        DEGRADED",
		"│ SCORE         87",
		"│ TELEMETRY     fresh",
		"│ PHASE         run",
		"│ ACTIVE RUNS   3",
		`{"level":"info","message":"ready"}`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, want %q", output, want)
		}
	}
}

func TestRenderOverviewShowsActionRequiredPanel(t *testing.T) {
	t.Parallel()

	output := RenderOverview(Model{
		TelemetryAvailable:      true,
		Status:                  "degraded",
		HealthScore:             87,
		LifecyclePhase:          "run",
		ActiveRuns:              3,
		BlockedItems:            2,
		ApprovalsWaiting:        4,
		ReviewQueueItems:        6,
		FailedWorkItems:         1,
		RecoveryRecommendations: 1,
	})

	for _, want := range []string{
		"┌─ ACTION REQUIRED ",
		"│ APPROVALS     4",
		"│ BLOCKED       2",
		"│ REVIEW QUEUE  6",
		"│ FAILED WORK   1",
		"│ RECOVERY      1",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, want action-required fragment %q", output, want)
		}
	}
}

func TestRenderOverviewUsesBoxedCockpitLayout(t *testing.T) {
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
		"┌─ ODIN OBSERVABILITY ",
		"│ HEALTH        DEGRADED",
		"│ SCORE         87",
		"│ TELEMETRY     fresh",
		"│ PHASE         run",
		"┌─ RECENT LOGS ",
		"│ 1714521600000000000  {\"level\":\"info\",\"message\":\"ready\"}",
		"└",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, want boxed cockpit fragment %q", output, want)
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
	if !strings.Contains(output, "┌─ RECENT LOGS ") || !strings.Contains(output, "│ unavailable: loki query failed") {
		t.Fatalf("output = %q, want unavailable logs", output)
	}
}
