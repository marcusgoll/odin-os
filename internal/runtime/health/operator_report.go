package health

import (
	"sort"
	"strings"
	"time"
)

type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
)

type CurrentHealthSnapshot struct {
	Status          Status    `json:"status"`
	GeneratedAt     time.Time `json:"generated_at"`
	ChecksEvaluated int       `json:"checks_evaluated"`
}

type Finding struct {
	Area         string   `json:"area"`
	Severity     Severity `json:"severity"`
	Observation  string   `json:"observation"`
	WhyItMatters string   `json:"why_it_matters"`
	Confidence   string   `json:"confidence"`
}

type RootCause struct {
	Area       string `json:"area"`
	Summary    string `json:"summary"`
	Confidence string `json:"confidence"`
}

type Recommendation struct {
	Action string `json:"action"`
	Reason string `json:"reason"`
}

type RecommendationGroups struct {
	Immediate []Recommendation `json:"immediate"`
	NearTerm  []Recommendation `json:"near_term"`
	Strategic []Recommendation `json:"strategic"`
}

type FinalVerdict struct {
	Status  Status `json:"status"`
	Summary string `json:"summary"`
}

type OperatorReport struct {
	CurrentHealth    CurrentHealthSnapshot `json:"current_health"`
	Findings         []Finding             `json:"findings"`
	RootCauses       []RootCause           `json:"root_causes"`
	Recommendations  RecommendationGroups  `json:"recommendations"`
	MissingTelemetry []string              `json:"missing_telemetry"`
	FinalVerdict     FinalVerdict          `json:"final_verdict"`
}

func BuildOperatorReport(raw Report) OperatorReport {
	report := OperatorReport{
		CurrentHealth: CurrentHealthSnapshot{
			Status:          raw.Status,
			GeneratedAt:     raw.GeneratedAt,
			ChecksEvaluated: len(raw.Checks),
		},
		FinalVerdict: FinalVerdict{
			Status:  raw.Status,
			Summary: verdictSummary(raw.Status),
		},
	}

	for _, check := range raw.Checks {
		finding, rootCause, recommendation, missingTelemetry := classifyCheck(check)
		if finding != nil {
			report.Findings = append(report.Findings, *finding)
		}
		if rootCause != nil {
			report.RootCauses = append(report.RootCauses, *rootCause)
		}
		if recommendation != nil {
			switch recommendationBucket(check.Status, finding) {
			case "immediate":
				report.Recommendations.Immediate = append(report.Recommendations.Immediate, *recommendation)
			case "strategic":
				report.Recommendations.Strategic = append(report.Recommendations.Strategic, *recommendation)
			default:
				report.Recommendations.NearTerm = append(report.Recommendations.NearTerm, *recommendation)
			}
		}
		if missingTelemetry != "" {
			report.MissingTelemetry = append(report.MissingTelemetry, missingTelemetry)
		}
	}

	sort.SliceStable(report.Findings, func(i, j int) bool {
		left := report.Findings[i]
		right := report.Findings[j]
		if findingOrder(left) != findingOrder(right) {
			return findingOrder(left) < findingOrder(right)
		}
		if left.Severity != right.Severity {
			return severityOrder(left.Severity) < severityOrder(right.Severity)
		}
		return left.Area < right.Area
	})

	report.MissingTelemetry = uniqueStrings(report.MissingTelemetry)
	if len(report.Recommendations.Strategic) == 0 && len(report.MissingTelemetry) > 0 {
		report.Recommendations.Strategic = append(report.Recommendations.Strategic, Recommendation{
			Action: "add telemetry coverage for missing evidence paths",
			Reason: "the report cannot confidently judge every subsystem from the available samples",
		})
	}

	return report
}

