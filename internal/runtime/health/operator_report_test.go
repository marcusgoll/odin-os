package health

import "testing"

func TestBuildOperatorReportRanksFailuresBeforeDegradedFindings(t *testing.T) {
	raw := Report{
		Status: StatusFailed,
		Checks: []Check{
			{Name: "database", Status: StatusFailed, Summary: "database connectivity failed"},
			{Name: "queue", Status: StatusDegraded, Summary: "queue pressure is above threshold"},
		},
	}

	got := BuildOperatorReport(raw)

	if len(got.Findings) < 2 {
		t.Fatalf("Findings len = %d, want at least 2", len(got.Findings))
	}
	if got.Findings[0].Area != "database" || got.Findings[0].Severity != SeverityCritical {
		t.Fatalf("first finding = %+v, want critical database finding", got.Findings[0])
	}
}

func TestBuildOperatorReportFlagsMissingTelemetry(t *testing.T) {
	raw := Report{
		Status: StatusDegraded,
		Checks: []Check{
			{Name: "executor", Status: StatusDegraded, Summary: "no executor health samples recorded"},
		},
	}

	got := BuildOperatorReport(raw)

	if len(got.MissingTelemetry) == 0 {
		t.Fatalf("MissingTelemetry = 0, want executor gap")
	}
}
