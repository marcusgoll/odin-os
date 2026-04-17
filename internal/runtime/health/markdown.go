package health

import (
	"strconv"
	"strings"
	"time"
)

func RenderMarkdownReport(report OperatorReport) string {
	var builder strings.Builder

	writeSectionHeading(&builder, "Current Health Snapshot")
	builder.WriteString("| Field | Value |\n")
	builder.WriteString("| --- | --- |\n")
	writeTableRow(&builder, "Status", string(report.CurrentHealth.Status))
	writeTableRow(&builder, "Generated At", formatReportTime(report.CurrentHealth.GeneratedAt))
	writeTableRow(&builder, "Checks Evaluated", strconv.Itoa(report.CurrentHealth.ChecksEvaluated))
	builder.WriteString("\n")

	writeSectionHeading(&builder, "Key Findings")
	if len(report.Findings) == 0 {
		builder.WriteString("No major issues detected\n\n")
	} else {
		builder.WriteString("| Area | Severity | Observation | Why It Matters | Confidence |\n")
		builder.WriteString("| --- | --- | --- | --- | --- |\n")
		for _, finding := range report.Findings {
			writeTableRow(&builder,
				finding.Area,
				string(finding.Severity),
				finding.Observation,
				finding.WhyItMatters,
				finding.Confidence,
			)
		}
		builder.WriteString("\n")
	}

	writeSectionHeading(&builder, "Likely Root Causes")
	if len(report.RootCauses) == 0 {
		builder.WriteString("None\n\n")
	} else {
		builder.WriteString("| Area | Summary | Confidence |\n")
		builder.WriteString("| --- | --- | --- |\n")
		for _, rootCause := range report.RootCauses {
			writeTableRow(&builder, rootCause.Area, rootCause.Summary, rootCause.Confidence)
		}
		builder.WriteString("\n")
	}

	writeSectionHeading(&builder, "Upgrade and Improvement Recommendations")
	writeRecommendationGroup(&builder, "Immediate", report.Recommendations.Immediate)
	writeRecommendationGroup(&builder, "Near-Term", report.Recommendations.NearTerm)
	writeRecommendationGroup(&builder, "Strategic", report.Recommendations.Strategic)

	writeSectionHeading(&builder, "Missing Telemetry")
	if len(report.MissingTelemetry) == 0 {
		builder.WriteString("None\n\n")
	} else {
		for _, item := range report.MissingTelemetry {
			builder.WriteString("- ")
			builder.WriteString(item)
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}

	writeSectionHeading(&builder, "Final Verdict")
	builder.WriteString("| Field | Value |\n")
	builder.WriteString("| --- | --- |\n")
	writeTableRow(&builder, "Status", string(report.FinalVerdict.Status))
	writeTableRow(&builder, "Summary", report.FinalVerdict.Summary)

	return builder.String()
}

func writeRecommendationGroup(builder *strings.Builder, title string, recommendations []Recommendation) {
	builder.WriteString("### ")
	builder.WriteString(title)
	builder.WriteString("\n")
	if len(recommendations) == 0 {
		builder.WriteString("None\n\n")
		return
	}
	builder.WriteString("| Action | Reason |\n")
	builder.WriteString("| --- | --- |\n")
	for _, recommendation := range recommendations {
		writeTableRow(builder, recommendation.Action, recommendation.Reason)
	}
	builder.WriteString("\n")
}

func writeSectionHeading(builder *strings.Builder, title string) {
	builder.WriteString("## ")
	builder.WriteString(title)
	builder.WriteString("\n")
}

func writeTableRow(builder *strings.Builder, values ...string) {
	builder.WriteString("|")
	for _, value := range values {
		builder.WriteString(" ")
		builder.WriteString(escapeMarkdownCell(value))
		builder.WriteString(" |")
	}
	builder.WriteString("\n")
}

func escapeMarkdownCell(value string) string {
	value = strings.ReplaceAll(value, "|", `\|`)
	value = strings.ReplaceAll(value, "\n", "<br>")
	return value
}

func formatReportTime(value time.Time) string {
	if value.IsZero() {
		return "0001-01-01T00:00:00Z"
	}
	return value.UTC().Format(time.RFC3339)
}