func classifyCheck(check Check) (*Finding, *RootCause, *Recommendation, string) {
	area := check.Name
	whyItMatters := whyItMattersForCheck(check.Name)
	confidence := "high"
	missingTelemetry := missingTelemetryForCheck(check)

	switch check.Status {
	case StatusFailed:
		severity := SeverityHigh
		if check.Name == "database" {
			severity = SeverityCritical
		}
		finding := &Finding{
			Area:         area,
			Severity:     severity,
			Observation:  check.Summary,
			WhyItMatters: whyItMatters,
			Confidence:   confidence,
		}
		rootCause := &RootCause{
			Area:       area,
			Summary:    check.Summary,
			Confidence: "high",
		}
		recommendation := &Recommendation{
			Action: actionForCheck(check, true),
			Reason: check.Summary,
		}
		return finding, rootCause, recommendation, missingTelemetry
	case StatusDegraded:
		severity := SeverityMedium
		if check.Name == "executor" || check.Name == "source_freshness" {
			severity = SeverityHigh
		}
		finding := &Finding{
			Area:         area,
			Severity:     severity,
			Observation:  check.Summary,
			WhyItMatters: whyItMatters,
			Confidence:   confidenceForCheck(check),
		}
		rootCause := &RootCause{
			Area:       area,
			Summary:    check.Summary,
			Confidence: confidenceForCheck(check),
		}
		recommendation := &Recommendation{
			Action: actionForCheck(check, false),
			Reason: check.Summary,
		}
		return finding, rootCause, recommendation, missingTelemetry
	default:
		return nil, nil, nil, ""
	}
}

func recommendationBucket(status Status, finding *Finding) string {
	if status == StatusFailed {
		return "immediate"
	}
	if finding != nil && finding.Severity == SeverityHigh {
		return "immediate"
	}
	return "near-term"
}

func actionForCheck(check Check, failed bool) string {
	switch check.Name {
	case "database":
		if failed {
			return "restore database connectivity"
		}
		return "verify database reachability"
	case "queue":
		return "reduce queue pressure"
	case "executor":
		return "refresh executor health samples"
	case "projections":
		return "refresh projection freshness data"
	case "source_freshness":
		return "rebuild source freshness records"
	case "registry":
		return "reconcile registry diagnostics"
	default:
		return "inspect " + check.Name
	}
}

func whyItMattersForCheck(name string) string {
	switch name {
	case "database":
		return "database access is required for runtime health decisions"
	case "registry":
		return "registry state affects command and report correctness"
	case "executor":
		return "executor samples determine whether work can be dispatched safely"
	case "queue":
		return "queue pressure affects throughput and latency"
	case "projections":
		return "stale projections can leave derived state out of sync"
	case "source_freshness":
		return "stale sources can hide outdated registry state"
	default:
		return "this subsystem is part of the runtime health contract"
	}
}

func confidenceForCheck(check Check) string {
	if missingTelemetryForCheck(check) != "" {
		return "reduced"
	}
	if strings.Contains(strings.ToLower(check.Summary), "stale") {
		return "reduced"
	}
	return "high"
}

func missingTelemetryForCheck(check Check) string {
	summary := strings.ToLower(check.Summary)
	switch check.Name {
	case "executor":
		if strings.Contains(summary, "no executor health samples recorded") || strings.Contains(summary, "no ") && strings.Contains(summary, "recorded") {
			return "executor health samples"
		}
	case "source_freshness":
		if strings.Contains(summary, "no registry compilation recorded") || strings.Contains(summary, "missing") {
			return "registry compilation records"
		}
	case "projections":
		if strings.Contains(summary, "missing") {
			return "projection freshness samples"
		}
	}
	if strings.Contains(summary, "missing") && check.Name != "" {
		return check.Name + " telemetry"
	}
	if strings.Contains(summary, "stale") && (check.Name == "executor" || check.Name == "source_freshness" || check.Name == "projections") {
		return check.Name + " telemetry"
	}
	return ""
}

func verdictSummary(status Status) string {
	switch status {
	case StatusFailed:
		return "one or more critical checks failed"
	case StatusDegraded:
		return "the system is operating with degraded subsystems"
	default:
		return "all evaluated checks are healthy"
	}
}

func findingOrder(finding Finding) int {
	switch finding.Severity {
	case SeverityCritical:
		return 0
	case SeverityHigh:
		return 1
	default:
		return 2
	}
}

func severityOrder(severity Severity) int {
	switch severity {
	case SeverityCritical:
		return 0
	case SeverityHigh:
		return 1
	default:
		return 2
	}
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
