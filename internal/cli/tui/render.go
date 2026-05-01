package tui

import (
	"fmt"
	"strings"
)

func RenderOverview(model Model) string {
	status := strings.ToUpper(model.Status)
	if status == "" || !model.TelemetryAvailable || model.TelemetryStale {
		status = "UNKNOWN"
	}

	var builder strings.Builder
	fmt.Fprintln(&builder, "ODIN OBSERVABILITY")
	fmt.Fprintf(&builder, "HEALTH: %s\n", status)
	if model.TelemetryAvailable {
		fmt.Fprintf(&builder, "HEALTH_SCORE: %d\n", model.HealthScore)
	} else {
		fmt.Fprintln(&builder, "HEALTH_SCORE: unknown")
	}
	fmt.Fprintf(&builder, "TELEMETRY_STALE: %t\n", model.TelemetryStale)
	if model.LifecyclePhase == "" {
		fmt.Fprintln(&builder, "LIFECYCLE_PHASE: unknown")
	} else {
		fmt.Fprintf(&builder, "LIFECYCLE_PHASE: %s\n", model.LifecyclePhase)
	}
	fmt.Fprintf(&builder, "ACTIVE_RUNS: %d\n", model.ActiveRuns)

	fmt.Fprintln(&builder, "RECENT_LOGS:")
	if model.LogsUnavailable != "" {
		fmt.Fprintf(&builder, "  unavailable: %s\n", model.LogsUnavailable)
		return builder.String()
	}
	if len(model.Logs) == 0 {
		fmt.Fprintln(&builder, "  none")
		return builder.String()
	}
	for _, entry := range model.Logs {
		line := strings.TrimSpace(entry.Line)
		if line == "" {
			line = "<empty>"
		}
		if entry.Timestamp != "" {
			fmt.Fprintf(&builder, "  %s %s\n", entry.Timestamp, line)
			continue
		}
		fmt.Fprintf(&builder, "  %s\n", line)
	}
	return builder.String()
}
