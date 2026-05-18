package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	defaultRenderWidth = 76
	minRenderWidth     = 52
	maxRenderWidth     = 160
	wideRenderWidth    = 118
	columnGap          = 2
)

const (
	ansiReset  = "\x1b[0m"
	ansiDim    = "\x1b[2m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiBlue   = "\x1b[34m"
	ansiCyan   = "\x1b[36m"
	ansiBold   = "\x1b[1m"
)

type renderOptions struct {
	Width int
	Color bool
}

type panel struct {
	Title string
	Rows  []string
	Span  bool
}

func RenderOverview(model Model) string {
	return renderOverview(model, renderOptions{Width: defaultRenderWidth})
}

func RenderOverviewForTerminal(model Model, width int, color bool) string {
	return renderOverview(model, renderOptions{Width: width, Color: color})
}

func renderOverview(model Model, options renderOptions) string {
	width := normalizedRenderWidth(options.Width)
	panels := []panel{
		actionPanel(model, options.Color),
		healthPanel(model, options.Color),
		liveExecutionPanel(model),
		activityPanel(model),
		flowPanel(model),
		agentsPanel(model),
		goalsPanel(model),
		schedulesPanel(model),
		pullRequestsPanel(model),
		approvalsPanel(model),
		recentLogsPanel(model),
		legacyLogsPanel(),
	}

	var builder strings.Builder
	if width >= wideRenderWidth {
		writeResponsivePanels(&builder, panels, width, options.Color)
		return builder.String()
	}
	for index, panel := range panels {
		writePanel(&builder, panel, width, options.Color)
		if index < len(panels)-1 {
			builder.WriteByte('\n')
		}
	}
	return builder.String()
}

func healthPanel(model Model, color bool) panel {
	status := strings.ToUpper(model.Status)
	if status == "" || !model.TelemetryAvailable || model.TelemetryStale {
		status = "UNKNOWN"
	}
	score := "unknown"
	if model.TelemetryAvailable {
		score = fmt.Sprintf("%d", model.HealthScore)
	}
	phase := valueOrUnknown(model.LifecyclePhase)
	telemetry := "fresh"
	if !model.TelemetryAvailable {
		telemetry = "unavailable"
	} else if model.TelemetryStale {
		telemetry = "stale"
	}

	rows := []string{}
	if model.Name != "" {
		rows = append(rows, labelledRow("NAME", model.Name))
	}
	rows = append(rows,
		labelledRow("WATCH", watchLabel(model)),
		labelledRow("HEALTH", styleStatus(status, color)),
		labelledRow("SCORE", styleScore(score, model.HealthScore, color)),
		labelledRow("TELEMETRY", styleTelemetry(telemetry, color)),
		labelledRow("PHASE", phase),
		labelledRow("ACTIVE RUNS", styleCount(model.ActiveRuns, model.ActiveRuns > 0, color)),
	)
	if model.OdinHealth.Summary != "" || model.OdinHealth.Status != "" || model.OdinHealth.Command != "" {
		rows = append(rows,
			labelledRow("READY", fmt.Sprintf("%t", model.OdinHealth.Ready)),
			labelledRow("SNAPSHOT", valueOrUnknown(model.OdinHealth.Summary)),
		)
		if model.OdinHealth.Command != "" {
			rows = append(rows, labelledRow("INSPECT", model.OdinHealth.Command))
		}
	}
	return panel{Title: "ODIN HEALTH", Rows: rows}
}

func actionPanel(model Model, color bool) panel {
	rows := snapshotPanelRows(model.ActionRequired)
	if len(rows) > 0 {
		if model.SnapshotUnavailable != "" {
			rows = append([]string{model.SnapshotUnavailable}, rows...)
		}
		return panel{Title: "ACTION REQUIRED", Rows: rows, Span: true}
	}
	if model.SnapshotUnavailable != "" {
		return panel{Title: "ACTION REQUIRED", Rows: []string{model.SnapshotUnavailable}, Span: true}
	}
	return panel{Title: "ACTION REQUIRED", Rows: []string{
		labelledRow("APPROVALS", styleCount(model.ApprovalsWaiting, model.ApprovalsWaiting > 0, color)),
		labelledRow("BLOCKED", styleCount(model.BlockedItems, model.BlockedItems > 0, color)),
		labelledRow("REVIEW QUEUE", styleCount(model.ReviewQueueItems, model.ReviewQueueItems > 0, color)),
		labelledRow("FAILED WORK", styleCount(model.FailedWorkItems, model.FailedWorkItems > 0, color)),
		labelledRow("RECOVERY", styleCount(model.RecoveryRecommendations, model.RecoveryRecommendations > 0, color)),
	}}
}

func liveExecutionPanel(model Model) panel {
	rows := snapshotPanelRows(model.LiveExecution)
	if len(rows) == 0 {
		rows = []string{fmt.Sprintf("active_runs=%d", model.ActiveRuns)}
		if model.ActiveRuns == 0 {
			rows = []string{"none"}
		}
	}
	return panel{Title: "LIVE EXECUTION", Rows: rows, Span: len(rows) > 2}
}

func activityPanel(model Model) panel {
	rows := snapshotPanelRows(model.Activity)
	if len(rows) == 0 {
		rows = []string{"none"}
	}
	return panel{Title: "ACTIVITY", Rows: rows, Span: len(rows) > 2}
}

func flowPanel(model Model) panel {
	rows := []string{"none"}
	if len(model.Flows) > 0 {
		rows = rows[:0]
		for _, flow := range model.Flows {
			rows = append(rows, fmt.Sprintf(
				"%s %s source=%s status=%s subject=%s",
				valueOrNone(flow.Direction),
				valueOrNone(flow.Ref),
				valueOrNone(flow.Source),
				valueOrNone(flow.Status),
				valueOrNone(flow.Subject),
			))
		}
	}
	return panel{Title: "INBOX / OUTBOX", Rows: rows, Span: true}
}

func agentsPanel(model Model) panel {
	rows := []string{"none"}
	if len(model.Agents) > 0 {
		rows = rows[:0]
		for _, agent := range model.Agents {
			rows = append(rows, fmt.Sprintf(
				"%s task=%s project=%s status=%s",
				valueOrNone(agent.Name),
				valueOrNone(agent.Task),
				valueOrNone(agent.Project),
				valueOrNone(agent.Status),
			))
		}
	}
	return panel{Title: "AGENTS RUNNING", Rows: rows}
}

func goalsPanel(model Model) panel {
	rows := []string{"none"}
	if len(model.Goals) > 0 {
		rows = rows[:0]
		for _, goal := range model.Goals {
			rows = append(rows, fmt.Sprintf(
				"goal=%d status=%s run=%s title=%s",
				goal.ID,
				valueOrNone(goal.Status),
				valueOrNone(goal.CurrentRun),
				valueOrNone(goal.Title),
			))
		}
	}
	return panel{Title: "CURRENT GOALS", Rows: rows}
}

func schedulesPanel(model Model) panel {
	rows := []string{"none"}
	if len(model.Schedules) > 0 {
		rows = rows[:0]
		for index, schedule := range model.Schedules {
			if index >= 6 {
				rows = append(rows, fmt.Sprintf("... %d more", len(model.Schedules)-index))
				break
			}
			rows = append(rows, fmt.Sprintf(
				"%s=%s",
				valueOrNone(schedule.Source),
				valueOrNone(schedule.Key),
			))
			rows = append(rows, fmt.Sprintf(
				"  project=%s status=%s due=%s",
				valueOrNone(schedule.Project),
				valueOrNone(schedule.Status),
				valueOrNone(schedule.DueStatus),
			))
			rows = append(rows, fmt.Sprintf(
				"  next=%s last_run=%s",
				valueOrNone(schedule.NextDueAt),
				valueOrNone(schedule.LastRanAt),
			))
			rows = append(rows, fmt.Sprintf(
				"  work_status=%s detail=%s work=%s",
				valueOrNone(schedule.LastWorkStatus),
				valueOrNone(schedule.LastWorkDetail),
				valueOrNone(schedule.LastWorkItem),
			))
			if schedule.LastWorkReview != "" {
				rows = append(rows, fmt.Sprintf(
					"  review=odin review show %s",
					valueOrNone(schedule.LastWorkReview),
				))
				rows = append(rows, fmt.Sprintf(
					"  retry=odin review act %s retry",
					valueOrNone(schedule.LastWorkReview),
				))
			}
		}
	}
	return panel{Title: "SCHEDULES + ROUTINES", Rows: rows, Span: true}
}

func pullRequestsPanel(model Model) panel {
	rows := []string{"none"}
	if len(model.PullRequests) > 0 {
		rows = rows[:0]
		for _, pr := range model.PullRequests {
			rows = append(rows, fmt.Sprintf(
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
	return panel{Title: "PROJECT PRS + CI", Rows: rows}
}

func approvalsPanel(model Model) panel {
	rows := []string{"none"}
	if len(model.Approvals) > 0 {
		rows = rows[:0]
		for _, approval := range model.Approvals {
			rows = append(rows, fmt.Sprintf(
				"approval=%d task=%s project=%s status=%s resolver=%s",
				approval.ID,
				valueOrNone(approval.Task),
				valueOrNone(approval.Project),
				valueOrNone(approval.Status),
				valueOrNone(approval.Resolver),
			))
		}
	}
	return panel{Title: "APPROVALS WAITING", Rows: rows}
}

func recentLogsPanel(model Model) panel {
	rows := []string{"none"}
	if model.LogsUnavailable != "" {
		rows = []string{
			"Loki unavailable - runtime panels continue from store projections",
			"unavailable: " + model.LogsUnavailable,
		}
		return panel{Title: "RECENT LOGS", Rows: rows, Span: true}
	}
	if len(model.Logs) > 0 {
		rows = rows[:0]
		for _, entry := range model.Logs {
			line := strings.TrimSpace(entry.Line)
			if line == "" {
				line = "<empty>"
			}
			if entry.Timestamp != "" {
				rows = append(rows, entry.Timestamp+"  "+line)
				continue
			}
			rows = append(rows, line)
		}
	}
	return panel{Title: "RECENT LOGS", Rows: rows, Span: true}
}

func legacyLogsPanel() panel {
	return panel{Title: "ODIN LOGS", Rows: []string{"see RECENT LOGS"}, Span: true}
}

func snapshotPanelRows(rows []SnapshotRow) []string {
	if len(rows) == 0 {
		return nil
	}
	rendered := make([]string, 0, len(rows)*3)
	for _, row := range rows {
		header := valueOrNone(row.Label)
		if row.Severity != "" {
			header += " severity=" + row.Severity
		}
		if row.ID != "" {
			header += " id=" + row.ID
		}
		rendered = append(rendered, header)
		if strings.TrimSpace(row.Summary) != "" {
			rendered = append(rendered, "  "+strings.TrimSpace(row.Summary))
		}
		if strings.TrimSpace(row.Command) != "" {
			rendered = append(rendered, "  inspect="+strings.TrimSpace(row.Command))
		}
	}
	return rendered
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

func valueOrUnknown(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}

func watchLabel(model Model) string {
	switch {
	case !model.TelemetryAvailable:
		return "watching store; telemetry offline"
	case model.TelemetryStale:
		return "telemetry stale"
	case model.ApprovalsWaiting > 0 || model.BlockedItems > 0 || model.FailedWorkItems > 0 || model.RecoveryRecommendations > 0:
		return "attention needed"
	case model.ActiveRuns > 0:
		return "active"
	default:
		return "quiet"
	}
}

func writeResponsivePanels(builder *strings.Builder, panels []panel, width int, color bool) {
	columnWidth := (width - columnGap) / 2
	for index := 0; index < len(panels); index++ {
		current := panels[index]
		if current.Span || index == len(panels)-1 || panels[index+1].Span {
			writePanel(builder, current, width, color)
			if index < len(panels)-1 {
				builder.WriteByte('\n')
			}
			continue
		}
		left := renderPanelLines(current, columnWidth, color)
		right := renderPanelLines(panels[index+1], width-columnWidth-columnGap, color)
		writePanelColumns(builder, left, right, columnGap)
		index++
		if index < len(panels)-1 {
			builder.WriteByte('\n')
		}
	}
}

func writePanel(builder *strings.Builder, panel panel, width int, color bool) {
	for _, line := range renderPanelLines(panel, width, color) {
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
}

func renderPanelLines(panel panel, width int, color bool) []string {
	width = normalizedPanelWidth(width)
	lines := []string{boxTop(panel.Title, width, color)}
	for _, row := range panel.Rows {
		lines = append(lines, boxRow(row, width))
	}
	lines = append(lines, boxBottom(width))
	return lines
}

func writePanelColumns(builder *strings.Builder, left []string, right []string, gap int) {
	height := len(left)
	if len(right) > height {
		height = len(right)
	}
	leftWidth := visibleLen(left[0])
	rightWidth := visibleLen(right[0])
	for index := 0; index < height; index++ {
		if index < len(left) {
			builder.WriteString(left[index])
		} else {
			builder.WriteString(strings.Repeat(" ", leftWidth))
		}
		builder.WriteString(strings.Repeat(" ", gap))
		if index < len(right) {
			builder.WriteString(right[index])
		} else {
			builder.WriteString(strings.Repeat(" ", rightWidth))
		}
		builder.WriteByte('\n')
	}
}

func boxTop(title string, width int, color bool) string {
	renderedTitle := title
	if color {
		renderedTitle = ansiBold + ansiCyan + title + ansiReset
	}
	prefix := "┌─ " + renderedTitle + " "
	return prefix + strings.Repeat("─", width-visibleLen(prefix)-1) + "┐"
}

func boxRow(text string, width int) string {
	contentWidth := width - 4
	text = strings.ReplaceAll(text, "\n", " ")
	text = truncateVisible(text, contentWidth)
	return "│ " + text + strings.Repeat(" ", contentWidth-visibleLen(text)) + " │"
}

func boxBottom(width int) string {
	return "└" + strings.Repeat("─", width-2) + "┘"
}

func normalizedRenderWidth(width int) int {
	if width <= 0 {
		return defaultRenderWidth
	}
	if width < minRenderWidth {
		return minRenderWidth
	}
	if width > maxRenderWidth {
		return maxRenderWidth
	}
	return width
}

func normalizedPanelWidth(width int) int {
	if width < minRenderWidth {
		return minRenderWidth
	}
	return width
}

func styleStatus(status string, color bool) string {
	if !color {
		return status
	}
	switch strings.ToUpper(status) {
	case "HEALTHY", "OK", "READY":
		return ansiGreen + status + ansiReset
	case "DEGRADED", "UNKNOWN":
		return ansiYellow + status + ansiReset
	default:
		return ansiRed + status + ansiReset
	}
}

func styleScore(score string, value int, color bool) string {
	if !color || score == "unknown" {
		return score
	}
	switch {
	case value >= 90:
		return ansiGreen + score + ansiReset
	case value >= 70:
		return ansiYellow + score + ansiReset
	default:
		return ansiRed + score + ansiReset
	}
}

func styleTelemetry(telemetry string, color bool) string {
	if !color {
		return telemetry
	}
	switch telemetry {
	case "fresh":
		return ansiGreen + telemetry + ansiReset
	case "stale":
		return ansiYellow + telemetry + ansiReset
	default:
		return ansiBlue + telemetry + ansiReset
	}
}

func styleCount(count int, attention bool, color bool) string {
	value := fmt.Sprintf("%d", count)
	if !color {
		return value
	}
	if attention {
		return ansiYellow + value + ansiReset
	}
	return ansiDim + value + ansiReset
}

func truncateVisible(text string, limit int) string {
	if visibleLen(text) <= limit {
		return text
	}
	plain := stripANSI(text)
	runes := []rune(plain)
	if len(runes) <= limit {
		return plain
	}
	return string(runes[:limit-3]) + "..."
}

func visibleLen(text string) int {
	length := 0
	for index := 0; index < len(text); {
		if text[index] == '\x1b' {
			next := index + 1
			if next < len(text) && text[next] == '[' {
				next++
				for next < len(text) {
					if text[next] >= '@' && text[next] <= '~' {
						next++
						break
					}
					next++
				}
				index = next
				continue
			}
		}
		_, size := utf8.DecodeRuneInString(text[index:])
		if size == 0 {
			break
		}
		length++
		index += size
	}
	return length
}

func stripANSI(text string) string {
	var builder strings.Builder
	for index := 0; index < len(text); {
		if text[index] == '\x1b' {
			next := index + 1
			if next < len(text) && text[next] == '[' {
				next++
				for next < len(text) {
					if text[next] >= '@' && text[next] <= '~' {
						next++
						break
					}
					next++
				}
				index = next
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(text[index:])
		if size == 0 {
			break
		}
		builder.WriteRune(r)
		index += size
	}
	return builder.String()
}
