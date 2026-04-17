package health

import (
	"strings"
	"testing"
)

func TestRenderMarkdownReportIncludesOperatorSections(t *testing.T) {
	report := OperatorReport{
		Findings:         []Finding{{Area: "database", Severity: SeverityCritical}},
		MissingTelemetry: []string{"executor freshness samples"},
	}

	output := RenderMarkdownReport(report)

	for _, want := range []string{
		"## Current Health Snapshot",
		"## Key Findings",
		"## Likely Root Causes",
		"## Upgrade and Improvement Recommendations",
		"## Missing Telemetry",
		"## Final Verdict",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q\n%s", want, output)
		}
	}
}

func TestRenderMarkdownReportRendersEmptyRootCausesCleanly(t *testing.T) {
	report := OperatorReport{
		CurrentHealth: CurrentHealthSnapshot{
			Status:          StatusHealthy,
			ChecksEvaluated: 2,
		},
		Findings:        []Finding{},
		Recommendations: RecommendationGroups{},
		FinalVerdict: FinalVerdict{
			Status:  StatusHealthy,
			Summary: "all evaluated checks are healthy",
		},
	}

	output := RenderMarkdownReport(report)

	if !strings.Contains(output, "## Likely Root Causes") {
		t.Fatalf("output missing Likely Root Causes heading\n%s", output)
	}
	if !strings.Contains(output, "None\n\n") {
		t.Fatalf("output missing clean empty-state marker\n%s", output)
	}
	if strings.Contains(output, "| Area | Summary | Confidence |") {
		t.Fatalf("output still renders a root causes table header for empty state\n%s", output)
	}
}

func TestRenderMarkdownReportOrdersFindingsBeforeRecommendations(t *testing.T) {
	report := OperatorReport{
		Findings: []Finding{
			{
				Area:         "database",
				Severity:     SeverityCritical,
				Observation:  "database connectivity failed",
				WhyItMatters: "database access is required for runtime health decisions",
				Confidence:   "high",
				Evidence:     []string{"check summary: database connectivity failed"},
			},
		},
		Recommendations: RecommendationGroups{
			Immediate: []Recommendation{
				{Action: "restore database connectivity", Reason: "database connectivity failed"},
			},
		},
	}

	output := RenderMarkdownReport(report)

	sections := []string{
		"## Current Health Snapshot",
		"## Key Findings",
		"## Likely Root Causes",
		"## Upgrade and Improvement Recommendations",
		"## Missing Telemetry",
		"## Final Verdict",
	}
	lastIdx := -1
	for _, section := range sections {
		idx := strings.Index(output, section)
		if idx == -1 {
			t.Fatalf("output missing required section %q\n%s", section, output)
		}
		if idx < lastIdx {
			t.Fatalf("section %q appears out of order\n%s", section, output)
		}
		lastIdx = idx
	}
	if !strings.Contains(output, "| Area | Severity | Observation | Why It Matters | Confidence | Evidence |") {
		t.Fatalf("output missing findings table header\n%s", output)
	}
}

func TestRenderMarkdownReportRendersFindingEvidence(t *testing.T) {
	report := OperatorReport{
		Findings: []Finding{
			{
				Area:         "queue",
				Severity:     SeverityMedium,
				Observation:  "queue pressure is above threshold",
				WhyItMatters: "queue pressure affects throughput and latency",
				Confidence:   "high",
				Evidence: []string{
					"check summary: queue pressure is above threshold",
					"observed at: 2026-04-17T05:22:00Z",
					"detail queued_tasks: 12",
				},
			},
		},
		FinalVerdict: FinalVerdict{
			Status:  StatusDegraded,
			Summary: "health is good, but operator coverage is incomplete",
		},
	}

	output := RenderMarkdownReport(report)

	if !strings.Contains(output, "| Area | Severity | Observation | Why It Matters | Confidence | Evidence |") {
		t.Fatalf("output missing evidence column header\n%s", output)
	}
	if !strings.Contains(output, "check summary: queue pressure is above threshold<br>observed at: 2026-04-17T05:22:00Z<br>detail queued_tasks: 12") {
		t.Fatalf("output missing rendered evidence entries\n%s", output)
	}
}

