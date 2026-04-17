package health

import "testing"

func TestBuildOperatorReportOmitsHealthyChecksFromOperatorSections(t *testing.T) {
	raw := Report{
		Status: StatusHealthy,
		Checks: []Check{
			{Name: "executor", Status: StatusHealthy, Summary: "executor health is fresh"},
			{Name: "queue", Status: StatusHealthy, Summary: "queue pressure is within threshold"},
		},
	}

	got := BuildOperatorReport(raw)

	if len(got.Findings) != 0 {
		t.Fatalf("Findings len = %d, want 0 for healthy checks", len(got.Findings))
	}
	if len(got.RootCauses) != 0 {
		t.Fatalf("RootCauses len = %d, want 0 for healthy checks", len(got.RootCauses))
	}
	if len(got.Recommendations.Immediate) != 0 || len(got.Recommendations.NearTerm) != 0 || len(got.Recommendations.Strategic) != 0 {
		t.Fatalf("Recommendations = %#v, want none for healthy checks", got.Recommendations)
	}
	if got.CurrentHealth.Status != StatusHealthy || got.CurrentHealth.ChecksEvaluated != 2 {
		t.Fatalf("CurrentHealth = %+v, want healthy snapshot with 2 checks evaluated", got.CurrentHealth)
	}
	if len(got.Coverage.Evaluated) != 2 || got.Coverage.Evaluated[0] != "executor" || got.Coverage.Evaluated[1] != "queue" {
		t.Fatalf("Coverage.Evaluated = %#v, want all healthy checks evaluated", got.Coverage.Evaluated)
	}
	if len(got.Coverage.Unknown) != 0 {
		t.Fatalf("Coverage.Unknown = %#v, want none for healthy checks", got.Coverage.Unknown)
	}
}

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

	if len(got.MissingTelemetry) != 1 {
		t.Fatalf("MissingTelemetry len = %d, want 1", len(got.MissingTelemetry))
	}
	if got.MissingTelemetry[0] != "executor health samples" {
		t.Fatalf("MissingTelemetry[0] = %q, want %q", got.MissingTelemetry[0], "executor health samples")
	}
}

func TestBuildOperatorReportRanksFailedFindingsBeforeDegradedWhenSeverityMatches(t *testing.T) {
	raw := Report{
		Status: StatusFailed,
		Checks: []Check{
			{Name: "source_freshness", Status: StatusFailed, Summary: "source freshness is unavailable"},
			{Name: "executor", Status: StatusDegraded, Summary: "executor health is unavailable or stale"},
		},
	}

	got := BuildOperatorReport(raw)

	if len(got.Findings) < 2 {
		t.Fatalf("Findings len = %d, want at least 2", len(got.Findings))
	}
	if got.Findings[0].Area != "source_freshness" || got.Findings[0].SourceStatus != StatusFailed {
		t.Fatalf("first finding = %+v, want failed source_freshness finding before degraded executor", got.Findings[0])
	}
	if got.Findings[1].Area != "executor" || got.Findings[1].SourceStatus != StatusDegraded {
		t.Fatalf("second finding = %+v, want degraded executor finding", got.Findings[1])
	}
}

func TestBuildOperatorReportUsesExplicitStaleMapping(t *testing.T) {
	raw := Report{
		Status: StatusDegraded,
		Checks: []Check{
			{Name: "source_freshness", Status: StatusDegraded, Summary: "source freshness is stale"},
		},
	}

	got := BuildOperatorReport(raw)

	if len(got.Findings) != 1 {
		t.Fatalf("Findings len = %d, want 1", len(got.Findings))
	}
	if got.Findings[0].Confidence != "reduced" {
		t.Fatalf("finding confidence = %q, want %q", got.Findings[0].Confidence, "reduced")
	}
	if len(got.MissingTelemetry) != 0 {
		t.Fatalf("MissingTelemetry = %#v, want none for explicit stale mapping", got.MissingTelemetry)
	}
	if len(got.Recommendations.Immediate) != 1 || got.Recommendations.Immediate[0].Action != "rebuild source freshness records" {
		t.Fatalf("Immediate recommendations = %#v, want rebuild source freshness records", got.Recommendations.Immediate)
	}
}

