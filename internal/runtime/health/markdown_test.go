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
	if !strings.Contains(output, "| Area | Severity | Observation | Why It Matters | Confidence |") {
		t.Fatalf("output missing findings table header\n%s", output)
	}
}