func TestRenderMarkdownReportRendersCoverageProvenanceAndRecommendationMetadata(t *testing.T) {
	report := OperatorReport{
		CurrentHealth: CurrentHealthSnapshot{
			Status:          StatusFailed,
			ChecksEvaluated: 2,
		},
		Coverage: CoverageMetadata{
			Evaluated: []string{"database"},
			Unknown:   []string{"cache"},
		},
		RootCauses: []RootCause{
			{Area: "database", Summary: "database connectivity failed", Confidence: "high", Provenance: "confirmed"},
			{Area: "cache", Summary: "cache shard unavailable", Confidence: "reduced", Provenance: "inferred"},
		},
		Recommendations: RecommendationGroups{
			Immediate: []Recommendation{
				{
					Action:              "restore database connectivity",
					Reason:              "database connectivity failed",
					ExpectedBenefit:     "restores database-backed health decisions",
					Effort:              "medium",
					Risk:                "high",
					ApprovalRequirement: "ops approval",
				},
			},
		},
		FinalVerdict: FinalVerdict{
			Status:  StatusFailed,
			Summary: "one or more critical checks failed",
		},
	}

	output := RenderMarkdownReport(report)

	for _, want := range []string{
		"## Current Health Snapshot",
		"### Coverage",
		"| Evaluated Areas | Unknown Areas |",
		"database",
		"cache",
		"### Confirmed",
		"### Inferred",
		"| Area | Summary | Confidence | Provenance |",
		"| Action | Reason | Expected Benefit | Effort | Risk | Approval Requirement |",
		"ops approval",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q\n%s", want, output)
		}
	}

	coverageIdx := strings.Index(output, "### Coverage")
	confirmedIdx := strings.Index(output, "### Confirmed")
	inferredIdx := strings.Index(output, "### Inferred")
	recommendationIdx := strings.Index(output, "| Action | Reason | Expected Benefit | Effort | Risk | Approval Requirement |")
	if coverageIdx == -1 || confirmedIdx == -1 || inferredIdx == -1 || recommendationIdx == -1 {
		t.Fatalf("output missing required metadata sections\n%s", output)
	}
	if !(coverageIdx < confirmedIdx && confirmedIdx < inferredIdx && inferredIdx < recommendationIdx) {
		t.Fatalf("metadata sections out of order\n%s", output)
	}
}

func TestRenderMarkdownReportDefaultsUnknownRootCauseProvenanceToInferred(t *testing.T) {
	report := OperatorReport{
		RootCauses: []RootCause{
			{Area: "database", Summary: "database connectivity failed", Confidence: "high", Provenance: "confirmed"},
			{Area: "cache", Summary: "cache shard unavailable", Confidence: "reduced", Provenance: ""},
			{Area: "search", Summary: "search latency elevated", Confidence: "reduced", Provenance: "mystery"},
		},
	}

	output := RenderMarkdownReport(report)

	if !strings.Contains(output, "### Confirmed") || !strings.Contains(output, "database") {
		t.Fatalf("output missing confirmed root cause\n%s", output)
	}
	if !strings.Contains(output, "### Inferred") || !strings.Contains(output, "cache") || !strings.Contains(output, "search") {
		t.Fatalf("output should render empty and unknown provenance root causes as inferred\n%s", output)
	}
	if strings.Contains(output, "mystery") {
		t.Fatalf("output should normalize unknown provenance instead of exposing raw value\n%s", output)
	}
}

func TestRenderMarkdownReportRendersHealthyCoverageAsEvaluatedOnly(t *testing.T) {
	report := OperatorReport{
		Coverage: CoverageMetadata{
			Evaluated: []string{"database", "registry"},
		},
		FinalVerdict: FinalVerdict{
			Status:  StatusHealthy,
			Summary: "all evaluated checks are healthy",
		},
	}

	output := RenderMarkdownReport(report)

	if !strings.Contains(output, "| database, registry | None |") {
		t.Fatalf("output should render healthy coverage with no unknown areas\n%s", output)
	}
	if strings.Contains(output, "Unknown Areas |\n| --- | --- |\n| None | database") {
		t.Fatalf("output incorrectly renders healthy areas as unknown\n%s", output)
	}
}