func TestBuildOperatorReportUsesExplicitUnmappedFallbackForUnknownChecks(t *testing.T) {
	raw := Report{
		Status: StatusFailed,
		Checks: []Check{
			{Name: "cache", Status: StatusFailed, Summary: "cache shard unavailable"},
			{Name: "search", Status: StatusDegraded, Summary: "search latency elevated"},
		},
	}

	got := BuildOperatorReport(raw)

	if len(got.Findings) != 2 {
		t.Fatalf("Findings len = %d, want 2", len(got.Findings))
	}
	if got.Findings[0].Area != "cache" || got.Findings[0].Observation != "cache shard unavailable" || got.Findings[0].Confidence != "reduced" {
		t.Fatalf("first finding = %+v, want explicit unmapped failed cache finding", got.Findings[0])
	}
	if got.Findings[1].Area != "search" || got.Findings[1].Observation != "search latency elevated" || got.Findings[1].Confidence != "reduced" {
		t.Fatalf("second finding = %+v, want explicit unmapped degraded search finding", got.Findings[1])
	}
	if len(got.Recommendations.Immediate) != 1 || got.Recommendations.Immediate[0].Action != "add an explicit operator mapping for cache" {
		t.Fatalf("Immediate recommendations = %#v, want explicit cache mapping recommendation", got.Recommendations.Immediate)
	}
	if len(got.Recommendations.NearTerm) != 1 || got.Recommendations.NearTerm[0].Action != "add an explicit operator mapping for search" {
		t.Fatalf("NearTerm recommendations = %#v, want explicit search mapping recommendation", got.Recommendations.NearTerm)
	}
	if len(got.MissingTelemetry) != 0 {
		t.Fatalf("MissingTelemetry = %#v, want none for unmapped checks", got.MissingTelemetry)
	}
}

func TestBuildOperatorReportTracksCoverageAndRootCauseProvenance(t *testing.T) {
	raw := Report{
		Status: StatusFailed,
		Checks: []Check{
			{Name: "database", Status: StatusFailed, Summary: "database connectivity failed"},
			{Name: "queue", Status: StatusDegraded, Summary: "queue pressure is above threshold"},
		},
	}

	got := BuildOperatorReport(raw)

	if diff := len(got.Coverage.Evaluated); diff != 2 || got.Coverage.Evaluated[0] != "database" || got.Coverage.Evaluated[1] != "queue" {
		t.Fatalf("Coverage.Evaluated = %#v, want all checks evaluated in order", got.Coverage.Evaluated)
	}
	if len(got.Coverage.Unknown) != 0 {
		t.Fatalf("Coverage.Unknown = %#v, want none for high-confidence checks", got.Coverage.Unknown)
	}

	if len(got.RootCauses) != 2 {
		t.Fatalf("RootCauses len = %d, want 2", len(got.RootCauses))
	}
	if got.RootCauses[0].Area != "database" || got.RootCauses[0].Provenance != "confirmed" {
		t.Fatalf("first root cause = %+v, want confirmed database cause", got.RootCauses[0])
	}
	if got.RootCauses[1].Area != "queue" || got.RootCauses[1].Provenance != "confirmed" {
		t.Fatalf("second root cause = %+v, want confirmed queue cause", got.RootCauses[1])
	}
}

func TestBuildOperatorReportMarksReducedConfidenceAndUnmappedChecksUnknown(t *testing.T) {
	raw := Report{
		Status: StatusFailed,
		Checks: []Check{
			{Name: "executor", Status: StatusDegraded, Summary: "no executor health samples recorded"},
			{Name: "cache", Status: StatusFailed, Summary: "cache shard unavailable"},
		},
	}

	got := BuildOperatorReport(raw)

	if len(got.Coverage.Evaluated) != 2 || got.Coverage.Evaluated[0] != "executor" || got.Coverage.Evaluated[1] != "cache" {
		t.Fatalf("Coverage.Evaluated = %#v, want both checks evaluated", got.Coverage.Evaluated)
	}
	if len(got.Coverage.Unknown) != 2 || got.Coverage.Unknown[0] != "executor" || got.Coverage.Unknown[1] != "cache" {
		t.Fatalf("Coverage.Unknown = %#v, want reduced-confidence and unmapped checks", got.Coverage.Unknown)
	}
	if len(got.RootCauses) != 2 || got.RootCauses[0].Provenance != "confirmed" || got.RootCauses[1].Provenance != "inferred" {
		t.Fatalf("RootCauses = %#v, want confirmed explicit and inferred unmapped causes", got.RootCauses)
	}
}
