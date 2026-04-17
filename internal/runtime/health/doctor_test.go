package health

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDoctorReportIsMachineParseable(t *testing.T) {
	t.Parallel()

	report := Report{
		Status: StatusHealthy,
		Checks: []Check{
			{
				Name:       "database",
				Status:     StatusHealthy,
				Summary:    "database reachable",
				ObservedAt: time.Date(2026, 4, 9, 20, 0, 0, 0, time.UTC),
			},
		},
	}

	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if decoded["status"] != string(StatusHealthy) {
		t.Fatalf("status = %v, want %q", decoded["status"], StatusHealthy)
	}
	if _, ok := decoded["checks"]; !ok {
		t.Fatalf("decoded report missing checks field")
	}
	if _, ok := decoded["generated_at"]; !ok {
		t.Fatalf("decoded report missing generated_at field")
	}
	if len(decoded) != 3 {
		t.Fatalf("decoded report top-level keys = %d, want 3", len(decoded))
	}
}
