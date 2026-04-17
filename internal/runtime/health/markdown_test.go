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

func TestRenderMarkdownReportOrdersFindingsBeforeRecommendations(t *testing.T) {
	report := OperatorReport{
		Findings: []Finding{
			{
				Area:         "database",
				Severity:     SeverityCritical,
				Observation:  "database connectivity failed",
				WhyItMatters: "database access is required for runtime health decisions",
				Confidence:   "high",
			},
		},
		Recommendations: RecommendationGroups{
			Immediate: []Recommendation{
				{Action: "restore database connectivity", Reason: "database connectivity failed"},
			},
		},
	}

	output := RenderMarkdownReport(report)

	findingsIdx := strings.Index(output, "## Key Findings")
	recommendationsIdx := strings.Index(output, "## Upgrade and Improvement Recommendations")
	if findingsIdx == -1 || recommendationsIdx == -1 {
		t.Fatalf("output missing required sections\n%s", output)
	}
	if findingsIdx > recommendationsIdx {
		t.Fatalf("findings section appears after recommendations\n%s", output)
	}

	if !strings.Contains(output, "| Area | Severity | Observation | Why It Matters | Confidence |") {
		t.Fatalf("output missing findings table header\n%s", output)
	}
}
