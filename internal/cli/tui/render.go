package tui

import (
	"fmt"
	"strings"
)

const renderWidth = 76

func RenderOverview(model Model) string {
	status := strings.ToUpper(model.Status)
	if status == "" || !model.TelemetryAvailable || model.TelemetryStale {
		status = "UNKNOWN"
	}
	score := "unknown"
	if model.TelemetryAvailable {
		score = fmt.Sprintf("%d", model.HealthScore)
	}
	phase := model.LifecyclePhase
	if phase == "" {
		phase = "unknown"
	}
	telemetry := "fresh"
	if model.TelemetryStale {
		telemetry = "stale"
	}

	var builder strings.Builder
	writeBoxTop(&builder, "ODIN OBSERVABILITY")
	writeBoxRow(&builder, labelledRow("HEALTH", status))
	writeBoxRow(&builder, labelledRow("SCORE", score))
	writeBoxRow(&builder, labelledRow("TELEMETRY", telemetry))
	writeBoxRow(&builder, labelledRow("PHASE", phase))
	writeBoxRow(&builder, labelledRow("ACTIVE RUNS", fmt.Sprintf("%d", model.ActiveRuns)))
	writeBoxBottom(&builder)
	builder.WriteByte('\n')

	writeBoxTop(&builder, "RECENT LOGS")
	if model.LogsUnavailable != "" {
		writeBoxRow(&builder, "unavailable: "+model.LogsUnavailable)
		writeBoxBottom(&builder)
		return builder.String()
	}
	if len(model.Logs) == 0 {
		writeBoxRow(&builder, "none")
		writeBoxBottom(&builder)
		return builder.String()
	}
	for _, entry := range model.Logs {
		line := strings.TrimSpace(entry.Line)
		if line == "" {
			line = "<empty>"
		}
		if entry.Timestamp != "" {
			writeBoxRow(&builder, entry.Timestamp+"  "+line)
			continue
		}
		writeBoxRow(&builder, line)
	}
	writeBoxBottom(&builder)
	return builder.String()
}

func labelledRow(label string, value string) string {
	return fmt.Sprintf("%-13s %s", label, value)
}

func writeBoxTop(builder *strings.Builder, title string) {
	prefix := "┌─ " + title + " "
	builder.WriteString(prefix)
	builder.WriteString(strings.Repeat("─", renderWidth-runeLen(prefix)-1))
	builder.WriteString("┐\n")
}

func writeBoxRow(builder *strings.Builder, text string) {
	contentWidth := renderWidth - 4
	text = strings.ReplaceAll(text, "\n", " ")
	text = truncateRunes(text, contentWidth)
	builder.WriteString("│ ")
	builder.WriteString(text)
	builder.WriteString(strings.Repeat(" ", contentWidth-runeLen(text)))
	builder.WriteString(" │\n")
}

func writeBoxBottom(builder *strings.Builder) {
	builder.WriteString("└")
	builder.WriteString(strings.Repeat("─", renderWidth-2))
	builder.WriteString("┘\n")
}

func truncateRunes(text string, limit int) string {
	if runeLen(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit-3]) + "..."
}

func runeLen(text string) int {
	return len([]rune(text))
}
