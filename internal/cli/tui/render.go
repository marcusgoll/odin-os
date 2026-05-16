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
	if model.Name != "" {
		writeBoxRow(&builder, labelledRow("NAME", model.Name))
	}
	writeBoxRow(&builder, labelledRow("HEALTH", status))
	writeBoxRow(&builder, labelledRow("SCORE", score))
	writeBoxRow(&builder, labelledRow("TELEMETRY", telemetry))
	writeBoxRow(&builder, labelledRow("PHASE", phase))
	writeBoxRow(&builder, labelledRow("ACTIVE RUNS", fmt.Sprintf("%d", model.ActiveRuns)))
	writeBoxBottom(&builder)
	builder.WriteByte('\n')

	writeBoxTop(&builder, "ACTION REQUIRED")
	writeBoxRow(&builder, labelledRow("APPROVALS", fmt.Sprintf("%d", model.ApprovalsWaiting)))
	writeBoxRow(&builder, labelledRow("BLOCKED", fmt.Sprintf("%d", model.BlockedItems)))
	writeBoxRow(&builder, labelledRow("REVIEW QUEUE", fmt.Sprintf("%d", model.ReviewQueueItems)))
	writeBoxRow(&builder, labelledRow("FAILED WORK", fmt.Sprintf("%d", model.FailedWorkItems)))
	writeBoxRow(&builder, labelledRow("RECOVERY", fmt.Sprintf("%d", model.RecoveryRecommendations)))
	writeBoxBottom(&builder)
	builder.WriteByte('\n')

	writeBoxTop(&builder, "AGENTS RUNNING")
	if len(model.Agents) == 0 {
		writeBoxRow(&builder, "none")
	} else {
		for _, agent := range model.Agents {
			writeBoxRow(&builder, fmt.Sprintf(
				"%s task=%s project=%s status=%s",
				valueOrNone(agent.Name),
				valueOrNone(agent.Task),
				valueOrNone(agent.Project),
				valueOrNone(agent.Status),
			))
		}
	}
	writeBoxBottom(&builder)
	builder.WriteByte('\n')

	writeBoxTop(&builder, "CURRENT GOALS")
	if len(model.Goals) == 0 {
		writeBoxRow(&builder, "none")
	} else {
		for _, goal := range model.Goals {
			writeBoxRow(&builder, fmt.Sprintf(
				"goal=%d status=%s run=%s title=%s",
				goal.ID,
				valueOrNone(goal.Status),
				valueOrNone(goal.CurrentRun),
				valueOrNone(goal.Title),
			))
		}
	}
	writeBoxBottom(&builder)
	builder.WriteByte('\n')

	writeBoxTop(&builder, "PROJECT PRS + CI")
	if len(model.PullRequests) == 0 {
		writeBoxRow(&builder, "none")
	} else {
		for _, pr := range model.PullRequests {
			writeBoxRow(&builder, fmt.Sprintf(
				"%s %s#%d state=%s ci=%s title=%s",
				valueOrNone(pr.Project),
				valueOrNone(pr.Repo),
				pr.Number,
				valueOrNone(pr.State),
				valueOrNone(pr.CI),
				valueOrNone(pr.Title),
			))
		}
	}
	writeBoxBottom(&builder)
	builder.WriteByte('\n')

	writeBoxTop(&builder, "APPROVALS WAITING")
	if len(model.Approvals) == 0 {
		writeBoxRow(&builder, "none")
	} else {
		for _, approval := range model.Approvals {
			writeBoxRow(&builder, fmt.Sprintf(
				"approval=%d task=%s project=%s status=%s resolver=%s",
				approval.ID,
				valueOrNone(approval.Task),
				valueOrNone(approval.Project),
				valueOrNone(approval.Status),
				valueOrNone(approval.Resolver),
			))
		}
	}
	writeBoxBottom(&builder)
	builder.WriteByte('\n')

	writeBoxTop(&builder, "ODIN LOGS")
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

func valueOrNone(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "none"
	}
	return value
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
