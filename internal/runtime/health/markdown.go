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

	writeSubsectionHeading(&builder, "Coverage")
	builder.WriteString("| Evaluated Areas | Unknown Areas |\n")
	builder.WriteString("| --- | --- |\n")
	writeTableRow(&builder, joinOrNone(report.Coverage.Evaluated), joinOrNone(report.Coverage.Unknown))
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
	writeSubsectionHeading(&builder, "Confirmed")
	confirmed := filterRootCauses(report.RootCauses, "confirmed")
	renderRootCauseGroup(&builder, confirmed)
	writeSubsectionHeading(&builder, "Inferred")
	inferred := filterRootCauses(report.RootCauses, "inferred")
	renderRootCauseGroup(&builder, inferred)

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
	builder.WriteString("| Action | Reason | Expected Benefit | Effort | Risk | Approval Requirement |\n")
	builder.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	for _, recommendation := range recommendations {
		writeTableRow(builder,
			recommendation.Action,
			recommendation.Reason,
			recommendation.ExpectedBenefit,
			recommendation.Effort,
			recommendation.Risk,
			recommendation.ApprovalRequirement,
		)
	}
	builder.WriteString("\n")
}

func writeSectionHeading(builder *strings.Builder, title string) {
	builder.WriteString("## ")
	builder.WriteString(title)
	builder.WriteString("\n")
}

func writeSubsectionHeading(builder *strings.Builder, title string) {
	builder.WriteString("### ")
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

func renderRootCauseGroup(builder *strings.Builder, rootCauses []RootCause) {
	if len(rootCauses) == 0 {
		builder.WriteString("None\n\n")
		return
	}
	builder.WriteString("| Area | Summary | Confidence | Provenance |\n")
	builder.WriteString("| --- | --- | --- | --- |\n")
	for _, rootCause := range rootCauses {
		writeTableRow(builder, rootCause.Area, rootCause.Summary, rootCause.Confidence, rootCause.Provenance)
	}
	builder.WriteString("\n")
}

func filterRootCauses(rootCauses []RootCause, provenance string) []RootCause {
	filtered := make([]RootCause, 0, len(rootCauses))
	for _, rootCause := range rootCauses {
		if rootCause.Provenance == provenance {
			filtered = append(filtered, rootCause)
		}
	}
	return filtered
}

func joinOrNone(values []string) string {
	if len(values) == 0 {
		return "None"
	}
	return strings.Join(values, ", ")
}
