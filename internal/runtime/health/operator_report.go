package health

import (
	"sort"
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
	SourceStatus Status   `json:"-"`
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

type reportRuleKey struct {
	Status  Status
	Summary string
}

type reportRule struct {
	Severity          Severity
	Confidence        string
	WhyItMatters      string
	Recommendation    string
	RecommendationSet string
	MissingTelemetry  string
}

var operatorReportRules = map[string]map[reportRuleKey]reportRule{
	"database": {
		{Status: StatusFailed, Summary: "database connectivity failed"}: {
			Severity:          SeverityCritical,
			Confidence:        "high",
			WhyItMatters:      "database access is required for runtime health decisions",
			Recommendation:    "restore database connectivity",
			RecommendationSet: "immediate",
		},
		{Status: StatusFailed, Summary: "database handle is not configured"}: {
			Severity:          SeverityCritical,
			Confidence:        "high",
			WhyItMatters:      "database access is required for runtime health decisions",
			Recommendation:    "configure the database handle",
			RecommendationSet: "immediate",
		},
	},
	"registry": {
		{Status: StatusDegraded, Summary: "registry diagnostics present"}: {
			Severity:          SeverityMedium,
			Confidence:        "high",
			WhyItMatters:      "registry state affects command and report correctness",
			Recommendation:    "reconcile registry diagnostics",
			RecommendationSet: "near-term",
		},
	},
	"executor": {
		{Status: StatusDegraded, Summary: "no executor health samples recorded"}: {
			Severity:          SeverityHigh,
			Confidence:        "reduced",
			WhyItMatters:      "executor samples determine whether work can be dispatched safely",
			Recommendation:    "record executor health samples",
			RecommendationSet: "immediate",
			MissingTelemetry:  "executor health samples",
		},
		{Status: StatusDegraded, Summary: "executor health is unavailable or stale"}: {
			Severity:          SeverityHigh,
			Confidence:        "reduced",
			WhyItMatters:      "executor samples determine whether work can be dispatched safely",
			Recommendation:    "refresh executor health samples",
			RecommendationSet: "immediate",
		},
		{Status: StatusHealthy, Summary: "executor health is fresh"}: {
			Confidence: "high",
		},
	},
	"queue": {
		{Status: StatusDegraded, Summary: "queue pressure is above threshold"}: {
			Severity:          SeverityMedium,
			Confidence:        "high",
			WhyItMatters:      "queue pressure affects throughput and latency",
			Recommendation:    "reduce queue pressure",
			RecommendationSet: "near-term",
		},
		{Status: StatusHealthy, Summary: "queue pressure is within threshold"}: {
			Confidence: "high",
		},
	},
	"projections": {
		{Status: StatusDegraded, Summary: "projection freshness is missing or stale"}: {
			Severity:          SeverityMedium,
			Confidence:        "reduced",
			WhyItMatters:      "stale projections can leave derived state out of sync",
			Recommendation:    "refresh projection freshness data",
			RecommendationSet: "near-term",
			MissingTelemetry:  "projection freshness samples",
		},
		{Status: StatusHealthy, Summary: "projection freshness is current"}: {
			Confidence: "high",
		},
	},
	"source_freshness": {
		{Status: StatusFailed, Summary: "source freshness is unavailable"}: {
			Severity:          SeverityHigh,
			Confidence:        "reduced",
			WhyItMatters:      "stale sources can hide outdated registry state",
			Recommendation:    "rebuild source freshness records",
			RecommendationSet: "immediate",
			MissingTelemetry:  "source freshness records",
		},
		{Status: StatusDegraded, Summary: "no registry compilation recorded"}: {
			Severity:          SeverityHigh,
			Confidence:        "reduced",
			WhyItMatters:      "stale sources can hide outdated registry state",
			Recommendation:    "rebuild source freshness records",
			RecommendationSet: "immediate",
			MissingTelemetry:  "registry compilation records",
		},
		{Status: StatusDegraded, Summary: "source freshness is stale"}: {
			Severity:          SeverityHigh,
			Confidence:        "reduced",
			WhyItMatters:      "stale sources can hide outdated registry state",
			Recommendation:    "rebuild source freshness records",
			RecommendationSet: "immediate",
		},
		{Status: StatusHealthy, Summary: "source freshness is current"}: {
			Confidence: "high",
		},
	},
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
		finding, rootCause, recommendation, recommendationSet, missingTelemetry := classifyCheck(check)
		if finding != nil {
			report.Findings = append(report.Findings, *finding)
		}
		if rootCause != nil {
			report.RootCauses = append(report.RootCauses, *rootCause)
		}
		if recommendation != nil {
			switch recommendationSet {
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
		if statusOrder(left.SourceStatus) != statusOrder(right.SourceStatus) {
			return statusOrder(left.SourceStatus) < statusOrder(right.SourceStatus)
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

func classifyCheck(check Check) (*Finding, *RootCause, *Recommendation, string, string) {
	if check.Status == StatusHealthy {
		return nil, nil, nil, "", ""
	}

	rule, ok := lookupReportRule(check)
	if !ok {
		return nil, nil, nil, "", ""
	}
	finding := &Finding{
		Area:         check.Name,
		Severity:     rule.Severity,
		Observation:  check.Summary,
		WhyItMatters: rule.WhyItMatters,
		Confidence:   rule.Confidence,
		SourceStatus: check.Status,
	}
	rootCause := &RootCause{
		Area:       check.Name,
		Summary:    check.Summary,
		Confidence: rule.Confidence,
	}
	recommendation := &Recommendation{
		Action: rule.Recommendation,
		Reason: check.Summary,
	}
	return finding, rootCause, recommendation, rule.RecommendationSet, rule.MissingTelemetry
}

func lookupReportRule(check Check) (reportRule, bool) {
	rules, ok := operatorReportRules[check.Name]
	if !ok {
		return unmappedReportRule(check), check.Status == StatusFailed || check.Status == StatusDegraded
	}

	rule, ok := rules[reportRuleKey{Status: check.Status, Summary: check.Summary}]
	if ok {
		return rule, true
	}

	if check.Status == StatusFailed || check.Status == StatusDegraded {
		return unmappedReportRule(check), true
	}
	return reportRule{}, false
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

func unmappedReportRule(check Check) reportRule {
	rule := reportRule{
		Confidence:   "reduced",
		WhyItMatters: "operator mapping is missing for this health check and needs to be added",
	}

	switch check.Status {
	case StatusFailed:
		rule.Severity = SeverityHigh
		rule.Recommendation = "add an explicit operator mapping for " + check.Name
		rule.RecommendationSet = "immediate"
	case StatusDegraded:
		rule.Severity = SeverityMedium
		rule.Recommendation = "add an explicit operator mapping for " + check.Name
		rule.RecommendationSet = "near-term"
	}

	return rule
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

func statusOrder(status Status) int {
	switch status {
	case StatusFailed:
		return 0
	case StatusDegraded:
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
